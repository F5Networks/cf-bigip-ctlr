package routingtable

import (
	"fmt"
	"sync"
	"time"

	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/logger"

	routing_api_models "code.cloudfoundry.org/routing-api/models"
	"github.com/uber-go/zap"
)

// RouteTable interface used for testing
//go:generate counterfeiter -o fakes/fake_routingtable.go . RouteTable
type RouteTable interface {
	PruneEntries(defaultTTL time.Duration)
	UpsertBackendServerKey(key RoutingKey, info BackendServerInfo) bool
	DeleteBackendServerKey(key RoutingKey, info BackendServerInfo) bool
	CompareEntries(newTable *RoutingTable) bool
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

// Entry holds all the backends
type Entry struct {
	Backends map[BackendServerKey]*BackendServerDetails
}

// RoutingTable holds all tcp routing information
type RoutingTable struct {
	sync.RWMutex
	Entries                    map[RoutingKey]Entry
	logger                     logger.Logger
	ticker                     *time.Ticker
	pruneStaleDropletsInterval time.Duration
	dropletStaleThreshold      time.Duration
}

// NewRoutingTable returns a new RoutingTable
func NewRoutingTable(logger logger.Logger, c *config.Config) *RoutingTable {
	return &RoutingTable{
		Entries: make(map[RoutingKey]Entry),
		logger:  logger,
		pruneStaleDropletsInterval: c.PruneStaleDropletsInterval,
		dropletStaleThreshold:      c.DropletStaleThreshold,
	}
}

// NewRoutingTableEntry accepts an array of BackendServerInfo and returns an Entry
func NewRoutingTableEntry(backends []BackendServerInfo) Entry {
	routingTableEntry := Entry{
		Backends: make(map[BackendServerKey]*BackendServerDetails),
	}
	for _, backend := range backends {
		backendServerKey := BackendServerKey{Address: backend.Address, Port: backend.Port}
		backendServerDetails := &BackendServerDetails{
			ModificationTag: backend.ModificationTag,
			TTL:             backend.TTL,
			UpdatedTime:     time.Now(),
		}

		routingTableEntry.Backends[backendServerKey] = backendServerDetails
	}
	return routingTableEntry
}

// DifferentFrom is used to determine whether the details have changed such that
// the routing configuration needs to be updated. e.g max number of connection
func (d BackendServerDetails) DifferentFrom(other *BackendServerDetails) bool {
	return d.UpdateSucceededBy(other) && false
}

// UpdateSucceededBy returns true if the current backend is older than other
func (d BackendServerDetails) UpdateSucceededBy(other *BackendServerDetails) bool {
	return d.ModificationTag.SucceededBy(&other.ModificationTag)
}

// DeleteSucceededBy returns true if the current backend is older than other
func (d BackendServerDetails) DeleteSucceededBy(other *BackendServerDetails) bool {
	return d.ModificationTag == other.ModificationTag || d.ModificationTag.SucceededBy(&other.ModificationTag)
}

// Expired returns true if the backend has passed it's ttl
func (d BackendServerDetails) Expired(defaultTTL time.Duration) bool {
	ttl := d.TTL
	if ttl == time.Duration(0) {
		ttl = defaultTTL
	}
	expiryTime := time.Now().Add(-ttl * time.Second)
	return expiryTime.After(d.UpdatedTime)
}

// PruneBackends checks the expiration of backends and deletes any that are expired
func (e Entry) PruneBackends(defaultTTL time.Duration) {
	for backendKey, details := range e.Backends {
		if details.Expired(defaultTTL) {
			delete(e.Backends, backendKey)
		}
	}
}

// PruneEntries removes stale backends - must be called with lock in place
func (table *RoutingTable) PruneEntries(defaultTTL time.Duration) {
	table.Lock()
	defer table.Unlock()
	for routeKey, entry := range table.Entries {
		entry.PruneBackends(defaultTTL)
		if len(entry.Backends) == 0 {
			delete(table.Entries, routeKey)
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
	logger := table.logger.Session("upsert-backend")

	existingEntry, routingKeyFound := table.Entries[key]
	if !routingKeyFound {
		logger.Debug("routing-key-not-found", zap.Object("routing-key", key))
		existingEntry = NewRoutingTableEntry([]BackendServerInfo{info})
		table.Entries[key] = existingEntry
		return true
	}

	newBackendKey, newBackendDetails := table.serverKeyDetailsFromInfo(info)
	currentBackendDetails, backendFound := existingEntry.Backends[newBackendKey]

	if !backendFound ||
		currentBackendDetails.UpdateSucceededBy(newBackendDetails) {
		logger.Debug("applying-change-to-table", zap.Object("old", currentBackendDetails), zap.Object("new", newBackendDetails))
		existingEntry.Backends[newBackendKey] = newBackendDetails
	} else {
		logger.Debug("skipping-stale-event", zap.Object("old", currentBackendDetails), zap.Object("new", newBackendDetails))
	}

	if !backendFound || currentBackendDetails.DifferentFrom(newBackendDetails) {
		return true
	}

	return false
}

// DeleteBackendServerKey returns true if routing configuration should be modified, false if it should not.
func (table *RoutingTable) DeleteBackendServerKey(key RoutingKey, info BackendServerInfo) bool {
	logger := table.logger.Session("delete-backend")

	backendServerKey, newDetails := table.serverKeyDetailsFromInfo(info)
	existingEntry, routingKeyFound := table.Entries[key]

	if routingKeyFound {
		existingDetails, backendFound := existingEntry.Backends[backendServerKey]

		if backendFound && existingDetails.DeleteSucceededBy(newDetails) {
			logger.Debug("removing-from-table", zap.Object("old", existingDetails), zap.Object("new", newDetails))
			delete(existingEntry.Backends, backendServerKey)
			if len(existingEntry.Backends) == 0 {
				delete(table.Entries, key)
			}
			return true
		}
		logger.Debug("skipping-stale-event", zap.Object("old", existingDetails), zap.Object("new", newDetails))
	}
	return false
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
					table.PruneEntries(table.pruneStaleDropletsInterval)
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

// CompareEntries returns a bool after comparing the new version of the Entries
// from the api server to the current state
func (table *RoutingTable) CompareEntries(newTable *RoutingTable) bool {
	newEntries := newTable.Entries
	oldEntries := table.Entries

	if len(newEntries) != len(table.Entries) {
		table.logger.Debug("Length of entries not equal", zap.Int("new", len(newEntries)), zap.Int("old", len(table.Entries)))
		return false
	}

	for routingKey, newTableEntry := range newEntries {
		oldTableEntry, found := oldEntries[routingKey]
		if !found {
			table.logger.Debug("routing key not found")
			return false
		}

		if len(newTableEntry.Backends) != len(oldTableEntry.Backends) {
			table.logger.Debug("Length of backends not equal")
			return false
		}

		for backendServerKey, newDetails := range newTableEntry.Backends {
			oldBackendDetails, foundDetail := oldTableEntry.Backends[backendServerKey]
			if !foundDetail {
				table.logger.Debug("backendServerKey not found")
				return false
			}
			table.UpsertBackendDetails(newDetails, oldBackendDetails)
		}
	}
	return true
}

// UpsertBackendDetails updates the UpdatedTime and ModificationTag with the
// newest state
func (table *RoutingTable) UpsertBackendDetails(
	newDetails *BackendServerDetails,
	oldDetails *BackendServerDetails,
) {
	table.logger.Debug(
		"upsert-backend-details",
		zap.Object("new", newDetails.ModificationTag),
		zap.Object("old", oldDetails.ModificationTag),
	)
	oldDetails.UpdatedTime = time.Now()
	oldDetails.ModificationTag = newDetails.ModificationTag
}

// Get returns the entry based off the key
func (table *RoutingTable) Get(key RoutingKey) Entry {
	return table.Entries[key]
}

// Size returns the number of routes
func (table *RoutingTable) Size() int {
	return len(table.Entries)
}

func (k RoutingKey) String() string {
	return fmt.Sprintf("%d", k.Port)
}
