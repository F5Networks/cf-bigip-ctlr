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

package f5router

import (
	"errors"
	"fmt"

	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/bigipResources"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/routeUpdate"
	"github.com/F5Networks/cf-bigip-ctlr/logger"
	"github.com/F5Networks/cf-bigip-ctlr/route"
	"github.com/F5Networks/cf-bigip-ctlr/servicebroker/planResources"

	"github.com/uber-go/zap"
)

type updateHTTP struct {
	logger   logger.Logger
	op       routeUpdate.Operation
	uri      route.Uri
	endpoint *route.Endpoint
	name     string
	protocol string
	planID   string
}

func createResources(
	hu updateHTTP,
	c *config.Config,
	description string,
	destination string,
	address string,
	port uint16,
) (bigipResources.Resources, error) {
	//  This will create the pool for the http update
	var iRule []string
	rs := bigipResources.Resources{}

	profile := []*bigipResources.ProfileRef{
		&bigipResources.ProfileRef{
			Name:      "http",
			Partition: "Common",
			Context:   "all",
		}, &bigipResources.ProfileRef{
			Name:      "tcp",
			Partition: "Common",
			Context:   "all",
		}}

	if c.SessionPersistence {
		jsessionPath, err := joinBigipPath(c.BigIP.Partitions[0], bigipResources.JsessionidIRuleName)
		if nil != err {
			return rs, err
		}
		iRule = append(iRule, jsessionPath)
	}

	poolPath, err := joinBigipPath(c.BigIP.Partitions[0], hu.name)
	if nil != err {
		return rs, err
	}

	if hu.endpoint != nil {
		address = hu.endpoint.Address
		port = hu.endpoint.Port
		description = makeDescription(hu.uri.String(), hu.endpoint.ApplicationId)
	}

	if address == "" || description == "" {
		err = errors.New("createResources error: missing endpoint address or description")
		return rs, err
	}

	vs := &bigipResources.Virtual{
		VirtualServerName:     hu.name,
		PoolName:              poolPath,
		Mode:                  "tcp",
		Enabled:               true,
		Destination:           destination,
		SourceAddress:         c.BigIP.Tier2IPRange,
		IRules:                iRule,
		Profiles:              profile,
		SourceAddrTranslation: bigipResources.SourceAddrTranslation{Type: "automap"},
	}

	rs.Virtuals = append(rs.Virtuals, vs)

	member := bigipResources.Member{
		Address: address,
		Port:    port,
		Session: "user-enabled",
	}
	pool := makePool(
		hu.name,
		description,
		[]bigipResources.Member{member},
		c.BigIP.LoadBalancingMode,
		fixupNames(c.BigIP.HealthMonitors),
	)
	rs.Pools = append(rs.Pools, pool)
	return rs, nil
}

// NewUpdate creates a new HTTP route update
func NewUpdate(
	logger logger.Logger,
	op routeUpdate.Operation,
	uri route.Uri,
	ep *route.Endpoint,
	planID string,
) (updateHTTP, error) {
	l := logger.Session("http-update")
	l.Debug("new-update", zap.String("URI", uri.String()))

	if len(uri) == 0 {
		return updateHTTP{}, errors.New("uri length of zero is not allowed")
	}

	if op == routeUpdate.Add || op == routeUpdate.Remove {
		return updateHTTP{
			logger:   l,
			op:       op,
			uri:      uri,
			endpoint: ep,
			name:     makeObjectName(uri.String()),
			protocol: "http",
		}, nil
	} else if op == routeUpdate.Bind || op == routeUpdate.Unbind {
		return updateHTTP{
			logger:   l,
			op:       op,
			uri:      uri,
			planID:   planID,
			name:     makeObjectName(uri.String()),
			protocol: "http",
		}, nil
	}

	return updateHTTP{}, fmt.Errorf("unrecognized route update operation: %v ", op)
}

// CreateBrokerDefaultResources creates default resources for broker route updates
func (hu updateHTTP) CreateBrokerDefaultResources(
	c *config.Config,
	description string,
	destination string,
	address string,
	port uint16,
) (bigipResources.Resources, error) {
	resources, err := createResources(hu, c, description, destination, address, port)
	return resources, err
}

//CreateResources creates default resources for route updates
func (hu updateHTTP) CreateResources(c *config.Config) (bigipResources.Resources, error) {
	resources, err := createResources(hu, c, "", "", "", 0)
	return resources, err
}

