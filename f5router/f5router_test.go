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
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"

	"github.com/F5Networks/cf-bigip-ctlr/bigipclient"
	fakeClient "github.com/F5Networks/cf-bigip-ctlr/bigipclient/fakes"
	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/bigipResources"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/routeUpdate"
	"github.com/F5Networks/cf-bigip-ctlr/route"
	"github.com/F5Networks/cf-bigip-ctlr/servicebroker/planResources"
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
			client := bigipclient.DefaultClient()

			r, err := NewF5Router(logger, nil, mw, client)
			Expect(r).To(BeNil())
			Expect(err).To(HaveOccurred())

			r, err = NewF5Router(logger, c, nil, client)
			Expect(r).To(BeNil())
			Expect(err).To(HaveOccurred())

			c.BigIP.URL = "http://example.com"
			r, err = NewF5Router(logger, c, mw, client)
			Expect(r).To(BeNil())
			Expect(err).To(HaveOccurred())

			c.BigIP.User = "admin"
			r, err = NewF5Router(logger, c, mw, client)
			Expect(r).To(BeNil())
			Expect(err).To(HaveOccurred())

			c.BigIP.Pass = "pass"
			r, err = NewF5Router(logger, c, mw, client)
			Expect(r).To(BeNil())
			Expect(err).To(HaveOccurred())

			c.BigIP.Partitions = []string{"cf"}
			r, err = NewF5Router(logger, c, mw, client)
			Expect(r).To(BeNil())
			Expect(err).To(HaveOccurred())

			c.BigIP.Tier2IPRange = "10.0.0.1/32"
			r, err = NewF5Router(logger, c, mw, client)
			Expect(r).To(BeNil())
			Expect(err).To(HaveOccurred())

			c.BigIP.ExternalAddr = "127.0.0.1"
			r, err = NewF5Router(logger, c, mw, client)
			Expect(r).NotTo(BeNil())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("httpUpdate", func() {
		var httpUpdate updateHTTP
		Context("UpdateResources", func() {
			var oldResources bigipResources.Resources
			var newResources bigipResources.Resources

			BeforeEach(func() {
				httpUpdate = updateHTTP{}
				policies := []*bigipResources.NameRef{&bigipResources.NameRef{
					Name:      "test-policy",
					Partition: "test",
				}}
				profiles := []*bigipResources.ProfileRef{&bigipResources.ProfileRef{
					Name:      "test-profile",
					Partition: "test",
					Context:   "all",
				}}
				oldResources.Virtuals = []*bigipResources.Virtual{&bigipResources.Virtual{
					VirtualServerName: "test-route-virtual",
					PoolName:          "test-route-pool",
					Profiles:          profiles,
					Policies:          policies,
				}}
				oldResources.Pools = []*bigipResources.Pool{&bigipResources.Pool{
					Name:         "test-route-pool",
					Balance:      "round-robin",
					MonitorNames: []string{"monitor1", "monitor2"},
				}}
				oldResources.Monitors = []*bigipResources.Monitor{&bigipResources.Monitor{
					Name: "test-route-monitor",
					Type: "http",
				}}
				newResources = bigipResources.Resources{}
			})

			It("should not update", func() {
				updatedResources := httpUpdate.UpdateResources(oldResources, newResources)
				Expect(updatedResources).To(Equal(oldResources))
			})

			It("should update virtuals profiles and policies only", func() {
				newPolicies := []*bigipResources.NameRef{&bigipResources.NameRef{
					Name:      "new-test-policy",
					Partition: "test",
				}}
				newProfiles := []*bigipResources.ProfileRef{&bigipResources.ProfileRef{
					Name:      "new-test-profile",
					Partition: "test",
					Context:   "clientside",
				}}
				newResources.Virtuals = []*bigipResources.Virtual{&bigipResources.Virtual{
					VirtualServerName: "update-route-virtual",
					PoolName:          "update-route-pool",
					Profiles:          newProfiles,
					Policies:          newPolicies,
				}}
				updatedResources := httpUpdate.UpdateResources(oldResources, newResources)

				Expect(len(updatedResources.Virtuals)).To(Equal(1))
				Expect(updatedResources.Virtuals[0].VirtualServerName).To(Equal(oldResources.Virtuals[0].VirtualServerName))
				Expect(updatedResources.Virtuals[0].PoolName).To(Equal(oldResources.Virtuals[0].PoolName))
				Expect(updatedResources.Virtuals[0].Profiles).To(Equal(newProfiles))
				Expect(updatedResources.Virtuals[0].Policies).To(Equal(newPolicies))
				Expect(len(updatedResources.Virtuals[0].Profiles)).To(Equal(1))
				Expect(len(updatedResources.Virtuals[0].Policies)).To(Equal(1))

				Expect(len(updatedResources.Pools)).To(Equal(1))
				Expect(len(updatedResources.Monitors)).To(Equal(1))
				Expect(updatedResources.Pools[0]).To(Equal(oldResources.Pools[0]))
				Expect(updatedResources.Monitors[0]).To(Equal(oldResources.Monitors[0]))
			})

			It("should update pools balance and monitor names only", func() {
				newResources.Pools = []*bigipResources.Pool{&bigipResources.Pool{
					Name:         "update-route-pool",
					Balance:      "fastest-node",
					MonitorNames: []string{"monitor3"},
				}}
				updatedResources := httpUpdate.UpdateResources(oldResources, newResources)

				Expect(len(updatedResources.Pools)).To(Equal(1))
				Expect(updatedResources.Pools[0].Name).To(Equal(oldResources.Pools[0].Name))
				Expect(updatedResources.Pools[0].Balance).To(Equal(newResources.Pools[0].Balance))
				Expect(updatedResources.Pools[0].MonitorNames).To(Equal(newResources.Pools[0].MonitorNames))
				Expect(len(updatedResources.Pools[0].MonitorNames)).To(Equal(1))

				Expect(len(updatedResources.Virtuals)).To(Equal(1))
				Expect(len(updatedResources.Monitors)).To(Equal(1))
				Expect(updatedResources.Virtuals[0]).To(Equal(oldResources.Virtuals[0]))
				Expect(updatedResources.Monitors[0]).To(Equal(oldResources.Monitors[0]))
			})

			It("should update monitors", func() {
				newResources.Monitors = []*bigipResources.Monitor{&bigipResources.Monitor{
					Name: "update-route-monitor",
					Type: "tcp",
				}}
				updatedResources := httpUpdate.UpdateResources(oldResources, newResources)

				Expect(len(updatedResources.Virtuals)).To(Equal(1))
				Expect(len(updatedResources.Pools)).To(Equal(1))
				Expect(len(updatedResources.Monitors)).To(Equal(1))
				Expect(updatedResources.Virtuals[0]).To(Equal(oldResources.Virtuals[0]))
				Expect(updatedResources.Pools[0]).To(Equal(oldResources.Pools[0]))
				Expect(updatedResources.Monitors[0]).To(Equal(newResources.Monitors[0]))
			})
		})

		Context("CreatePlanResources", func() {
			var plan planResources.Plan
			var logger *test_util.TestZapLogger
			var c *config.Config

			BeforeEach(func() {
				logger := test_util.NewTestZapLogger("create-plan-resources-test")
				httpUpdate = updateHTTP{logger: logger}
				plan = planResources.Plan{
					Name: "test-plan",
				}
				c = config.DefaultConfig()
				c.BigIP.Partitions = []string{"test"}
			})

			AfterEach(func() {
				if nil != logger {
					logger.Close()
				}
			})

			It("should create virtual resources from plan", func() {
				plan.VirtualServer = planResources.VirtualType{
					Policies:    []string{"/test/policy"},
					Profiles:    []string{"/test/profile"},
					SslProfiles: []string{"/test/sslProfile"},
				}
				resources := httpUpdate.CreatePlanResources(c, plan)

				Expect(len(resources.Pools)).To(Equal(1))
				Expect(len(resources.Monitors)).To(Equal(0))
				Expect(len(resources.Virtuals)).To(Equal(1))
				Expect(len(resources.Virtuals[0].Policies)).To(Equal(1))
				Expect(len(resources.Virtuals[0].Profiles)).To(Equal(2))

				Expect(resources.Pools[0]).To(Equal(&bigipResources.Pool{}))
				Expect(resources.Virtuals[0].Policies).To(Equal([]*bigipResources.NameRef{
					&bigipResources.NameRef{
						Name:      "policy",
						Partition: "test",
					}}))
				Expect(resources.Virtuals[0].Profiles[0]).To(Equal(&bigipResources.ProfileRef{
					Name:      "profile",
					Partition: "test",
					Context:   "all",
				}))
				Expect(resources.Virtuals[0].Profiles[1]).To(Equal(&bigipResources.ProfileRef{
					Name:      "sslProfile",
					Partition: "test",
					Context:   "serverside",
				}))
			})

			It("should not create virtual resources", func() {
				plan.VirtualServer = planResources.VirtualType{}
				resources := httpUpdate.CreatePlanResources(c, plan)

				Expect(len(resources.Pools)).To(Equal(1))
				Expect(len(resources.Virtuals)).To(Equal(1))
				Expect(len(resources.Monitors)).To(Equal(0))

				Expect(resources.Pools[0]).To(Equal(&bigipResources.Pool{}))
				Expect(resources.Virtuals[0]).To(Equal(&bigipResources.Virtual{}))
			})

			It("should create pool resources from plan", func() {
				monitors := []bigipResources.Monitor{
					bigipResources.Monitor{
						Name: "monitor1",
						Type: "http",
					},
					bigipResources.Monitor{
						Name: "monitor2",
						Type: "tcp",
					},
				}
				plan.Pool = planResources.PoolType{
					Balance:        "round-robin",
					HealthMonitors: monitors,
				}
				resources := httpUpdate.CreatePlanResources(c, plan)

				Expect(len(resources.Virtuals)).To(Equal(1))
				Expect(resources.Virtuals[0]).To(Equal(&bigipResources.Virtual{}))
				Expect(len(resources.Pools)).To(Equal(1))
				Expect(len(resources.Monitors)).To(Equal(2))
				Expect(len(resources.Pools[0].MonitorNames)).To(Equal(2))
				Expect(resources.Pools[0].Balance).To(Equal("round-robin"))
				Expect(resources.Pools[0].MonitorNames).To(Equal([]string{"/test/monitor1", "/test/monitor2"}))
				Expect(*resources.Monitors[0]).To(Equal(monitors[0]))
				Expect(*resources.Monitors[1]).To(Equal(monitors[1]))
			})

			It("should not create pool resources", func() {
				plan.Pool = planResources.PoolType{}
				resources := httpUpdate.CreatePlanResources(c, plan)

				Expect(len(resources.Virtuals)).To(Equal(1))
				Expect(len(resources.Pools)).To(Equal(1))
				Expect(len(resources.Monitors)).To(Equal(0))

				Expect(resources.Pools[0]).To(Equal(&bigipResources.Pool{}))
				Expect(resources.Virtuals[0]).To(Equal(&bigipResources.Virtual{}))
			})
		})
	})

	Describe("running router", func() {
		var mw *MockWriter
		var router *F5Router
		var err error
		var up updateHTTP
		var logger *test_util.TestZapLogger
		var c *config.Config
		var client *bigipclient.BigIPClient

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
				up, _ = NewUpdate(logger, routeUpdate.Add, pair.url, pair.ep, "")
				router.UpdateRoute(up)
			}
		}

		BeforeEach(func() {
			logger = test_util.NewTestZapLogger("router-test")
			c = makeConfig()
			mw = &MockWriter{}
			client = bigipclient.DefaultClient()

			router, err = NewF5Router(logger, c, mw, client)

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
			matchConfig(mw, expectedConfigs[update], false)
			update++

			// make some changes and update the verification function
			up, err = NewUpdate(logger, routeUpdate.Remove, "bar.cf.com", barEndpoint, "")
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			up, err = NewUpdate(logger, routeUpdate.Remove, "baz.cf.com/segment1", baz2Segment1Endpoint, "")
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			up, err = NewUpdate(logger, routeUpdate.Add, "baz.cf.com", baz2Endpoint, "")
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			up, err = NewUpdate(logger, routeUpdate.Remove, "*.foo.cf.com", wildFooEndpoint, "")
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			up, err = NewUpdate(logger, routeUpdate.Remove, "ser*.cf.com", wildEndEndpoint, "")
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			up, err = NewUpdate(logger, routeUpdate.Remove, "ser*es.cf.com", wildMidEndpoint, "")
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			up, err = NewUpdate(logger, routeUpdate.Remove, "*vices.cf.com", wildBeginEndpoint, "")
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			up, err = NewUpdate(logger, routeUpdate.Remove, "foo.cf.com", fooEndpoint, "")
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			// config1
			matchConfig(mw, expectedConfigs[update], false)
			update++

			up, err = NewUpdate(logger, routeUpdate.Add, "qux.cf.com", quxEndpoint, "")
			Expect(err).NotTo(HaveOccurred())
			router.UpdateRoute(up)

			// config2
			matchConfig(mw, expectedConfigs[update], false)

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

			router, err = NewF5Router(logger, c, mw, client)

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

			//config3
			matchConfig(mw, expectedConfigs[3], false)
		})
		Context("service broker route updates", func() {
			var (
				regularEndpoint1,
				regularEndpoint2,
				brokerEndpoint1,
				brokerEndpoint2,
				brokerEndpoint3,
				brokerEndpoint4,
				brokerEndpoint5 *route.Endpoint
			)

			BeforeEach(func() {
				regularEndpoint1 = makeEndpoint("127.0.0.1")
				regularEndpoint2 = makeEndpoint("127.0.0.2")
				brokerEndpoint1 = makeEndpoint("127.0.1.1")
				brokerEndpoint2 = makeEndpoint("127.0.1.2")
				brokerEndpoint3 = makeEndpoint("127.0.1.3")
				brokerEndpoint4 = makeEndpoint("127.0.1.4")
				brokerEndpoint5 = makeEndpoint("127.0.1.5")

				// Add broker plans to router
				plans := make(map[string]planResources.Plan)

				// Updates to virtual and pool but no health monitors
				plans["plan1"] = planResources.Plan{
					ID: "plan1",
					Pool: planResources.PoolType{
						Balance: "ratio-member",
						HealthMonitors: []bigipResources.Monitor{
							bigipResources.Monitor{
								Name: "/Common/existing-bigip-monitor",
							},
							bigipResources.Monitor{
								Name: "plan1-monitor1",
								Type: "http",
							},
						},
					},
					VirtualServer: planResources.VirtualType{
						Policies:    []string{"/test/plan1-policy"},
						Profiles:    []string{"/test/plan1-profile"},
						SslProfiles: []string{"/test/plan1-ssl-profile"},
					},
				}

				// Updates to pool health monitors only
				plans["plan2"] = planResources.Plan{
					ID: "plan2",
					Pool: planResources.PoolType{
						HealthMonitors: []bigipResources.Monitor{
							bigipResources.Monitor{
								Name: "plan2-monitor1",
								Type: "http",
							},
							bigipResources.Monitor{
								Name: "plan2-monitor2",
								Type: "tcp",
							},
						},
					},
				}

				router.AddPlans(plans)

				err = router.VerifyPlanExists("plan1")
				Expect(err).NotTo(HaveOccurred())
				err = router.VerifyPlanExists("plan2")
				Expect(err).NotTo(HaveOccurred())
			})

			It("should handle service broker updates", func() {
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

				// Add regular route to router
				up, err := NewUpdate(logger, routeUpdate.Add, "regular.cf.com/1", regularEndpoint1, "")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				matchConfig(mw, expectedConfigs[7], false)

				// Add a regular route that will have broker updates bound to it
				up, err = NewUpdate(logger, routeUpdate.Add, "broker.cf.com/1", brokerEndpoint1, "")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Remove a regular route that was added before the test
				up, err = NewUpdate(logger, routeUpdate.Remove, "regular.cf.com/1", regularEndpoint1, "")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Add broker defined updates to a route without endpoint this should not yet create bigip objects
				up, err = NewUpdate(logger, routeUpdate.Bind, "broker.cf.com/2", nil, "plan1")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Same as above except this route will never get an endpoint so these bigip objects will never be created
				up, err = NewUpdate(logger, routeUpdate.Bind, "broker.cf.com/3", nil, "plan1")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Add a regular route that will have broker updates bound to it
				up, err = NewUpdate(logger, routeUpdate.Add, "broker.cf.com/4", brokerEndpoint1, "")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Add a regular route that will have broker updates bound to it
				up, err = NewUpdate(logger, routeUpdate.Add, "broker.cf.com/5", brokerEndpoint1, "")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Add a regular route that will have broker updates bound to it
				up, err = NewUpdate(logger, routeUpdate.Add, "broker.cf.com/6", brokerEndpoint1, "")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				matchConfig(mw, expectedConfigs[8], false)

				// Add a regular route
				up, err = NewUpdate(logger, routeUpdate.Add, "regular.cf.com/2", regularEndpoint2, "")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Add broker update to route with existing endpoint
				up, err = NewUpdate(logger, routeUpdate.Bind, "broker.cf.com/1", nil, "plan2")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Add broker update to route with existing endpoint
				up, err = NewUpdate(logger, routeUpdate.Bind, "broker.cf.com/4", nil, "plan1")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Add broker update to route with existing endpoint
				up, err = NewUpdate(logger, routeUpdate.Bind, "broker.cf.com/5", nil, "plan2")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Add broker update to route with existing endpoint
				up, err = NewUpdate(logger, routeUpdate.Bind, "broker.cf.com/6", nil, "plan2")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Double bind update to route with existing endpoint make sure objects are not duplicated
				up, err = NewUpdate(logger, routeUpdate.Bind, "broker.cf.com/4", nil, "plan1")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Double bind update to route with existing endpoint make sure objects are not duplicated
				up, err = NewUpdate(logger, routeUpdate.Bind, "broker.cf.com/5", nil, "plan2")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Add route with endpoint to a broker updated route
				up, err = NewUpdate(logger, routeUpdate.Add, "broker.cf.com/2", brokerEndpoint2, "")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				matchConfig(mw, expectedConfigs[9], false)

				// Remove a regular route
				up, err = NewUpdate(logger, routeUpdate.Remove, "regular.cf.com/2", regularEndpoint2, "")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Remove a broker updated route
				up, err = NewUpdate(logger, routeUpdate.Remove, "broker.cf.com/2", brokerEndpoint2, "")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Unbind a broker updated route (reset to config defaults)
				up, err = NewUpdate(logger, routeUpdate.Unbind, "broker.cf.com/1", nil, "")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Unbind a broker updated route (reset to config defaults)
				up, err = NewUpdate(logger, routeUpdate.Unbind, "broker.cf.com/4", nil, "")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Unbind a broker updated route (reset to config defaults)
				up, err = NewUpdate(logger, routeUpdate.Unbind, "broker.cf.com/5", nil, "")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Double unbind update to route with existing endpoint make sure objects are not duplicated
				up, err = NewUpdate(logger, routeUpdate.Unbind, "broker.cf.com/4", nil, "plan1")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Double unbind update to route with existing endpoint make sure objects are not duplicated
				up, err = NewUpdate(logger, routeUpdate.Unbind, "broker.cf.com/5", nil, "plan2")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				// Unbind a broker updated route without an endpoint (noop since its bigip objects are never created)
				up, err = NewUpdate(logger, routeUpdate.Unbind, "broker.cf.com/3", nil, "")
				Expect(err).NotTo(HaveOccurred())
				router.UpdateRoute(up)

				matchConfig(mw, expectedConfigs[10], false)

				os <- MockSignal(123)
				Eventually(done).Should(BeClosed(), "timed out waiting for Run to complete")
			})
		})

		Context("retry backoff", func() {
			var fakeBigIPClient *fakeClient.FakeClient

			BeforeEach(func() {
				fakeBigIPClient = &fakeClient.FakeClient{}
			})

			It("handles retry backoff recovery", func() {
				networkError1 := mockNetworkError{
					error:   errors.New("mock network error 1"),
					retTime: false,
					retTemp: true,
				}
				fakeBigIPClient.GetReturnsOnCall(0, nil, networkError1)

				networkError2 := mockNetworkError{
					error:   errors.New("mock network error 2"),
					retTime: true,
					retTemp: false,
				}
				fakeBigIPClient.GetReturnsOnCall(1, nil, networkError2)

				fakeBigIPClient.GetReturnsOnCall(2, []byte("was not found"), nil)

				done := make(chan struct{})
				os := make(chan os.Signal)
				ready := make(chan struct{})

				router, err = NewF5Router(logger, c, mw, fakeBigIPClient)

				go func() {
					defer GinkgoRecover()
					Expect(func() {
						err = router.Run(os, ready)
						Expect(err).NotTo(HaveOccurred())
						close(done)
					}).NotTo(Panic())
				}()

				Eventually(logger).Should(Say(`"bigip-client-get-recoverable-error","source":"router-test","data":{"error":"mock network error 1"`))
				Eventually(logger).Should(Say("Retrying in 1 seconds"))
				Eventually(logger).Should(Say(`"retry-backoff-encountered-error-retrying","source":"router-test","data":{"error":"mock network error 2"`))
				Eventually(logger).Should(Say("Retrying in 2 seconds"))
				Eventually(logger).Should(Say("data group cf-ctlr-data-group not found on the BIG-IP"))
				Eventually(ready).Should(BeClosed())
			})

			It("handles retry backoff fatal error", func() {
				networkError1 := mockNetworkError{
					error:   errors.New("mock network error 1"),
					retTime: true,
					retTemp: true,
				}
				fakeBigIPClient.GetReturnsOnCall(0, nil, networkError1)

				networkError2 := mockNetworkError{
					error:   errors.New("mock network error 2"),
					retTime: false,
					retTemp: false,
				}
				fakeBigIPClient.GetReturnsOnCall(1, nil, networkError2)

				done := make(chan struct{})
				os := make(chan os.Signal)
				ready := make(chan struct{})

				router, err = NewF5Router(logger, c, mw, fakeBigIPClient)

				go func() {
					defer GinkgoRecover()
					Expect(func() {
						err = router.Run(os, ready)
						Expect(err).NotTo(HaveOccurred())
						close(done)
					}).NotTo(Panic())
				}()

				Eventually(logger).Should(Say(`"bigip-client-get-recoverable-error","source":"router-test","data":{"error":"mock network error 1"`))
				Eventually(logger).Should(Say("Retrying in 1 seconds"))
				Eventually(logger).Should(Say("failed-to-fetch-existing-controller-data-group"))
				Eventually(ready).Should(BeClosed())
			})
		})

		Context("fake BIG-IP provides a response", func() {
			var server *ghttp.Server
			var fakeDataGroup *bigipResources.InternalDataGroup

			BeforeEach(func() {
				server = ghttp.NewServer()
			})

			AfterEach(func() {
				//shut down the server between tests
				server.Close()
			})
			It("should be able to use a pre-existing datagroup", func() {
				fakeDataGroup = createFakeDataGroup()
				server.AppendHandlers(ghttp.RespondWithJSONEncoded(http.StatusOK, fakeDataGroup))

				done := make(chan struct{})
				os := make(chan os.Signal)
				ready := make(chan struct{})

				// Update the config
				c.BigIP.URL = server.URL()

				router, err = NewF5Router(logger, c, mw, client)

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
			It("should be able to use a pre-existing broker data group", func() {
				fakeDataGroup = createFakeBrokerDataGroup()
				server.AppendHandlers(ghttp.RespondWithJSONEncoded(http.StatusNotFound, "was not found"))
				server.AppendHandlers(ghttp.RespondWithJSONEncoded(http.StatusOK, fakeDataGroup))

				registerTestRoutes := func() {
					noPlanEndpoint := makeEndpoint("127.0.0.1")
					plan1Endpoint := makeEndpoint("127.0.1.1")
					plan2Endpoint := makeEndpoint("127.0.1.2")
					bunkPlanEndpoint := makeEndpoint("127.0.1.3")

					pairs := []routePair{
						routePair{"noPlan.cf.com", noPlanEndpoint},
						routePair{"plan1.cf.com", plan1Endpoint},
						routePair{"plan2.cf.com", plan2Endpoint},
						routePair{"bunkPlan.cf.com", bunkPlanEndpoint},
					}
					for _, pair := range pairs {
						up, _ = NewUpdate(logger, routeUpdate.Add, pair.url, pair.ep, "")
						router.UpdateRoute(up)
					}
				}

				done := make(chan struct{})
				os := make(chan os.Signal)
				ready := make(chan struct{})

				// Update the config
				c.BigIP.URL = server.URL()
				c.BrokerMode = true

				router, err = NewF5Router(logger, c, mw, client)

				// Add broker plans to router
				plans := make(map[string]planResources.Plan)
				plans["plan1-GUID"] = planResources.Plan{
					ID:   "plan1-GUID",
					Name: "plan1",
					VirtualServer: planResources.VirtualType{
						Policies: []string{"/Common/Policy1", "/Common/Policy2"},
					},
					Pool: planResources.PoolType{
						HealthMonitors: []bigipResources.Monitor{
							bigipResources.Monitor{
								Name:     "Monitor1",
								Type:     "http",
								Interval: 16,
								Timeout:  5,
								Send:     "Hello",
							},
						},
					},
				}
				plans["plan2-GUID"] = planResources.Plan{
					ID:   "plan2-GUID",
					Name: "plan2",
					VirtualServer: planResources.VirtualType{
						Profiles:    []string{"/Common/Profile1", "/Common/Profile2"},
						SslProfiles: []string{"/Common/SSLProfile1", "/Common/SSLProfile2"},
					},
					Pool: planResources.PoolType{
						Balance: "ratio-node",
					},
				}
				router.AddPlans(plans)

				go func() {
					defer GinkgoRecover()
					Expect(func() {
						err = router.Run(os, ready)
						Expect(err).NotTo(HaveOccurred())
						close(done)
					}).NotTo(Panic())
				}()
				Eventually(logger).Should(Say("process-broker-data-group-processed-data-group"))
				Eventually(ready).Should(BeClosed())

				// Rebuild routes with data group info
				registerTestRoutes()

				// Skip bigip validation since its URL is determined by the ghttp
				// server mocking it so it will change
				matchConfig(mw, expectedConfigs[11], true)
			})
		})

		Context("fail cases", func() {
			It("should error when not passing a URI for route update", func() {
				_, updateErr := NewUpdate(logger, routeUpdate.Add, "", fooEndpoint, "")
				Expect(updateErr).To(MatchError("uri length of zero is not allowed"))
			})

			It("should error when a policy name is not formatted correctly", func() {
				done := make(chan struct{})
				os := make(chan os.Signal)
				ready := make(chan struct{})

				c.BigIP.Policies = []string{"fakepolicy", "/cf/anotherpolicy"}

				router, err = NewF5Router(logger, c, mw, client)

				go func() {
					defer GinkgoRecover()
					Expect(func() {
						err = router.Run(os, ready)
						Expect(err).NotTo(HaveOccurred())
						close(done)
					}).NotTo(Panic())
				}()
				Eventually(ready).Should(BeClosed())

				Eventually(logger).Should(Say("skipping-policy-names"))
			})

			It("should error when exceeding the ip range", func() {
				done := make(chan struct{})
				os := make(chan os.Signal)
				ready := make(chan struct{})

				router, err = NewF5Router(logger, c, mw, client)
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
				matchConfig(mw, expectedConfigs[6], false)

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
				router, err = NewF5Router(logger, c, mw, client)

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
				matchConfig(mw, expectedConfigs[4], false)
			})

			It("should update tcp routes", func() {
				registerTCP()
				matchConfig(mw, expectedConfigs[4], false)
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

				matchConfig(mw, expectedConfigs[5], false)
			})

			It("should not add the same pool member more than once", func() {
				registerTCP()
				matchConfig(mw, expectedConfigs[4], false)

				member := bigipResources.Member{Address: "10.0.0.1", Port: 5000, Session: "user-enabled"}
				ad, _ := NewTCPUpdate(c, logger, routeUpdate.Add, 6010, member)
				router.UpdateRoute(ad)
				router.UpdateRoute(ad)
				router.UpdateRoute(ad)

				matchConfig(mw, expectedConfigs[4], false)

			})

		})

	})
})

