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

	"github.com/F5Networks/cf-bigip-ctlr/bigipclient"
	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/bigipResources"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/routeUpdate"
	"github.com/F5Networks/cf-bigip-ctlr/logger"
	"github.com/F5Networks/cf-bigip-ctlr/servicebroker/planResources"
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
	// InternalDataGroupName on BIG-IP
	InternalDataGroupName = "cf-ctlr-data-group"
)

// concurrent safe map of service broker plans
type mutexPlansMap struct {
	lock  sync.Mutex
	plans map[string]planResources.Plan
}

// tier2VSInfo holds info related to the tier2 vips
type tier2VSInfo struct {
	// map of ip/ports that are currently in use
	usedPorts map[string]*bigipResources.VirtualAddress
	// ip/ports that were assigned but now ununsed, used before new ones are assigned
	reapedPorts []*bigipResources.VirtualAddress
	// port that will be assigned next for tier2 dest port
	holderPort int32
	// tier2 dest ip currently being assigned to the tier2 vips
	holderIP net.IP
	// tier2 net the holderIP belongs to
	ipNet *net.IPNet
}

// Router interface for the F5Router
//go:generate counterfeiter -o fakes/fake_router.go . Router
type Router interface {
	AddPlans(plans map[string]planResources.Plan)
	VerifyPlanExists(planID string) error
	UpdateRoute(ru routeUpdate.RouteUpdate)
}

