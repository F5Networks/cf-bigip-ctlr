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
	"github.com/cf-bigip-ctlr/config"
	"github.com/cf-bigip-ctlr/logger"

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

	// virtual server policy/profile reference
	nameRef struct {
		Name      string `json:"name"`
		Partition string `json:"partition"`
	}

	// virtual server backend backend
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
		SSLProfile     *sslProfile     `json:"sslProfile,omitempty"`
		Policies       []*nameRef      `json:"policies,omitempty"`
		Profiles       []*nameRef      `json:"profiles,omitempty"`
	}

	routeItem struct {
		Backend  backend  `json:"backend"`
		Frontend frontend `json:"frontend"`
		Policies policies `json:"policies,omitempty"`
	}

	// RouteConfig main virtual server configuration
	routeConfig struct {
		Item routeItem `json:"virtualServer"`
	}

	routeMap     map[string]*routeConfig
	ruleMap      map[string]*rule
	routeConfigs []*routeConfig

	// F5Router controller of BigIP configuration objects
	F5Router struct {
		c           *config.Config
		logger      logger.Logger
		m           routeMap
		r           ruleMap
		wildcards   ruleMap
		queue       workqueue.RateLimitingInterface
		writer      *ConfigWriter
		drainUpdate bool
	}

	operation int
	vsType    int
	poolData  struct {
		Name     string
		URI      string
		Endpoint string
		Wildcard bool
	}
	virtualData struct {
		Name string
		T    vsType
	}

	workItem struct {
		op   operation
		data interface{}
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
)
