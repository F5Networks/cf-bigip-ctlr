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

	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/logger"
	"github.com/F5Networks/cf-bigip-ctlr/registry"
	"github.com/F5Networks/cf-bigip-ctlr/registry/container"
	"github.com/F5Networks/cf-bigip-ctlr/route"

	"github.com/uber-go/zap"
	"k8s.io/client-go/util/workqueue"
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

// Helper to add a leading slash to bigip paths
func fixupNames(names []string) []string {
	var fixed []string
	for _, val := range names {
		if !strings.HasPrefix(val, "/") {
			val = "/" + val
		}
		fixed = append(fixed, val)
	}
	return fixed
}

// NewF5Router create the F5Router route controller
func NewF5Router(
	logger logger.Logger,
	c *config.Config,
	writer Writer,
) (*F5Router, error) {
	r := F5Router{
		c:         c,
		logger:    logger,
		r:         make(ruleMap),
		wildcards: make(ruleMap),
		queue:     workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		writer:    writer,
	}

	err := r.validateConfig()
	if nil != err {
		return nil, err
	}

	err = r.writeInitialConfig()
	if nil != err {
		return nil, err
	}

	r.routeVSHTTP = r.makeVirtual(HTTPRouterName, HTTP)
	if 0 != len(c.BigIP.SSLProfiles) {
		r.routeVSHTTPS = r.makeVirtual(HTTPSRouterName, HTTPS)
	}
	return &r, nil
}

// Run start the F5Router controller
func (r *F5Router) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	r.logger.Info("f5router-starting")

	done := make(chan struct{})
	go r.runWorker(done)

	close(ready)

	r.logger.Info("f5router-started")
	<-signals
	r.queue.ShutDown()
	<-done
	r.logger.Info("f5router-exited")
	return nil
}

func (r *F5Router) validateConfig() error {
	if nil == r.c {
		return errors.New("no configuration provided")
	}

	if nil == r.writer {
		return errors.New("no functional writer provided")
	}

	if 0 == len(r.c.BigIP.URL) ||
		0 == len(r.c.BigIP.User) ||
		0 == len(r.c.BigIP.Pass) ||
		0 == len(r.c.BigIP.Partitions) ||
		0 == len(r.c.BigIP.ExternalAddr) {
		return fmt.Errorf(
			"required parameter missing; URL, User, Pass, Partitions, ExternalAddr "+
				"must have value: %+v", r.c.BigIP)
	}

	if 0 == len(r.c.BigIP.HealthMonitors) {
		r.c.BigIP.HealthMonitors = []string{"/Common/tcp_half_open"}
	}

	if 0 == len(r.c.BigIP.Profiles) {
		r.c.BigIP.Profiles = []string{"/Common/http"}
	}

	return nil
}

func (r *F5Router) makeVirtual(
	name string,
	t vsType,
) *virtual {
	var port int32
	var sslProfiles []*nameRef

	if t == HTTP {
		port = 80
	} else if t == HTTPS {
		port = 443
		sslProfiles = r.generateNameList(r.c.BigIP.SSLProfiles)
	}

	vs := &virtual{
		VirtualServerName: name,
		//FIXME need to handle multiple partitions
		Partition: r.c.BigIP.Partitions[0],
		Mode:      "http",
		VirtualAddress: &virtualAddress{
			BindAddr: r.c.BigIP.ExternalAddr,
			Port:     port,
		},
	}
	plcs := r.generatePolicyList()
	prfls := append(r.generateNameList(r.c.BigIP.Profiles), sslProfiles...)
	vs.Policies = plcs
	vs.Profiles = prfls
	return vs
}

func (r *F5Router) writeInitialConfig() error {
	sections := make(map[string]interface{})
	sections["global"] = globalConfig{
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
	n = append(n, r.generateNameList(r.c.BigIP.Policies)...)
	n = append(n, &nameRef{
		Name:      CFRoutingPolicyName,
		Partition: r.c.BigIP.Partitions[0], // FIXME handle multiple partitions
	})
	return n
}

func (r *F5Router) createPolicies(rs *resources, wg *sync.WaitGroup) {
	defer wg.Done()

	rs.Policies = policies{
		r.makeRoutePolicy(CFRoutingPolicyName),
	}
}

func (r *F5Router) createVirtuals(rs *resources, wg *sync.WaitGroup) {
	defer wg.Done()

	rs.Virtuals = append(rs.Virtuals, r.routeVSHTTP)
	if nil != r.routeVSHTTPS {
		rs.Virtuals = append(rs.Virtuals, r.routeVSHTTPS)
	}
}

func (r *F5Router) createPools(rs *resources, ru routeUpdate, wg *sync.WaitGroup) {
	defer wg.Done()

	ru.R.WalkNodesWithPool(func(t *container.Trie) {
		var addrs []string
		t.Pool.Each(func(e *route.Endpoint) {
			addrs = append(addrs, e.CanonicalAddr())
		})
		uri := t.ToPath()
		p := r.makePool(makePoolName(uri), uri, addrs...)
		rs.Pools = append(rs.Pools, p)
	})
}

func (r *F5Router) createResources(ru routeUpdate) *resources {
	rs := &resources{}

	var wg sync.WaitGroup

	wg.Add(1)
	go r.createPolicies(rs, &wg)

	wg.Add(1)
	go r.createVirtuals(rs, &wg)

	wg.Add(1)
	go r.createPools(rs, ru, &wg)

	wg.Wait()

	return rs
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
	if ru.Op == registry.Add {
		tryUpdate = r.processRouteAdd(ru)
	} else if ru.Op == registry.Update {
		tryUpdate = true
	} else if ru.Op == registry.Remove {
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

			sections["global"] = globalConfig{
				LogLevel:       r.c.Logging.Level,
				VerifyInterval: r.c.BigIP.VerifyInterval,
			}

			sections["bigip"] = r.c.BigIP

			sections["resources"] = r.createResources(ru)

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
) *pool {
	prefixHealthMonitors := fixupNames(r.c.BigIP.HealthMonitors)
	return &pool{
		Name: name,
		//FIXME need to handle multiple partitions
		Partition:       r.c.BigIP.Partitions[0],
		Balance:         r.c.BigIP.LoadBalancingMode,
		ServiceName:     uri,
		ServicePort:     -1, // unused
		PoolMemberAddrs: addrs,
		MonitorNames:    prefixHealthMonitors,
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
			Values:   []string{strings.TrimPrefix(u.Host, "*")},
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
	go sortRules(r.wildcards, &w, len(r.r))

	wg.Wait()

	rls = append(rls, w...)

	plcy.Rules = rls

	r.logger.Debug("f5router-policy-create", zap.Object("policy", plcy))
	return &plcy
}

func (r *F5Router) processRouteAdd(ru routeUpdate) bool {
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

	return true
}

func (r *F5Router) processRouteRemove(ru routeUpdate) bool {
	var ret bool
	pool := ru.R.LookupWithoutWildcard(ru.URI)
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
	}
	return ret
}

// RouteUpdate send update information to processor
func (r *F5Router) RouteUpdate(
	op registry.Operation,
	reg registry.Registry,
	uri route.Uri,
) {
	if 0 == len(uri) {
		r.logger.Warn("f5router-skipping-update",
			zap.String("reason", "empty uri"),
			zap.String("operation", op.String()),
		)
		return
	}

	r.logger.Debug("f5router-updating-pool",
		zap.String("operation", op.String()),
		zap.String("uri", uri.String()),
	)

	ru := routeUpdate{
		Name: makePoolName(uri.String()),
		URI:  uri,
		R:    reg,
		Op:   op,
	}
	r.queue.Add(ru)
}
