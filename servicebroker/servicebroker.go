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
	"net/http"
	"os"

	"code.cloudfoundry.org/lager"

	"github.com/F5Networks/cf-bigip-ctlr/common/uuid"
	"github.com/F5Networks/cf-bigip-ctlr/config"
	cfLogger "github.com/F5Networks/cf-bigip-ctlr/logger"
	"github.com/F5Networks/cf-bigip-ctlr/schema"
	"github.com/pivotal-cf/brokerapi"
	"github.com/uber-go/zap"
)

type (
	// Plans holds our plans
	Plans struct {
		Plans []Plan `json:"plans"`
	}

	// Plan used for per route config
	Plan struct {
		Name          string      `json:"name,omitempty"`
		Description   string      `json:"description,omitempty"`
		Pool          PoolType    `json:"pool,omitempty"`
		VirtualServer VirtualType `json:"virtualServer,omitempty"`
		ID            string
	}

	// PoolType holds pool info
	PoolType struct {
		Balance        string              `json:"balance,omitempty"`
		HealthMonitors []HealthMonitorType `json:"healthMonitors,omitempty"`
	}

	// VirtualType holds virtual info
	VirtualType struct {
		Policies    []string `json:"policies,omitempty"`
		Profiles    []string `json:"profiles,omitempty"`
		SslProfiles []string `json:"sslProfiles,omitempty"`
	}

	// HealthMonitorType holds info for health monitors
	HealthMonitorType struct {
		Name     string `json:"name,omitempty"`
		Interval int    `json:"interval,omitempty"`
		Protocol string `json:"protocol,omitempty"`
		Send     string `json:"send,omitempty"`
		Timeout  int    `json:"timeout,omitempty"`
	}
)

// ServiceBroker for F5 services
type ServiceBroker struct {
	Config  *config.ServiceBrokerConfig
	logger  cfLogger.Logger
	Handler http.Handler
	plans   map[string]Plan
}

// NewServiceBroker returns an http.Handler for a service brokers routes
func NewServiceBroker(c *config.Config, logger cfLogger.Logger) (*ServiceBroker, error) {
	serviceBroker := &ServiceBroker{
		Config: &c.Broker,
		logger: logger.Session("service-broker"),
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
	// FIXME (rtalley): Currently the cf-brokerapi package only supports lager
	// as its logging utility. There is an open issue:
	// https://github.com/pivotal-cf/brokerapi/issues/46 to allow different
	// loggers to be used. When this issue closes then our logger.Logger
	// should be used as the broker logger.
	brokerLogger := lager.NewLogger("f5-service-broker")
	serviceBroker.Handler = brokerapi.New(serviceBroker, brokerLogger, brokerCredentials)
	return serviceBroker, nil
}

//ProcessPlans pulls plans from the env and validates them against the schema
func (broker *ServiceBroker) ProcessPlans() error {
	val, ok := os.LookupEnv("SERVICE_BROKER_CONFIG")
	if !ok {
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
	return nil
}

func (broker *ServiceBroker) processPlans(val string) (map[string]Plan, error) {
	var plans Plans
	planMap := make(map[string]Plan)

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

// Provision available service
func (broker *ServiceBroker) Provision(ctx context.Context, instanceID string, details brokerapi.ProvisionDetails, asyncAllowed bool) (brokerapi.ProvisionedServiceSpec, error) {
	return brokerapi.ProvisionedServiceSpec{}, nil
}

// Deprovision service
func (broker *ServiceBroker) Deprovision(ctx context.Context, instanceID string, details brokerapi.DeprovisionDetails, asyncAllowed bool) (brokerapi.DeprovisionServiceSpec, error) {
	return brokerapi.DeprovisionServiceSpec{}, nil
}

// Bind instance to service
func (broker *ServiceBroker) Bind(ctx context.Context, instanceID string, bindingID string, details brokerapi.BindDetails) (brokerapi.Binding, error) {
	return brokerapi.Binding{}, nil
}

// Unbind instance from service
func (broker *ServiceBroker) Unbind(ctx context.Context, instanceID string, bindingID string, details brokerapi.UnbindDetails) error {
	return nil
}

// LastOperation for provisioning status
func (broker *ServiceBroker) LastOperation(ctx context.Context, instanceID string, operationData string) (brokerapi.LastOperation, error) {
	return brokerapi.LastOperation{}, nil
}

// Update a service
func (broker *ServiceBroker) Update(ctx context.Context, instanceID string, details brokerapi.UpdateDetails, asyncAllowed bool) (brokerapi.UpdateServiceSpec, error) {
	return brokerapi.UpdateServiceSpec{}, nil
}