// F5Router controller of BigIP configuration objects
type F5Router struct {
	c                    *config.Config
	logger               logger.Logger
	r                    bigipResources.RuleMap
	wildcards            bigipResources.RuleMap
	queue                workqueue.RateLimitingInterface
	writer               Writer
	routeVSHTTP          *bigipResources.Virtual
	routeVSHTTPS         *bigipResources.Virtual
	virtualResources     map[string]*bigipResources.Virtual
	poolResources        map[string]*bigipResources.Pool
	ruleResources        map[string]*bigipResources.IRule
	monitorResources     map[string][]*bigipResources.Monitor
	internalDataGroup    map[string]*bigipResources.InternalDataGroupRecord
	tier2VSInfo          tier2VSInfo
	firstSyncDone        bool
	unmappedResourcesMap map[string]bigipResources.Resources
	plansMap             mutexPlansMap
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

func generateProfileList(names []string, context string) ([]*bigipResources.ProfileRef, error) {
	var refs []*bigipResources.ProfileRef
	nameRefs, err := generateNameList(names)
	for _, ref := range nameRefs {
		refs = append(refs, &bigipResources.ProfileRef{
			Name:      ref.Name,
			Partition: ref.Partition,
			Context:   context,
		})
	}
	return refs, err
}

func generateNameList(names []string) ([]*bigipResources.NameRef, error) {
	var refs []*bigipResources.NameRef
	var skipped []string
	for i := range names {
		p := strings.TrimPrefix(names[i], "/")
		parts := strings.Split(p, "/")
		if 2 == len(parts) {
			refs = append(refs, &bigipResources.NameRef{
				Name:      parts[1],
				Partition: parts[0],
			})
		} else {
			skipped = append(skipped, p)
		}
	}
	if len(skipped) != 0 {
		return refs, fmt.Errorf("skipped names: %v need format /[partition]/[name]", skipped)
	}

	return refs, nil
}

// NewF5Router create the F5Router route controller
func NewF5Router(
	logger logger.Logger,
	c *config.Config,
	writer Writer,
) (*F5Router, error) {
	r := F5Router{
		c:                    c,
		logger:               logger,
		r:                    make(bigipResources.RuleMap),
		wildcards:            make(bigipResources.RuleMap),
		queue:                workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		writer:               writer,
		virtualResources:     make(map[string]*bigipResources.Virtual),
		poolResources:        make(map[string]*bigipResources.Pool),
		ruleResources:        make(map[string]*bigipResources.IRule),
		monitorResources:     make(map[string][]*bigipResources.Monitor),
		unmappedResourcesMap: make(map[string]bigipResources.Resources),
		plansMap:             mutexPlansMap{plans: make(map[string]planResources.Plan)},
		tier2VSInfo:          tier2VSInfo{usedPorts: make(map[string]*bigipResources.VirtualAddress), holderPort: 10000},
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
		err = r.createHTTPVirtuals()
		if nil != err {
			return nil, err
		}
		r.initiRule(bigipResources.HTTPForwardingiRuleName, bigipResources.ForwardToVIPiRule)
	}

	return &r, nil
}

// AddPlans adds service broker provided plans to the router
func (r *F5Router) AddPlans(plans map[string]planResources.Plan) {
	r.plansMap.lock.Lock()
	r.plansMap.plans = plans
	r.plansMap.lock.Unlock()
}

// VerifyPlanExists verifies that the plan exists for the router
func (r *F5Router) VerifyPlanExists(planID string) error {
	if planID == "" {
		return errors.New("planID not included")
	}

	r.plansMap.lock.Lock()
	defer r.plansMap.lock.Unlock()
	if _, ok := r.plansMap.plans[planID]; !ok {
		return errors.New("plan for planID: " + planID + " not found")
	}

	return nil
}

// Run start the F5Router controller
func (r *F5Router) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	r.logger.Info("f5router-starting")

	// See if there is an existing data group on the BIG-IP, this is used to store
	// our tier2 vip ip:port information so the controller doesn't end up in a bad
	// state on restart
	dg, err := fetchExistingDataGroup(r.c)
	if nil != err {
		r.logger.Warn(
			"failed-to-fetch-existing-data-group",
			zap.Error(err),
		)
		// We didn't get back a valid data group so it needs to be set to an empty one
		r.internalDataGroup = make(map[string]*bigipResources.InternalDataGroupRecord)
	} else {
		// Got a valid data group back so use it
		r.processCachedDataGroup(dg)
	}

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
		0 == len(r.c.BigIP.ExternalAddr) ||
		0 == len(r.c.BigIP.Tier2IPRange) {
		return fmt.Errorf(
			"required parameter missing; URL, User, Pass, Partitions, ExternalAddr, "+
				"Tier2IPRange must have value: %+v", r.c.BigIP)
	}

	// Verify the ExternalAddr provided is a valid IP address
	va := &bigipResources.VirtualAddress{
		BindAddr: r.c.BigIP.ExternalAddr,
		Port:     int32(80),
	}
	_, err := verifyDestAddress(va, r.c.BigIP.Partitions[0])
	if nil != err {
		return err
	}

	ipAddr, ipNet, err := net.ParseCIDR(r.c.BigIP.Tier2IPRange)
	if nil != err {
		return err
	}
	if ipAddr.IsLoopback() {
		return fmt.Errorf("Loopback not allowed: %s", ipAddr)
	}

	r.tier2VSInfo.holderIP = ipAddr
	r.tier2VSInfo.ipNet = ipNet

	if 0 == len(r.c.BigIP.HealthMonitors) {
		r.c.BigIP.HealthMonitors = []string{"/Common/tcp_half_open"}
	}

	if 0 == len(r.c.BigIP.Profiles) {
		r.c.BigIP.Profiles = []string{"/Common/http", "/Common/tcp"}
	} else {
		exist := checkForString(r.c.BigIP.Profiles, "/Common/tcp")
		if !exist {
			r.c.BigIP.Profiles = append(r.c.BigIP.Profiles, "/Common/tcp")
		}
	}

	return nil
}

func (r *F5Router) initiRule(name string, code string) {
	iRule := bigipResources.IRule{
		Name: name,
		Code: code,
	}
	r.ruleResources[name] = &iRule
}

