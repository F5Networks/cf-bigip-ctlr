/*
 * Portions Copyright (c) 2017, F5 Networks, Inc.
 */

package registry

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/f5router"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/routeUpdate"
	"github.com/F5Networks/cf-bigip-ctlr/logger"
	"github.com/F5Networks/cf-bigip-ctlr/metrics"
	"github.com/F5Networks/cf-bigip-ctlr/registry/container"
	"github.com/F5Networks/cf-bigip-ctlr/route"

	"github.com/uber-go/zap"
)

//go:generate counterfeiter -o fakes/fake_registry.go . Registry
type Registry interface {
	Register(uri route.Uri, endpoint *route.Endpoint)
	Unregister(uri route.Uri, endpoint *route.Endpoint)
	Lookup(uri route.Uri) *route.Pool
	LookupWithInstance(uri route.Uri, appID, appIndex string) *route.Pool
	LookupWithoutWildcard(uri route.Uri) *route.Pool
	WalkNodesWithPool(func(f *container.Trie))
	StartPruningCycle()
	StopPruningCycle()
	NumUris() int
	NumEndpoints() int
	MarshalJSON() ([]byte, error)
}

type PruneStatus int

const (
	CONNECTED = PruneStatus(iota)
	DISCONNECTED
)

type RouteRegistry struct {
	sync.RWMutex

	logger logger.Logger

	// Access to the Trie datastructure should be governed by the RWMutex of RouteRegistry
	byURI *container.Trie

	// used for ability to suspend pruning
	suspendPruning func() bool
	pruningStatus  PruneStatus

	pruneStaleDropletsInterval time.Duration
	dropletStaleThreshold      time.Duration

	reporter metrics.RouteRegistryReporter

	ticker           *time.Ticker
	timeOfLastUpdate time.Time

	routerGroupGUID string

	listener routeUpdate.Listener

	c *config.Config
}

func NewRouteRegistry(
	logger logger.Logger,
	c *config.Config,
	listener routeUpdate.Listener,
	reporter metrics.RouteRegistryReporter,
	routerGroupGUID string,
) *RouteRegistry {
	r := &RouteRegistry{}
	r.logger = logger
	r.byURI = container.NewTrie()

	r.pruneStaleDropletsInterval = c.PruneStaleDropletsInterval
	r.dropletStaleThreshold = c.DropletStaleThreshold
	r.suspendPruning = func() bool { return false }

	r.reporter = reporter
	r.routerGroupGUID = routerGroupGUID
	r.listener = listener
	r.c = c
	return r
}

func (r *RouteRegistry) Register(uri route.Uri, endpoint *route.Endpoint) {
	t := time.Now()

	r.Lock()

	routekey := uri.RouteKey()

	var updateRoute bool
	pool := r.byURI.Find(routekey)
	if pool == nil {
		contextPath := parseContextPath(uri)
		pool = route.NewPool(r.dropletStaleThreshold/4, contextPath)
		r.byURI.Insert(routekey, pool)
		r.logger.Debug("uri-added", zap.Stringer("uri", routekey))
		updateRoute = true
	} else {
		if nil == pool.FindById(endpoint.CanonicalAddr()) {
			updateRoute = true
		}
	}

	endpointAdded := pool.Put(endpoint)
	if endpointAdded && updateRoute && nil != r.listener {
		r.updateRouter(routeUpdate.Add, routekey, endpoint)
	}

	r.timeOfLastUpdate = t
	r.Unlock()

	r.reporter.CaptureRegistryMessage(endpoint)

	routerGroupGUID := r.routerGroupGUID
	if routerGroupGUID == "" {
		routerGroupGUID = "-"
	}

	zapData := []zap.Field{
		zap.Stringer("uri", uri),
		zap.String("router-group-guid", routerGroupGUID),
		zap.String("backend", endpoint.CanonicalAddr()),
		zap.Object("modification_tag", endpoint.ModificationTag),
	}

	if endpointAdded {
		r.logger.Debug("endpoint-registered", zapData...)
	} else {
		r.logger.Debug("endpoint-not-registered", zapData...)
	}
}

func (r *RouteRegistry) Unregister(uri route.Uri, endpoint *route.Endpoint) {
	routerGroupGUID := r.routerGroupGUID
	if routerGroupGUID == "" {
		routerGroupGUID = "-"
	}

	zapData := []zap.Field{
		zap.Stringer("uri", uri),
		zap.String("router-group-guid", routerGroupGUID),
		zap.String("backend", endpoint.CanonicalAddr()),
		zap.Object("modification_tag", endpoint.ModificationTag),
	}

	r.Lock()

	uri = uri.RouteKey()

	pool := r.byURI.Find(uri)
	if pool != nil {
		endpointRemoved := pool.Remove(endpoint)
		emptiedPool := pool.IsEmpty()

		if emptiedPool {
			r.byURI.Delete(uri)
		}

		if endpointRemoved {
			if nil != r.listener {
				r.updateRouter(routeUpdate.Remove, uri, endpoint)
			}
			r.logger.Debug("endpoint-unregistered", zapData...)
		} else {
			r.logger.Debug("endpoint-not-unregistered", zapData...)
		}
	}

	r.Unlock()
	r.reporter.CaptureUnregistryMessage(endpoint)
}

func (r *RouteRegistry) Lookup(uri route.Uri) *route.Pool {
	started := time.Now()

	r.RLock()

	uri = uri.RouteKey()
	var err error
	pool := r.byURI.MatchUri(uri)
	for pool == nil && err == nil {
		uri, err = uri.NextWildcard()
		pool = r.byURI.MatchUri(uri)
	}

	r.RUnlock()
	endLookup := time.Now()
	r.reporter.CaptureLookupTime(endLookup.Sub(started))
	return pool
}

