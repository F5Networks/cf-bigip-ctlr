package routefetcher

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/F5Networks/cf-bigip-ctlr/logger"
	"github.com/F5Networks/cf-bigip-ctlr/routingtable"

	"code.cloudfoundry.org/routing-api"
	apimodels "code.cloudfoundry.org/routing-api/models"
	uaaclient "code.cloudfoundry.org/uaa-go-client"
	"github.com/uber-go/zap"
)

// TCPFetcher knows how to connect to the routing API and get tcp events
// satisfies RouteClient interface
type TCPFetcher struct {
	logger           logger.Logger
	routingTable     *routingtable.RoutingTable
	syncing          bool
	routingAPIClient routing_api.Client
	uaaClient        uaaclient.Client
	cachedEvents     []routing_api.TcpEvent
	lock             *sync.Mutex
	defaultTTL       time.Duration
	protocol         string
}

// NewTCPFetcher initializes a TCPFetcher
func NewTCPFetcher(
	logger logger.Logger,
	routingTable *routingtable.RoutingTable,
	routingAPIClient routing_api.Client,
	uaaClient uaaclient.Client,
	defaultTTL time.Duration,
) *TCPFetcher {
	return &TCPFetcher{
		logger:           logger,
		routingTable:     routingTable,
		lock:             new(sync.Mutex),
		syncing:          false,
		routingAPIClient: routingAPIClient,
		uaaClient:        uaaClient,
		cachedEvents:     nil,
		defaultTTL:       defaultTTL,
		protocol:         "tcp",
	}
}

// ClientProtocol returns the protocol of the RouteClient
func (tcpFetcher *TCPFetcher) ClientProtocol() string {
	return tcpFetcher.protocol
}

// FetchRoutes grabs the current state from the API server and will update
// RouteTable if differences exist
func (tcpFetcher *TCPFetcher) FetchRoutes() error {
	logger := tcpFetcher.logger.Session("bulk-sync")
	logger.Debug("starting")

	defer func() {
		tcpFetcher.lock.Lock()
		tcpFetcher.applyCachedEvents()
		tcpFetcher.syncing = false
		tcpFetcher.cachedEvents = nil
		tcpFetcher.lock.Unlock()
		logger.Debug("completed")
	}()

	tcpFetcher.lock.Lock()
	tcpFetcher.syncing = true
	tcpFetcher.cachedEvents = []routing_api.TcpEvent{}
	tcpFetcher.lock.Unlock()

	useCachedToken := true
	var err error
	var tcpRouteMappings []apimodels.TcpRouteMapping
	for count := 0; count < 2; count++ {
		token, tokenErr := tcpFetcher.uaaClient.FetchToken(!useCachedToken)
		if tokenErr != nil {
			logger.Error("error-fetching-token", zap.Error(tokenErr))
			return tokenErr
		}
		tcpFetcher.routingAPIClient.SetToken(token.AccessToken)
		tcpRouteMappings, err = tcpFetcher.routingAPIClient.TcpRouteMappings()
		if err != nil {
			logger.Error("error-fetching-routes", zap.Error(err))
			if err.Error() == "unauthorized" {
				useCachedToken = false
				logger.Info("retrying-sync")
			} else {
				return err
			}
		} else {
			break
		}
	}
	logger.Debug("fetched-tcp-routes", zap.Int("num-routes", len(tcpRouteMappings)))
	if err == nil {
		// Create a new routingtable for comparison to the current known state
		otherRouteTable := tcpFetcher.routingTableFromRouteMapping(tcpRouteMappings)
		tcpFetcher.routingTable.Lock()
		tableEqual := tcpFetcher.routingTable.CompareEntries(otherRouteTable)
		logger.Debug("table-equal", zap.Bool("equal", tableEqual))
		if !tableEqual {
			tcpFetcher.routingTable.Entries = otherRouteTable.Entries
			// FIXME add call to f5router
		}
		tcpFetcher.routingTable.Unlock()
	}
	return nil
}

func (tcpFetcher *TCPFetcher) applyCachedEvents() {
	tcpFetcher.logger.Debug("applying-cached-events", zap.Int("cache_size", len(tcpFetcher.cachedEvents)))
	defer tcpFetcher.logger.Debug("applied-cached-events")
	for _, e := range tcpFetcher.cachedEvents {
		tcpFetcher.handleEvent(e)
	}
}

// Syncing returns the sync state of the tcpFetcher
func (tcpFetcher *TCPFetcher) Syncing() bool {
	tcpFetcher.lock.Lock()
	defer tcpFetcher.lock.Unlock()
	return tcpFetcher.syncing
}