func (r *F5Router) createHTTPVirtuals() error {
	plcs, err := generateNameList(r.c.BigIP.Policies)
	if err != nil {
		r.logger.Warn("f5router-skipping-policy-names", zap.Error(err))
	}
	plcs = append(plcs, &bigipResources.NameRef{
		Name:      CFRoutingPolicyName,
		Partition: r.c.BigIP.Partitions[0], // FIXME handle multiple partitions
	})
	prfls, err := generateProfileList(r.c.BigIP.Profiles, "all")
	if err != nil {
		r.logger.Warn("f5router-skipping-profile-names", zap.Error(err))
	}
	iRulePath, err := joinBigipPath(r.c.BigIP.Partitions[0], bigipResources.HTTPForwardingiRuleName)
	if nil != err {
		return err
	}
	iRule := []string{iRulePath}

	if r.c.SessionPersistence {
		r.initiRule(bigipResources.JsessionidIRuleName, bigipResources.JsessionidIRule)
	}

	srcAddrTrans := bigipResources.SourceAddrTranslation{Type: "automap"}

	va := &bigipResources.VirtualAddress{
		BindAddr: r.c.BigIP.ExternalAddr,
		Port:     80,
	}
	dest, err := verifyDestAddress(va, r.c.BigIP.Partitions[0])
	if nil != err {
		return err
	}

	r.virtualResources[HTTPRouterName] = &bigipResources.Virtual{
		VirtualServerName:     HTTPRouterName,
		Mode:                  "tcp",
		Enabled:               true,
		Destination:           dest,
		Policies:              plcs,
		Profiles:              prfls,
		IRules:                iRule,
		SourceAddrTranslation: srcAddrTrans,
	}

	if 0 != len(r.c.BigIP.SSLProfiles) {
		sslProfiles, err := generateProfileList(r.c.BigIP.SSLProfiles, "clientside")
		if err != nil {
			r.logger.Warn("f5router-skipping-sslProfile-names", zap.Error(err))
		}
		prfls = append(prfls, sslProfiles...)

		va := &bigipResources.VirtualAddress{
			BindAddr: r.c.BigIP.ExternalAddr,
			Port:     443,
		}
		dest, err := verifyDestAddress(va, r.c.BigIP.Partitions[0])
		if nil != err {
			return err
		}

		r.virtualResources[HTTPSRouterName] = &bigipResources.Virtual{
			VirtualServerName:     HTTPSRouterName,
			Mode:                  "tcp",
			Enabled:               true,
			Destination:           dest,
			Policies:              plcs,
			Profiles:              prfls,
			IRules:                iRule,
			SourceAddrTranslation: srcAddrTrans,
		}
	}
	return nil
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

func (r *F5Router) createiRules(pm bigipResources.PartitionMap, partition string, wg *sync.WaitGroup) {
	defer wg.Done()

	for _, rule := range r.ruleResources {
		pm[partition].IRules = append(pm[partition].IRules, rule)
	}
}

func (r *F5Router) createMonitors(pm bigipResources.PartitionMap, partition string, wg *sync.WaitGroup) {
	defer wg.Done()

	for _, monitors := range r.monitorResources {
		for _, monitor := range monitors {
			var found bool
			for _, addedMonitor := range pm[partition].Monitors {
				if addedMonitor.Name == monitor.Name {
					found = true
					break
				}
			}
			if !found {
				pm[partition].Monitors = append(pm[partition].Monitors, monitor)
			}
		}
	}
}

func (r *F5Router) createInternalDataGroup(pm bigipResources.PartitionMap, partition string, wg *sync.WaitGroup) {
	defer wg.Done()
	dataGroup := bigipResources.NewInternalDataGroup(InternalDataGroupName)
	for _, record := range r.internalDataGroup {
		dataGroup.Records = append(dataGroup.Records, record)
	}
	pm[partition].InternalDataGroups = append(pm[partition].InternalDataGroups, dataGroup)
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

	wg.Add(1)
	go r.createiRules(pm, partition, &wg)

	wg.Add(1)
	go r.createMonitors(pm, partition, &wg)

	wg.Add(1)
	go r.createInternalDataGroup(pm, partition, &wg)

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
		} else if ru.Op() == routeUpdate.Bind {
			r.processRouteBind(ru)
		} else if ru.Op() == routeUpdate.Unbind {
			r.processRouteUnbind(ru)
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
			if !r.firstSyncDone {
				r.truncateInternalDataGroup()
				r.firstSyncDone = true
			}
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
	name string,
	descrip string,
	members []bigipResources.Member,
	balanceMode string,
	hmNames []string,
) *bigipResources.Pool {

	return &bigipResources.Pool{
		Name:         name,
		Balance:      balanceMode,
		Members:      members,
		MonitorNames: hmNames,
		Description:  descrip,
	}
}

func verifyDestAddress(va *bigipResources.VirtualAddress, partition string) (string, error) {
	ip, rd := splitIPWithRouteDomain(va.BindAddr)
	if len(rd) > 0 {
		rd = "%" + rd
	}
	addr := net.ParseIP(va.BindAddr)
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
			va.Port)
		return destination, nil
	}
	return "", fmt.Errorf("invalid address: %s", va.BindAddr)
}

