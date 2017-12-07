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

package servicebroker_test

import (
	"context"
	"encoding/json"
	"os"

	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/servicebroker"
	"github.com/F5Networks/cf-bigip-ctlr/test_util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-cf/brokerapi"
)

var _ = Describe("ServiceBroker", func() {
	var (
		instanceID string
		cfg        *config.Config
		broker     *servicebroker.ServiceBroker
		ctx        context.Context
		logger     *test_util.TestZapLogger
		err        error
	)

	BeforeEach(func() {
		instanceID = "test-instance"
		logger = test_util.NewTestZapLogger("schema-test")
		ctx = context.Background()
		cfg = config.DefaultConfig()
		cfg.BrokerMode = true
		cfg.Status.User = "username"
		cfg.Status.Pass = "password"

		broker, err = servicebroker.NewServiceBroker(cfg, logger)
		Expect(err).To(BeNil())
	})

	AfterEach(func() {
		if nil != logger {
			logger.Close()
		}
	})

	It("should return services", func() {
		service := broker.Services(ctx)[0]

		Expect(service.ID).To(Equal(cfg.Broker.ID))
		Expect(service.Name).To(Equal(cfg.Broker.Name))
		Expect(service.Description).To(Equal(cfg.Broker.Description))
		Expect(service.Bindable).Should(BeFalse())
		Expect(service.Tags).Should(ConsistOf("f5"))
		Expect(service.PlanUpdatable).Should(BeFalse())
		Expect(service.Plans).Should(BeEmpty())
		Expect(service.Requires).Should(Equal([]brokerapi.RequiredPermission{"route_forwarding"}))
		Expect(service.DashboardClient).Should(BeNil())
	})

	It("should return a provisioned service", func() {
		details := brokerapi.ProvisionDetails{
			ServiceID:        "serviceID",
			PlanID:           "planID",
			OrganizationGUID: "organizationGUID",
			SpaceGUID:        "spaceGUID",
			RawContext:       json.RawMessage(`{"context: true"}`),
			RawParameters:    json.RawMessage(`{"parameters: [1,2,3]"}`),
		}
		spec, err := broker.Provision(ctx, instanceID, details, true)

		Expect(spec).To(Equal(brokerapi.ProvisionedServiceSpec{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return a deprovisioned service", func() {
		details := brokerapi.DeprovisionDetails{
			PlanID:    "planID",
			ServiceID: "serviceID",
		}
		spec, err := broker.Deprovision(ctx, instanceID, details, false)

		Expect(spec).To(Equal(brokerapi.DeprovisionServiceSpec{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return a service binding", func() {
		bindSource := brokerapi.BindResource{
			AppGuid:            "appGUID",
			Route:              "/route",
			CredentialClientID: "credentialClientID",
		}
		details := brokerapi.BindDetails{
			AppGUID:       "appGUID",
			PlanID:        "planID",
			ServiceID:     "serviceID",
			BindResource:  &bindSource,
			RawContext:    json.RawMessage(`{"context": false}`),
			RawParameters: json.RawMessage(`{"parameters: [a,b,c]"}`),
		}
		binding, err := broker.Bind(ctx, instanceID, "bindingID", details)

		Expect(binding).To(Equal(brokerapi.Binding{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("should unbind a service", func() {
		details := brokerapi.UnbindDetails{
			PlanID:    "planID",
			ServiceID: "serviceID",
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
			PlanID:    "planID",
			ServiceID: "serviceID",
			OrgID:     "orgID",
			SpaceID:   "spaceID",
		}
		details := brokerapi.UpdateDetails{
			ServiceID:      "serviceID",
			PlanID:         "planID",
			RawParameters:  json.RawMessage(`{"parameters": [1,2,3]}`),
			PreviousValues: previousValues,
		}
		spec, err := broker.Update(ctx, instanceID, details, true)

		Expect(spec).To(Equal(brokerapi.UpdateServiceSpec{}))
		Expect(err).NotTo(HaveOccurred())
	})

	Context("process plans", func() {
		BeforeEach(func() {
			os.Setenv("TEST_MODE", "true")
		})

		AfterEach(func() {
			os.Unsetenv("SERVICE_BROKER_CONFIG")
			os.Unsetenv("TEST_MODE")
		})

		It("errors when env variable is not set", func() {
			err := broker.ProcessPlans()
			Expect(err).To(MatchError("SERVICE_BROKER_CONFIG not found in environment"))
		})

		It("returns plans when the plan is valid", func() {
			os.Setenv("SERVICE_BROKER_CONFIG", `{"plans":[{"description":"arggg","name":"test","virtualServer":{"policies":["potato"]}}]}`)

			err := broker.ProcessPlans()
			Expect(err).To(BeNil())
			service := broker.Services(ctx)[0]
			Expect(len(service.Plans)).To(Equal(1))
		})
	})
})
