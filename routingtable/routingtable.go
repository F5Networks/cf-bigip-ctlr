/*
 * Portions Copyright (c) 2017,2018, F5 Networks, Inc.
 */

package routingtable

import (
	"fmt"
	"sync"
	"time"

	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/f5router"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/bigipResources"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/routeUpdate"
	"github.com/F5Networks/cf-bigip-ctlr/logger"

	routing_api_models "code.cloudfoundry.org/routing-api/models"
	"github.com/uber-go/zap"
)

// RouteTable interacts with RoutingTable
//go:generate counterfeiter -o fakes/fake_routingtable.go . RouteTable
type RouteTable interface {
	UpsertBackendServerKey(key RoutingKey, info BackendServerInfo) bool
	DeleteBackendServerKey(key RoutingKey, info BackendServerInfo) bool
	NumberOfRoutes() int
	NumberOfBackends(key RoutingKey) int
}

// RoutingKey is the route port
type RoutingKey struct {
	Port uint16
}

// BackendServerInfo provides all info needed to create a backend
type BackendServerInfo struct {
	Address         string
	Port            uint16
	ModificationTag routing_api_models.ModificationTag
	TTL             time.Duration
}

// BackendServerKey is the endpoints info
type BackendServerKey struct {
	Address string
	Port    uint16
}

// BackendServerDetails holds version info for that endpoint
type BackendServerDetails struct {
	ModificationTag routing_api_models.ModificationTag
	TTL             time.Duration
	UpdatedTime     time.Time
}

// entry holds all the backends
type entry struct {
	backends map[BackendServerKey]*BackendServerDetails
}

// RoutingTable holds all tcp routing information
type RoutingTable struct {
	sync.RWMutex
	c                          *config.Config
	entries                    map[RoutingKey]entry
	logger                     logger.Logger
	ticker                     *time.Ticker
	pruneStaleDropletsInterval time.Duration
	dropletStaleThreshold      time.Duration
	listener                   routeUpdate.Listener
}

// NewRoutingTable returns a new RoutingTable
func NewRoutingTable(logger logger.Logger, c *config.Config, listener routeUpdate.Listener) *RoutingTable {
	return &RoutingTable{
		c:       c,
		entries: make(map[RoutingKey]entry),
		logger:  logger,
		pruneStaleDropletsInterval: c.PruneStaleDropletsInterval,
		dropletStaleThreshold:      c.DropletStaleThreshold,
		listener:                   listener,
	}
}

// newRoutingTableEntry accepts an array of BackendServerInfo and returns an entry
func newRoutingTableEntry(backends []BackendServerInfo) entry {
	routingTableEntry := entry{
		backends: make(map[BackendServerKey]*BackendServerDetails),
	}
	for _, backend := range backends {
		backendServerKey := BackendServerKey{Address: backend.Address, Port: backend.Port}
		backendServerDetails := &BackendServerDetails{
			ModificationTag: backend.ModificationTag,
			TTL:             backend.TTL,
			UpdatedTime:     time.Now(),
		}

		routingTableEntry.backends[backendServerKey] = backendServerDetails
	}
	return routingTableEntry
}

// differentFrom is used to determine whether the details have changed such that
// the routing configuration needs to be updated. e.g max number of connection
func (d BackendServerDetails) differentFrom(other *BackendServerDetails) bool {
	return d.updateSucceededBy(other) && false
}

// updateSucceededBy returns true if the current backend is older than other
func (d BackendServerDetails) updateSucceededBy(other *BackendServerDetails) bool {
	return d.ModificationTag.SucceededBy(&other.ModificationTag)
}

// deleteSucceededBy returns true if the current backend is older than other
func (d BackendServerDetails) deleteSucceededBy(other *BackendServerDetails) bool {
	return d.ModificationTag == other.ModificationTag || d.ModificationTag.SucceededBy(&other.ModificationTag)
}

// expired returns true if the backend has passed it's ttl
func (d BackendServerDetails) expired(defaultTTL time.Duration) bool {
	ttl := d.TTL
	if ttl == time.Duration(0) {
		ttl = defaultTTL
	}
	expiryTime := time.Now().Add(-ttl * time.Second)
	return expiryTime.After(d.UpdatedTime)
}

// pruneBackends checks the expiration of backends and deletes any that are expired
func (e entry) pruneBackends(defaultTTL time.Duration) []BackendServerKey {
	var removedBackends []BackendServerKey
	for backendKey, details := range e.backends {
		if details.expired(defaultTTL) {
			removedBackends = append(removedBackends, backendKey)
			delete(e.backends, backendKey)
		}
	}
	return removedBackends
}

// pruneEntries removes stale backends - only call through pruning cycle
func (table *RoutingTable) pruneEntries(defaultTTL time.Duration) {
	table.Lock()
	defer table.Unlock()
	for routeKey, entry := range table.entries {
		removed := entry.pruneBackends(defaultTTL)
		if len(removed) > 0 && table.listener != nil {
			for _, backend := range removed {
				table.updateRouter(routeUpdate.Remove, routeKey.Port, backend.Address, backend.Port)
			}
		}
		if len(entry.backends) == 0 {
			delete(table.entries, routeKey)
		}
	}
}