// checkForString loops over a slice to see if a string exists in it
func checkForString(list []string, text string) bool {
	for _, val := range list {
		if val == text {
			return true
		}
	}
	return false
}

func joinBigipPath(partition, objName string) (string, error) {
	if objName == "" {
		return "", errors.New("object name is blank")
	}
	if partition == "" {
		return objName, nil
	}
	return fmt.Sprintf("/%s/%s", partition, objName), nil
}

func fetchExistingDataGroup(c *config.Config) (*bigipResources.InternalDataGroup, error) {
	url := fmt.Sprintf(
		"%s/mgmt/tm/ltm/data-group/internal/~%s~%s",
		c.BigIP.URL,
		c.BigIP.Partitions[0],
		InternalDataGroupName,
	)
	data, err := bigipclient.Get(url, c.BigIP.User, c.BigIP.Pass)
	if nil != err {
		return nil, err
	}
	if strings.Contains(string(data), "was not found") {
		return nil, fmt.Errorf("data group %s not found on the BIG-IP", InternalDataGroupName)
	}

	var idg *bigipResources.InternalDataGroup
	err = json.Unmarshal(data, &idg)
	if nil != err {
		return nil, err
	}

	return idg, nil
}

// assignVSPort creates the holder BindAddr and Port the vs will use, these
// virtuals are being routed to through an iRule using the virtual command
func (r *F5Router) assignVSPort(vs *bigipResources.Virtual) error {
	var va *bigipResources.VirtualAddress
	var err error
	key := vs.VirtualServerName

	existingVS, exist := r.virtualResources[key]
	// vs doesn't exist yet so we need to create the ip:port to use as the dest
	if !exist {
		// check if the info exists in the internalDataGroup
		dgr, exist := r.internalDataGroup[key]
		// the mapping doesn't exist yet so assign it
		if !exist {
			va, err = r.getAvailableVirtualAddress()
			if nil != err {
				return err
			}

			data, errEncode := va.Encode()
			if nil != errEncode {
				return errEncode
			}
			r.internalDataGroup[key] = &bigipResources.InternalDataGroupRecord{
				Name: key,
				Data: data,
			}

		} else {
			va, err = dgr.ReturnTier2VirtualAddress()
			if nil != err {
				return err
			}
		}

		dest, err := verifyDestAddress(va, r.c.BigIP.Partitions[0])
		if err != nil {
			return err
		}
		vs.Destination = dest

		return nil
	}
	// if the vs does exist set the destination on our route update to the current dest
	vs.Destination = existingVS.Destination
	return nil
}

