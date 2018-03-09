/*
 * Portions Copyright (c) 2017,2018, F5 Networks, Inc.
 */
package main_test

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/route"
	"github.com/F5Networks/cf-bigip-ctlr/test"
	"github.com/F5Networks/cf-bigip-ctlr/test/common"
	"github.com/F5Networks/cf-bigip-ctlr/test_util"

	"code.cloudfoundry.org/localip"
	"github.com/nats-io/nats"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
	. "github.com/onsi/gomega/gexec"
	"github.com/onsi/gomega/ghttp"
	"gopkg.in/yaml.v2"
)

const defaultPruneInterval = 1
const defaultPruneThreshold = 2

var _ = Describe("Router Integration", func() {

	var (
		tmpdir         string
		natsPort       uint16
		natsRunner     *test_util.NATSRunner
		ctlrSession    *Session
		oauthServerURL string
	)

	writeConfig := func(config *config.Config, cfgFile string) {
		cfgBytes, err := yaml.Marshal(config)
		Expect(err).ToNot(HaveOccurred())
		ioutil.WriteFile(cfgFile, cfgBytes, os.ModePerm)
	}

	configDrainSetup := func(cfg *config.Config, pruneInterval, pruneThreshold, drainWait int) {
		// ensure the threshold is longer than the interval that we check,
		// because we set the route's timestamp to time.Now() on the interval
		// as part of pausing
		cfg.PruneStaleDropletsInterval = time.Duration(pruneInterval) * time.Second
		cfg.DropletStaleThreshold = time.Duration(pruneThreshold) * time.Second
		cfg.StartResponseDelayInterval = 1 * time.Second
		cfg.EndpointTimeout = 5 * time.Second
		cfg.DrainTimeout = 1 * time.Second
		cfg.DrainWait = time.Duration(drainWait) * time.Second
		cfg.RouteMode = "all"
	}

	createConfig := func(
		cfgFile string,
		statusPort uint16,
		pruneInterval int,
		pruneThreshold int,
		drainWait int,
		suspendPruning bool,
		natsPorts ...uint16,
	) *config.Config {
		cfg := test_util.SpecConfig(statusPort, natsPorts...)

		configDrainSetup(cfg, pruneInterval, pruneThreshold, drainWait)

		cfg.SuspendPruningIfNatsUnavailable = suspendPruning
		caCertsPath := filepath.Join("test", "assets", "certs", "uaa-ca.pem")
		caCertsPath, err := filepath.Abs(caCertsPath)
		Expect(err).ToNot(HaveOccurred())
		cfg.LoadBalancerHealthyThreshold = 0
		cfg.OAuth = config.OAuthConfig{
			TokenEndpoint:     "127.0.0.1",
			Port:              8443,
			ClientName:        "client-id",
			ClientSecret:      "client-secret",
			SkipSSLValidation: false,
			CACerts:           caCertsPath,
		}
		cfg.BigIP = config.BigIPConfig{
			URL:          "http://bigip.example.com",
			User:         "test",
			Pass:         "insecure",
			Partitions:   []string{"cloud-foundry"},
			ExternalAddr: "127.0.0.1",
			DriverCmd:    "testdata/fake_driver.py",
			Tier2IPRange: "10.0.0.1/32",
		}

		writeConfig(cfg, cfgFile)
		return cfg
	}

	startCtlrSession := func(cfgFile string) *Session {
		ctlrCmd := exec.Command(ctlrPath, "-c", cfgFile)
		session, err := Start(ctlrCmd, GinkgoWriter, GinkgoWriter)
		Expect(err).ToNot(HaveOccurred())
		var eventsSessionLogs []byte
		Eventually(func() string {
			logAdd, err := ioutil.ReadAll(session.Out)
			Expect(err).ToNot(HaveOccurred(), "ctlr session closed")
			eventsSessionLogs = append(eventsSessionLogs, logAdd...)
			return string(eventsSessionLogs)
		}, 70*time.Second).Should(SatisfyAll(
			ContainSubstring(`starting`),
			MatchRegexp(`Successfully-connected-to-nats.*localhost:\d+`),
			ContainSubstring(`f5router-started`),
			ContainSubstring(`f5router-driver-started`),
		))
		ctlrSession = session
		return session
	}

	stopCtlr := func(ctlrSession *Session) {
		err := ctlrSession.Command.Process.Signal(syscall.SIGTERM)
		Expect(err).ToNot(HaveOccurred())
		Eventually(ctlrSession, 5).Should(Exit(0))
	}

	BeforeEach(func() {
		var err error
		tmpdir, err = ioutil.TempDir("", "ctlr")
		Expect(err).ToNot(HaveOccurred())

		natsPort = test_util.NextAvailPort()
		natsRunner = test_util.NewNATSRunner(int(natsPort))
		natsRunner.Start()
		oauthServerURL = oauthServer.Addr()
	})

	AfterEach(func() {
		if natsRunner != nil {
			natsRunner.Stop()
		}

		os.RemoveAll(tmpdir)

		if ctlrSession != nil && ctlrSession.ExitCode() == -1 {
			stopCtlr(ctlrSession)
		}
	})

	Context("When Dropsonde is misconfigured", func() {
		It("fails to start", func() {
			statusPort := test_util.NextAvailPort()

			cfgFile := filepath.Join(tmpdir, "config.yml")
			config := createConfig(cfgFile, statusPort, defaultPruneInterval, defaultPruneThreshold, 0, false, natsPort)
			config.Logging.MetronAddress = ""
			writeConfig(config, cfgFile)

			ctlrCmd := exec.Command(ctlrPath, "-c", cfgFile)
			ctlrSession, _ = Start(ctlrCmd, GinkgoWriter, GinkgoWriter)
			Eventually(ctlrSession, 5*time.Second).Should(Exit(1))
		})
	})

	It("logs component logs", func() {
		statusPort := test_util.NextAvailPort()
		cfgFile := filepath.Join(tmpdir, "config.yml")
		createConfig(cfgFile, statusPort, defaultPruneInterval, defaultPruneThreshold, 0, false, natsPort)

		ctlrSession = startCtlrSession(cfgFile)

		Eventually(ctlrSession.Out.Contents).Should(ContainSubstring("Component Controller registered successfully"))
	})

	It("has Nats connectivity", func() {
		localIP, err := localip.LocalIP()
		Expect(err).ToNot(HaveOccurred())

		statusPort := test_util.NextAvailPort()

		cfgFile := filepath.Join(tmpdir, "config.yml")
		config := createConfig(cfgFile, statusPort, defaultPruneInterval, defaultPruneThreshold, 0, false, natsPort)

		ctlrSession = startCtlrSession(cfgFile)

		mbusClient, err := newMessageBus(config)
		Expect(err).ToNot(HaveOccurred())

		zombieApp := test.NewGreetApp([]route.Uri{"zombie.vcap.me"}, mbusClient, nil)
		zombieApp.Listen()

		runningApp := test.NewGreetApp([]route.Uri{"innocent.bystander.vcap.me"}, mbusClient, nil)
		runningApp.AddHandler("/some-path", func(w http.ResponseWriter, r *http.Request) {
			defer GinkgoRecover()
			w.WriteHeader(http.StatusOK)
		})

		runningApp.Listen()

		routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)

		Eventually(func() bool { return appRegistered(routesUri, zombieApp) }).Should(BeTrue())
		Eventually(func() bool { return appRegistered(routesUri, runningApp) }).Should(BeTrue())

		heartbeatInterval := 200 * time.Millisecond
		zombieTicker := time.NewTicker(heartbeatInterval)
		runningTicker := time.NewTicker(heartbeatInterval)

		go func() {
			for {
				select {
				case <-zombieTicker.C:
					zombieApp.Register()
				case <-runningTicker.C:
					runningApp.Register()
				}
			}
		}()

		zombieApp.VerifyAppStatus(200)
		runningApp.VerifyAppStatus(200)

		// Give enough time to register multiple times
		time.Sleep(heartbeatInterval * 3)

		// kill registration ticker => kill app (must be before stopping NATS since app.Register is fake and queues messages in memory)
		zombieTicker.Stop()

		natsRunner.Stop()

		staleCheckInterval := config.PruneStaleDropletsInterval
		staleThreshold := config.DropletStaleThreshold
		// Give router time to make a bad decision (i.e. prune routes)
		time.Sleep(10 * (staleCheckInterval + staleThreshold))

		// While NATS is down all routes should go down
		Eventually(func() bool { return appUnregistered(routesUri, zombieApp) }).Should(BeTrue())
		Eventually(func() bool { return appUnregistered(routesUri, runningApp) }).Should(BeTrue())

		natsRunner.Start()

		// After NATS starts up the zombie should stay gone
		Eventually(func() bool { return appUnregistered(routesUri, zombieApp) }).Should(BeTrue())
		Eventually(func() bool { return appRegistered(routesUri, runningApp) }).Should(BeTrue())

		uri := fmt.Sprintf("http://%s:%d/%s", "innocent.bystander.vcap.me", runningApp.Port(), "some-path")
		_, err = http.Get(uri)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("when nats server shuts down and comes back up", func() {
		It("should not panic, log the disconnection, and reconnect", func() {
			localIP, err := localip.LocalIP()
			Expect(err).ToNot(HaveOccurred())

			statusPort := test_util.NextAvailPort()

			cfgFile := filepath.Join(tmpdir, "config.yml")
			config := createConfig(cfgFile, statusPort, defaultPruneInterval, defaultPruneThreshold, 0, false, natsPort)
			config.NatsClientPingInterval = 1 * time.Second
			writeConfig(config, cfgFile)
			ctlrSession = startCtlrSession(cfgFile)

			mbusClient, err := newMessageBus(config)
			Expect(err).ToNot(HaveOccurred())

			zombieApp := test.NewGreetApp([]route.Uri{"zombie.vcap.me"}, mbusClient, nil)
			zombieApp.Listen()

			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)

			Eventually(func() bool { return appRegistered(routesUri, zombieApp) }).Should(BeTrue())

			heartbeatInterval := 200 * time.Millisecond
			zombieTicker := time.NewTicker(heartbeatInterval)

			go func() {
				for {
					select {
					case <-zombieTicker.C:
						zombieApp.Register()
					}
				}
			}()

			zombieApp.VerifyAppStatus(200)

			natsRunner.Stop()

			Eventually(ctlrSession).Should(Say("nats-connection-disconnected"))
			Eventually(ctlrSession, time.Second*25).Should(Say("nats-connection-still-disconnected"))
			natsRunner.Start()
			Eventually(ctlrSession, time.Second*5).Should(Say("nats-connection-reconnected"))
			Consistently(ctlrSession, time.Second*25).ShouldNot(Say("nats-connection-still-disconnected"))
			Consistently(ctlrSession.ExitCode, 150*time.Second).ShouldNot(Equal(1))
		})
	})

	Context("multiple nats server", func() {
		var (
			config         *config.Config
			cfgFile        string
			natsPort2      uint16
			statusPort     uint16
			natsRunner2    *test_util.NATSRunner
			pruneInterval  int
			pruneThreshold int
		)

		BeforeEach(func() {
			natsPort2 = test_util.NextAvailPort()
			natsRunner2 = test_util.NewNATSRunner(int(natsPort2))

			statusPort = test_util.NextAvailPort()

			cfgFile = filepath.Join(tmpdir, "config.yml")
			pruneInterval = 2
			pruneThreshold = 10
			config = createConfig(cfgFile, statusPort, pruneInterval, pruneThreshold, 0, false, natsPort, natsPort2)
		})

		AfterEach(func() {
			natsRunner2.Stop()
		})

		JustBeforeEach(func() {
			ctlrSession = startCtlrSession(cfgFile)
		})

		It("fails over to second nats server before pruning", func() {
			localIP, err := localip.LocalIP()
			Expect(err).ToNot(HaveOccurred())

			mbusClient, err := newMessageBus(config)
			Expect(err).ToNot(HaveOccurred())

			runningApp := test.NewGreetApp([]route.Uri{"demo.vcap.me"}, mbusClient, nil)
			runningApp.Listen()

			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)

			Eventually(func() bool { return appRegistered(routesUri, runningApp) }).Should(BeTrue())

			heartbeatInterval := 200 * time.Millisecond
			runningTicker := time.NewTicker(heartbeatInterval)

			go func() {
				for {
					select {
					case <-runningTicker.C:
						runningApp.Register()
					}
				}
			}()

			runningApp.VerifyAppStatus(200)

			// Give enough time to register multiple times
			time.Sleep(heartbeatInterval * 3)

			natsRunner.Stop()
			natsRunner2.Start()

			staleCheckInterval := config.PruneStaleDropletsInterval
			staleThreshold := config.DropletStaleThreshold
			// Give router time to make a bad decision (i.e. prune routes)
			sleepTime := (2 * staleCheckInterval) + (2 * staleThreshold)
			time.Sleep(sleepTime)

			// Expect not to have pruned the routes as it fails over to next NAT server
			runningApp.VerifyAppStatus(200)

			natsRunner.Start()

		})

		Context("when suspend_pruning_if_nats_unavailable enabled", func() {

			BeforeEach(func() {
				natsPort2 = test_util.NextAvailPort()
				natsRunner2 = test_util.NewNATSRunner(int(natsPort2))

				statusPort = test_util.NextAvailPort()

				cfgFile = filepath.Join(tmpdir, "config.yml")
				pruneInterval = 2
				pruneThreshold = 10
				suspendPruningIfNatsUnavailable := true
				config = createConfig(cfgFile, statusPort, pruneInterval, pruneThreshold, 0, suspendPruningIfNatsUnavailable, natsPort, natsPort2)
			})

			It("does not prune routes when nats is unavailable", func() {
				localIP, err := localip.LocalIP()
				Expect(err).ToNot(HaveOccurred())

				mbusClient, err := newMessageBus(config)
				Expect(err).ToNot(HaveOccurred())

				runningApp := test.NewGreetApp([]route.Uri{"demo.vcap.me"}, mbusClient, nil)
				runningApp.Listen()

				routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)

				Eventually(func() bool { return appRegistered(routesUri, runningApp) }).Should(BeTrue())

				heartbeatInterval := 200 * time.Millisecond
				runningTicker := time.NewTicker(heartbeatInterval)

				go func() {
					for {
						select {
						case <-runningTicker.C:
							runningApp.Register()
						}
					}
				}()

				runningApp.VerifyAppStatus(200)

				// Give enough time to register multiple times
				time.Sleep(heartbeatInterval * 3)

				natsRunner.Stop()

				staleCheckInterval := config.PruneStaleDropletsInterval
				staleThreshold := config.DropletStaleThreshold

				// Give router time to make a bad decision (i.e. prune routes)
				sleepTime := (2 * staleCheckInterval) + (2 * staleThreshold)
				time.Sleep(sleepTime)

				// Expect not to have pruned the routes after nats goes away
				runningApp.VerifyAppStatus(200)
			})
		})
	})

	Context("when no oauth config is specified", func() {
		Context("and routing api is disabled", func() {
			It("is able to start up", func() {
				statusPort := test_util.NextAvailPort()

				cfgFile := filepath.Join(tmpdir, "config.yml")
				cfg := createConfig(cfgFile, statusPort, defaultPruneInterval, defaultPruneThreshold, 0, false, natsPort)
				cfg.OAuth = config.OAuthConfig{}
				writeConfig(cfg, cfgFile)

				// The process should not have any error.
				session := startCtlrSession(cfgFile)
				stopCtlr(session)
			})
		})
	})

	Context("when routing api is disabled", func() {
		var (
			cfgFile string
			cfg     *config.Config
		)

		BeforeEach(func() {
			statusPort := test_util.NextAvailPort()

			cfgFile = filepath.Join(tmpdir, "config.yml")

			cfg = createConfig(cfgFile, statusPort, defaultPruneInterval, defaultPruneThreshold, 0, false, natsPort)
			writeConfig(cfg, cfgFile)
		})

		It("doesn't start the route fetcher", func() {
			session := startCtlrSession(cfgFile)
			Eventually(session).ShouldNot(Say("setting-up-routing-api"))
			stopCtlr(session)
		})

	})

	Context("when the routing api is enabled", func() {
		var (
			config           *config.Config
			routingApiServer *ghttp.Server
			cfgFile          string
			responseBytes    []byte
			verifyAuthHeader http.HandlerFunc
		)

		BeforeEach(func() {
			statusPort := test_util.NextAvailPort()

			cfgFile = filepath.Join(tmpdir, "config.yml")
			config = createConfig(cfgFile, statusPort, defaultPruneInterval, defaultPruneThreshold, 0, false, natsPort)

			responseBytes = []byte(`[{
				"guid": "abc123",
				"name": "router_group_name",
				"type": "http"
			}]`)
		})

		JustBeforeEach(func() {
			routingApiServer = ghttp.NewUnstartedServer()
			routingApiServer.RouteToHandler(
				"GET", "/routing/v1/router_groups", ghttp.CombineHandlers(
					verifyAuthHeader,
					func(w http.ResponseWriter, req *http.Request) {
						if req.URL.Query().Get("name") != "router_group_name" {
							ghttp.RespondWith(http.StatusNotFound, []byte(`error: router group not found`))(w, req)
							return
						}
						ghttp.RespondWith(http.StatusOK, responseBytes)(w, req)
					},
				),
			)
			path, err := regexp.Compile("/routing/v1/.*")
			Expect(err).ToNot(HaveOccurred())
			routingApiServer.RouteToHandler(
				"GET", path, ghttp.CombineHandlers(
					verifyAuthHeader,
					ghttp.RespondWith(http.StatusOK, `[{}]`),
				),
			)
			routingApiServer.AppendHandlers(
				func(rw http.ResponseWriter, req *http.Request) {
					defer GinkgoRecover()
					Expect(true).To(
						BeFalse(),
						fmt.Sprintf(
							"Received unhandled request: %s %s",
							req.Method,
							req.URL.RequestURI(),
						),
					)
				},
			)
			routingApiServer.Start()

			config.RoutingApi.Uri, config.RoutingApi.Port = uriAndPort(routingApiServer.URL())

		})
		AfterEach(func() {
			routingApiServer.Close()
		})

		Context("when the routing api auth is disabled ", func() {
			BeforeEach(func() {
				verifyAuthHeader = func(rw http.ResponseWriter, r *http.Request) {}
			})
			It("uses the no-op token fetcher", func() {
				config.RoutingApi.AuthDisabled = true
				writeConfig(config, cfgFile)

				// note, this will start with routing api, but will not be able to connect
				session := startCtlrSession(cfgFile)
				defer stopCtlr(session)
				Eventually(ctlrSession.Out.Contents).Should(ContainSubstring("using-noop-token-fetcher"))
			})
		})

		Context("when the routing api auth is enabled (default)", func() {
			Context("when uaa is available on tls port", func() {
				BeforeEach(func() {
					verifyAuthHeader = func(rw http.ResponseWriter, req *http.Request) {
						defer GinkgoRecover()
						Expect(req.Header.Get("Authorization")).ToNot(BeEmpty())
						Expect(req.Header.Get("Authorization")).ToNot(
							Equal("bearer"),
							fmt.Sprintf(
								`"bearer" shouldn't be the only string in the "Authorization" header. Req: %s %s`,
								req.Method,
								req.URL.RequestURI(),
							),
						)
					}
					config.OAuth.TokenEndpoint, config.OAuth.Port = hostnameAndPort(oauthServerURL)
				})

				It("fetches a token from uaa", func() {
					writeConfig(config, cfgFile)

					session := startCtlrSession(cfgFile)
					defer stopCtlr(session)
					Eventually(ctlrSession.Out.Contents).Should(ContainSubstring("started-fetching-token"))
				})
				It("does not exit", func() {
					config.RouterGroupName = "router_group_name"
					writeConfig(config, cfgFile)

					ctlrCmd := exec.Command(ctlrPath, "-c", cfgFile)
					session, err := Start(ctlrCmd, GinkgoWriter, GinkgoWriter)
					Expect(err).ToNot(HaveOccurred())
					defer session.Terminate()
					Consistently(session, 5*time.Second).ShouldNot(Exit(1))
				})
				Context("when a router group is provided", func() {
					It("logs the router group name and the guid", func() {
						config.RouterGroupName = "router_group_name"
						writeConfig(config, cfgFile)
						ctlrCmd := exec.Command(ctlrPath, "-c", cfgFile)
						session, err := Start(ctlrCmd, GinkgoWriter, GinkgoWriter)
						Expect(err).ToNot(HaveOccurred())
						expectedLog := `retrieved-router-group","source":"cf-bigip-ctlr.stdout","data":{"router-group":"router_group_name","router-group-guid":"abc123"}`
						Eventually(session).Should(Say(expectedLog))
					})
					Context("when given an invalid router group", func() {
						It("does exit with status 1", func() {
							config.RouterGroupName = "invalid_router_group"
							writeConfig(config, cfgFile)

							ctlrCmd := exec.Command(ctlrPath, "-c", cfgFile)
							session, err := Start(ctlrCmd, GinkgoWriter, GinkgoWriter)
							Expect(err).ToNot(HaveOccurred())
							defer session.Terminate()
							Eventually(session, 30*time.Second).Should(Say("router group not found"))
							Eventually(session, 5*time.Second).Should(Exit(1))
						})
					})
					Context("when the given router_group matches a tcp router group", func() {
						BeforeEach(func() {
							responseBytes = []byte(`[{
								"guid": "abc123",
								"name": "router_group_name",
								"reservable_ports":"1024-65535",
								"type": "tcp"
							}]`)
						})

						It("does exit with status 1", func() {
							config.RouterGroupName = "router_group_name"
							writeConfig(config, cfgFile)

							ctlrCmd := exec.Command(ctlrPath, "-c", cfgFile)
							session, err := Start(ctlrCmd, GinkgoWriter, GinkgoWriter)
							Expect(err).ToNot(HaveOccurred())
							defer session.Terminate()
							Eventually(session, 30*time.Second).Should(Say("expected-router-group-type-http"))
							Eventually(session, 5*time.Second).Should(Exit(1))
						})
					})
				})
				Context("when a router group is not provided", func() {
					It("logs the router group name and guid as '-'", func() {
						writeConfig(config, cfgFile)
						ctlrCmd := exec.Command(ctlrPath, "-c", cfgFile)
						session, err := Start(ctlrCmd, GinkgoWriter, GinkgoWriter)
						Expect(err).ToNot(HaveOccurred())
						expectedLog := `retrieved-router-group","source":"cf-bigip-ctlr.stdout","data":{"router-group":"-","router-group-guid":"-"}`
						Eventually(session).Should(Say(expectedLog))
					})
				})
			})

			Context("when the uaa is not available", func() {
				It("ctlr exits with non-zero code", func() {
					writeConfig(config, cfgFile)

					ctlrCmd := exec.Command(ctlrPath, "-c", cfgFile)
					session, err := Start(ctlrCmd, GinkgoWriter, GinkgoWriter)
					Expect(err).ToNot(HaveOccurred())
					defer session.Terminate()
					Eventually(session, 30*time.Second).Should(Say("unable-to-fetch-token"))
					Eventually(session, 5*time.Second).Should(Exit(1))
				})
			})

			Context("when routing api is not available", func() {
				BeforeEach(func() {
					config.OAuth.TokenEndpoint, config.OAuth.Port = hostnameAndPort(oauthServerURL)
				})
				It("ctlr exits with non-zero code", func() {
					routingApiServer.Close()
					writeConfig(config, cfgFile)

					ctlrCmd := exec.Command(ctlrPath, "-c", cfgFile)
					session, err := Start(ctlrCmd, GinkgoWriter, GinkgoWriter)
					Expect(err).ToNot(HaveOccurred())
					defer session.Terminate()
					Eventually(session, 30*time.Second).Should(Say("routing-api-connection-failed"))
					Eventually(session, 5*time.Second).Should(Exit(1))
				})
			})
		})

		Context("when tls for uaa is disabled", func() {
			It("fails fast", func() {
				config.OAuth.Port = -1
				writeConfig(config, cfgFile)

				ctlrCmd := exec.Command(ctlrPath, "-c", cfgFile)
				session, err := Start(ctlrCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).ToNot(HaveOccurred())
				defer session.Terminate()
				Eventually(session, 30*time.Second).Should(Say("tls-not-enabled"))
				Eventually(session, 5*time.Second).Should(Exit(1))
			})
		})

	})

	Context("service broker", func() {
		var (
			config  *config.Config
			cfgFile string
		)

		BeforeEach(func() {
			statusPort := test_util.NextAvailPort()

			cfgFile = filepath.Join(tmpdir, "config.yml")
			config = createConfig(cfgFile, statusPort, defaultPruneInterval, defaultPruneThreshold, 0, false, natsPort)
		})
		It("fails to start when SERVICE_BROKER_CONFIG is missing", func() {
			config.BrokerMode = true
			writeConfig(config, cfgFile)

			ctlrCmd := exec.Command(ctlrPath, "-c", cfgFile)
			session, err := Start(ctlrCmd, GinkgoWriter, GinkgoWriter)
			Expect(err).ToNot(HaveOccurred())
			defer session.Terminate()
			Eventually(session, 30*time.Second).Should(Say("process-broker-plan-error"))
			Eventually(session, 5*time.Second).Should(Exit(1))
		})

		Context("with SERVICE_BROKER_CONFIG", func() {
			AfterEach(func() {
				os.Unsetenv("SERVICE_BROKER_CONFIG")
			})

			It("fails to start with an invalid plan", func() {
				os.Setenv("SERVICE_BROKER_CONFIG",
					`{"plans":[]}`)
				config.BrokerMode = true
				writeConfig(config, cfgFile)

				ctlrCmd := exec.Command(ctlrPath, "-c", cfgFile)
				session, err := Start(ctlrCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).ToNot(HaveOccurred())
				defer session.Terminate()
				Eventually(session, 30*time.Second).Should(Say("Plans failed schema validation"))
				Eventually(session, 5*time.Second).Should(Exit(1))
			})
		})
	})
})

