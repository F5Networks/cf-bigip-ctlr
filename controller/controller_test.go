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

package controller_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"syscall"
	"time"

	cfg "github.com/F5Networks/cf-bigip-ctlr/config"
	. "github.com/F5Networks/cf-bigip-ctlr/controller"
	"github.com/F5Networks/cf-bigip-ctlr/logger"
	"github.com/F5Networks/cf-bigip-ctlr/mbus"
	fakeMetrics "github.com/F5Networks/cf-bigip-ctlr/metrics/fakes"
	rregistry "github.com/F5Networks/cf-bigip-ctlr/registry"
	"github.com/F5Networks/cf-bigip-ctlr/route"
	"github.com/F5Networks/cf-bigip-ctlr/routingtable"
	"github.com/F5Networks/cf-bigip-ctlr/test"
	"github.com/F5Networks/cf-bigip-ctlr/test/common"
	"github.com/F5Networks/cf-bigip-ctlr/test_util"
	vvarz "github.com/F5Networks/cf-bigip-ctlr/varz"

	"github.com/nats-io/nats"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
	"github.com/tedsuo/ifrit/sigmon"
)

var _ = Describe("Controller", func() {
	var (
		natsRunner *test_util.NATSRunner
		natsPort   uint16
		config     *cfg.Config

		mbusClient   *nats.Conn
		registry     *rregistry.RouteRegistry
		routingTable *routingtable.RoutingTable
		varz         vvarz.Varz
		logger       logger.Logger
		controller   *Controller
		statusPort   uint16
	)

	BeforeEach(func() {
		natsPort = test_util.NextAvailPort()
		statusPort = test_util.NextAvailPort()

		config = test_util.SpecConfig(statusPort, natsPort)
	})

	JustBeforeEach(func() {
		natsRunner = test_util.NewNATSRunner(int(natsPort))
		natsRunner.Start()

		mbusClient = natsRunner.MessageBus
		logger = test_util.NewTestZapLogger("controller-test")
		registry = rregistry.NewRouteRegistry(logger, config, nil, new(fakeMetrics.FakeRouteRegistryReporter), "")
		routingTable = routingtable.NewRoutingTable(logger, config, nil)
		varz = vvarz.NewVarz(registry)

		var err error
		controller, err = NewController(logger, config, mbusClient, registry, routingTable, varz)

		Expect(err).ToNot(HaveOccurred())

		opts := &mbus.SubscriberOpts{
			ID: "test",
			MinimumRegisterIntervalInSeconds: int(config.StartResponseDelayInterval.Seconds()),
			PruneThresholdInSeconds:          int(config.DropletStaleThreshold.Seconds()),
		}
		subscriber := mbus.NewSubscriber(logger.Session("subscriber"), mbusClient, registry, nil, opts, "")

		members := grouper.Members{
			{Name: "subscriber", Runner: subscriber},
			{Name: "controller", Runner: controller},
		}
		group := grouper.NewOrdered(os.Interrupt, members)
		monitor := ifrit.Invoke(sigmon.New(group, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1))
		<-monitor.Ready()
	})

	AfterEach(func() {
		if natsRunner != nil {
			natsRunner.Stop()
		}

		if controller != nil {
			controller.Stop()
		}
	})

	Context("Stop", func() {
		It("no longer responds to component requests", func() {
			host := fmt.Sprintf("http://%s:%d/routes", config.Ip, config.Status.Port)

			req, err := http.NewRequest("GET", host, nil)
			Expect(err).ToNot(HaveOccurred())
			req.SetBasicAuth("user", "pass")

			sendAndReceive(req, http.StatusOK)

			controller.Stop()
			controller = nil

			Eventually(func() error {
				req, err = http.NewRequest("GET", host, nil)
				Expect(err).ToNot(HaveOccurred())

				_, err = http.DefaultClient.Do(req)
				return err
			}).Should(HaveOccurred())
		})
	})

	Context("Start", func() {
		BeforeEach(func() {
			config.StartResponseDelayInterval = 2 * time.Second
		})
		It("should log waiting delay value", func() {
			Eventually(logger).Should(gbytes.Say("controller-waiting-for-route-registration"))
			verify_health(fmt.Sprintf("localhost:%d", statusPort))
		})
	})

	It("registry contains last updated varz", func() {
		app1 := test.NewGreetApp([]route.Uri{"test1.vcap.me"}, mbusClient, nil)
		app1.Listen()

		Eventually(func() bool {
			return appRegistered(registry, app1)
		}).Should(BeTrue())

		time.Sleep(100 * time.Millisecond)
		initialUpdateTime := fetchRecursively(readVarz(varz), "ms_since_last_registry_update").(float64)

		app2 := test.NewGreetApp([]route.Uri{"test2.vcap.me"}, mbusClient, nil)
		app2.Listen()
		Eventually(func() bool {
			return appRegistered(registry, app2)
		}).Should(BeTrue())

		// updateTime should be after initial update time
		updateTime := fetchRecursively(readVarz(varz), "ms_since_last_registry_update").(float64)
		Expect(updateTime).To(BeNumerically("<", initialUpdateTime))
	})

	It("handles a /routes request", func() {
		var client http.Client
		var req *http.Request
		var resp *http.Response
		var err error

		err = mbusClient.Publish("router.register",
			[]byte(`{"dea":"dea1","app":"app1","uris":["test.com"],"host":"1.2.3.4","port":1234,"tags":{},"private_instance_id":"private_instance_id",
																	    "private_instance_index": "2"}`))
		Expect(err).ToNot(HaveOccurred())
		time.Sleep(250 * time.Millisecond)

		host := fmt.Sprintf("http://%s:%d/routes", config.Ip, config.Status.Port)

		req, err = http.NewRequest("GET", host, nil)
		Expect(err).ToNot(HaveOccurred())
		req.SetBasicAuth("user", "pass")

		resp, err = client.Do(req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp).ToNot(BeNil())
		Expect(resp.StatusCode).To(Equal(200))

		body, err := ioutil.ReadAll(resp.Body)
		defer resp.Body.Close()
		Expect(err).ToNot(HaveOccurred())
		Expect(string(body)).To(MatchRegexp(".*1\\.2\\.3\\.4:1234.*\n"))
	})
})

