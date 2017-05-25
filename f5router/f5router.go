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
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/cf-bigip-ctlr/config"
	"github.com/cf-bigip-ctlr/logger"
	"github.com/cf-bigip-ctlr/registry/container"
	"github.com/cf-bigip-ctlr/route"

	"github.com/uber-go/zap"
	"k8s.io/client-go/util/workqueue"
)

const (
	// Add operation
	Add operation = iota
	// Update operation
	Update
	// Remove operation
	Remove
)

const (
	// HTTP virtual server without SSL termination on port 80
	HTTP vsType = iota
	// HTTPS virtual server with SSL termination on port 443
	HTTPS
)

const (
	// HTTPRouterName HTTP virtual server name
	HTTPRouterName = "routing-vip-http"
	// HTTPSRouterName HTTPS virtual server name
	HTTPSRouterName = "routing-vip-https"
	// CFRoutingPolicyName Policy name for CF routing
	CFRoutingPolicyName = "cf-routing-policy"
)

func (r rules) Len() int           { return len(r) }
func (r rules) Less(i, j int) bool { return r[i].FullURI < r[j].FullURI }
func (r rules) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }

func (op operation) String() string {
	switch op {
	case Add:
		return "Add"
	case Update:
		return "Update"
	case Remove:
		return "Remove"
	}

	return "Unknown"
}

func makePoolName(uri string) string {
	var name string
	if strings.HasPrefix(uri, "*.") {
		name = strings.TrimPrefix(uri, "*.")
	} else {
		sum := sha256.Sum256([]byte(uri))
		index := strings.Index(uri, ".")

		name = fmt.Sprintf("%s-%x", uri[:index], sum[:8])
	}
	return name
}

