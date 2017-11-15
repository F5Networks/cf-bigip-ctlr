/*-
 * Copyright (c) 2016,2017, F5 Networks, Inc.
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
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"

	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/bigipResources"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/routeUpdate"
	"github.com/F5Networks/cf-bigip-ctlr/route"
	"github.com/F5Networks/cf-bigip-ctlr/test_util"

	"code.cloudfoundry.org/routing-api/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	. "github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/ghttp"
)

var _ = Describe("F5Router", func() {
	Describe("sorting", func() {
		Context("rules", func() {
			It("should sort correctly", func() {
				l7 := bigipResources.Rules{}

				expectedList := make(bigipResources.Rules, 10)

				p := bigipResources.Rule{}
				p.FullURI = "bar"
				l7 = append(l7, &p)
				expectedList[1] = &p

				p = bigipResources.Rule{}
				p.FullURI = "foo"
				l7 = append(l7, &p)
				expectedList[5] = &p

				p = bigipResources.Rule{}
				p.FullURI = "foo"
				l7 = append(l7, &p)
				expectedList[7] = &p

				p = bigipResources.Rule{}
				p.FullURI = "baz"
				l7 = append(l7, &p)
				expectedList[2] = &p

				p = bigipResources.Rule{}
				p.FullURI = "foo"
				l7 = append(l7, &p)
				expectedList[6] = &p

				p = bigipResources.Rule{}
				p.FullURI = "foo"
				l7 = append(l7, &p)
				expectedList[9] = &p

				p = bigipResources.Rule{}
				p.FullURI = "baz"
				l7 = append(l7, &p)
				expectedList[3] = &p

				p = bigipResources.Rule{}
				p.FullURI = "foo"
				l7 = append(l7, &p)
				expectedList[8] = &p

				p = bigipResources.Rule{}
				p.FullURI = "foo"
				l7 = append(l7, &p)
				expectedList[4] = &p

				p = bigipResources.Rule{}
				p.FullURI = "bar"
				l7 = append(l7, &p)
				expectedList[0] = &p

				sort.Sort(l7)

				for i := range expectedList {
					Expect(l7[i]).To(Equal(expectedList[i]),
						"Sorted list elements should be equal")
				}
			})
		})
	})

	Describe("verify configs", func() {
		It("should process the config correctly", func() {
			logger := test_util.NewTestZapLogger("router-test")
			mw := &MockWriter{}
			c := config.DefaultConfig()

			r, err := NewF5Router(logger, nil, mw)
			Expect(r).To(BeNil())
			Expect(err).To(HaveOccurred())

			r, err = NewF5Router(logger, c, nil)
			Expect(r).To(BeNil())
			Expect(err).To(HaveOccurred())

			c.BigIP.URL = "http://example.com"
			r, err = NewF5Router(logger, c, mw)
			Expect(r).To(BeNil())
			Expect(err).To(HaveOccurred())

			c.BigIP.User = "admin"
			r, err = NewF5Router(logger, c, mw)
			Expect(r).To(BeNil())
			Expect(err).To(HaveOccurred())

			c.BigIP.Pass = "pass"
			r, err = NewF5Router(logger, c, mw)
			Expect(r).To(BeNil())
			Expect(err).To(HaveOccurred())

			c.BigIP.Partitions = []string{"cf"}
			r, err = NewF5Router(logger, c, mw)
			Expect(r).To(BeNil())
			Expect(err).To(HaveOccurred())

			c.BigIP.Tier2IPRange = "10.0.0.1/32"
			r, err = NewF5Router(logger, c, mw)
			Expect(r).To(BeNil())
			Expect(err).To(HaveOccurred())

			c.BigIP.ExternalAddr = "127.0.0.1"
			r, err = NewF5Router(logger, c, mw)
			Expect(r).NotTo(BeNil())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("running router", func() {
		var mw *MockWriter
		var router *F5Router
		var err error
		var up updateHTTP
		var logger *test_util.TestZapLogger
		var c *config.Config

		var (
			fooEndpoint,
			barEndpoint,
			bar2Endpoint,
			bazEndpoint,
			baz2Endpoint,
			bazSegment1Endpoint,
			baz2Segment1Endpoint,
			bazSegment3Endpoint,
			baz2Segment3Endpoint,
			wildCfEndpoint,
			wildFooEndpoint,
			wildEndEndpoint,
			wildMidEndpoint,
			wildBeginEndpoint,
			wild2Endpoint,
			quxEndpoint *route.Endpoint
		)

		registerRoutes := func() {
			// This should produce 10 tier2 vips, pools and rules
			rp := []routePair{
				routePair{"foo.cf.com", fooEndpoint},
				routePair{"bar.cf.com", barEndpoint},
				routePair{"bar.cf.com", bar2Endpoint},
				routePair{"baz.cf.com", bazEndpoint},
				routePair{"baz.cf.com/segment1", bazSegment1Endpoint},
				routePair{"baz.cf.com/segment1", baz2Segment1Endpoint},
				routePair{"baz.cf.com/segment1/segment2/segment3", bazSegment3Endpoint},
				routePair{"baz.cf.com/segment1/segment2/segment3", baz2Segment3Endpoint},
				routePair{"*.cf.com", wildCfEndpoint},
				routePair{"*.foo.cf.com", wildFooEndpoint},
				routePair{"ser*.cf.com", wildEndEndpoint},
				routePair{"ser*es.cf.com", wildMidEndpoint},
				routePair{"*vices.cf.com", wildBeginEndpoint},
				routePair{"*vic*.cf.com", wild2Endpoint},
			}
			for _, pair := range rp {
				up, _ = NewUpdate(logger, routeUpdate.Add, pair.url, pair.ep)
				router.UpdateRoute(up)
			}
		}

		BeforeEach(func() {
			logger = test_util.NewTestZapLogger("router-test")
			c = makeConfig()
			mw = &MockWriter{}

			router, err = NewF5Router(logger, c, mw)

			Expect(router).NotTo(BeNil(), "%v", router)
			Expect(err).NotTo(HaveOccurred())

			fooEndpoint = makeEndpoint("127.0.0.1")
			barEndpoint = makeEndpoint("127.0.1.1")
			bar2Endpoint = makeEndpoint("127.0.1.2")
			bazEndpoint = makeEndpoint("127.0.2.1")
			baz2Endpoint = makeEndpoint("127.0.2.2")
			bazSegment1Endpoint = makeEndpoint("127.0.3.1")
			baz2Segment1Endpoint = makeEndpoint("127.0.3.2")
			bazSegment3Endpoint = makeEndpoint("127.0.4.1")
			baz2Segment3Endpoint = makeEndpoint("127.0.4.2")
			wildCfEndpoint = makeEndpoint("127.0.5.1")
			wildFooEndpoint = makeEndpoint("127.0.6.1")
			wildEndEndpoint = makeEndpoint("127.0.6.2")
			wildMidEndpoint = makeEndpoint("127.0.6.3")
			wildBeginEndpoint = makeEndpoint("127.0.6.4")
			wild2Endpoint = makeEndpoint("127.0.7.1")
			quxEndpoint = makeEndpoint("127.0.7.1")

		})

		AfterEach(func() {
			if nil != logger {
				logger.Close()
			}
		})

		It("should run", func() {
			done := make(chan struct{})
			os := make(chan os.Signal)
			ready := make(chan struct{})

			go func() {
				defer GinkgoRecover()
				Expect(func() {
					err = router.Run(os, ready)
					Expect(err).NotTo(HaveOccurred())
					close(done)
				}).NotTo(Panic())
			}()
			// wait for the router to be ready
			Eventually(ready).Should(BeClosed(), "timed out waiting for ready")
			// send a kill signal to the router
			os <- MockSignal(123)
			//wait for the router to stop
			Eventually(done).Should(BeClosed(), "timed out waiting for Run to complete")
		})

		It("should update routes", func() {
			done := make(chan struct{})
			os := make(chan os.Signal)
			ready := make(chan struct{})
			update := 0

			go func() {
				defer GinkgoRecover()
				Expect(func() {
					err = router.Run(os, ready)
					Expect(err).NotTo(HaveOccurred())
					close(done)
				}).NotTo(Panic())
			}()

			registerRoutes()
			// config0
			matchConfig(mw, expectedConfigs[update])
			update++

			// make some changes and update the verification function
			up, err = NewUpdate(logger, routeUpdate.Remove, "bar.cf.com", barEndpoint)
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			up, err = NewUpdate(logger, routeUpdate.Remove, "baz.cf.com/segment1", baz2Segment1Endpoint)
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			up, err = NewUpdate(logger, routeUpdate.Add, "baz.cf.com", baz2Endpoint)
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			up, err = NewUpdate(logger, routeUpdate.Remove, "*.foo.cf.com", wildFooEndpoint)
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			up, err = NewUpdate(logger, routeUpdate.Remove, "ser*.cf.com", wildEndEndpoint)
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			up, err = NewUpdate(logger, routeUpdate.Remove, "ser*es.cf.com", wildMidEndpoint)
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			up, err = NewUpdate(logger, routeUpdate.Remove, "*vices.cf.com", wildBeginEndpoint)
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			up, err = NewUpdate(logger, routeUpdate.Remove, "foo.cf.com", fooEndpoint)
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			// config1
			matchConfig(mw, expectedConfigs[update])
			update++

			up, err = NewUpdate(logger, routeUpdate.Add, "qux.cf.com", quxEndpoint)
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			// config2
			matchConfig(mw, expectedConfigs[update])

			os <- MockSignal(123)
			Eventually(done).Should(BeClosed(), "timed out waiting for Run to complete")
		})

		It("should handle policies, profiles, session persistence, and health monitors", func() {
			done := make(chan struct{})
			os := make(chan os.Signal)
			ready := make(chan struct{})

			c.BigIP.HealthMonitors = []string{"Common/potato"}
			c.BigIP.SSLProfiles = []string{"Common/clientssl"}
			c.BigIP.Profiles = []string{"Common/http", "/Common/fakeprofile"}
			c.BigIP.Policies = []string{"Common/fakepolicy", "/cf/anotherpolicy"}
			c.BigIP.LoadBalancingMode = "least-connections-member"
			c.SessionPersistence = false

			router, err = NewF5Router(logger, c, mw)

			go func() {
				defer GinkgoRecover()
				Expect(func() {
					err = router.Run(os, ready)
					Expect(err).NotTo(HaveOccurred())
					close(done)
				}).NotTo(Panic())
			}()
			Eventually(ready).Should(BeClosed())

			registerRoutes()

			matchConfig(mw, expectedConfigs[3])
		})
		Context("fake BIG-IP provides a response", func() {
			var server *ghttp.Server
			var fakeDataGroup *bigipResources.InternalDataGroup

			BeforeEach(func() {
				server = ghttp.NewServer()
				fakeDataGroup = createFakeDataGroup()
				server.AppendHandlers(ghttp.RespondWithJSONEncoded(http.StatusOK, fakeDataGroup))
			})

			AfterEach(func() {
				//shut down the server between tests
				server.Close()
			})
			It("should be able to use a pre-existing datagroup", func() {
				done := make(chan struct{})
				os := make(chan os.Signal)
				ready := make(chan struct{})

				// Update the config
				c.BigIP.URL = server.URL()

				router, err = NewF5Router(logger, c, mw)

				go func() {
					defer GinkgoRecover()
					Expect(func() {
						err = router.Run(os, ready)
						Expect(err).NotTo(HaveOccurred())
						close(done)
					}).NotTo(Panic())
				}()
				// Verify we got the data back from the "BIG-IP"
				Eventually(logger).Should(Say("add-ports-to-used-ports.*60000"))
				Eventually(ready).Should(BeClosed())

				registerRoutes()

				var virtuals []*bigipResources.Virtual

				// Wait for all routes to be registered
				Eventually(func() int {
					if _, ok := mw.getInput().Resources["cf"]; ok {
						virtuals = mw.getInput().Resources["cf"].Virtuals
						return len(virtuals)
					}
					return 0
				}).Should(Equal(11))

				// Dests we know should exist on two virtuals
				idDests := []string{"/cf/10.0.0.1:50000", "/cf/10.0.0.1:60000"}
				var dests []string
				for _, virtual := range virtuals {
					if virtual.Destination == idDests[0] || virtual.Destination == idDests[1] {
						dests = append(dests, virtual.Destination)
					}
				}
				// Marshall the virtuals for ease of printing in the error
				data, _ := json.Marshal(virtuals)
				Expect(dests).To(ConsistOf(idDests), "Not all datagroup destinations were assigned, virtuals: %s", data)

				// Verify none of the virtuals ended up with the bunk dest addrs put in the data group
				badDests := []string{"/cf/10.0.0.1:70000", "/cf/10.1.1.1:10019"}
				for _, virtual := range virtuals {
					Expect(virtual.Destination).NotTo(SatisfyAny(
						Equal(badDests[0]),
						Equal(badDests[1]),
					))
				}

			})

		})

		Context("fail cases", func() {
			It("should error when not passing a URI for route update", func() {
				_, updateErr := NewUpdate(logger, routeUpdate.Add, "", fooEndpoint)
				Expect(updateErr).To(MatchError("uri length of zero is not allowed"))
			})

			It("should error when a policy name is not formatted correctly", func() {
				done := make(chan struct{})
				os := make(chan os.Signal)
				ready := make(chan struct{})

				c.BigIP.Policies = []string{"fakepolicy", "/cf/anotherpolicy"}

				router, err = NewF5Router(logger, c, mw)

				go func() {
					defer GinkgoRecover()
					Expect(func() {
						err = router.Run(os, ready)
						Expect(err).NotTo(HaveOccurred())
						close(done)
					}).NotTo(Panic())
				}()
				Eventually(ready).Should(BeClosed())

				Eventually(logger).Should(Say("f5router-skipping-name"))
			})

			It("should error when exceeding the ip range", func() {
				done := make(chan struct{})
				os := make(chan os.Signal)
				ready := make(chan struct{})

				router, err = NewF5Router(logger, c, mw)
				// Set the current port to the max
				router.tier2VSInfo.holderPort = 65535

				go func() {
					defer GinkgoRecover()
					Expect(func() {
						err = router.Run(os, ready)
						Expect(err).NotTo(HaveOccurred())
						close(done)
					}).NotTo(Panic())
				}()
				Eventually(ready).Should(BeClosed())

				registerRoutes()

				Eventually(logger).Should(Say("Ran out of available IP addresses."))
				// Verify the config, we should only see one 2nd tier vip, pool and rule
				matchConfig(mw, expectedConfigs[6])

			})

		})
		Context("tcp routing", func() {
			registerTCP := func() {
				ups := []tcpPair{
					{port: 6010, member: bigipResources.Member{Address: "10.0.0.1", Port: 5000, Session: "user-enabled"}},
					{port: 6010, member: bigipResources.Member{Address: "10.0.0.1", Port: 5001, Session: "user-enabled"}},
					{port: 6010, member: bigipResources.Member{Address: "10.0.0.1", Port: 5002, Session: "user-enabled"}},
					{port: 6020, member: bigipResources.Member{Address: "10.0.0.1", Port: 6000, Session: "user-enabled"}},
					{port: 6030, member: bigipResources.Member{Address: "10.0.0.1", Port: 7000, Session: "user-enabled"}},
					{port: 6040, member: bigipResources.Member{Address: "10.0.0.1", Port: 8000, Session: "user-enabled"}},
					{port: 6050, member: bigipResources.Member{Address: "10.0.0.1", Port: 9000, Session: "user-enabled"}},
				}
				for _, pair := range ups {
					up, _ := NewTCPUpdate(c, logger, routeUpdate.Add, pair.port, pair.member)
					router.UpdateRoute(up)
				}
			}
			BeforeEach(func() {
				c.RoutingMode = config.TCP
				router, err = NewF5Router(logger, c, mw)

				done := make(chan struct{})
				os := make(chan os.Signal)
				ready := make(chan struct{})

				go func() {
					defer GinkgoRecover()
					Expect(func() {
						err = router.Run(os, ready)
						Expect(err).NotTo(HaveOccurred())
						close(done)
					}).NotTo(Panic())
				}()

			})

			It("should register tcp routes", func() {
				registerTCP()
				matchConfig(mw, expectedConfigs[4])
			})

			It("should update tcp routes", func() {
				registerTCP()
				matchConfig(mw, expectedConfigs[4])
				// add a pool member to existing pool
				member := bigipResources.Member{Address: "10.0.0.1", Port: 6001, Session: "user-enabled"}
				ad, _ := NewTCPUpdate(c, logger, routeUpdate.Add, 6020, member)
				router.UpdateRoute(ad)
				// add a new pool - new pool and vs created
				member = bigipResources.Member{Address: "10.0.0.1", Port: 6000, Session: "user-enabled"}
				ad2, _ := NewTCPUpdate(c, logger, routeUpdate.Add, 6060, member)
				router.UpdateRoute(ad2)
				// remove a pool member with other members left
				member = bigipResources.Member{Address: "10.0.0.1", Port: 5000, Session: "user-enabled"}
				rmEP, _ := NewTCPUpdate(c, logger, routeUpdate.Remove, 6010, member)
				router.UpdateRoute(rmEP)
				// remove a pool member and none are left - pool and vs is deleted
				member = bigipResources.Member{Address: "10.0.0.1", Port: 9000, Session: "user-enabled"}
				rmEP2, _ := NewTCPUpdate(c, logger, routeUpdate.Remove, 6050, member)
				router.UpdateRoute(rmEP2)

				matchConfig(mw, expectedConfigs[5])
			})

			It("should not add the same pool member more than once", func() {
				registerTCP()
				matchConfig(mw, expectedConfigs[4])

				member := bigipResources.Member{Address: "10.0.0.1", Port: 5000, Session: "user-enabled"}
				ad, _ := NewTCPUpdate(c, logger, routeUpdate.Add, 6010, member)
				router.UpdateRoute(ad)
				router.UpdateRoute(ad)
				router.UpdateRoute(ad)

				matchConfig(mw, expectedConfigs[4])

			})

		})

	})
})

type configMatcher struct {
	Global    bigipResources.GlobalConfig `json:"global"`
	BigIP     config.BigIPConfig          `json:"bigip"`
	Resources bigipResources.PartitionMap `json:"resources"`
}

type testRoutes struct {
	Key         route.Uri
	Addrs       []*route.Endpoint
	ContextPath string
}

type MockWriter struct {
	sync.Mutex
	input []byte
}

type routePair struct {
	url route.Uri
	ep  *route.Endpoint
}

type tcpPair struct {
	port   uint16
	member bigipResources.Member
}

func (mw *MockWriter) GetOutputFilename() string {
	return "mock-file"
}

func (mw *MockWriter) Write(input []byte) (n int, err error) {
	mw.Lock()
	defer mw.Unlock()
	mw.input = input

	return len(input), nil
}

func (mw *MockWriter) getInput() *configMatcher {
	mw.Lock()
	defer mw.Unlock()
	var m configMatcher
	err := json.Unmarshal(mw.input, &m)
	Expect(err).To(BeNil())
	return &m
}

type MockSignal int

func (ms MockSignal) String() string {
	return "mock signal"
}

func (ms MockSignal) Signal() {
	return
}

func makeConfig() *config.Config {
	c := config.DefaultConfig()
	c.BigIP.URL = "http://example.com"
	c.BigIP.User = "admin"
	c.BigIP.Pass = "pass"
	c.BigIP.Partitions = []string{"cf"}
	c.BigIP.ExternalAddr = "127.0.0.1"
	c.BigIP.Tier2IPRange = "10.0.0.1/32"

	return c
}

func makeEndpoint(addr string) *route.Endpoint {
	r := route.NewEndpoint("1",
		addr,
		80,
		"1",
		"1",
		make(map[string]string),
		1,
		"",
		models.ModificationTag{
			Guid:  "1",
			Index: 1,
		},
	)
	return r
}

func matchConfig(mw *MockWriter, expected []byte) {
	var matcher configMatcher
	err := json.Unmarshal(expected, &matcher)
	ExpectWithOffset(1, err).To(BeNil())

	EventuallyWithOffset(1, func() bigipResources.GlobalConfig {
		return mw.getInput().Global
	}).Should(Equal(matcher.Global))

	EventuallyWithOffset(1, func() config.BigIPConfig {
		return mw.getInput().BigIP
	}).Should(Equal(matcher.BigIP))

	matchVirtuals(mw, matcher)

	matchPools(mw, matcher)

	matchPolicies(mw, matcher)

	matchMonitors(mw, matcher)

	matchInternalDataGroups(mw, matcher)
}

func matchVirtuals(mw *MockWriter, matcher configMatcher) {
	for _, virtual := range matcher.Resources["cf"].Virtuals {
		EventuallyWithOffset(2, func() []*bigipResources.Virtual {
			if _, ok := mw.getInput().Resources["cf"]; ok {
				return mw.getInput().Resources["cf"].Virtuals
			}
			return nil
		}).Should(ContainElement(virtual))
	}
	EventuallyWithOffset(2, func() int {
		return len(mw.getInput().Resources["cf"].Virtuals)
	}).Should(Equal(len(matcher.Resources["cf"].Virtuals)),
		fmt.Sprintf("Expected %v \n to match \n %v",
			format.Object(mw.getInput().Resources["cf"].Virtuals, 1),
			format.Object(matcher.Resources["cf"].Virtuals, 1),
		),
	)
}

func matchPools(mw *MockWriter, matcher configMatcher) {
	for _, pool := range matcher.Resources["cf"].Pools {
		EventuallyWithOffset(2, func() []*bigipResources.Pool {
			if _, ok := mw.getInput().Resources["cf"]; ok {
				return mw.getInput().Resources["cf"].Pools
			}
			return nil
		}).Should(ContainElement(pool))
	}
	EventuallyWithOffset(2, func() int {
		return len(mw.getInput().Resources["cf"].Pools)
	}).Should(Equal(len(matcher.Resources["cf"].Pools)),
		fmt.Sprintf("Expected %v \n to match \n %v",
			format.Object(mw.getInput().Resources["cf"].Pools, 1),
			format.Object(matcher.Resources["cf"].Pools, 1),
		),
	)
}

func matchPolicies(mw *MockWriter, matcher configMatcher) {
	for _, policy := range matcher.Resources["cf"].Policies {
		EventuallyWithOffset(2, func() []*bigipResources.Policy {
			if _, ok := mw.getInput().Resources["cf"]; ok {
				return mw.getInput().Resources["cf"].Policies
			}
			return nil
		}).Should(ContainElement(policy))
	}
	EventuallyWithOffset(2, func() int {
		return len(mw.getInput().Resources["cf"].Policies)
	}).Should(Equal(len(matcher.Resources["cf"].Policies)),
		fmt.Sprintf("Expected %v \n to match \n %v",
			format.Object(mw.getInput().Resources["cf"].Policies, 1),
			format.Object(matcher.Resources["cf"].Policies, 1),
		),
	)
}

func matchMonitors(mw *MockWriter, matcher configMatcher) {
	for _, monitor := range matcher.Resources["cf"].Monitors {
		EventuallyWithOffset(2, func() []*bigipResources.Monitor {
			if _, ok := mw.getInput().Resources["cf"]; ok {
				return mw.getInput().Resources["cf"].Monitors
			}
			return nil
		}).Should(ContainElement(monitor))
	}
	EventuallyWithOffset(2, func() int {
		return len(mw.getInput().Resources["cf"].Monitors)
	}).Should(Equal(len(matcher.Resources["cf"].Monitors)),
		fmt.Sprintf("Expected %v \n to match \n %v",
			format.Object(mw.getInput().Resources["cf"].Monitors, 1),
			format.Object(matcher.Resources["cf"].Monitors, 1),
		),
	)
}

func matchInternalDataGroups(mw *MockWriter, matcher configMatcher) {
	for _, monitor := range matcher.Resources["cf"].InternalDataGroups[0].Records {
		EventuallyWithOffset(2, func() []*bigipResources.InternalDataGroupRecord {
			if _, ok := mw.getInput().Resources["cf"]; ok {
				return mw.getInput().Resources["cf"].InternalDataGroups[0].Records
			}
			return nil
		}).Should(ContainElement(monitor))
	}
	EventuallyWithOffset(2, func() int {
		return len(mw.getInput().Resources["cf"].InternalDataGroups[0].Records)
	}).Should(Equal(len(matcher.Resources["cf"].InternalDataGroups[0].Records)),
		fmt.Sprintf("Expected %v \n to match \n %v",
			format.Object(mw.getInput().Resources["cf"].InternalDataGroups[0].Records, 1),
			format.Object(matcher.Resources["cf"].InternalDataGroups[0].Records, 1),
		),
	)
}

func createFakeDataGroup() *bigipResources.InternalDataGroup {
	fake := &bigipResources.InternalDataGroup{
		Name: "Fake Data",
		Records: []*bigipResources.InternalDataGroupRecord{
			// Invalid ip, outside the range so should not be used
			&bigipResources.InternalDataGroupRecord{Name: "cf-foo-e500900501f76ce8", Data: "eyJiaW5kQWRkciI6IjEwLjEuMS4xIiwicG9ydCI6MTAwMTl9"},
			// Invalid port, outside the range so should not be used
			&bigipResources.InternalDataGroupRecord{Name: "cf-bar-d21aa8a505891ac9", Data: "eyJiaW5kQWRkciI6IjEwLjAuMC4xIiwicG9ydCI6NzAwMDB9"},
			// Valid IP, port 50000
			&bigipResources.InternalDataGroupRecord{Name: "cf-baz-9a96ddcfe07bb46e", Data: "eyJiaW5kQWRkciI6IjEwLjAuMC4xIiwicG9ydCI6NTAwMDB9"},
			// Valid IP, port 60000
			&bigipResources.InternalDataGroupRecord{Name: "cf-baz-beac6f8bec5a4446", Data: "eyJiaW5kQWRkciI6IjEwLjAuMC4xIiwicG9ydCI6NjAwMDB9"},
		},
	}
	return fake
}
