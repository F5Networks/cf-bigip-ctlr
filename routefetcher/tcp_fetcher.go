/*
 * Portions Copyright (c) 2017, F5 Networks, Inc.
 */

package routefetcher

import (
	"fmt"
	"reflect"
	"time"

	"github.com/F5Networks/cf-bigip-ctlr/logger"
	"github.com/F5Networks/cf-bigip-ctlr/routingtable"
	"github.com/cloudfoundry/dropsonde/metrics"

	"code.cloudfoundry.org/routing-api"
	"code.cloudfoundry.org/routing-api/models"
	uaaclient "code.cloudfoundry.org/uaa-go-client"
	"github.com/uber-go/zap"
)

// TCPFetcher knows how to connect to the routing API and get tcp events
// satisfies RouteClient interface
type TCPFetcher struct {
	logger           logger.Logger
	routingTable     routingtable.RouteTable
	routingAPIClient routing_api.Client
	uaaClient        uaaclient.Client
	endpoints        []models.TcpRouteMapping
	protocol         string
}

// NewTCPFetcher initializes a TCPFetcher
func NewTCPFetcher(
	logger logger.Logger,
	routingTable routingtable.RouteTable,
	routingAPIClient routing_api.Client,
	uaaClient uaaclient.Client,
) *TCPFetcher {
	return &TCPFetcher{
		logger:           logger,
		routingTable:     routingTable,
		routingAPIClient: routingAPIClient,
		uaaClient:        uaaClient,
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
	logger.Debug("TCP-syncer-fetch-routes-started")

	useCachedToken := true
	var err error
	var tcpRouteMappings []models.TcpRouteMapping
	for count := 0; count < 2; count++ {
		tcpFetcher.logger.Debug("TCP-syncer-fetching-token")
		token, tokenErr := tcpFetcher.uaaClient.FetchToken(!useCachedToken)
		if tokenErr != nil {
			metrics.IncrementCounter(TokenFetchErrors)
			return tokenErr
		}
		tcpFetcher.routingAPIClient.SetToken(token.AccessToken)
		tcpFetcher.logger.Debug("TCP-syncer-fetching-routes")
		tcpRouteMappings, err = tcpFetcher.routingAPIClient.TcpRouteMappings()
		if err != nil {
			if err.Error() == unauthorized {
				useCachedToken = false
			} else {
				return err
			}
		} else {
			break
		}
	}
	if err != nil {
		return err
	}

	logger.Debug("TCP-syncer-refreshing-endpoints", zap.Int("number-of-routes", len(tcpRouteMappings)))

	tcpFetcher.refreshTCPEndpoints(tcpRouteMappings)

	return nil
}

// HandleEvent handles events from the api client
func (tcpFetcher *TCPFetcher) HandleEvent(e interface{}) {
	logger := tcpFetcher.logger.Session("handle-event")
	event, ok := e.(routing_api.TcpEvent)
	if !ok {
		logger.Warn("received-wrong-event-type",
			zap.String("event-type", fmt.Sprint(reflect.TypeOf(event))),
		)
	}

	routingKey, backendServerInfo := tcpFetcher.toRoutingTableEntry(event.TcpRouteMapping)

	logger.Debug("starting", zap.Object("event", event))
	defer logger.Debug("finished")
	action := event.Action
	switch action {
	case "Upsert":
		tcpFetcher.routingTable.UpsertBackendServerKey(routingKey, backendServerInfo)
	case "Delete":
		tcpFetcher.routingTable.DeleteBackendServerKey(routingKey, backendServerInfo)
	default:
		logger.Info("unknown-event-action")
	}
}

func (tcpFetcher *TCPFetcher) refreshTCPEndpoints(validRoutes []models.TcpRouteMapping) {
	tcpFetcher.deleteTCPEndpoints(validRoutes)

	tcpFetcher.endpoints = validRoutes

	for _, aRoute := range tcpFetcher.endpoints {
		routingKey, backendServerInfo := tcpFetcher.toRoutingTableEntry(aRoute)
		tcpFetcher.routingTable.UpsertBackendServerKey(routingKey, backendServerInfo)
	}
}

func (tcpFetcher *TCPFetcher) deleteTCPEndpoints(validRoutes []models.TcpRouteMapping) {
	var diff []models.TcpRouteMapping

	for _, curRoute := range tcpFetcher.endpoints {
		routeFound := false

		for _, validRoute := range validRoutes {
			if routeEqualsTCP(curRoute, validRoute) {
				routeFound = true
				break
			}
		}

		if !routeFound {
			diff = append(diff, curRoute)
		}
	}

	for _, aRoute := range diff {
		routingKey, backendServerInfo := tcpFetcher.toRoutingTableEntry(aRoute)
		tcpFetcher.routingTable.DeleteBackendServerKey(routingKey, backendServerInfo)
	}
}

func (tcpFetcher *TCPFetcher) toRoutingTableEntry(
	routeMapping models.TcpRouteMapping,
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

func routeEqualsTCP(old, other models.TcpRouteMapping) bool {
	if (old.ExternalPort == other.ExternalPort) &&
		(old.HostIP == other.HostIP) &&
		(old.HostPort == other.HostPort) {
		return true
	}
	return false
}