// HandleEvent handles events from the api client
func (tcpFetcher *TCPFetcher) HandleEvent(e interface{}) {
	event, ok := e.(routing_api.TcpEvent)
	if !ok {
		tcpFetcher.logger.Warn("recieved-wrong-event-type",
			zap.String("event-type", fmt.Sprint(reflect.TypeOf(event))),
		)
	}
	tcpFetcher.lock.Lock()
	defer tcpFetcher.lock.Unlock()

	if tcpFetcher.syncing {
		tcpFetcher.logger.Debug("caching-events")
		tcpFetcher.cachedEvents = append(tcpFetcher.cachedEvents, event)
	} else {
		tcpFetcher.handleEvent(event)
	}
}

func (tcpFetcher *TCPFetcher) handleEvent(event routing_api.TcpEvent) {
	logger := tcpFetcher.logger.Session("handle-event")
	logger.Debug("starting", zap.Object("event", event))
	defer logger.Debug("finished")
	action := event.Action
	switch action {
	case "Upsert":
		tcpFetcher.handleUpsert(event.TcpRouteMapping)
	case "Delete":
		tcpFetcher.handleDelete(event.TcpRouteMapping)
	default:
		logger.Info("unknown-event-action")
	}
}

func (tcpFetcher *TCPFetcher) toRoutingTableEntry(
	routeMapping apimodels.TcpRouteMapping,
) (routingtable.RoutingKey, routingtable.BackendServerInfo) {
	tcpFetcher.logger.Debug("converting-tcp-route-mapping", zap.Object("tcp-route", routeMapping))
	routingKey := routingtable.RoutingKey{Port: routeMapping.ExternalPort}

	var ttl time.Duration
	if routeMapping.TTL != nil {
		ttl = time.Duration(*routeMapping.TTL)
	}

	backendServerInfo := routingtable.BackendServerInfo{
		Address:         routeMapping.HostIP,
		Port:            routeMapping.HostPort,
		ModificationTag: routeMapping.ModificationTag,
		TTL:             ttl,
	}
	return routingKey, backendServerInfo
}

func (tcpFetcher *TCPFetcher) handleUpsert(
	routeMapping apimodels.TcpRouteMapping,
) {
	routingKey, backendServerInfo := tcpFetcher.toRoutingTableEntry(routeMapping)
	tcpFetcher.routingTable.Lock()
	defer tcpFetcher.routingTable.Unlock()
	if tcpFetcher.routingTable.UpsertBackendServerKey(routingKey, backendServerInfo) && !tcpFetcher.syncing {
		tcpFetcher.logger.Info("tcp-route-Upsert")
		// return tcpFetcher.configurer.Configure(*tcpFetcher.routingTable)
	}
}

func (tcpFetcher *TCPFetcher) handleDelete(
	routeMapping apimodels.TcpRouteMapping,
) {
	routingKey, backendServerInfo := tcpFetcher.toRoutingTableEntry(routeMapping)
	tcpFetcher.routingTable.Lock()
	defer tcpFetcher.routingTable.Unlock()
	if tcpFetcher.routingTable.DeleteBackendServerKey(routingKey, backendServerInfo) && !tcpFetcher.syncing {
		tcpFetcher.logger.Info("tcp-route-Delete")
		// return tcpFetcher.configurer.Configure(*tcpFetcher.routingTable)
	}
}

// routingTableFromRouteMapping provides a RoutingTable with only the Entries populated
func (tcpFetcher *TCPFetcher) routingTableFromRouteMapping(
	tcpRouteMappings []apimodels.TcpRouteMapping,
) *routingtable.RoutingTable {
	otherRouteTable := &routingtable.RoutingTable{}
	tableEntries := make(map[routingtable.RoutingKey]routingtable.Entry)

	for _, routeMapping := range tcpRouteMappings {
		routingKey, backendServerInfo := tcpFetcher.toRoutingTableEntry(routeMapping)
		existingEntry, routingKeyFound := tableEntries[routingKey]
		if !routingKeyFound {
			routeEntry := routingtable.NewRoutingTableEntry([]routingtable.BackendServerInfo{backendServerInfo})
			tableEntries[routingKey] = routeEntry
		} else {
			backendKey, backendDetails := newBackend(backendServerInfo)
			existingEntry.Backends[backendKey] = backendDetails
		}
	}

	otherRouteTable.Entries = tableEntries
	return otherRouteTable
}

func newBackend(
	info routingtable.BackendServerInfo,
) (routingtable.BackendServerKey, *routingtable.BackendServerDetails) {
	return routingtable.BackendServerKey{Address: info.Address, Port: info.Port},
		&routingtable.BackendServerDetails{
			ModificationTag: info.ModificationTag,
			TTL:             info.TTL,
			UpdatedTime:     time.Now(),
		}
}
