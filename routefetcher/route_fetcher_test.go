package routefetcher_test

import (
	"errors"
	"os"
	"time"

	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/logger"
	testRegistry "github.com/F5Networks/cf-bigip-ctlr/registry/fakes"
	"github.com/F5Networks/cf-bigip-ctlr/route"
	. "github.com/F5Networks/cf-bigip-ctlr/routefetcher"
	testTable "github.com/F5Networks/cf-bigip-ctlr/routingtable/fakes"
	"github.com/F5Networks/cf-bigip-ctlr/test_util"

	"code.cloudfoundry.org/clock/fakeclock"
	"code.cloudfoundry.org/routing-api"
	fake_routing_api "code.cloudfoundry.org/routing-api/fake_routing_api"
	"code.cloudfoundry.org/routing-api/models"
	testUaaClient "code.cloudfoundry.org/uaa-go-client/fakes"
	"code.cloudfoundry.org/uaa-go-client/schema"
	metrics_fakes "github.com/cloudfoundry/dropsonde/metric_sender/fake"
	"github.com/cloudfoundry/dropsonde/metrics"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/tedsuo/ifrit"
)

var sender *metrics_fakes.FakeMetricSender

func init() {
	sender = metrics_fakes.NewFakeMetricSender()
	metrics.Initialize(sender, nil)
}