// CreatePlanResources creates bigip resources from plan
func (hu updateHTTP) CreatePlanResources(c *config.Config, plan planResources.Plan) bigipResources.Resources {
	resources := bigipResources.Resources{}

	// Create bigip virtual server
	virtual := bigipResources.Virtual{}
	var newProfiles []*bigipResources.ProfileRef
	var newSslProfiles []*bigipResources.ProfileRef
	var err error

	if len(plan.VirtualServer.Policies) != 0 {
		virtual.Policies, err = generateNameList(plan.VirtualServer.Policies)
		if err != nil {
			hu.logger.Warn("skipping-policy-names", zap.Error(err))
		}
	}
	if len(plan.VirtualServer.Profiles) != 0 {
		newProfiles, err = generateProfileList(plan.VirtualServer.Profiles, "all")
		if err != nil {
			hu.logger.Warn("skipping-profile-names", zap.Error(err))
		}
	}
	if len(plan.VirtualServer.SslProfiles) != 0 {
		newSslProfiles, err = generateProfileList(plan.VirtualServer.SslProfiles, "serverside")
		if err != nil {
			hu.logger.Warn("skipping-sslProfile-names", zap.Error(err))
		}
	}

	newProfiles = append(newProfiles, newSslProfiles...)
	if len(newProfiles) != 0 {
		virtual.Profiles = newProfiles
	}
	resources.Virtuals = append(resources.Virtuals, &virtual)

	// Create bigip pool
	pool := bigipResources.Pool{}
	if plan.Pool.Balance != "" {
		pool.Balance = plan.Pool.Balance
	}

	// Create bigip health monitors
	if len(plan.Pool.HealthMonitors) != 0 {
		hmNames := []string{}
		monitors := []*bigipResources.Monitor{}

		for i := range plan.Pool.HealthMonitors {
			name := plan.Pool.HealthMonitors[i].Name

			// Create custom monitor and attach to pool
			if plan.Pool.HealthMonitors[i].Type != "" {
				hmName, err := joinBigipPath(c.BigIP.Partitions[0], name)
				if err != nil {
					hu.logger.Warn("plan-pool-name-error", zap.Error(err))
				} else {
					hmNames = append(hmNames, hmName)
					monitors = append(monitors, &plan.Pool.HealthMonitors[i])
				}
			} else {
				// Monitor already exists on bigip attach name to pool
				hmNames = append(hmNames, name)
			}
		}
		pool.MonitorNames = hmNames
		resources.Monitors = monitors
	}
	resources.Pools = append(resources.Pools, &pool)

	return resources
}

// UpdateResources updates old bigip resources into new bigip resources
func (hu updateHTTP) UpdateResources(
	oldResources bigipResources.Resources,
	newResources bigipResources.Resources,
) bigipResources.Resources {
	updatedResources := oldResources

	// Update bigip virtual server
	if len(newResources.Virtuals) != 0 {
		if len(newResources.Virtuals[0].Profiles) != 0 {
			updatedResources.Virtuals[0].Profiles = newResources.Virtuals[0].Profiles
		}
		if len(newResources.Virtuals[0].Policies) != 0 {
			updatedResources.Virtuals[0].Policies = newResources.Virtuals[0].Policies
		}
	}
	// Update bigip pool
	if len(newResources.Pools) != 0 {
		if newResources.Pools[0].Balance != "" {
			updatedResources.Pools[0].Balance = newResources.Pools[0].Balance
		}
		if len(newResources.Pools[0].MonitorNames) != 0 {
			updatedResources.Pools[0].MonitorNames = newResources.Pools[0].MonitorNames
		}
	}
	// Update bigip health monitor
	if len(newResources.Monitors) != 0 {
		updatedResources.Monitors = newResources.Monitors
	}

	return updatedResources
}

func (hu updateHTTP) Protocol() string {
	return hu.protocol
}

func (hu updateHTTP) Op() routeUpdate.Operation {
	return hu.op
}

func (hu updateHTTP) URI() route.Uri {
	return hu.uri
}

func (hu updateHTTP) Name() string {
	return hu.name
}

func (hu updateHTTP) AppID() string {
	return hu.endpoint.ApplicationId
}

func (hu updateHTTP) Route() string {
	return hu.uri.String()
}

func (hu updateHTTP) PlanID() string {
	return hu.planID
}