type mockNetworkError struct {
	error   error
	retTime bool
	retTemp bool
}

func (m mockNetworkError) Error() string {
	return m.error.Error()
}
func (m mockNetworkError) Timeout() bool {
	return m.retTime
}
func (m mockNetworkError) Temporary() bool {
	return m.retTemp
}

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

func matchConfig(mw *MockWriter, expected []byte, skipBigIPValidation bool) {
	var matcher configMatcher
	err := json.Unmarshal(expected, &matcher)
	ExpectWithOffset(1, err).To(BeNil())

	EventuallyWithOffset(1, func() bigipResources.GlobalConfig {
		return mw.getInput().Global
	}).Should(Equal(matcher.Global))

	if !skipBigIPValidation {
		EventuallyWithOffset(1, func() config.BigIPConfig {
			return mw.getInput().BigIP
		}).Should(Equal(matcher.BigIP))
	}

	matchVirtuals(mw, matcher)

	matchPools(mw, matcher)

	matchPolicies(mw, matcher)

	matchMonitors(mw, matcher)

	matchInternalDataGroups(mw, matcher)

	matchIRules(mw, matcher)
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
	for _, dataGroup := range matcher.Resources["cf"].InternalDataGroups {
		EventuallyWithOffset(2, func() string {
			if _, ok := mw.getInput().Resources["cf"]; ok {
				for _, dg := range mw.getInput().Resources["cf"].InternalDataGroups {
					if dg.Name == dataGroup.Name {
						matchInternalDataGroupRecords(dg, dataGroup)
						return dg.Name
					}
				}
			}
			return ""
		}).Should(Equal(dataGroup.Name), fmt.Sprintf("Expected %s data group", dataGroup.Name))
	}
}

