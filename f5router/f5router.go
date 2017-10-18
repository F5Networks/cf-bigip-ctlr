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
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/bigipResources"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/routeUpdate"
	"github.com/F5Networks/cf-bigip-ctlr/logger"

	"github.com/uber-go/zap"
	"k8s.io/client-go/util/workqueue"
)

const (
	// HTTPRouterName HTTP virtual server name
	HTTPRouterName = "routing-vip-http"
	// HTTPSRouterName HTTPS virtual server name
	HTTPSRouterName = "routing-vip-https"
	// CFRoutingPolicyName Policy name for CF routing
	CFRoutingPolicyName = "cf-routing-policy"
)

// F5Router controller of BigIP configuration objects
type F5Router struct {
	c                *config.Config
	logger           logger.Logger
	r                bigipResources.RuleMap
	wildcards        bigipResources.RuleMap
	queue            workqueue.RateLimitingInterface
	writer           Writer
	routeVSHTTP      *bigipResources.Virtual
	routeVSHTTPS     *bigipResources.Virtual
	virtualResources map[string]*bigipResources.Virtual
	poolResources    map[string]*bigipResources.Pool
}

func verifyRouteURI(ru updateHTTP) error {
	uri := ru.URI().String()
	if strings.Count(uri, "*") > 1 {
		return fmt.Errorf("Invalid URI: %s multiple wildcards are not supported", uri)
	}
	return nil
}

