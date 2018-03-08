/*-
 * Copyright (c) 2017,2018, F5 Networks, Inc.
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

package bigipResources

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/F5Networks/cf-bigip-ctlr/route"
)

type (
	// GlobalConfig for logging and checking the bigip
	GlobalConfig struct {
		LogLevel       string `json:"log-level"`
		VerifyInterval int    `json:"verify-interval"`
	}

	// VirtualAddress is frontend bindaddr and port
	VirtualAddress struct {
		BindAddr string `json:"bindAddr,omitempty"`
		Port     int32  `json:"port,omitempty"`
	}

	// NameRef virtual server policy/profile reference
	NameRef struct {
		Name      string `json:"name"`
		Partition string `json:"partition"`
	}

	// ProfileRef references to pre-existing profiles
	ProfileRef struct {
		Name      string `json:"name"`
		Partition string `json:"partition"`
		Context   string `json:"context"` // 'clientside', 'serverside', or 'all'
	}

	// Configs for each BIG-IP partition
	PartitionMap map[string]*Resources

	// Resources is what gets written to and dumped out for the python side
	Resources struct {
		Virtuals           []*Virtual           `json:"virtualServers,omitempty"`
		Pools              []*Pool              `json:"pools,omitempty"`
		Monitors           []*Monitor           `json:"monitors,omitempty"`
		Policies           []*Policy            `json:"l7Policies,omitempty"`
		IRules             []*IRule             `json:"iRules,omitempty"`
		InternalDataGroups []*InternalDataGroup `json:"internalDataGroups,omitempty"`
	}

	// Virtual server frontend
	Virtual struct {
		VirtualServerName     string                `json:"name"`
		PoolName              string                `json:"pool,omitempty"`
		Mode                  string                `json:"ipProtocol,omitempty"`
		Enabled               bool                  `json:"enabled,omitempty"`
		Destination           string                `json:"destination,omitempty"`
		SourceAddress         string                `json:"source,omitempty"`
		Policies              []*NameRef            `json:"policies,omitempty"`
		Profiles              []*ProfileRef         `json:"profiles,omitempty"`
		IRules                []string              `json:"rules,omitempty"`
		SourceAddrTranslation SourceAddrTranslation `json:"sourceAddressTranslation,omitempty"`
	}

	// Pool Member
	Member struct {
		Address string `json:"address"`
		Port    uint16 `json:"port"`
		Session string `json:"session,omitempty"`
	}

	// Pool backend
	Pool struct {
		Name         string   `json:"name"`
		Balance      string   `json:"loadBalancingMode"`
		Members      []Member `json:"members"`
		MonitorNames []string `json:"monitors"`
		Description  string   `json:"description"`
	}

	// backend health monitor
	Monitor struct {
		Name     string `json:"name"`
		Interval int    `json:"interval,omitempty"`
		Type     string `json:"type"`
		Send     string `json:"send,omitempty"`
		Recv     string `json:"recv,omitempty"`
		Timeout  int    `json:"timeout,omitempty"`
	}

	// Action for a rule
	Action struct {
		Forward     bool   `json:"forward,omitempty"`
		Name        string `json:"name"`
		Pool        string `json:"pool,omitempty"`
		Request     bool   `json:"request"`
		Expression  string `json:"expression,omitempty"`
		TmName      string `json:"tmName,omitempty"`
		Tcl         bool   `json:"tcl,omitempty"`
		SetVariable bool   `json:"setVariable,omitempty"`
	}

	// Condition for a rule
	Condition struct {
		Equals      bool     `json:"equals,omitempty"`
		StartsWith  bool     `json:"startsWith,omitempty"`
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

	// Rule builds up a Policy
	Rule struct {
		FullURI     string       `json:"-"`
		Actions     []*Action    `json:"actions"`
		Conditions  []*Condition `json:"conditions"`
		Name        string       `json:"name"`
		Ordinal     int          `json:"ordinal"`
		Description string       `json:"description"`
	}

	// Policy is the final object for the BIG-IP
	Policy struct {
		Controls    []string `json:"controls"`
		Description string   `json:"description,omitempty"`
		Legacy      bool     `json:"legacy"`
		Name        string   `json:"name"`
		Requires    []string `json:"requires"`
		Rules       []*Rule  `json:"rules"`
		Strategy    string   `json:"strategy"`
	}

	// IRule definition
	IRule struct {
		Name string `json:"name"`
		Code string `json:"apiAnonymous"`
	}

	// InternalDataGroup holds our records
	InternalDataGroup struct {
		Name    string                     `json:"name"`
		Records []*InternalDataGroupRecord `json:"records"`
	}

	// InternalDataGroupRecord holds the name and data for a record
	InternalDataGroupRecord struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}

	// SourceAddrTranslation is the Virtual Server Source Address Translation
	SourceAddrTranslation struct {
		Type string `json:"type"`
	}

	Policies []*Policy
	Rules    []*Rule
	RouteMap map[route.Uri]*Pool
	RuleMap  map[route.Uri]*Rule
)

func (r Rules) Len() int           { return len(r) }
func (r Rules) Less(i, j int) bool { return r[i].FullURI < r[j].FullURI }
func (r Rules) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }

func (va VirtualAddress) String() string {
	return fmt.Sprintf("%s:%s", va.BindAddr, strconv.Itoa(int(va.Port)))
}

// Encode returns an encoded string of a VirtualAddress
func (va VirtualAddress) Encode() (string, error) {
	js, err := json.Marshal(va)
	if nil != err {
		return "", err
	}
	str := base64.StdEncoding.EncodeToString(js)
	return str, nil
}