func (r *F5Router) getAvailableVirtualAddress() (*bigipResources.VirtualAddress, error) {
	var va *bigipResources.VirtualAddress
	// Use a reaped va if we have one
	if len(r.tier2VSInfo.reapedPorts) != 0 {
		va = r.tier2VSInfo.reapedPorts[0]
		// delete from a slice https://github.com/golang/go/wiki/SliceTricks
		r.tier2VSInfo.reapedPorts[0] = r.tier2VSInfo.reapedPorts[len(r.tier2VSInfo.reapedPorts)-1]
		r.tier2VSInfo.reapedPorts[len(r.tier2VSInfo.reapedPorts)-1] = nil
		r.tier2VSInfo.reapedPorts = r.tier2VSInfo.reapedPorts[:len(r.tier2VSInfo.reapedPorts)-1]
		return va, nil
	}
	// No reaped va so check for the next available one
	va = &bigipResources.VirtualAddress{
		BindAddr: r.tier2VSInfo.holderIP.String(),
		Port:     r.tier2VSInfo.holderPort,
	}
	_, found := r.tier2VSInfo.usedPorts[va.String()]
	for found {
		// check if we are at the last address valid for a va
		if r.tier2VSInfo.holderPort == 65535 {
			// increement the holderip to the next address
			err := r.incrementHolderIP()
			if nil != err {
				return nil, err
			}
			// reset our ports back to our starting point
			r.tier2VSInfo.holderPort = 10000
		} else {
			r.tier2VSInfo.holderPort = r.tier2VSInfo.holderPort + 1
			va = &bigipResources.VirtualAddress{
				BindAddr: r.tier2VSInfo.holderIP.String(),
				Port:     r.tier2VSInfo.holderPort,
			}
			_, found = r.tier2VSInfo.usedPorts[va.String()]
		}
	}
	r.tier2VSInfo.usedPorts[va.String()] = va
	return va, nil
}

func (r *F5Router) incrementHolderIP() error {
	ipCopy := make(net.IP, len(r.tier2VSInfo.holderIP))
	copy(ipCopy, r.tier2VSInfo.holderIP)
	// https://groups.google.com/d/msg/golang-nuts/zlcYA4qk-94/TWRFHeXJCcYJ
	for i := len(ipCopy) - 1; i >= 0; i-- {
		ipCopy[i]++
		if ipCopy[i] > 0 {
			break
		}
	}
	// verify the ip is in the range provided
	contains := r.tier2VSInfo.ipNet.Contains(ipCopy)
	if !contains {
		return errors.New("Ran out of available IP addresses. Restart the controller with a larger address space")
	}
	r.tier2VSInfo.holderIP = ipCopy
	return nil
}

// processCachedDataGroup uses the cached data group off the BIG-IP to populate
// our map of used ports and populate the internalDataGroup
func (r *F5Router) processCachedDataGroup(dg *bigipResources.InternalDataGroup) {
	newDataGroup := make(map[string]*bigipResources.InternalDataGroupRecord)
	for _, record := range dg.Records {
		// Decode the record from the bigip to a virtual address
		va, err := record.ReturnTier2VirtualAddress()
		if nil != err {
			r.logger.Warn("process-cached-data-group-error", zap.Error(err), zap.Object("record", record))
			continue
		}

		vaIP := net.ParseIP(va.BindAddr)
		// Only keep the record and va if the IP and port is in the accepted range
		if (nil != vaIP) && (r.tier2VSInfo.ipNet.Contains(vaIP) && (va.Port <= 65535)) {
			_, ok := r.tier2VSInfo.usedPorts[va.String()]
			if !ok {
				r.tier2VSInfo.usedPorts[va.String()] = va
				newDataGroup[record.Name] = record
			}
		}
	}
	r.logger.Debug("add-ports-to-used-ports", zap.Object("used-ports", r.tier2VSInfo.usedPorts))
	r.internalDataGroup = newDataGroup
}