func makeObjectName(uri string) string {
	var name string
	if strings.HasPrefix(uri, "*.") {
		name = "cf-" + strings.TrimPrefix(uri, "*.")
	} else if strings.Contains(uri, "*") {
		name = "cf-" + strings.Replace(uri, "*", "_", -1)
	} else {
		sum := sha256.Sum256([]byte(uri))
		index := strings.Index(uri, ".")

		name = fmt.Sprintf("cf-%s-%x", uri[:index], sum[:8])
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

// Make the description that gets applied to the pool and rule to translate
// from the hashed name to the associated uri and app GUID in CF
func makeDescription(uri string, appID string) string {
	s := "route: " + uri
	if appID == "" {
		return s
	}
	s += " - App GUID: " + appID
	return s
}

// NewF5Router create the F5Router route controller
func NewF5Router(
	logger logger.Logger,
	c *config.Config,
	writer Writer,
) (*F5Router, error) {
	r := F5Router{
		c:                c,
		logger:           logger,
		r:                make(bigipResources.RuleMap),
		wildcards:        make(bigipResources.RuleMap),
		queue:            workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		writer:           writer,
		virtualResources: make(map[string]*bigipResources.Virtual),
		poolResources:    make(map[string]*bigipResources.Pool),
	}

	err := r.validateConfig()
	if nil != err {
		return nil, err
	}

	err = r.writeInitialConfig()
	if nil != err {
		return nil, err
	}

	// Create the HTTP virtuals if we are not in TCP only mode
	if c.RoutingMode != config.TCP {
		r.createHTTPVirtuals()
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

func (r *F5Router) createHTTPVirtuals() {
	plcs := r.generatePolicyList()
	prfls := r.generateNameList(r.c.BigIP.Profiles)
	srcAddrTrans := bigipResources.SourceAddrTranslation{Type: "automap"}
	va := &bigipResources.VirtualAddress{
		BindAddr: r.c.BigIP.ExternalAddr,
		Port:     80,
	}
	r.virtualResources[HTTPRouterName] =
		makeVirtual(
			HTTPRouterName,
			"",
			r.c.BigIP.Partitions[0],
			va,
			"tcp",
			plcs,
			prfls,
			srcAddrTrans,
		)

	if 0 != len(r.c.BigIP.SSLProfiles) {
		sslProfiles := r.generateNameList(r.c.BigIP.SSLProfiles)
		prfls = append(prfls, sslProfiles...)
		va := &bigipResources.VirtualAddress{
			BindAddr: r.c.BigIP.ExternalAddr,
			Port:     443,
		}
		r.virtualResources[HTTPSRouterName] =
			makeVirtual(
				HTTPSRouterName,
				"",
				r.c.BigIP.Partitions[0],
				va,
				"tcp",
				plcs,
				prfls,
				srcAddrTrans,
			)
	}
}

func (r *F5Router) writeInitialConfig() error {
	sections := make(map[string]interface{})
	sections["global"] = bigipResources.GlobalConfig{
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

func (r *F5Router) generateNameList(names []string) []*bigipResources.NameRef {
	var refs []*bigipResources.NameRef
	for i := range names {
		p := strings.TrimPrefix(names[i], "/")
		parts := strings.Split(p, "/")
		if 2 == len(parts) {
			refs = append(refs, &bigipResources.NameRef{
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

func (r *F5Router) generatePolicyList() []*bigipResources.NameRef {
	var n []*bigipResources.NameRef
	n = append(n, r.generateNameList(r.c.BigIP.Policies)...)
	n = append(n, &bigipResources.NameRef{
		Name:      CFRoutingPolicyName,
		Partition: r.c.BigIP.Partitions[0], // FIXME handle multiple partitions
	})
	return n
}

func (r *F5Router) createPolicies(pm bigipResources.PartitionMap, partition string, wg *sync.WaitGroup) {
	defer wg.Done()
	if len(r.wildcards) != 0 || len(r.r) != 0 {
		pm[partition].Policies = bigipResources.Policies{
			r.makeRoutePolicy(CFRoutingPolicyName),
		}
	}
}

func (r *F5Router) createVirtuals(pm bigipResources.PartitionMap, partition string, wg *sync.WaitGroup) {
	defer wg.Done()

	for _, virtual := range r.virtualResources {
		pm[partition].Virtuals = append(pm[partition].Virtuals, virtual)
	}
}

func (r *F5Router) createPools(pm bigipResources.PartitionMap, partition string, wg *sync.WaitGroup) {
	defer wg.Done()

	for _, pool := range r.poolResources {
		pm[partition].Pools = append(pm[partition].Pools, pool)
	}
}

// Create a partition entry in the map if it doesn't exist
func initPartitionData(pm bigipResources.PartitionMap, partition string) {
	if _, ok := pm[partition]; !ok {
		pm[partition] = &bigipResources.Resources{}
	}
}

func (r *F5Router) createResources() bigipResources.PartitionMap {
	// Organize the data as a map of arrays of resources (per partition)
	pm := bigipResources.PartitionMap{}

	//FIXME need to handle multiple partitions
	partition := r.c.BigIP.Partitions[0]
	initPartitionData(pm, partition)

	var wg sync.WaitGroup

	wg.Add(1)
	go r.createPolicies(pm, partition, &wg)

	wg.Add(1)
	go r.createVirtuals(pm, partition, &wg)

	wg.Add(1)
	go r.createPools(pm, partition, &wg)

	wg.Wait()

	return pm
}

func (r *F5Router) process() bool {
	item, quit := r.queue.Get()
	if quit {
		r.logger.Debug("f5router-quit-signal-received")
		return false
	}

	defer r.queue.Done(item)

	var err error
	r.logger.Debug("f5router-received-update-request")
	switch ru := item.(type) {
	case updateHTTP:
		if ru.Op() == routeUpdate.Add {
			r.processRouteAdd(ru)
		} else if ru.Op() == routeUpdate.Remove {
			r.processRouteRemove(ru)
		}
	case updateTCP:
		if ru.Op() == routeUpdate.Add {
			r.processTCPRouteAdd(ru)
		} else if ru.Op() == routeUpdate.Remove {
			r.processTCPRouteRemove(ru)
		}
	default:
		r.logger.Warn("f5router-unknown-workitem",
			zap.Error(errors.New("workqueue delivered unsupported work type")))
	}

	if nil != err {
		r.logger.Warn("f5router-process-error", zap.Error(err))
	} else {
		l := r.queue.Len()
		if 0 == l {

			sections := make(map[string]interface{})

			sections["global"] = bigipResources.GlobalConfig{
				LogLevel:       r.c.Logging.Level,
				VerifyInterval: r.c.BigIP.VerifyInterval,
			}

			sections["bigip"] = r.c.BigIP

			sections["resources"] = r.createResources()

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
				zap.Int("length", l),
			)
		}
	}
	return true
}

// makePool create Pool-Only configuration item
func makePool(
	c *config.Config,
	name string,
	descrip string,
	members ...bigipResources.Member,
) *bigipResources.Pool {

	prefixHealthMonitors := fixupNames(c.BigIP.HealthMonitors)
	return &bigipResources.Pool{
		Name:         name,
		Balance:      c.BigIP.LoadBalancingMode,
		Members:      members,
		MonitorNames: prefixHealthMonitors,
		Description:  descrip,
	}
}

func makeVirtual(
	name string,
	poolName string,
	partition string,
	virtualAddress *bigipResources.VirtualAddress,
	mode string,
	policies []*bigipResources.NameRef,
	profiles []*bigipResources.NameRef,
	srcAddrTrans bigipResources.SourceAddrTranslation,
) *bigipResources.Virtual {
	// Validate the IP address, and create the destination
	ip, rd := split_ip_with_route_domain(virtualAddress.BindAddr)
	if len(rd) > 0 {
		rd = "%" + rd
	}
	addr := net.ParseIP(ip)
	if nil != addr {
		var format string
		if nil != addr.To4() {
			format = "/%s/%s%s:%d"
		} else {
			format = "/%s/%s%s.%d"
		}
		destination := fmt.Sprintf(
			format,
			partition,
			ip,
			rd,
			virtualAddress.Port)

		vs := &bigipResources.Virtual{
			Enabled:               true,
			VirtualServerName:     name,
			PoolName:              poolName,
			Destination:           destination,
			Mode:                  mode,
			Policies:              policies,
			Profiles:              profiles,
			SourceAddrTranslation: srcAddrTrans,
		}
		return vs
	} else {
		return nil
	}
}

func (r *F5Router) makeRouteRule(ru updateHTTP) (*bigipResources.Rule, error) {
	_u := "scheme://" + ru.URI().String()
	_u = strings.TrimSuffix(_u, "/")
	u, err := url.Parse(_u)
	if nil != err {
		return nil, err
	}

	var b bytes.Buffer
	b.WriteRune('/')
	b.WriteString(r.c.BigIP.Partitions[0]) //FIXME update to use mutliple partitions
	b.WriteRune('/')
	b.WriteString(ru.Name())

	a := bigipResources.Action{
		Forward: true,
		Name:    "0",
		Pool:    b.String(),
		Request: true,
	}

	uriString := ru.URI().String()

	var c []*bigipResources.Condition
	if strings.Contains(uriString, "*") {
		splits := strings.Split(u.Host, "*")
		numSplits := len(splits)
		ruleIndex := 0
		if strings.HasPrefix(u.Host, splits[0]) {
			if splits[0] != "" {
				c = append(c, &bigipResources.Condition{
					StartsWith: true,
					Host:       true,
					HTTPHost:   true,
					Name:       strconv.Itoa(ruleIndex),
					Index:      ruleIndex,
					Request:    true,
					Values:     []string{splits[0]},
				})
				ruleIndex++
			}
			splits = splits[1:]
			numSplits--
		}
		if strings.HasSuffix(u.Host, splits[numSplits-1]) {
			if splits[numSplits-1] != "" {
				c = append(c, &bigipResources.Condition{
					EndsWith: true,
					Host:     true,
					HTTPHost: true,
					Name:     strconv.Itoa(ruleIndex),
					Index:    ruleIndex,
					Request:  true,
					Values:   []string{splits[numSplits-1]},
				})
			}
		}
	} else {
		c = append(c, &bigipResources.Condition{
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
				c = append(c, &bigipResources.Condition{
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

	rl := bigipResources.Rule{
		FullURI:     uriString,
		Actions:     []*bigipResources.Action{&a},
		Conditions:  c,
		Name:        ru.Name(),
		Description: makeDescription(uriString, ru.AppID()),
	}

	r.logger.Debug("f5router-rule-create", zap.Object("rule", rl))

	return &rl, nil
}

func (r *F5Router) makeRoutePolicy(policyName string) *bigipResources.Policy {
	plcy := bigipResources.Policy{
		Controls: []string{"forwarding"},
		Legacy:   true,
		Name:     policyName,
		Requires: []string{"http"},
		Rules:    []*bigipResources.Rule{},
		Strategy: "/Common/first-match",
	}

	var wg sync.WaitGroup
	wg.Add(2)
	sortRules := func(r bigipResources.RuleMap, rls *bigipResources.Rules, ordinal int) {
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

	rls := bigipResources.Rules{}
	go sortRules(r.r, &rls, 0)

	w := bigipResources.Rules{}
	go sortRules(r.wildcards, &w, len(r.r))

	wg.Wait()

	rls = append(rls, w...)

	plcy.Rules = rls

	r.logger.Debug("f5router-policy-create", zap.Object("policy", plcy))
	return &plcy
}

func (r *F5Router) processRouteAdd(ru updateHTTP) {
	r.logger.Debug("process-HTTP-route-add", zap.String("name", ru.Name()), zap.String("route", ru.Route()))

	err := verifyRouteURI(ru)
	if nil != err {
		r.logger.Warn("f5router-URI-error", zap.Error(err))
		return
	}

	rs := ru.CreateResources(r.c)
	r.addPool(rs.Pools[0])

	r.addRule(ru)
}

func (r *F5Router) processRouteRemove(ru updateHTTP) {
	r.logger.Debug("process-HTTP-route-remove", zap.String("name", ru.Name()), zap.String("route", ru.Route()))

	err := verifyRouteURI(ru)
	if nil != err {
		r.logger.Warn("f5router-URI-error", zap.Error(err))
		return
	}

	rs := ru.CreateResources(r.c)
	poolRemoved := r.removePool(rs.Pools[0])
	if poolRemoved {
		r.removeRule(ru)
	}
}

func (r *F5Router) processTCPRouteAdd(ru updateTCP) {
	r.logger.Debug("process-TCP-route-add", zap.String("name", ru.Name()), zap.String("route", ru.Route()))

	resources := ru.CreateResources(r.c)
	r.addPool(resources.Pools[0])
	r.addVirtual(resources.Virtuals[0])
}

func (r *F5Router) processTCPRouteRemove(ru updateTCP) {
	r.logger.Debug("process-TCP-route-remove", zap.String("name", ru.Name()), zap.String("route", ru.Route()))

	resources := ru.CreateResources(r.c)
	poolRemoved := r.removePool(resources.Pools[0])
	if poolRemoved {
		r.removeVirtual(resources.Virtuals[0].VirtualServerName)
	}
}

func (r *F5Router) addPool(pool *bigipResources.Pool) {
	key := pool.Name

	p, exists := r.poolResources[key]

	if exists {
		for _, addr := range p.Members {
			// Currently only a single update comes through at a time so we always
			// know to look at the first addr
			if addr == pool.Members[0] {
				return
			}
		}
		p.Members = append(p.Members, pool.Members...)
	} else {
		r.poolResources[key] = pool
	}

}

// removePool returns true when the pool is deleted else false
func (r *F5Router) removePool(pool *bigipResources.Pool) bool {
	key := pool.Name

	p, exists := r.poolResources[key]
	if exists {
		for i, addr := range p.Members {
			// Currently only a single update comes through at a time so we always
			// know to look at the first addr
			if addr == pool.Members[0] {
				p.Members[i] = p.Members[len(p.Members)-1]
				p.Members[len(p.Members)-1] = bigipResources.Member{"", 0, ""}
				p.Members = p.Members[:len(p.Members)-1]
			}
		}
		// delete the pool and virtual if there are no members
		if len(p.Members) == 0 {
			delete(r.poolResources, key)
			return true
		}
	}

	return false
}

func (r *F5Router) addVirtual(vs *bigipResources.Virtual) {
	key := vs.VirtualServerName

	_, exist := r.virtualResources[key]
	if !exist {
		r.virtualResources[key] = vs
	}
}

func (r *F5Router) removeVirtual(key string) {
	delete(r.virtualResources, key)
}

func (r *F5Router) addRule(ru updateHTTP) {
	rule, err := r.makeRouteRule(ru)
	if nil != err {
		r.logger.Warn("f5router-rule-error", zap.Error(err))
	}

	if strings.Contains(ru.URI().String(), "*") {
		r.wildcards[ru.URI()] = rule
		r.logger.Debug("f5router-wildcard-rule-updated",
			zap.String("name", ru.Name()),
			zap.String("uri", ru.URI().String()),
		)
	} else {
		r.r[ru.URI()] = rule
		r.logger.Debug("f5router-app-rule-updated",
			zap.String("name", ru.Name()),
			zap.String("uri", ru.URI().String()),
		)
	}
}

func (r *F5Router) removeRule(ru updateHTTP) {
	if strings.Contains(ru.URI().String(), "*") {
		delete(r.wildcards, ru.URI())
		r.logger.Debug("f5router-wildcard-rule-removed",
			zap.String("name", ru.Name()),
			zap.String("uri", ru.URI().String()),
		)
	} else {
		delete(r.r, ru.URI())
		r.logger.Debug("f5router-app-rule-removed",
			zap.String("name", ru.Name()),
			zap.String("uri", ru.URI().String()),
		)
	}
}

// UpdateRoute send update information to processor
func (r *F5Router) UpdateRoute(ru routeUpdate.RouteUpdate) {
	r.logger.Debug("f5router-updating-pool",
		zap.String("operation", ru.Op().String()),
		zap.String("route-type", ru.Protocol()),
		zap.String("route", ru.Route()),
	)
	r.queue.Add(ru)
}
