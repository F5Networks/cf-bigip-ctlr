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
	"github.com/F5Networks/cf-bigip-ctlr/config"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-cf/brokerapi"
)

var _ = Describe("ServiceBroker", func() {
	var instanceID string
	var cfg config.ServiceBrokerConfig
	var broker ServiceBroker
	var ctx context.Context

	BeforeEach(func() {
		instanceID = "test-instance"
		cfg = config.ServiceBrokerConfig{
			"test-broker-id",
			"test-broker",
			"service broker for test",
			"f5-service-broker",
			"service broker to be used for testing",
			"http://documentation.com",
			"http://support.com",
			"http://image.com",
			"F5 Service Broker",
			"Username",
			"Password",
		}
		broker = ServiceBroker{&cfg}
		ctx = context.Background()
	})

	It("should return services", func() {
		service := broker.Services(ctx)[0]

		Expect(service.ID).To(Equal(cfg.ID))
		Expect(service.Name).To(Equal(cfg.Name))
		Expect(service.Description).To(Equal(cfg.Description))
		Expect(service.Bindable).Should(BeTrue())
		Expect(service.Tags).Should(ConsistOf("f5"))
		Expect(service.PlanUpdatable).Should(BeFalse())
		Expect(service.Plans).Should(BeEmpty())
		Expect(service.Requires).Should(BeNil())
		Expect(service.DashboardClient).Should(BeNil())
		Expect(service.Metadata.DisplayName).To(Equal(cfg.DisplayName))
		Expect(service.Metadata.ImageUrl).To(Equal(cfg.ImageURL))
		Expect(service.Metadata.LongDescription).To(Equal(cfg.LongDescription))
		Expect(service.Metadata.ProviderDisplayName).To(Equal(cfg.ProviderName))
		Expect(service.Metadata.DocumentationUrl).To(Equal(cfg.DocumentationURL))
		Expect(service.Metadata.SupportUrl).To(Equal(cfg.SupportURL))
		Expect(service.Metadata.Shareable).Should(BeNil())
	})

	It("should return a provisioned service", func() {
		details := brokerapi.ProvisionDetails{
			"serviceID",
			"planID",
			"organizationGUID",
			"spaceGUID",
			json.RawMessage(`{"context: true"}`),
			json.RawMessage(`{"parameters: [1,2,3]"}`),
		}
		spec, err := broker.Provision(ctx, instanceID, details, true)

		Expect(spec).To(Equal(brokerapi.ProvisionedServiceSpec{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return a deprovisioned service", func() {
		details := brokerapi.DeprovisionDetails{
			"planID",
			"serviceID",
		}
		spec, err := broker.Deprovision(ctx, instanceID, details, false)

		Expect(spec).To(Equal(brokerapi.DeprovisionServiceSpec{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return a service binding", func() {
		bindSource := brokerapi.BindResource{
			"appGUID",
			"/route",
			"credentialClientID",
		}
		details := brokerapi.BindDetails{
			"appGUID",
			"planID",
			"serviceID",
			&bindSource,
			json.RawMessage(`{"context": false}`),
			json.RawMessage(`{"parameters: [a,b,c]"}`),
		}
		binding, err := broker.Bind(ctx, instanceID, "bindingID", details)

		Expect(binding).To(Equal(brokerapi.Binding{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("should unbind a service", func() {
		details := brokerapi.UnbindDetails{
			"planID",
			"serviceID",
		}
		err := broker.Unbind(ctx, instanceID, "bindingID", details)

		Expect(err).NotTo(HaveOccurred())
	})

	It("should return a last operation", func() {
		lastOp, err := broker.LastOperation(ctx, instanceID, "test-data")

		Expect(lastOp).To(Equal(brokerapi.LastOperation{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return an updated service", func() {
		previousValues := brokerapi.PreviousValues{
			"planID",
			"serviceID",
			"orgID",
			"spaceID",
		}
		details := brokerapi.UpdateDetails{
			"serviceID",
			"planID",
			json.RawMessage(`{"parameters": [1,2,3]}`),
			previousValues,
		}
		spec, err := broker.Update(ctx, instanceID, details, true)

		Expect(spec).To(Equal(brokerapi.UpdateServiceSpec{}))
		Expect(err).NotTo(HaveOccurred())
	})
})