var _ = Describe("RouteFetcher", func() {
	var (
		cfg    *config.Config
		token  *schema.Token
		logger logger.Logger

		uaaClient *testUaaClient.FakeClient
		client    *fake_routing_api.FakeClient

		errorChannel chan error
		clock        *fakeclock.FakeClock

		process ifrit.Process

		routeClient RouteClient
		fetcher     *RouteFetcher
	)

	BeforeEach(func() {
		cfg = config.DefaultConfig()
		cfg.PruneStaleDropletsInterval = 2 * time.Millisecond
		token = &schema.Token{
			AccessToken: "access_token",
			ExpiresIn:   5,
		}
		logger = test_util.NewTestZapLogger("test")

		uaaClient = &testUaaClient.FakeClient{}
		client = &fake_routing_api.FakeClient{}

		errorChannel = make(chan error)
		clock = fakeclock.NewFakeClock(time.Now())
	})

	Context("http", func() {
		var (
			registry    *testRegistry.FakeRegistry
			eventSource *fake_routing_api.FakeEventSource
			response    []models.Route

			eventChannel chan routing_api.Event
		)
		BeforeEach(func() {
			retryInterval := 0
			registry = &testRegistry.FakeRegistry{}

			eventChannel = make(chan routing_api.Event)
			eventSource = &fake_routing_api.FakeEventSource{}
			client.SubscribeToEventsWithMaxRetriesReturns(eventSource, nil)

			localEventChannel := eventChannel
			localErrorChannel := errorChannel

			eventSource.NextStub = func() (routing_api.Event, error) {
				select {
				case e := <-localErrorChannel:
					return routing_api.Event{}, e
				case event := <-localEventChannel:
					return event, nil
				}
			}

			routeClient = NewHTTPFetcher(logger, uaaClient, client, registry)
			fetcher = NewRouteFetcher(logger, uaaClient, cfg, client, retryInterval, clock, routeClient)

		})

		AfterEach(func() {
			close(errorChannel)
			close(eventChannel)
		})

		Describe("FetchRoutes", func() {
			BeforeEach(func() {
				uaaClient.FetchTokenReturns(token, nil)

				response = []models.Route{
					models.NewRoute(
						"foo",
						1,
						"1.1.1.1",
						"guid",
						"rs",
						0,
					),
					models.NewRoute(
						"foo",
						2,
						"2.2.2.2",
						"guid",
						"route-service-url",
						0,
					),
					models.NewRoute(
						"bar",
						3,
						"3.3.3.3",
						"guid",
						"rs",
						0,
					),
				}

				*response[0].TTL = 1
				*response[1].TTL = 1
				*response[2].TTL = 1
			})

			It("updates the route registry", func() {
				client.RoutesReturns(response, nil)

				err := routeClient.FetchRoutes()
				Expect(err).ToNot(HaveOccurred())

				Expect(registry.RegisterCallCount()).To(Equal(3))

				for i := 0; i < 3; i++ {
					expectedRoute := response[i]
					uri, endpoint := registry.RegisterArgsForCall(i)
					Expect(uri).To(Equal(route.Uri(expectedRoute.Route)))
					Expect(endpoint).To(Equal(
						route.NewEndpoint(expectedRoute.LogGuid,
							expectedRoute.IP, uint16(expectedRoute.Port),
							expectedRoute.LogGuid,
							"",
							nil,
							*expectedRoute.TTL,
							expectedRoute.RouteServiceUrl,
							expectedRoute.ModificationTag,
						)))
				}
			})

			It("uses cache when fetching token from UAA", func() {
				client.RoutesReturns(response, nil)

				err := routeClient.FetchRoutes()
				Expect(err).ToNot(HaveOccurred())
				Expect(uaaClient.FetchTokenCallCount()).To(Equal(1))
				Expect(uaaClient.FetchTokenArgsForCall(0)).To(Equal(false))
			})

			Context("when a cached token is invalid", func() {
				BeforeEach(func() {
					count := 0
					client.RoutesStub = func() ([]models.Route, error) {
						if count == 0 {
							count++
							return nil, errors.New("unauthorized")
						} else {
							return response, nil
						}
					}
				})

				It("uses cache when fetching token from UAA", func() {
					client = &fake_routing_api.FakeClient{}
					err := routeClient.FetchRoutes()
					Expect(err).ToNot(HaveOccurred())
					Expect(uaaClient.FetchTokenCallCount()).To(Equal(2))
					Expect(uaaClient.FetchTokenArgsForCall(0)).To(Equal(false))
					Expect(uaaClient.FetchTokenArgsForCall(1)).To(Equal(true))
				})
			})

			It("removes unregistered routes", func() {
				secondResponse := []models.Route{
					response[0],
				}

				client.RoutesReturns(response, nil)

				err := routeClient.FetchRoutes()
				Expect(err).ToNot(HaveOccurred())
				Expect(registry.RegisterCallCount()).To(Equal(3))

				client.RoutesReturns(secondResponse, nil)

				err = routeClient.FetchRoutes()
				Expect(err).ToNot(HaveOccurred())
				Expect(registry.RegisterCallCount()).To(Equal(4))
				Expect(registry.UnregisterCallCount()).To(Equal(2))

				expectedUnregisteredRoutes := []models.Route{
					response[1],
					response[2],
				}

				for i := 0; i < 2; i++ {
					expectedRoute := expectedUnregisteredRoutes[i]
					uri, endpoint := registry.UnregisterArgsForCall(i)
					Expect(uri).To(Equal(route.Uri(expectedRoute.Route)))
					Expect(endpoint).To(Equal(
						route.NewEndpoint(expectedRoute.LogGuid,
							expectedRoute.IP,
							uint16(expectedRoute.Port),
							expectedRoute.LogGuid,
							"",
							nil,
							*expectedRoute.TTL,
							expectedRoute.RouteServiceUrl,
							expectedRoute.ModificationTag,
						)))
				}
			})

			Context("when the routing api returns an error", func() {
				Context("error is not unauthorized error", func() {
					It("returns an error", func() {
						client.RoutesReturns(nil, errors.New("Oops!"))

						err := routeClient.FetchRoutes()
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(Equal("Oops!"))
						Expect(uaaClient.FetchTokenCallCount()).To(Equal(1))
						Expect(uaaClient.FetchTokenArgsForCall(0)).To(BeFalse())
					})
				})

				Context("error is unauthorized error", func() {
					It("returns an error", func() {
						client.RoutesReturns(nil, errors.New("unauthorized"))

						err := routeClient.FetchRoutes()
						Expect(uaaClient.FetchTokenCallCount()).To(Equal(2))
						Expect(uaaClient.FetchTokenArgsForCall(0)).To(BeFalse())
						Expect(uaaClient.FetchTokenArgsForCall(1)).To(BeTrue())
						Expect(client.RoutesCallCount()).To(Equal(2))
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(Equal("unauthorized"))
					})
				})
			})

			Context("When the token fetcher returns an error", func() {
				BeforeEach(func() {
					uaaClient.FetchTokenReturns(nil, errors.New("token fetcher error"))
				})

				It("returns an error", func() {
					currentTokenFetchErrors := sender.GetCounter(TokenFetchErrors)
					err := routeClient.FetchRoutes()
					Expect(err).To(HaveOccurred())
					Expect(uaaClient.FetchTokenCallCount()).To(Equal(1))
					Expect(registry.RegisterCallCount()).To(Equal(0))
					Eventually(func() uint64 {
						return sender.GetCounter(TokenFetchErrors)
					}).Should(BeNumerically(">", currentTokenFetchErrors))
				})
			})

		})

		Describe("Run", func() {
			BeforeEach(func() {
				uaaClient.FetchTokenReturns(token, nil)
				client.RoutesReturns(response, nil)
				fetcher.FetchRoutesInterval = 10 * time.Millisecond
			})

			JustBeforeEach(func() {
				process = ifrit.Invoke(fetcher)
			})

			AfterEach(func() {
				process.Signal(os.Interrupt)
				Eventually(process.Wait(), 5*time.Second).Should(Receive())
			})

			It("subscribes for events", func() {
				Eventually(client.SubscribeToEventsWithMaxRetriesCallCount).Should(Equal(1))
			})

			Context("on specified interval", func() {
				BeforeEach(func() {
					client.SubscribeToEventsWithMaxRetriesReturns(&fake_routing_api.FakeEventSource{}, errors.New("not used"))
				})

				It("fetches routes", func() {
					clock.Increment(cfg.PruneStaleDropletsInterval + 100*time.Millisecond)
					Eventually(client.RoutesCallCount, 2*time.Second, 50*time.Millisecond).Should(Equal(1))
					clock.Increment(cfg.PruneStaleDropletsInterval + 100*time.Millisecond)
					Eventually(client.RoutesCallCount, 2*time.Second, 50*time.Millisecond).Should(Equal(2))
				})

				It("uses cache when fetching token from uaa", func() {
					clock.Increment(cfg.PruneStaleDropletsInterval + 100*time.Millisecond)
					Eventually(client.RoutesCallCount, 2*time.Second, 50*time.Millisecond).Should(Equal(1))
					Expect(uaaClient.FetchTokenArgsForCall(0)).To(Equal(false))
				})
			})

			Context("when token fetcher returns error", func() {
				BeforeEach(func() {
					uaaClient.FetchTokenReturns(nil, errors.New("Unauthorized"))
				})

				It("logs the error", func() {
					currentTokenFetchErrors := sender.GetCounter(TokenFetchErrors)

					Eventually(logger).Should(gbytes.Say("Unauthorized"))

					Eventually(uaaClient.FetchTokenCallCount).Should(BeNumerically(">=", 2))
					Expect(client.SubscribeToEventsWithMaxRetriesCallCount()).Should(Equal(0))
					Expect(client.RoutesCallCount()).Should(Equal(0))

					Eventually(func() uint64 {
						return sender.GetCounter(TokenFetchErrors)
					}).Should(BeNumerically(">", currentTokenFetchErrors))
				})
			})

			Describe("Event cycle", func() {
				BeforeEach(func() {
					fetcher.FetchRoutesInterval = 5 * time.Minute // Ignore syncing cycle
				})

				Context("and the event source successfully subscribes", func() {
					It("responds to events", func() {
						Eventually(client.SubscribeToEventsWithMaxRetriesCallCount).Should(Equal(1))
						route := models.NewRoute("z.a.k", 63, "42.42.42.42", "Tomato", "route-service-url", 1)
						eventChannel <- routing_api.Event{
							Action: "Delete",
							Route:  route,
						}
						Eventually(registry.UnregisterCallCount).Should(BeNumerically(">=", 1))
					})

					It("refreshes all routes", func() {
						Eventually(client.RoutesCallCount).Should(Equal(1))
					})

					It("responds to errors and closes the old subscription", func() {
						errorChannel <- errors.New("beep boop im a robot")
						Eventually(eventSource.CloseCallCount).Should(Equal(1))
					})

					It("responds to errors, and retries subscribing", func() {
						currentSubscribeEventsErrors := sender.GetCounter(SubscribeEventsErrors)

						fetchTokenCallCount := uaaClient.FetchTokenCallCount()
						subscribeCallCount := client.SubscribeToEventsWithMaxRetriesCallCount()

						errorChannel <- errors.New("beep boop im a robot")

						Eventually(logger).Should(gbytes.Say("beep boop im a robot"))

						Eventually(uaaClient.FetchTokenCallCount).Should(BeNumerically(">", fetchTokenCallCount))
						Eventually(client.SubscribeToEventsWithMaxRetriesCallCount).Should(BeNumerically(">", subscribeCallCount))

						Eventually(func() uint64 {
							return sender.GetCounter(SubscribeEventsErrors)
						}).Should(BeNumerically(">", currentSubscribeEventsErrors))
					})
				})

				Context("and the event source fails to subscribe", func() {
					Context("with error other than unauthorized", func() {
						BeforeEach(func() {
							client.SubscribeToEventsWithMaxRetriesStub = func(uint16) (routing_api.EventSource, error) {
								err := errors.New("i failed to subscribe")
								return &fake_routing_api.FakeEventSource{}, err
							}
						})

						It("logs the error and tries again", func() {
							fetchTokenCallCount := uaaClient.FetchTokenCallCount()
							subscribeCallCount := client.SubscribeToEventsWithMaxRetriesCallCount()

							currentSubscribeEventsErrors := sender.GetCounter(SubscribeEventsErrors)

							Eventually(logger).Should(gbytes.Say("i failed to subscribe"))

							Eventually(uaaClient.FetchTokenCallCount).Should(BeNumerically(">", fetchTokenCallCount))
							Eventually(client.SubscribeToEventsWithMaxRetriesCallCount).Should(BeNumerically(">", subscribeCallCount))

							Eventually(func() uint64 {
								return sender.GetCounter(SubscribeEventsErrors)
							}).Should(BeNumerically(">", currentSubscribeEventsErrors))
						})
					})

					Context("with unauthorized error", func() {
						BeforeEach(func() {
							client.SubscribeToEventsWithMaxRetriesStub = func(uint16) (routing_api.EventSource, error) {
								err := errors.New("unauthorized")
								return &fake_routing_api.FakeEventSource{}, err
							}
						})

						It("logs the error and tries again by not using cached access token", func() {
							currentSubscribeEventsErrors := sender.GetCounter(SubscribeEventsErrors)
							Eventually(logger).Should(gbytes.Say("unauthorized"))
							Eventually(uaaClient.FetchTokenCallCount).Should(BeNumerically(">", 2))
							Expect(uaaClient.FetchTokenArgsForCall(0)).To(BeFalse())
							Expect(uaaClient.FetchTokenArgsForCall(1)).To(BeTrue())

							Eventually(func() uint64 {
								return sender.GetCounter(SubscribeEventsErrors)
							}).Should(BeNumerically(">", currentSubscribeEventsErrors))
						})
					})
				})
			})
		})

		Describe("HandleEvent", func() {
			Context("When the event is an Upsert", func() {
				It("registers the route from the registry", func() {
					eventRoute := models.NewRoute(
						"z.a.k",
						63,
						"42.42.42.42",
						"Tomato",
						"route-service-url",
						1,
					)

					event := routing_api.Event{
						Action: "Upsert",
						Route:  eventRoute,
					}

					routeClient.HandleEvent(event)
					Expect(registry.RegisterCallCount()).To(Equal(1))
					uri, endpoint := registry.RegisterArgsForCall(0)
					Expect(uri).To(Equal(route.Uri(eventRoute.Route)))
					Expect(endpoint).To(Equal(
						route.NewEndpoint(
							eventRoute.LogGuid,
							eventRoute.IP,
							uint16(eventRoute.Port),
							eventRoute.LogGuid,
							"",
							nil,
							*eventRoute.TTL,
							eventRoute.RouteServiceUrl,
							eventRoute.ModificationTag,
						)))
				})
			})

			Context("When the event is a DELETE", func() {
				It("unregisters the route from the registry", func() {
					eventRoute := models.NewRoute(
						"z.a.k",
						63,
						"42.42.42.42",
						"Tomato",
						"route-service-url",
						1,
					)

					event := routing_api.Event{
						Action: "Delete",
						Route:  eventRoute,
					}

					routeClient.HandleEvent(event)
					Expect(registry.UnregisterCallCount()).To(Equal(1))
					uri, endpoint := registry.UnregisterArgsForCall(0)
					Expect(uri).To(Equal(route.Uri(eventRoute.Route)))
					Expect(endpoint).To(Equal(
						route.NewEndpoint(
							eventRoute.LogGuid,
							eventRoute.IP,
							uint16(eventRoute.Port),
							eventRoute.LogGuid,
							"",
							nil,
							*eventRoute.TTL,
							eventRoute.RouteServiceUrl,
							eventRoute.ModificationTag,
						)))
				})
			})

			Context("When the event type is incorrect", func() {
				It("logs a warning", func() {
					event := routing_api.TcpEvent{
						Action:          "Delete",
						TcpRouteMapping: models.TcpRouteMapping{},
					}

					routeClient.HandleEvent(event)
					Expect(registry.UnregisterCallCount()).To(Equal(0))
					Expect(registry.RegisterCallCount()).To(Equal(0))
					Eventually(logger).Should(gbytes.Say("recieved-wrong-event-type"))
				})
			})
		})
	})

	Context("tcp", func() {
		var (
			routeTable  *testTable.FakeRouteTable
			eventSource *fake_routing_api.FakeTcpEventSource
			TCPresponse []models.TcpRouteMapping

			eventChannel chan routing_api.TcpEvent
		)
		BeforeEach(func() {
			retryInterval := 0
			routeTable = &testTable.FakeRouteTable{}

			eventChannel = make(chan routing_api.TcpEvent)
			eventSource = &fake_routing_api.FakeTcpEventSource{}
			client.SubscribeToTcpEventsWithMaxRetriesReturns(eventSource, nil)

			localEventChannel := eventChannel
			localErrorChannel := errorChannel

			eventSource.NextStub = func() (routing_api.TcpEvent, error) {
				select {
				case e := <-localErrorChannel:
					return routing_api.TcpEvent{}, e
				case event := <-localEventChannel:
					return event, nil
				}
			}

			routeClient = NewTCPFetcher(logger, routeTable, client, uaaClient)
			fetcher = NewRouteFetcher(logger, uaaClient, cfg, client, retryInterval, clock, routeClient)

		})

		AfterEach(func() {
			close(errorChannel)
			close(eventChannel)
		})

		Describe("FetchRoutes", func() {
			BeforeEach(func() {
				uaaClient.FetchTokenReturns(token, nil)

				TCPresponse = []models.TcpRouteMapping{
					models.NewTcpRouteMapping(
						"fakeRouterGroup",
						1001,
						"1.1.1.1",
						2020,
						1,
					),
					models.NewTcpRouteMapping(
						"fakeRouterGroup",
						1001,
						"2.2.2.2",
						2020,
						1,
					),
					models.NewTcpRouteMapping(
						"fakeRouterGroup",
						1002,
						"3.3.3.3",
						2020,
						1,
					),
				}

			})

			It("calls upsert when routes don't exist", func() {
				client.TcpRouteMappingsReturns(TCPresponse, nil)

				err := routeClient.FetchRoutes()
				Expect(err).ToNot(HaveOccurred())

				Expect(routeTable.UpsertBackendServerKeyCallCount()).To(Equal(3))
				Eventually(logger).Should(gbytes.Say("number-of-routes..3"))
			})

			It("uses cache when fetching token from UAA", func() {
				client.TcpRouteMappingsReturns(TCPresponse, nil)

				err := routeClient.FetchRoutes()
				Expect(err).ToNot(HaveOccurred())
				Expect(uaaClient.FetchTokenCallCount()).To(Equal(1))
				Expect(uaaClient.FetchTokenArgsForCall(0)).To(Equal(false))
			})

			Context("when a cached token is invalid", func() {
				BeforeEach(func() {
					count := 0
					client.TcpRouteMappingsStub = func() ([]models.TcpRouteMapping, error) {
						if count == 0 {
							count++
							return nil, errors.New("unauthorized")
						} else {
							return TCPresponse, nil
						}
					}
				})

				It("uses cache when fetching token from UAA", func() {
					client = &fake_routing_api.FakeClient{}
					err := routeClient.FetchRoutes()
					Expect(err).ToNot(HaveOccurred())
					Expect(uaaClient.FetchTokenCallCount()).To(Equal(2))
					Expect(uaaClient.FetchTokenArgsForCall(0)).To(Equal(false))
					Expect(uaaClient.FetchTokenArgsForCall(1)).To(Equal(true))
				})
			})

			Context("when the routing api returns an error", func() {
				Context("error is not unauthorized error", func() {
					It("returns an error", func() {
						client.TcpRouteMappingsReturns(nil, errors.New("Oops!"))

						err := routeClient.FetchRoutes()
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(Equal("Oops!"))
						Expect(uaaClient.FetchTokenCallCount()).To(Equal(1))
						Expect(uaaClient.FetchTokenArgsForCall(0)).To(BeFalse())
					})
				})

				Context("error is unauthorized error", func() {
					It("returns an error", func() {
						client.TcpRouteMappingsReturns(nil, errors.New("unauthorized"))

						err := routeClient.FetchRoutes()
						Expect(uaaClient.FetchTokenCallCount()).To(Equal(2))
						Expect(uaaClient.FetchTokenArgsForCall(0)).To(BeFalse())
						Expect(uaaClient.FetchTokenArgsForCall(1)).To(BeTrue())
						Expect(client.TcpRouteMappingsCallCount()).To(Equal(2))
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(Equal("unauthorized"))
					})
				})
			})

			Context("When the token fetcher returns an error", func() {
				BeforeEach(func() {
					uaaClient.FetchTokenReturns(nil, errors.New("token fetcher error"))
				})

				It("returns an error", func() {
					currentTokenFetchErrors := sender.GetCounter(TokenFetchErrors)
					err := routeClient.FetchRoutes()
					Expect(err).To(HaveOccurred())
					Expect(uaaClient.FetchTokenCallCount()).To(Equal(1))
					Eventually(func() uint64 {
						return sender.GetCounter(TokenFetchErrors)
					}).Should(BeNumerically(">", currentTokenFetchErrors))
				})
			})

		})

		Describe("Run", func() {
			BeforeEach(func() {
				uaaClient.FetchTokenReturns(token, nil)
				client.TcpRouteMappingsReturns(TCPresponse, nil)
				fetcher.FetchRoutesInterval = 10 * time.Millisecond
			})

			JustBeforeEach(func() {
				process = ifrit.Invoke(fetcher)
			})

			AfterEach(func() {
				process.Signal(os.Interrupt)
				Eventually(process.Wait(), 5*time.Second).Should(Receive())
			})

			It("subscribes for events", func() {
				Eventually(client.SubscribeToTcpEventsWithMaxRetriesCallCount).Should(Equal(1))
			})

			Context("on specified interval", func() {
				BeforeEach(func() {
					client.SubscribeToTcpEventsWithMaxRetriesReturns(
						&fake_routing_api.FakeTcpEventSource{},
						errors.New("not used"),
					)
				})

				It("fetches routes", func() {
					clock.Increment(cfg.PruneStaleDropletsInterval + 100*time.Millisecond)
					Eventually(client.TcpRouteMappingsCallCount, 2*time.Second, 50*time.Millisecond).Should(Equal(1))
					clock.Increment(cfg.PruneStaleDropletsInterval + 100*time.Millisecond)
					Eventually(client.TcpRouteMappingsCallCount, 2*time.Second, 50*time.Millisecond).Should(Equal(2))
				})

				It("uses cache when fetching token from uaa", func() {
					clock.Increment(cfg.PruneStaleDropletsInterval + 100*time.Millisecond)
					Eventually(client.TcpRouteMappingsCallCount, 2*time.Second, 50*time.Millisecond).Should(Equal(1))
					Expect(uaaClient.FetchTokenArgsForCall(0)).To(Equal(false))
				})
			})

			Context("when token fetcher returns error", func() {
				BeforeEach(func() {
					uaaClient.FetchTokenReturns(nil, errors.New("Unauthorized"))
				})

				It("logs the error", func() {
					currentTokenFetchErrors := sender.GetCounter(TokenFetchErrors)

					Eventually(logger).Should(gbytes.Say("Unauthorized"))

					Eventually(uaaClient.FetchTokenCallCount).Should(BeNumerically(">=", 2))
					Expect(client.SubscribeToTcpEventsWithMaxRetriesCallCount()).Should(Equal(0))
					Expect(client.TcpRouteMappingsCallCount()).Should(Equal(0))

					Eventually(func() uint64 {
						return sender.GetCounter(TokenFetchErrors)
					}).Should(BeNumerically(">", currentTokenFetchErrors))
				})
			})

			Describe("Event cycle", func() {
				BeforeEach(func() {
					fetcher.FetchRoutesInterval = 5 * time.Minute // Ignore syncing cycle
				})

				Context("and the event source successfully subscribes", func() {
					It("responds to events", func() {
						routeTable.DeleteBackendServerKeyReturns(true)
						Eventually(client.SubscribeToTcpEventsWithMaxRetriesCallCount).Should(Equal(1))
						route := models.NewTcpRouteMapping("fakeRouterGroup", 1001, "1.1.1.1", 2020, 1)
						eventChannel <- routing_api.TcpEvent{
							Action:          "Delete",
							TcpRouteMapping: route,
						}
						Eventually(routeTable.DeleteBackendServerKeyCallCount).Should(Equal(1))
					})

					It("refreshes all routes", func() {
						Eventually(client.TcpRouteMappingsCallCount).Should(Equal(1))
					})

					It("responds to errors and closes the old subscription", func() {
						errorChannel <- errors.New("beep boop im a robot")
						Eventually(eventSource.CloseCallCount).Should(Equal(1))
					})

					It("responds to errors, and retries subscribing", func() {
						currentSubscribeEventsErrors := sender.GetCounter(SubscribeEventsErrors)

						fetchTokenCallCount := uaaClient.FetchTokenCallCount()
						subscribeCallCount := client.SubscribeToTcpEventsWithMaxRetriesCallCount()

						errorChannel <- errors.New("beep boop im a robot")

						Eventually(logger).Should(gbytes.Say("beep boop im a robot"))

						Eventually(uaaClient.FetchTokenCallCount).Should(BeNumerically(">", fetchTokenCallCount))
						Eventually(client.SubscribeToTcpEventsWithMaxRetriesCallCount).Should(BeNumerically(">", subscribeCallCount))

						Eventually(func() uint64 {
							return sender.GetCounter(SubscribeEventsErrors)
						}).Should(BeNumerically(">", currentSubscribeEventsErrors))
					})
				})

				Context("and the event source fails to subscribe", func() {
					Context("with error other than unauthorized", func() {
						BeforeEach(func() {
							client.SubscribeToTcpEventsWithMaxRetriesStub = func(uint16) (routing_api.TcpEventSource, error) {
								err := errors.New("i failed to subscribe")
								return &fake_routing_api.FakeTcpEventSource{}, err
							}
						})

						It("logs the error and tries again", func() {
							fetchTokenCallCount := uaaClient.FetchTokenCallCount()
							subscribeCallCount := client.SubscribeToTcpEventsWithMaxRetriesCallCount()

							currentSubscribeEventsErrors := sender.GetCounter(SubscribeEventsErrors)

							Eventually(logger).Should(gbytes.Say("i failed to subscribe"))

							Eventually(uaaClient.FetchTokenCallCount).Should(BeNumerically(">", fetchTokenCallCount))
							Eventually(client.SubscribeToTcpEventsWithMaxRetriesCallCount).Should(BeNumerically(">", subscribeCallCount))

							Eventually(func() uint64 {
								return sender.GetCounter(SubscribeEventsErrors)
							}).Should(BeNumerically(">", currentSubscribeEventsErrors))
						})
					})

					Context("with unauthorized error", func() {
						BeforeEach(func() {
							client.SubscribeToTcpEventsWithMaxRetriesStub = func(uint16) (routing_api.TcpEventSource, error) {
								err := errors.New("unauthorized")
								return &fake_routing_api.FakeTcpEventSource{}, err
							}
						})

						It("logs the error and tries again by not using cached access token", func() {
							currentSubscribeEventsErrors := sender.GetCounter(SubscribeEventsErrors)
							Eventually(logger).Should(gbytes.Say("unauthorized"))
							Eventually(uaaClient.FetchTokenCallCount).Should(BeNumerically(">", 2))
							Expect(uaaClient.FetchTokenArgsForCall(0)).To(BeFalse())
							Expect(uaaClient.FetchTokenArgsForCall(1)).To(BeTrue())

							Eventually(func() uint64 {
								return sender.GetCounter(SubscribeEventsErrors)
							}).Should(BeNumerically(">", currentSubscribeEventsErrors))
						})
					})
				})
			})
		})

		Describe("HandleEvent", func() {
			Context("When the event is an Upsert", func() {
				It("registers the route to the registry", func() {
					route := models.NewTcpRouteMapping("fakeRouterGroup", 1001, "1.1.1.1", 2020, 1)
					event := routing_api.TcpEvent{
						Action:          "Upsert",
						TcpRouteMapping: route,
					}

					routeClient.HandleEvent(event)
					Expect(routeTable.UpsertBackendServerKeyCallCount()).To(Equal(1))
				})
			})

			Context("When the event is a DELETE", func() {
				It("unregisters the route from the registry", func() {
					route := models.NewTcpRouteMapping("fakeRouterGroup", 1001, "1.1.1.1", 2020, 1)
					event := routing_api.TcpEvent{
						Action:          "Delete",
						TcpRouteMapping: route,
					}

					routeClient.HandleEvent(event)
					Expect(routeTable.DeleteBackendServerKeyCallCount()).To(Equal(1))
				})
			})

			Context("When the event Action is unkown", func() {
				It("logs and does not call the routing table", func() {
					route := models.NewTcpRouteMapping("fakeRouterGroup", 1001, "1.1.1.1", 2020, 1)
					event := routing_api.TcpEvent{
						Action:          "Yarp",
						TcpRouteMapping: route,
					}

					routeClient.HandleEvent(event)
					Expect(routeTable.DeleteBackendServerKeyCallCount()).To(Equal(0))
					Expect(routeTable.UpsertBackendServerKeyCallCount()).To(Equal(0))
					Eventually(logger).Should(gbytes.Say("unknown-event-action"))
				})
			})
			Context("When the event type is incorrect", func() {
				It("logs a warning", func() {
					event := routing_api.Event{
						Action: "Delete",
						Route:  models.Route{},
					}

					routeClient.HandleEvent(event)
					Expect(routeTable.DeleteBackendServerKeyCallCount()).To(Equal(0))
					Expect(routeTable.UpsertBackendServerKeyCallCount()).To(Equal(0))
					Eventually(logger).Should(gbytes.Say("recieved-wrong-event-type"))
				})
			})
		})
	})
})
