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
	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/pivotal-cf/brokerapi"
)

// ServiceBroker for F5 services
type ServiceBroker struct {
	Config *config.ServiceBrokerConfig
}

// Services that are available for provisioning
func (broker *ServiceBroker) Services(ctx context.Context) []brokerapi.Service {
	return []brokerapi.Service{
		brokerapi.Service{
			ID:          broker.Config.ID,
			Name:        broker.Config.Name,
			Description: broker.Config.Description,
			Bindable:    true,
			Plans:       []brokerapi.ServicePlan{},
			Metadata: &brokerapi.ServiceMetadata{
				DisplayName:         broker.Config.DisplayName,
				LongDescription:     broker.Config.LongDescription,
				DocumentationUrl:    broker.Config.DocumentationURL,
				SupportUrl:          broker.Config.SupportURL,
				ImageUrl:            broker.Config.ImageURL,
				ProviderDisplayName: broker.Config.ProviderName,
			},
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
