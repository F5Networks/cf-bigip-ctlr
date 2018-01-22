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

package servicebroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/F5Networks/cf-bigip-ctlr/common/uuid"
	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/f5router"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/routeUpdate"
	cfLogger "github.com/F5Networks/cf-bigip-ctlr/logger"
	"github.com/F5Networks/cf-bigip-ctlr/route"
	"github.com/F5Networks/cf-bigip-ctlr/schema"
	"github.com/F5Networks/cf-bigip-ctlr/servicebroker/planResources"
	"github.com/pivotal-cf/brokerapi"
	"github.com/uber-go/zap"
)

// ServiceBroker for F5 services
type ServiceBroker struct {
	Config  *config.ServiceBrokerConfig
	logger  cfLogger.Logger
	Handler http.Handler
	router  f5router.Router
	plans   map[string]planResources.Plan
}

func (broker *ServiceBroker) processPlans(val string) (map[string]planResources.Plan, error) {
	var plans planResources.Plans
	planMap := make(map[string]planResources.Plan)

	err := json.Unmarshal([]byte(val), &plans)
	if nil != err {
		return nil, err
	}

	for _, plan := range plans.Plans {
		guid, err := uuid.GenerateUUID()
		if nil != err {
			broker.logger.Warn("process-plans-error", zap.Error(err), zap.Object("skipping-plan", plan))
		}
		plan.ID = guid
		planMap[guid] = plan
	}
	return planMap, nil
}

func (broker *ServiceBroker) prepPlansforAPIResponse() []brokerapi.ServicePlan {
	var apiPlans []brokerapi.ServicePlan
	for id, plan := range broker.plans {
		apiPlans = append(apiPlans,
			brokerapi.ServicePlan{
				ID:          id,
				Name:        plan.Name,
				Description: plan.Description,
				Bindable:    brokerapi.FreeValue(true),
			})
	}
	return apiPlans
}

// NewServiceBroker returns an http.Handler for a service brokers routes
func NewServiceBroker(
	c *config.Config,
	logger cfLogger.Logger,
	router f5router.Router,
) (*ServiceBroker, error) {
	serviceBroker := &ServiceBroker{
		Config: &c.Broker,
		logger: logger.Session("service-broker"),
		router: router,
	}
	// create our unique ID
	guid, err := uuid.GenerateUUID()
	if nil != err {
		return nil, err
	}
	serviceBroker.Config.ID = guid

	brokerCredentials := brokerapi.BrokerCredentials{
		Username: c.Status.User,
		Password: c.Status.Pass,
	}

	serviceBroker.Handler = brokerapi.New(serviceBroker, cfLogger.NewLagerAdapter(logger), brokerCredentials)
	return serviceBroker, nil
}

//ProcessPlans pulls plans from the env and validates them against the schema
func (broker *ServiceBroker) ProcessPlans() error {
	val, ok := os.LookupEnv("SERVICE_BROKER_CONFIG")
	if !ok || len(val) == 0 {
		return errors.New("SERVICE_BROKER_CONFIG not found in environment")
	}
	broker.logger.Debug("process-plans-config", zap.String("SERVICE_BROKER_CONFIG", val))

	valid, err := schema.VerifySchema(val, broker.logger)
	if nil != err {
		return err
	}

	if !valid {
		return errors.New("Plans failed schema validation")
	}

	planMap, err := broker.processPlans(val)
	if nil != err {
		return err
	}
	broker.plans = planMap
	broker.router.AddPlans(planMap)

	return nil
}

// Services that are available for provisioning
func (broker *ServiceBroker) Services(ctx context.Context) []brokerapi.Service {
	plans := broker.prepPlansforAPIResponse()

	return []brokerapi.Service{
		brokerapi.Service{
			ID:          broker.Config.ID,
			Name:        broker.Config.Name,
			Description: broker.Config.Description,
			Bindable:    false,
			Requires:    []brokerapi.RequiredPermission{brokerapi.PermissionRouteForwarding},
			Plans:       plans,
			Tags: []string{
				"f5",
			},
		},
	}
}