// truncateInternalDataGroup cleans up after our first drain of the queue
// Create a new data group and list of used ports then compare the old list
// of used ports to the new, anything not in the new list is added to
// reapedPorts for reuse
func (r *F5Router) truncateInternalDataGroup() {
	newDataGroup := make(map[string]*bigipResources.InternalDataGroupRecord)
	newUsedPorts := make(map[string]*bigipResources.VirtualAddress)

	for vsName := range r.virtualResources {
		record, exists := r.internalDataGroup[vsName]
		if exists {
			va, err := record.ReturnTier2VirtualAddress()
			if nil != err {
				r.logger.Warn("truncate-internal-data-group-error", zap.Error(err), zap.Object("record", record))
				continue
			}
			newDataGroup[vsName] = record
			newUsedPorts[va.String()] = va
		}
	}
	r.logger.Debug(
		"truncate-internal-data-group",
		zap.Object("old-used-ports", r.tier2VSInfo.usedPorts),
		zap.Object("new-used-ports", newUsedPorts),
	)
	for key, value := range r.tier2VSInfo.usedPorts {
		if _, ok := newUsedPorts[key]; !ok {
			r.tier2VSInfo.reapedPorts = append(r.tier2VSInfo.reapedPorts, value)
		}
	}
	r.internalDataGroup = newDataGroup
	r.tier2VSInfo.usedPorts = newUsedPorts

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
		Name:        "0",
		Request:     true,
		Expression:  ru.Name(),
		TmName:      "target_vip",
		Tcl:         true,
		SetVariable: true,
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
		r.logger.Error("f5router-URI-error", zap.Error(err))
		return
	}

	// Create default resources and update them if resource updates exist for this route
	rs, err := ru.CreateResources(r.c)
	if nil != err {
		r.logger.Error("process-HTTP-route-add-error-create-resources", zap.Error(err))
		return
	}
	if resources, ok := r.unmappedResourcesMap[ru.Name()]; ok {
		rs = ru.UpdateResources(rs, resources)
	}

	err = r.assignVSPort(rs.Virtuals[0])
	if nil != err {
		r.logger.Error(
			"process-HTTP-route-add-error-assign-vs-port",
			zap.String("Route", ru.Route()),
			zap.Error(err))
		return
	}

	if len(rs.Monitors) != 0 {
		r.addMonitors(rs.Pools[0].Name, rs.Monitors)
	}
	r.addPool(rs.Pools[0])
	r.addVirtual(rs.Virtuals[0])
	r.addRule(ru)
}

func (r *F5Router) processRouteBind(ru updateHTTP) {
	name := ru.Name()
	planID := ru.PlanID()
	existingPool := r.poolResources[name]
	existingVirtual := r.virtualResources[name]
	r.plansMap.lock.Lock()
	defer r.plansMap.lock.Unlock()

	// There is a mapped route (with an endpoint) that will be updated
	if (existingPool != nil) && (existingVirtual != nil) {
		var rs bigipResources.Resources

		// Get copy of members before they are removed
		members := make([]bigipResources.Member, len(existingPool.Members))
		copy(members, existingPool.Members)
		r.removePool(existingPool)
		r.removeVirtual(name)

		// Bind updates to this mapped route
		rs = bigipResources.Resources{
			Virtuals: []*bigipResources.Virtual{existingVirtual},
			Pools:    []*bigipResources.Pool{existingPool},
		}

		if plan, ok := r.plansMap.plans[planID]; ok {
			if len(plan.Pool.HealthMonitors) != 0 {
				r.removeMonitors(existingPool.Name)
			}
			planResources := ru.CreatePlanResources(r.c, plan)
			rs = ru.UpdateResources(rs, planResources)
		} else {
			r.logger.Warn("process-HTTP-route-update-bind-error",
				zap.String("Update-Mapped-Route-Error",
					fmt.Sprintf("could not find plan %s for route %s keeping defaults", planID, ru.Route())))
		}

		// Members are not updated and should be added back
		rs.Pools[0].Members = members
		if len(rs.Monitors) != 0 {
			r.addMonitors(rs.Pools[0].Name, rs.Monitors)
		}
		r.addPool(rs.Pools[0])
		r.addVirtual(rs.Virtuals[0])
	} else {
		// Bind updates to this unmapped route
		if plan, ok := r.plansMap.plans[planID]; ok {
			planResources := ru.CreatePlanResources(r.c, plan)
			r.unmappedResourcesMap[name] = planResources
		} else {
			r.logger.Warn("process-HTTP-route-update-bind-error",
				zap.String("Update-Unmapped-Route-Error",
					fmt.Sprintf("could not find plan %s for unmapped route noop", planID)))
		}
	}
}