func matchInternalDataGroupRecords(dg, matcherDg *bigipResources.InternalDataGroup) {
	for _, record := range matcherDg.Records {
		Expect(dg.Records).Should(ContainElement(record),
			fmt.Sprintf("%v missing from data group %v", record, dg.Records),
		)
	}
	Expect(len(dg.Records)).Should(Equal(len(matcherDg.Records)),
		fmt.Sprintf("Expected %v \n to match \n %v", format.Object(dg, 1), format.Object(matcherDg, 1)),
	)
}

func matchIRules(mw *MockWriter, matcher configMatcher) {
	for _, iRule := range matcher.Resources["cf"].IRules {
		EventuallyWithOffset(2, func() []*bigipResources.IRule {
			if _, ok := mw.getInput().Resources["cf"]; ok {
				return mw.getInput().Resources["cf"].IRules
			}
			return nil
		}).Should(ContainElement(iRule))
	}
	EventuallyWithOffset(2, func() int {
		return len(mw.getInput().Resources["cf"].IRules)
	}).Should(Equal(len(matcher.Resources["cf"].IRules)),
		fmt.Sprintf("Expected %v \n to match \n %v",
			format.Object(mw.getInput().Resources["cf"].IRules, 1),
			format.Object(matcher.Resources["cf"].IRules, 1),
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

func createFakeBrokerDataGroup() *bigipResources.InternalDataGroup {
	return &bigipResources.InternalDataGroup{
		Name: "fake-broker-data",
		Records: []*bigipResources.InternalDataGroupRecord{
			&bigipResources.InternalDataGroupRecord{Name: "bindingID1", Data: "plan1.cf.com|plan1"},
			&bigipResources.InternalDataGroupRecord{Name: "bindingID2", Data: "plan2.cf.com|plan2"},
			&bigipResources.InternalDataGroupRecord{Name: "bindingID3", Data: "bunkPlan.cf.com|plan3"},
		},
	}
}