func (table *RoutingTable) serverKeyDetailsFromInfo(
	info BackendServerInfo,
) (BackendServerKey, *BackendServerDetails) {
	return BackendServerKey{Address: info.Address, Port: info.Port},
		&BackendServerDetails{
			ModificationTag: info.ModificationTag,
			TTL:             info.TTL,
			UpdatedTime:     time.Now(),
		}
}

// UpsertBackendServerKey returns true if routing configuration should be modified, false if it should not.
func (table *RoutingTable) UpsertBackendServerKey(key RoutingKey, info BackendServerInfo) bool {
	var update bool
	logger := table.logger.Session("upsert-backend")
	table.Lock()
	defer table.Unlock()
	existingEntry, routingKeyFound := table.entries[key]
	if !routingKeyFound {
		logger.Debug("routing-key-not-found", zap.Object("routing-key", key))
		existingEntry = newRoutingTableEntry([]BackendServerInfo{info})
		table.entries[key] = existingEntry
		update = true
	}

	newBackendKey, newBackendDetails := table.serverKeyDetailsFromInfo(info)
	currentBackendDetails, backendFound := existingEntry.backends[newBackendKey]

	if !backendFound ||
		currentBackendDetails.updateSucceededBy(newBackendDetails) {
		logger.Debug("applying-change-to-table", zap.Object("old", currentBackendDetails), zap.Object("new", newBackendDetails))
		existingEntry.backends[newBackendKey] = newBackendDetails
	} else {
		logger.Debug("skipping-stale-event", zap.Object("old", currentBackendDetails), zap.Object("new", newBackendDetails))
	}

	if !backendFound || currentBackendDetails.differentFrom(newBackendDetails) {
		update = true
	}

	if update && table.listener != nil {
		table.updateRouter(routeUpdate.Add, key.Port, info.Address, info.Port)
	}

	return update
}

// DeleteBackendServerKey returns true if routing configuration should be modified, false if it should not.
func (table *RoutingTable) DeleteBackendServerKey(key RoutingKey, info BackendServerInfo) bool {
	var update bool
	logger := table.logger.Session("delete-backend")
	table.Lock()
	defer table.Unlock()
	backendServerKey, newDetails := table.serverKeyDetailsFromInfo(info)
	existingEntry, routingKeyFound := table.entries[key]

	if routingKeyFound {
		existingDetails, backendFound := existingEntry.backends[backendServerKey]

		if backendFound && existingDetails.deleteSucceededBy(newDetails) {
			logger.Debug("removing-from-table", zap.Object("old", existingDetails), zap.Object("new", newDetails))
			delete(existingEntry.backends, backendServerKey)
			if len(existingEntry.backends) == 0 {
				delete(table.entries, key)
			}
			update = true
		}
		logger.Debug("skipping-stale-event", zap.Object("old", existingDetails), zap.Object("new", newDetails))
	}

	if update && table.listener != nil {
		table.updateRouter(routeUpdate.Remove, key.Port, info.Address, info.Port)
	}

	return update
}

// StartPruningCycle kicks off the prune ticker
func (table *RoutingTable) StartPruningCycle() {
	if table.pruneStaleDropletsInterval > 0 {
		table.Lock()
		table.ticker = time.NewTicker(table.pruneStaleDropletsInterval)
		table.Unlock()

		go func() {
			for {
				select {
				case <-table.ticker.C:
					table.logger.Debug("start-pruning-routes")
					table.pruneEntries(table.pruneStaleDropletsInterval)
					table.logger.Debug("finished-pruning-routes")
				}
			}
		}()
	}
}

// StopPruningCycle stops the cycle
func (table *RoutingTable) StopPruningCycle() {
	table.Lock()
	if table.ticker != nil {
		table.ticker.Stop()
	}
	table.Unlock()
}

// NumberOfRoutes returns the number of routes
func (table *RoutingTable) NumberOfRoutes() int {
	table.RLock()
	defer table.RUnlock()
	return len(table.entries)
}

// NumberOfBackends returns the number of backends for a particular route
func (table *RoutingTable) NumberOfBackends(key RoutingKey) int {
	table.RLock()
	defer table.RUnlock()
	entry, ok := table.entries[key]
	if !ok {
		return 0
	}
	return len(entry.backends)
}

func (table *RoutingTable) RouteExists(key RoutingKey) bool {
	table.RLock()
	defer table.RUnlock()
	_, found := table.entries[key]
	return found
}

func (table *RoutingTable) BackendExists(key RoutingKey, bk BackendServerKey) bool {
	table.RLock()
	defer table.RUnlock()
	routes, entryFound := table.entries[key]
	if !entryFound {
		return false
	}
	_, backendFound := routes.backends[bk]
	return backendFound
}

func (table *RoutingTable) updateRouter(
	op routeUpdate.Operation,
	routePort uint16,
	ea string,
	ep uint16,
) {
	member := bigipResources.Member{
		Address: ea,
		Port:    ep,
		Session: "user-enabled",
	}
	update, err := f5router.NewTCPUpdate(table.c, table.logger, op, routePort, member)

	if nil != err {
		table.logger.Warn(
			"skipping-TCP-route-update",
			zap.Error(err),
		)
	} else {
		table.listener.UpdateRoute(update)
	}
}

func (k RoutingKey) String() string {
	return fmt.Sprintf("%d", k.Port)
}
