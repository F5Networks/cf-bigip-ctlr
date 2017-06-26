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
	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/logger"

	"github.com/F5Networks/cf-bigip-ctlr/registry"
	"github.com/F5Networks/cf-bigip-ctlr/route"

	"k8s.io/client-go/util/workqueue"
)

type (
	globalConfig struct {
		LogLevel       string `json:"log-level"`
		VerifyInterval int    `json:"verify-interval"`
	}

	// frontend bindaddr and port
	virtualAddress struct {
		BindAddr string `json:"bindAddr,omitempty"`
		Port     int32  `json:"port,omitempty"`
	}

	// virtual server policy/profile reference
	nameRef struct {
		Name      string `json:"name"`
		Partition string `json:"partition"`
	}

	resources struct {
		Virtuals []*virtual `json:"virtualServers,omitempty"`
		Pools    []*pool    `json:"pools,omitempty"`
		Monitors []*monitor `json:"monitors,omitempty"`
		Policies []*policy  `json:"l7Policies,omitempty"`
	}

	// virtual server frontend
	virtual struct {
		VirtualServerName string `json:"name"`
		PoolName          string `json:"pool"`
		// Mutual parameter, partition
		Partition string `json:"partition"`

		// VirtualServer parameters
		Mode           string          `json:"mode,omitempty"`
		VirtualAddress *virtualAddress `json:"virtualAddress,omitempty"`
		Policies       []*nameRef      `json:"policies,omitempty"`
		Profiles       []*nameRef      `json:"profiles,omitempty"`
	}

	// pool backend
	pool struct {
		Name            string   `json:"name"`
		Partition       string   `json:"partition"`
		ServiceName     string   `json:"serviceName"`
		ServicePort     int32    `json:"servicePort"`
		Balance         string   `json:"balance"`
		PoolMemberAddrs []string `json:"poolMemberAddrs"`
		MonitorNames    []string `json:"monitor"`
	}

	// backend health monitor
	monitor struct {
		Name      string `json:"name"`
		Partition string `json:"partition"`
		Interval  int    `json:"interval,omitempty"`
		Protocol  string `json:"protocol"`
		Send      string `json:"send,omitempty"`
		Timeout   int    `json:"timeout,omitempty"`
	}

	action struct {
		Forward bool   `json:"forward"`
		Name    string `json:"name"`
		Pool    string `json:"pool"`
		Request bool   `json:"request"`
	}

	condition struct {
		Equals      bool     `json:"equals,omitempty"`
		EndsWith    bool     `json:"endsWith,omitempty"`
		Host        bool     `json:"host,omitempty"`
		HTTPHost    bool     `json:"httpHost,omitempty"`
		HTTPURI     bool     `json:"httpUri,omitempty"`
		PathSegment bool     `json:"pathSegment,omitempty"`
		Name        string   `json:"name"`
		Index       int      `json:"index"`
		Request     bool     `json:"request"`
		Values      []string `json:"values"`
	}

	rule struct {
		FullURI    string       `json:"-"`
		Actions    []*action    `json:"actions"`
		Conditions []*condition `json:"conditions"`
		Name       string       `json:"name"`
		Ordinal    int          `json:"ordinal"`
	}

	policy struct {
		Controls    []string `json:"controls"`
		Description string   `json:"description,omitempty"`
		Legacy      bool     `json:"legacy"`
		Name        string   `json:"name"`
		Partition   string   `json:"partition"`
		Requires    []string `json:"requires"`
		Rules       []*rule  `json:"rules"`
		Strategy    string   `json:"strategy"`
	}

	policies []*policy
	rules    []*rule
	routeMap map[route.Uri]*pool
	ruleMap  map[route.Uri]*rule

	// F5Router controller of BigIP configuration objects
	F5Router struct {
		c            *config.Config
		logger       logger.Logger
		r            ruleMap
		wildcards    ruleMap
		queue        workqueue.RateLimitingInterface
		writer       Writer
		routeVSHTTP  *virtual
		routeVSHTTPS *virtual
		drainUpdate  bool
	}

	vsType      int
	routeUpdate struct {
		Name string
		URI  route.Uri
		R    registry.Registry
		Op   registry.Operation
	}
)