func readVarz(v vvarz.Varz) map[string]interface{} {
	varz_byte, err := v.MarshalJSON()
	Expect(err).ToNot(HaveOccurred())

	varz_data := make(map[string]interface{})
	err = json.Unmarshal(varz_byte, &varz_data)
	Expect(err).ToNot(HaveOccurred())

	return varz_data
}

func fetchRecursively(x interface{}, s ...string) interface{} {
	var ok bool

	for _, y := range s {
		z := x.(map[string]interface{})
		x, ok = z[y]
		Expect(ok).To(BeTrue(), fmt.Sprintf("no key: %s", s))
	}

	return x
}

func sendAndReceive(req *http.Request, statusCode int) []byte {
	var client http.Client
	resp, err := client.Do(req)
	Expect(err).ToNot(HaveOccurred())
	Expect(resp).ToNot(BeNil())
	Expect(resp.StatusCode).To(Equal(statusCode))
	defer resp.Body.Close()

	bytes, err := ioutil.ReadAll(resp.Body)
	Expect(err).ToNot(HaveOccurred())

	return bytes
}

func verify_health(host string) {
	var req *http.Request
	path := "/health"

	req, _ = http.NewRequest("GET", "http://"+host+path, nil)
	bytes := verify_success(req)
	Expect(string(bytes)).To(ContainSubstring("ok"))
}

func verify_success(req *http.Request) []byte {
	return sendAndReceive(req, http.StatusOK)
}

func appRegistered(
	registry *rregistry.RouteRegistry,
	app *common.TestApp,
) bool {
	for _, url := range app.Urls() {
		pool := registry.Lookup(url)
		if pool == nil {
			return false
		}
	}

	return true
}

func appUnregistered(
	registry *rregistry.RouteRegistry,
	app *common.TestApp,
) bool {
	for _, url := range app.Urls() {
		pool := registry.Lookup(url)
		if pool != nil {
			return false
		}
	}

	return true
}