func uriAndPort(url string) (string, int) {
	parts := strings.Split(url, ":")
	uri := strings.Join(parts[0:2], ":")
	port, _ := strconv.Atoi(parts[2])
	return uri, port
}

func hostnameAndPort(url string) (string, int) {
	parts := strings.Split(url, ":")
	hostname := parts[0]
	port, _ := strconv.Atoi(parts[1])
	return hostname, port
}
func newMessageBus(c *config.Config) (*nats.Conn, error) {
	natsMembers := make([]string, len(c.Nats))
	options := nats.DefaultOptions
	for _, info := range c.Nats {
		uri := url.URL{
			Scheme: "nats",
			User:   url.UserPassword(info.User, info.Pass),
			Host:   fmt.Sprintf("%s:%d", info.Host, info.Port),
		}
		natsMembers = append(natsMembers, uri.String())
	}
	options.Servers = natsMembers
	return options.Connect()
}

func appRegistered(routesUri string, app *common.TestApp) bool {
	routeFound, err := routeExists(routesUri, string(app.Urls()[0]))
	return err == nil && routeFound
}

func appUnregistered(routesUri string, app *common.TestApp) bool {
	routeFound, err := routeExists(routesUri, string(app.Urls()[0]))
	return err == nil && !routeFound
}

func routeExists(routesEndpoint, routeName string) (bool, error) {
	resp, err := http.Get(routesEndpoint)
	if err != nil {
		fmt.Println("Failed to get from routes endpoint")
		return false, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		bytes, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		Expect(err).ToNot(HaveOccurred())
		routes := make(map[string]interface{})
		err = json.Unmarshal(bytes, &routes)
		Expect(err).ToNot(HaveOccurred())

		_, found := routes[routeName]
		return found, nil

	default:
		return false, errors.New("Didn't get an OK response")
	}
}

