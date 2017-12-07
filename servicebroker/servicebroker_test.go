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
	"errors"
	"os"

	"github.com/F5Networks/cf-bigip-ctlr/config"
	fakeF5router "github.com/F5Networks/cf-bigip-ctlr/f5router/fakes"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/routeUpdate"
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
		router     *fakeF5router.FakeRouter
	)

	BeforeEach(func() {
		instanceID = "test-instance"
		logger = test_util.NewTestZapLogger("schema-test")
		ctx = context.Background()
		cfg = config.DefaultConfig()
		cfg.BrokerMode = true
		cfg.Status.User = "username"
		cfg.Status.Pass = "password"
		router = &fakeF5router.FakeRouter{}

		broker, err = servicebroker.NewServiceBroker(cfg, logger, router)
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
		router.VerifyPlanExistsReturns(nil)
		spec, err := broker.Provision(ctx, instanceID, details, true)

		Expect(spec).To(Equal(brokerapi.ProvisionedServiceSpec{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("provision should error if plan not found", func() {
		details := brokerapi.ProvisionDetails{
			ServiceID:        "serviceID",
			PlanID:           "planID",
			OrganizationGUID: "organizationGUID",
			SpaceGUID:        "spaceGUID",
			RawContext:       json.RawMessage(`{"context: true"}`),
			RawParameters:    json.RawMessage(`{"parameters: [1,2,3]"}`),
		}
		router.VerifyPlanExistsReturns(errors.New("test_error"))
		spec, err := broker.Provision(ctx, instanceID, details, true)

		Expect(spec).To(Equal(brokerapi.ProvisionedServiceSpec{}))
		Expect(err).To(HaveOccurred())
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
			Route:              "test.route/path",
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

		router.VerifyPlanExistsReturns(nil)
		binding, err := broker.Bind(ctx, instanceID, "bindingID", details)

		Expect(binding).To(Equal(brokerapi.Binding{}))
		Expect(err).NotTo(HaveOccurred())

		updateHTTP := router.UpdateRouteArgsForCall(0)
		Expect(updateHTTP.Op()).To(Equal(routeUpdate.Bind))
		Expect(updateHTTP.Route()).To(Equal("test.route/path"))
		Expect(updateHTTP.Name()).To(Equal("cf-test-d101b0ff1b32ef04"))
		Expect(updateHTTP.Protocol()).To(Equal("http"))
	})

	It("bind should error if a bind_resource is not given", func() {
		details := brokerapi.BindDetails{
			AppGUID:       "appGUID",
			PlanID:        "planID",
			ServiceID:     "serviceID",
			RawContext:    json.RawMessage(`{"context": false}`),
			RawParameters: json.RawMessage(`{"parameters: [a,b,c]"}`),
		}

		binding, err := broker.Bind(ctx, instanceID, "bindingID", details)

		Expect(binding).To(Equal(brokerapi.Binding{}))
		Expect(err).To(HaveOccurred())
	})

	It("bind should error if a route is not given", func() {
		bindSource := brokerapi.BindResource{
			AppGuid:            "appGUID",
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
		Expect(err).To(HaveOccurred())
	})

	It("bind should error if a plan is not found", func() {
		bindSource := brokerapi.BindResource{
			AppGuid:            "appGUID",
			Route:              "test.route/path",
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

		router.VerifyPlanExistsReturns(errors.New("test_error"))
		binding, err := broker.Bind(ctx, instanceID, "bindingID", details)

		Expect(binding).To(Equal(brokerapi.Binding{}))
		Expect(err).To(HaveOccurred())
	})

	It("should unbind a bound service", func() {
		// Bind route that will be unbound in the subsequent Unbind call
		bindSource := brokerapi.BindResource{
			AppGuid:            "appGUID",
			Route:              "test.route/path",
			CredentialClientID: "credentialClientID",
		}
		bindDetails := brokerapi.BindDetails{
			AppGUID:       "appGUID",
			PlanID:        "planID",
			ServiceID:     "serviceID",
			BindResource:  &bindSource,
			RawContext:    json.RawMessage(`{"context": false}`),
			RawParameters: json.RawMessage(`{"parameters: [a,b,c]"}`),
		}

		router.VerifyPlanExistsReturns(nil)
		_, err := broker.Bind(ctx, instanceID, "bindingID", bindDetails)
		updateHTTP := router.UpdateRouteArgsForCall(0)
		Expect(err).NotTo(HaveOccurred())
		Expect(updateHTTP.Op()).To(Equal(routeUpdate.Bind))
		Expect(updateHTTP.Route()).To(Equal("test.route/path"))
		Expect(updateHTTP.Name()).To(Equal("cf-test-d101b0ff1b32ef04"))
		Expect(updateHTTP.Protocol()).To(Equal("http"))

		unbindDetails := brokerapi.UnbindDetails{
			PlanID:    "planID",
			ServiceID: "serviceID",
		}
		err = broker.Unbind(ctx, instanceID, "bindingID", unbindDetails)

		Expect(err).NotTo(HaveOccurred())

		updateHTTP = router.UpdateRouteArgsForCall(1)
		Expect(updateHTTP.Op()).To(Equal(routeUpdate.Unbind))
		Expect(updateHTTP.Route()).To(Equal("test.route/path"))
		Expect(updateHTTP.Name()).To(Equal("cf-test-d101b0ff1b32ef04"))
		Expect(updateHTTP.Protocol()).To(Equal("http"))
	})

	It("unbind should error if binding not found", func() {
		unbindDetails := brokerapi.UnbindDetails{
			PlanID:    "planID",
			ServiceID: "serviceID",
		}
		err = broker.Unbind(ctx, instanceID, "bindingID", unbindDetails)

		Expect(err).To(HaveOccurred())
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
			os.Setenv("SERVICE_BROKER_CONFIG",
				`{"plans":[{"description":"arggg","name":"test","virtualServer":{"policies":["potato"]}}]}`)

			err := broker.ProcessPlans()
			Expect(err).To(BeNil())
			service := broker.Services(ctx)[0]
			Expect(len(service.Plans)).To(Equal(1))
		})
	})
})
