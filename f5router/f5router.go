/*-
 * Copyright (c) 2017, F5 Networks, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package f5router

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/cf-bigip-ctlr/config"
	"github.com/cf-bigip-ctlr/logger"
	"github.com/cf-bigip-ctlr/route"

	"github.com/uber-go/zap"
	"k8s.io/client-go/util/workqueue"
)

type (
	// frontend ssl profile
	sslProfile struct {
		F5ProfileName string `json:"f5ProfileName,omitempty"`
	}

	// frontend bindaddr and port
	virtualAddress struct {
		BindAddr string `json:"bindAddr,omitempty"`
		Port     int32  `json:"port,omitempty"`
	}

	// backend health monitor
	healthMonitor struct {
		Interval int    `json:"interval,omitempty"`
		Protocol string `json:"protocol"`
		Send     string `json:"send,omitempty"`
		Timeout  int    `json:"timeout,omitempty"`
	}

	// virtual server backend
	backend struct {
		ServiceName     string          `json:"serviceName"`
		ServicePort     int32           `json:"servicePort"`
		PoolMemberAddrs []string        `json:"poolMemberAddrs"`
		HealthMonitors  []healthMonitor `json:"healthMonitors,omitempty"`
	}

	// virtual server frontend
	frontend struct {
		Name string `json:"virtualServerName"`
		// Mutual parameter, partition
		Partition string `json:"partition"`

		// VirtualServer parameters
		Balance        string          `json:"balance,omitempty"`
		Mode           string          `json:"mode,omitempty"`
		VirtualAddress *virtualAddress `json:"virtualAddress,omitempty"`
		SslProfile     *sslProfile     `json:"sslProfile,omitempty"`
	}

	routeItem struct {
		Backend  backend  `json:"backend"`
		Frontend frontend `json:"frontend"`
	}

	// RouteConfig main virtual server configuration
	RouteConfig struct {
		Item routeItem `json:"virtualServer"`
	}

	routeMap     map[string]*RouteConfig
	routeConfigs []*RouteConfig

	// F5Router controller of BigIP configuration objects
	F5Router struct {
		c           *config.Config
		logger      logger.Logger
		m           routeMap
		queue       workqueue.RateLimitingInterface
		writer      *ConfigWriter
		drainUpdate bool
	}

	operation int
	poolData  struct {
		Name     string
		URI      string
		Endpoint string
		Wildcard bool
	}
	virtualData struct {
		Name string
		Addr string
		Port int32
	}

	workItem struct {
		op   operation
		data interface{}
	}
)

const (
	add operation = iota
	remove
)

// NewF5Router create the F5Router route controller
func NewF5Router(logger logger.Logger, c *config.Config) (*F5Router, error) {
	writer, err := NewConfigWriter(logger)
	if nil != err {
		return nil, err
	}
	r := F5Router{
		c:      c,
		logger: logger,
		m:      make(routeMap),
		queue:  workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		writer: writer,
	}
	return &r, nil
}

// ConfigWriter return the internal config writer instance
func (r *F5Router) ConfigWriter() *ConfigWriter {
	return r.writer
}

// Run start the F5Router controller
func (r *F5Router) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	r.logger.Info("f5router-starting")

	done := make(chan struct{})
	go r.runWorker(done)

	close(ready)
	<-signals
	r.queue.ShutDown()
	<-done
	r.logger.Info("f5router-exited")
	return nil
}

func (r *F5Router) runWorker(done chan<- struct{}) {
	r.logger.Debug("f5router-starting-worker")
	for r.process() {
	}
	r.logger.Debug("f5router-stopping-worker")
	close(done)
}

func (r *F5Router) process() bool {
	item, quit := r.queue.Get()
	if quit {
		r.logger.Debug("f5router-quit-signal-received")
		return false
	}

	defer r.queue.Done(item)

	workItem, ok := item.(workItem)
	if false == ok {
		r.logger.Warn("f5router-unknown-workitem",
			zap.Error(errors.New("workqueue delivered unsupported work type")))
		return true
	}

	var tryUpdate bool
	var err error
	switch work := workItem.data.(type) {
	case poolData:
		r.logger.Debug("f5router-received-pool-request")
		tryUpdate, err = r.processPool(workItem.op, work)
	case virtualData:
		r.logger.Debug("f5router-received-virtual-request")
		tryUpdate, err = r.processVirtual(workItem.op, work)
	default:
		r.logger.Warn("f5router-unknown-request",
			zap.Error(errors.New("workqueue item contains unsupported work request")))
	}

	if false == r.drainUpdate {
		r.drainUpdate = tryUpdate
	}

	if nil != err {
		r.logger.Warn("f5router-process-error", zap.Error(err))
	} else {
		l := r.queue.Len()
		if true == r.drainUpdate && 0 == l {
			r.drainUpdate = false

			sections := make(map[string]interface{})
			sections["global"] = config.GlobalSection{
				LogLevel:       r.c.Logging.Level,
				VerifyInterval: r.c.BigIP.VerifyInterval,
			}
			sections["bigip"] = r.c.BigIP

			services := routeConfigs{}
			for _, rc := range r.m {
				services = append(services, rc)
			}
			sections["services"] = services

			r.logger.Debug("f5router-drain", zap.Object("writing", sections))

			output, err := json.Marshal(sections)
			if nil != err {
				r.logger.Warn("f5router-config-marshal-error", zap.Error(err))
			} else {
				n, err := r.writer.Write(output)
				if nil != err {
					r.logger.Warn("f5router-config-write-error", zap.Error(err))
				} else if len(output) != n {
					r.logger.Warn("f5router-config-short-write", zap.Error(err))
				} else {
					r.logger.Debug("f5router-wrote-config",
						zap.Int("number-services", len(services)),
					)
				}
			}
		} else {
			r.logger.Debug("f5router-write-not-ready",
				zap.Bool("update", r.drainUpdate),
				zap.Int("length", l),
			)
		}
	}
	return true
}

// makePool create Pool-Only configuration item
func (r *F5Router) makePool(
	name string,
	uri string,
	addrs ...string,
) *RouteConfig {
	return &RouteConfig{
		Item: routeItem{
			Backend: backend{
				ServiceName:     uri,
				ServicePort:     -1, // unused
				PoolMemberAddrs: addrs,
			},
			Frontend: frontend{
				Name: name,
				//TODO need to handle multiple partitions
				Partition: r.c.BigIP.Partition[0],
				Balance:   r.c.BigIP.Balance,
				Mode:      "http",
			},
		},
	}
}

func (r *F5Router) processPool(op operation, p poolData) (bool, error) {
	if op == add {
		return r.processPoolAdd(p), nil
	} else if op == remove {
		return r.processPoolRemove(p), nil
	} else {
		return false, fmt.Errorf("received unsupported pool operation %v", op)
	}
}

func (r *F5Router) processPoolAdd(p poolData) bool {
	var ret bool
	if pool, ok := r.m[p.Name]; ok {
		var found bool
		for _, e := range pool.Item.Backend.PoolMemberAddrs {
			if e == p.Endpoint {
				found = true
				break
			}
		}
		if false == found {
			pool.Item.Backend.PoolMemberAddrs =
				append(pool.Item.Backend.PoolMemberAddrs, p.Endpoint)
			ret = true
			r.logger.Debug("f5router-pool-updated", zap.Object("pool-config", pool))
		} else {
			r.logger.Debug("f5router-pool-not-updated", []zap.Field{
				zap.String("wanted", p.Endpoint),
				zap.Object("have", pool),
			}...)
		}
	} else {
		pool := r.makePool(p.Name, p.URI, p.Endpoint)
		r.m[p.Name] = pool
		ret = true
		r.logger.Debug("f5router-pool-created", zap.Object("pool-config", pool))
	}

	return ret
}

func (r *F5Router) processPoolRemove(p poolData) bool {
	var ret bool
	if pool, ok := r.m[p.Name]; ok {
		for i, e := range pool.Item.Backend.PoolMemberAddrs {
			if e == p.Endpoint {
				pool.Item.Backend.PoolMemberAddrs = append(
					pool.Item.Backend.PoolMemberAddrs[:i],
					pool.Item.Backend.PoolMemberAddrs[i+1:]...)
				break
			}
		}
		r.logger.Debug("f5router-pool-endpoint-removed",
			zap.String("removed", p.Endpoint),
			zap.Object("remaining", pool),
		)
		ret = true

		if 0 == len(pool.Item.Backend.PoolMemberAddrs) {
			delete(r.m, p.Name)
			r.logger.Debug("f5router-pool-removed")
		}
	} else {
		r.logger.Debug("f5router-pool-not-found", zap.String("uri", p.Name))
	}

	return ret
}

func (r *F5Router) queuePoolWork(
	name string,
	op operation,
	uri string,
	wild bool,
	endpoint *route.Endpoint,
) {
	p := poolData{
		Name:     name,
		URI:      uri,
		Endpoint: endpoint.CanonicalAddr(),
		Wildcard: wild,
	}
	w := workItem{
		op:   op,
		data: p,
	}
	r.queue.Add(w)
}

func makePoolName(uri string) string {
	sum := sha256.Sum256([]byte(uri))
	index := strings.Index(uri, ".")

	name := fmt.Sprintf("%s-%x", uri[:index], sum[:8])
	return name
}

// UpdatePoolEndpoints create Pool-Only config or update existing endpoint
func (r *F5Router) UpdatePoolEndpoints(
	uri string,
	endpoint *route.Endpoint,
) {
	r.logger.Debug("f5router-updating-pool",
		zap.String("uri", uri),
		zap.String("endpoint", endpoint.CanonicalAddr()),
	)

	var (
		u    string
		name string
		wild bool
	)
	if strings.HasPrefix(uri, "*.") {
		u = strings.TrimPrefix(uri, "*.")
		name = u
		wild = true
	} else {
		u = uri
		name = makePoolName(uri)
	}
	r.queuePoolWork(name, add, u, wild, endpoint)
}

// RemovePoolEndpoints remove endpoint from config, if empty remove
// VirtualServer
func (r *F5Router) RemovePoolEndpoints(
	uri string,
	endpoint *route.Endpoint,
) {
	r.logger.Debug("f5router-removing-pool",
		zap.String("uri", uri),
		zap.String("endpoint", endpoint.CanonicalAddr()),
	)
	var (
		u    string
		name string
		wild bool
	)
	if strings.HasPrefix(uri, "*.") {
		u = strings.TrimPrefix(uri, "*.")
		name = u
		wild = true
	} else {
		u = uri
		name = makePoolName(uri)
	}
	r.queuePoolWork(name, remove, u, wild, endpoint)
}

func (r *F5Router) makeVirtual(
	name string,
	addr string,
	port int32,
) *RouteConfig {
	vs := &RouteConfig{
		Item: routeItem{
			Backend: backend{
				ServiceName:     name,
				ServicePort:     -1,         // unused
				PoolMemberAddrs: []string{}, // unused
			},
			Frontend: frontend{
				Name: name,
				//TODO need to handle multiple partitions
				Partition: r.c.BigIP.Partition[0],
				Balance:   r.c.BigIP.Balance,
				Mode:      "http",
				VirtualAddress: &virtualAddress{
					BindAddr: addr,
					Port:     port,
				},
			},
		},
	}
	return vs
}

func (r *F5Router) processVirtual(op operation, v virtualData) (bool, error) {
	if op == add {
		return r.processVirtualAdd(v), nil
	} else if op == remove {
		return r.processVirtualRemove(v), nil
	} else {
		return false, fmt.Errorf("received unsupported virtual operation %v", op)
	}
}

func (r *F5Router) processVirtualAdd(v virtualData) bool {
	vs := r.makeVirtual(v.Name, v.Addr, v.Port)
	r.m[v.Name] = vs
	r.logger.Debug("f5router-virtual-server-updated", zap.Object("virtual", vs))
	return true
}

func (r *F5Router) processVirtualRemove(v virtualData) bool {
	delete(r.m, v.Name)
	r.logger.Debug("f5router-virtual-server-removed", zap.String("virtual", v.Name))
	return true
}

func (r *F5Router) queueVirtualWork(
	op operation,
	name string,
	addr string,
	port int32,
) {
	vs := virtualData{
		Name: name,
		Addr: addr,
		Port: port,
	}

	w := workItem{
		op:   op,
		data: vs,
	}
	r.queue.Add(w)
}

// UpdateVirtualServer create VirtualServer config with VirtualAddress frontend
func (r *F5Router) UpdateVirtualServer(
	name string,
	addr string,
	port int32,
) {
	r.logger.Debug("f5router-updating-virtual-server",
		zap.String("name", name),
		zap.String("address", fmt.Sprintf("%s:%d", addr, port)),
	)
	r.queueVirtualWork(add, name, addr, port)
}

// RemoveVirtualServer delete VirtualServer config
func (r *F5Router) RemoveVirtualServer(
	name string,
) {
	r.logger.Debug("f5router-removing-virtual-server",
		zap.String("name", name),
	)
	r.queueVirtualWork(remove, name, "", -1)
}