func setupTlsServer() *ghttp.Server {
	oauthServer := ghttp.NewUnstartedServer()

	caCertsPath := path.Join("test", "assets", "certs")
	caCertsPath, err := filepath.Abs(caCertsPath)
	Expect(err).ToNot(HaveOccurred())

	public := filepath.Join(caCertsPath, "server.pem")
	private := filepath.Join(caCertsPath, "server.key")
	cert, err := tls.LoadX509KeyPair(public, private)
	Expect(err).ToNot(HaveOccurred())

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA},
	}
	oauthServer.HTTPTestServer.TLS = tlsConfig
	oauthServer.AllowUnhandledRequests = true
	oauthServer.UnhandledRequestStatusCode = http.StatusOK

	publicKey := "-----BEGIN PUBLIC KEY-----\\n" +
		"MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDHFr+KICms+tuT1OXJwhCUmR2d\\n" +
		"KVy7psa8xzElSyzqx7oJyfJ1JZyOzToj9T5SfTIq396agbHJWVfYphNahvZ/7uMX\\n" +
		"qHxf+ZH9BL1gk9Y6kCnbM5R60gfwjyW1/dQPjOzn9N394zd2FJoFHwdq9Qs0wBug\\n" +
		"spULZVNRxq7veq/fzwIDAQAB\\n" +
		"-----END PUBLIC KEY-----"

	data := fmt.Sprintf("{\"alg\":\"rsa\", \"value\":\"%s\"}", publicKey)
	oauthServer.RouteToHandler("GET", "/token_key",
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/token_key"),
			ghttp.RespondWith(http.StatusOK, data)),
	)
	oauthServer.RouteToHandler("POST", "/oauth/token",
		func(w http.ResponseWriter, req *http.Request) {
			jsonBytes := []byte(`{"access_token":"some-token", "expires_in":10}`)
			w.Write(jsonBytes)
		})
	return oauthServer
}

func newTlsListener(listener net.Listener) net.Listener {
	caCertsPath := path.Join("test", "assets", "certs")
	caCertsPath, err := filepath.Abs(caCertsPath)
	Expect(err).ToNot(HaveOccurred())

	public := filepath.Join(caCertsPath, "server.pem")
	private := filepath.Join(caCertsPath, "server.key")
	cert, err := tls.LoadX509KeyPair(public, private)
	Expect(err).ToNot(HaveOccurred())

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA},
	}

	return tls.NewListener(listener, tlsConfig)
}