func (r *RouteRegistry) LookupWithInstance(
	uri route.Uri,
	appID string,
	appIndex string,
) *route.Pool {
	uri = uri.RouteKey()
	p := r.Lookup(uri)

	if p == nil {
		return nil
	}

	var surgicalPool *route.Pool

	p.Each(func(e *route.Endpoint) {
		if (e.ApplicationId == appID) && (e.PrivateInstanceIndex == appIndex) {
			surgicalPool = route.NewPool(0, "")
			surgicalPool.Put(e)
		}
	})
	return surgicalPool
}

func (r *RouteRegistry) LookupWithoutWildcard(uri route.Uri) *route.Pool {
	r.RLock()

	uri = uri.RouteKey()
	pool := r.byURI.Find(uri)

	r.RUnlock()

	return pool
}

func (r *RouteRegistry) WalkNodesWithPool(f func(*container.Trie)) {
	r.RLock()

	r.byURI.EachNodeWithPool(f)

	r.RUnlock()
}

func (r *RouteRegistry) StartPruningCycle() {
	if r.pruneStaleDropletsInterval > 0 {
		r.Lock()
		r.ticker = time.NewTicker(r.pruneStaleDropletsInterval)
		r.Unlock()

		go func() {
			for {
				select {
				case <-r.ticker.C:
					r.logger.Debug("start-pruning-routes")
					r.pruneStaleDroplets()
					r.logger.Debug("finished-pruning-routes")
					msSinceLastUpdate := uint64(time.Since(r.TimeOfLastUpdate()) / time.Millisecond)
					r.reporter.CaptureRouteStats(r.NumUris(), msSinceLastUpdate)
				}
			}
		}()
	}
}

func (r *RouteRegistry) StopPruningCycle() {
	r.Lock()
	if r.ticker != nil {
		r.ticker.Stop()
	}
	r.Unlock()
}

func (registry *RouteRegistry) NumUris() int {
	registry.RLock()
	uriCount := registry.byURI.PoolCount()
	registry.RUnlock()

	return uriCount
}

func (r *RouteRegistry) TimeOfLastUpdate() time.Time {
	r.RLock()
	t := r.timeOfLastUpdate
	r.RUnlock()

	return t
}

func (r *RouteRegistry) NumEndpoints() int {
	r.RLock()
	count := r.byURI.EndpointCount()
	r.RUnlock()

	return count
}

func (r *RouteRegistry) MarshalJSON() ([]byte, error) {
	r.RLock()
	defer r.RUnlock()

	return json.Marshal(r.byURI.ToMap())
}

func (r *RouteRegistry) pruneStaleDroplets() {
	r.Lock()
	defer r.Unlock()

	// suspend pruning if option enabled and if NATS is unavailable
	if r.suspendPruning() {
		r.logger.Info("prune-suspended")
		r.pruningStatus = DISCONNECTED
		return
	}
	if r.pruningStatus == DISCONNECTED {
		// if we are coming back from being disconnected from source,
		// bulk update routes / mark updated to avoid pruning right away
		r.logger.Debug("prune-unsuspended-refresh-routes-start")
		r.freshenRoutes()
		r.logger.Debug("prune-unsuspended-refresh-routes-complete")
	}
	r.pruningStatus = CONNECTED

	routerGroupGUID := r.routerGroupGUID
	if routerGroupGUID == "" {
		routerGroupGUID = "-"
	}

	r.byURI.EachNodeWithPool(func(t *container.Trie) {
		endpoints := t.Pool.PruneEndpoints(r.dropletStaleThreshold)
		t.Snip()
		if len(endpoints) > 0 {
			addresses := []string{}
			for _, e := range endpoints {
				addresses = append(addresses, e.CanonicalAddr())
				if nil != r.listener {
					r.updateRouter(routeUpdate.Remove, route.Uri(t.ToPath()), e)
				}
			}
			r.logger.Info("pruned-route",
				zap.String("uri", t.ToPath()),
				zap.Object("endpoints", addresses),
				zap.String("router-group-guid", routerGroupGUID),
			)
		}
	})
}

func (r *RouteRegistry) SuspendPruning(f func() bool) {
	r.Lock()
	r.suspendPruning = f
	r.Unlock()
}

// bulk update to mark pool / endpoints as updated
func (r *RouteRegistry) freshenRoutes() {
	now := time.Now()
	r.byURI.EachNodeWithPool(func(t *container.Trie) {
		t.Pool.MarkUpdated(now)
	})
}

func (r *RouteRegistry) updateRouter(
	updateType routeUpdate.Operation,
	uri route.Uri,
	endpoint *route.Endpoint,
) {
	update, err := f5router.NewUpdate(r.logger, updateType, uri, endpoint)
	if nil != err {
		r.logger.Warn("f5router-skipping-update",
			zap.Error(err),
			zap.String("operation", routeUpdate.Remove.String()),
		)
	} else {
		r.listener.UpdateRoute(update)
	}
}

func parseContextPath(uri route.Uri) string {
	contextPath := "/"
	split := strings.SplitN(strings.TrimPrefix(uri.String(), "/"), "/", 2)

	if len(split) > 1 {
		contextPath += split[1]
	}

	if idx := strings.Index(string(contextPath), "?"); idx >= 0 {
		contextPath = contextPath[0:idx]
	}

	return contextPath
}