// Provision verify that the requested plan exists for the bigip service
func (broker *ServiceBroker) Provision(
	ctx context.Context,
	instanceID string,
	details brokerapi.ProvisionDetails,
	asyncAllowed bool,
) (brokerapi.ProvisionedServiceSpec, error) {
	spec := brokerapi.ProvisionedServiceSpec{}

	err := broker.router.VerifyPlanExists(details.PlanID)
	if err != nil {
		broker.logger.Warn("broker-provision-plan-error", zap.Error(err))
	}

	return spec, err
}

// Deprovision stubbed since the bigip service will not be deprovisioned for now
func (broker *ServiceBroker) Deprovision(
	ctx context.Context,
	instanceID string,
	details brokerapi.DeprovisionDetails,
	asyncAllowed bool,
) (brokerapi.DeprovisionServiceSpec, error) {
	return brokerapi.DeprovisionServiceSpec{}, nil
}

// Bind apply bigip configurations to bigip objects based on requested bind route
func (broker *ServiceBroker) Bind(
	ctx context.Context,
	instanceID string,
	bindID string,
	details brokerapi.BindDetails,
) (brokerapi.Binding, error) {
	var err error
	apiErr := false
	binding := brokerapi.Binding{}

	if details.BindResource == nil {
		apiErr = true
	} else if details.BindResource.Route == "" {
		apiErr = true
	}

	if apiErr {
		err = errors.New("only route services bindings are supported")
		return binding, err
	}
	routeURI := details.BindResource.Route

	if bindID == "" {
		err = fmt.Errorf("BindID missing for route: %s", routeURI)
	}

	err = broker.router.VerifyPlanExists(details.PlanID)
	if err != nil {
		return binding, err
	}

	// Map bindID to route URI then create new route update to give to router
	ru, err := f5router.NewUpdate(broker.logger, routeUpdate.Bind, route.Uri(routeURI), nil, details.PlanID)
	if err != nil {
		return binding, err
	}
	broker.router.AddBindIDRouteURIPlanNameMapping(bindID, routeURI, details.PlanID)
	broker.router.UpdateRoute(ru)

	return binding, nil
}

// Unbind remove applied bigip configurations and bigip objects based on requested unbind route
func (broker *ServiceBroker) Unbind(
	ctx context.Context,
	instanceID string,
	bindID string,
	details brokerapi.UnbindDetails,
) error {
	var err error
	routeURI := broker.router.GetRouteURIFromBindID(bindID)

	if routeURI == "" {
		err = fmt.Errorf("unbind route not found for bindID: %s", bindID)
		return err
	}

	// Unmap bindID to route URI then create new route update to give to router=
	ru, err := f5router.NewUpdate(broker.logger, routeUpdate.Unbind, route.Uri(routeURI), nil, "")
	if err != nil {
		return err
	}
	broker.router.RemoveBindIDRouteURIPlanNameMapping(bindID)
	broker.router.UpdateRoute(ru)

	return nil
}

// LastOperation stubbed since we do not allow for async operations
func (broker *ServiceBroker) LastOperation(
	ctx context.Context,
	instanceID string,
	operationData string,
) (brokerapi.LastOperation, error) {
	return brokerapi.LastOperation{}, nil
}

// Update stubbed since we do not currently have route context to apply these updates
func (broker *ServiceBroker) Update(
	ctx context.Context,
	instanceID string,
	details brokerapi.UpdateDetails,
	asyncAllowed bool,
) (brokerapi.UpdateServiceSpec, error) {
	broker.logger.Warn("broker-update-warning",
		zap.String("broker-event-type-not-supported", "update event not supported"))
	return brokerapi.UpdateServiceSpec{}, nil
}