// NewF5Router create the F5Router route controller
func NewF5Router(logger logger.Logger, c *config.Config) (*F5Router, error) {
	writer, err := NewConfigWriter(logger)
	if nil != err {
		return nil, err
	}
	r := F5Router{
		c:         c,
		logger:    logger,
		m:         make(routeMap),
		r:         make(ruleMap),
		wildcards: make(ruleMap),
		queue:     workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		writer:    writer,
	}

	err = r.writeInitialConfig()
	if nil != err {
		return nil, err
	}

	r.routeVSHTTP = r.makeVirtual(HTTPRouterName, HTTP)
	if 0 != len(c.BigIP.SSLProfile) {
		r.routeVSHTTPS = r.makeVirtual(HTTPSRouterName, HTTPS)
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

func (r *F5Router) makeVirtual(
	name string,
	t vsType,
) *routeConfig {
	var port int32
	var ssl *sslProfile

	if t == HTTP {
		port = 80
	} else if t == HTTPS {
		port = 443
		ssl = &sslProfile{
			F5ProfileName: r.c.BigIP.SSLProfile,
		}
	}

	vs := &routeConfig{
		Item: routeItem{
			Backend: backend{
				ServiceName:     name,
				ServicePort:     -1,         // unused
				PoolMemberAddrs: []string{}, // unused
			},
			Frontend: frontend{
				Name: name,
				//FIXME need to handle multiple partitions
				Partition: r.c.BigIP.Partitions[0],
				Balance:   r.c.BigIP.Balance,
				Mode:      "http",
				VirtualAddress: &virtualAddress{
					BindAddr: r.c.BigIP.ExternalAddr,
					Port:     port,
				},
				SSLProfile: ssl,
			},
		},
	}
	plcs := r.generatePolicyList()
	prfls := r.generateNameList(r.c.BigIP.Profiles)
	vs.Item.Frontend.Policies = plcs
	vs.Item.Frontend.Profiles = prfls
	return vs
}

func (r *F5Router) writeInitialConfig() error {
	sections := make(map[string]interface{})
	sections["global"] = config.GlobalSection{
		LogLevel:       r.c.Logging.Level,
		VerifyInterval: r.c.BigIP.VerifyInterval,
	}
	sections["bigip"] = r.c.BigIP

	output, err := json.Marshal(sections)
	if nil != err {
		return fmt.Errorf("failed marshaling initial config: %v", err)
	}
	n, err := r.writer.Write(output)
	if nil != err {
		return fmt.Errorf("failed writing initial config: %v", err)
	} else if len(output) != n {
		return fmt.Errorf("short write from initial config")
	}

	return nil
}

func (r *F5Router) runWorker(done chan<- struct{}) {
	r.logger.Debug("f5router-starting-worker")
	for r.process() {
	}
	r.logger.Debug("f5router-stopping-worker")
	close(done)
}

func (r *F5Router) generateNameList(names []string) []*nameRef {
	var refs []*nameRef
	for i := range names {
		p := strings.TrimPrefix(names[i], "/")
		parts := strings.Split(p, "/")
		if 2 == len(parts) {
			refs = append(refs, &nameRef{
				Name:      parts[1],
				Partition: parts[0],
			})
		} else {
			r.logger.Warn("f5router-skipping-name",
				zap.String("parse-error",
					fmt.Sprintf("skipping name %s, need format /[partition]/[name]",
						p)),
			)
		}
	}

	return refs
}

func (r *F5Router) generatePolicyList() []*nameRef {
	var n []*nameRef
	n = append(n, r.generateNameList(r.c.BigIP.Policies.PreRouting)...)
	n = append(n, &nameRef{
		Name:      CFRoutingPolicyName,
		Partition: r.c.BigIP.Partitions[0], // FIXME handle multiple partitions
	})
	n = append(n, r.generateNameList(r.c.BigIP.Policies.PostRouting)...)
	return n
}

func (r *F5Router) process() bool {
	item, quit := r.queue.Get()
	if quit {
		r.logger.Debug("f5router-quit-signal-received")
		return false
	}

	defer r.queue.Done(item)

	ru, ok := item.(routeUpdate)
	if false == ok {
		r.logger.Warn("f5router-unknown-workitem",
			zap.Error(errors.New("workqueue delivered unsupported work type")))
		return true
	}

	var tryUpdate bool
	var err error
	r.logger.Debug("f5router-received-pool-request")
	if ru.Op == Add {
		tryUpdate = r.processRouteAdd(ru)
	} else if ru.Op == Update {
		tryUpdate = true
	} else if ru.Op == Remove {
		tryUpdate = r.processRouteRemove(ru)
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

			sections["policies"] = policies{r.makeRoutePolicy(CFRoutingPolicyName)}

			services := routeConfigs{}
			services = append(services, r.routeVSHTTP)
			if nil != r.routeVSHTTP {
				services = append(services, r.routeVSHTTPS)
			}

			ru.T.EachNodeWithPool(func(t *container.Trie) {
				var addrs []string
				t.Pool.Each(func(e *route.Endpoint) {
					addrs = append(addrs, e.CanonicalAddr())
				})
				uri := t.ToPath()
				p := r.makePool(makePoolName(uri), uri, addrs...)
				services = append(services, p)
			})

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
) *routeConfig {
	return &routeConfig{
		Item: routeItem{
			Backend: backend{
				ServiceName:     uri,
				ServicePort:     -1, // unused
				PoolMemberAddrs: addrs,
			},
			Frontend: frontend{
				Name: name,
				//FIXME need to handle multiple partitions
				Partition: r.c.BigIP.Partitions[0],
				Balance:   r.c.BigIP.Balance,
				Mode:      "http",
			},
		},
	}
}

func (r *F5Router) makeRouteRule(ru routeUpdate) (*rule, error) {
	_u := "scheme://" + ru.URI.String()
	_u = strings.TrimSuffix(_u, "/")
	u, err := url.Parse(_u)
	if nil != err {
		return nil, err
	}

	var b bytes.Buffer
	b.WriteRune('/')
	b.WriteString(r.c.BigIP.Partitions[0]) //FIXME update to use mutliple partitions
	b.WriteRune('/')
	b.WriteString(ru.Name)

	a := action{
		Forward: true,
		Name:    "0",
		Pool:    b.String(),
		Request: true,
	}

	var c []*condition
	if true == strings.HasPrefix(ru.URI.String(), "*.") {
		c = append(c, &condition{
			EndsWith: true,
			Host:     true,
			HTTPHost: true,
			Name:     "0",
			Index:    0,
			Request:  true,
			Values:   []string{strings.TrimPrefix(u.Host, "*.")},
		})
	} else {
		c = append(c, &condition{
			Equals:   true,
			Host:     true,
			HTTPHost: true,
			Name:     "0",
			Index:    0,
			Request:  true,
			Values:   []string{u.Host},
		})

		if 0 != len(u.EscapedPath()) {
			path := strings.TrimPrefix(u.EscapedPath(), "/")
			segments := strings.Split(path, "/")

			for i, v := range segments {
				c = append(c, &condition{
					Equals:      true,
					HTTPURI:     true,
					PathSegment: true,
					Name:        strconv.Itoa(i + 1),
					Index:       i + 1,
					Request:     true,
					Values:      []string{v},
				})
			}
		}
	}

	rl := rule{
		FullURI:    ru.URI.String(),
		Actions:    []*action{&a},
		Conditions: c,
		Name:       ru.Name,
	}

	r.logger.Debug("f5router-rule-create", zap.Object("rule", rl))
	return &rl, nil
}

func (r *F5Router) makeRoutePolicy(policyName string) *policy {
	plcy := policy{
		Controls:  []string{"forwarding"},
		Legacy:    true,
		Name:      policyName,
		Partition: r.c.BigIP.Partitions[0], //FIXME handle multiple partitions
		Requires:  []string{"http"},
		Rules:     []*rule{},
		Strategy:  "/Common/first-match",
	}

	var wg sync.WaitGroup
	wg.Add(2)
	sortRules := func(r ruleMap, rls *rules, ordinal int) {
		for _, v := range r {
			*rls = append(*rls, v)
		}

		sort.Sort(sort.Reverse(*rls))

		for _, v := range *rls {
			v.Ordinal = ordinal
			ordinal++
		}
		wg.Done()
	}

	rls := rules{}
	go sortRules(r.r, &rls, 0)

	w := rules{}
	go sortRules(r.wildcards, &w, len(r.m))

	wg.Wait()

	rls = append(rls, w...)

	plcy.Rules = rls

	r.logger.Debug("f5router-policy-create", zap.Object("policy", plcy))
	return &plcy
}

func (r *F5Router) processRouteAdd(ru routeUpdate) bool {
	var ret bool
	rule, err := r.makeRouteRule(ru)
	if nil != err {
		r.logger.Warn("f5router-rule-error", zap.Error(err))
		return false
	}

	if true == strings.HasPrefix(ru.URI.String(), "*.") {
		r.wildcards[ru.URI] = rule
		r.logger.Debug("f5router-wildcard-rule-updated",
			zap.String("name", ru.Name),
			zap.String("uri", ru.URI.String()),
		)
	} else {
		r.r[ru.URI] = rule
		r.logger.Debug("f5router-app-rule-updated",
			zap.String("name", ru.Name),
			zap.String("uri", ru.URI.String()),
		)
	}

	return ret
}

func (r *F5Router) processRouteRemove(ru routeUpdate) bool {
	var ret bool
	pool := ru.T.Find(ru.URI)
	if nil == pool || pool.IsEmpty() {
		if true == strings.HasPrefix(ru.URI.String(), "*.") {
			delete(r.wildcards, ru.URI)
			r.logger.Debug("f5router-wildcard-rule-removed",
				zap.String("name", ru.Name),
				zap.String("uri", ru.URI.String()),
			)
		} else {
			delete(r.r, ru.URI)
			r.logger.Debug("f5router-app-rule-removed",
				zap.String("name", ru.Name),
				zap.String("uri", ru.URI.String()),
			)
		}
		ret = true
	} else {
		r.logger.Debug("f5router-route-not-found", zap.String("uri", ru.Name))
	}

	return ret
}

// RouteUpdate send update information to processor
func (r *F5Router) RouteUpdate(
	op operation,
	t *container.Trie,
	uri route.Uri,
) {
	r.logger.Debug("f5router-updating-pool",
		zap.String("operation", op.String()),
		zap.String("uri", uri.String()),
	)

	ru := routeUpdate{
		Name: makePoolName(uri.String()),
		URI:  uri,
		T:    t,
		Op:   op,
	}
	r.queue.Add(ru)
}