func (r *F5Router) processRouteUnbind(ru updateHTTP) {
	name := ru.Name()
	existingPool := r.poolResources[name]
	existingVirtual := r.virtualResources[name]

	// There is a mapped route (with an endpoint) that will be updated
	if (existingPool != nil) && (existingVirtual != nil) {
		var rs bigipResources.Resources
		var err error

		// Get copy of members before they are removed
		members := make([]bigipResources.Member, len(existingPool.Members))
		copy(members, existingPool.Members)
		r.removePool(existingPool)
		r.removeVirtual(name)

		// Unbind updates to this mapped route
		r.removeMonitors(existingPool.Name)
		rs, err = ru.CreateBrokerDefaultResources(
			r.c,
			existingPool.Description,
			existingVirtual.Destination,
			members[0].Address,
			members[0].Port,
		)
		if nil != err {
			r.logger.Error("process-HTTP-route-update-unbind-error", zap.Error(err))
			return
		}

		// Members are not updated and should be added back
		rs.Pools[0].Members = members
		r.addPool(rs.Pools[0])
		r.addVirtual(rs.Virtuals[0])
	} else {
		// Unbind updates to this unmapped route
		delete(r.unmappedResourcesMap, name)
	}
}

func (r *F5Router) processRouteRemove(ru updateHTTP) {
	r.logger.Debug("process-HTTP-route-remove", zap.String("name", ru.Name()), zap.String("route", ru.Route()))

	err := verifyRouteURI(ru)
	if nil != err {
		r.logger.Error("f5router-URI-error", zap.Error(err))
		return
	}

	rs, err := ru.CreateResources(r.c)
	if nil != err {
		r.logger.Error("process-HTTP-route-remove-error", zap.Error(err))
		return
	}
	poolRemoved := r.removePool(rs.Pools[0])
	if poolRemoved {
		// delete the health monitors associated with this pool
		r.removeMonitors(rs.Pools[0].Name)
		// delete the rule for the vip
		r.removeRule(ru)
		// delete the tier2 vip
		vsName := rs.Virtuals[0].VirtualServerName
		r.removeVirtual(vsName)
		// delete the mapping of the vs name to the destination
		delete(r.tier2VSInfo.usedPorts, vsName)
		// the tier2 vip is deleted, remove the internal data group entry for it
		record, exist := r.internalDataGroup[vsName]
		if exist {
			va, err := record.ReturnTier2VirtualAddress()
			if nil != err {
				r.logger.Warn("process-HTTP-route-remove-error", zap.Object("record", record), zap.Error(err))
			} else {
				// Add the virtual address to reaped ports for reuse
				r.tier2VSInfo.reapedPorts = append(r.tier2VSInfo.reapedPorts, va)
			}
			delete(r.internalDataGroup, vsName)
		}
	}
}

func (r *F5Router) processTCPRouteAdd(ru updateTCP) {
	r.logger.Debug("process-TCP-route-add", zap.String("name", ru.Name()), zap.String("route", ru.Route()))

	rs, err := ru.CreateResources(r.c)
	if nil != err {
		r.logger.Error("process-TCP-route-add-error", zap.Error(err))
		return
	}
	r.addPool(rs.Pools[0])
	r.addVirtual(rs.Virtuals[0])
}

func (r *F5Router) processTCPRouteRemove(ru updateTCP) {
	r.logger.Debug("process-TCP-route-remove", zap.String("name", ru.Name()), zap.String("route", ru.Route()))

	rs, err := ru.CreateResources(r.c)
	if nil != err {
		r.logger.Error("process-TCP-route-remove-error", zap.Error(err))
		return
	}
	poolRemoved := r.removePool(rs.Pools[0])
	if poolRemoved {
		r.removeVirtual(rs.Virtuals[0].VirtualServerName)
	}
}

func (r *F5Router) addMonitors(poolName string, monitors []*bigipResources.Monitor) {
	r.monitorResources[poolName] = monitors
}

func (r *F5Router) removeMonitors(poolName string) {
	delete(r.monitorResources, poolName)
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
				p.Members[len(p.Members)-1] = bigipResources.Member{Address: "", Port: 0, Session: ""}
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
	// WARNING: This only accepts hashable types!
	r.queue.Add(ru)
}
