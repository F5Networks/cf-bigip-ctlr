package proxy_test

import (
	"bytes"
	"crypto/tls"
	"net/http/httptest"
	"time"

	fakelogger "github.com/cf-bigip-ctlr/access_log/fakes"
	"github.com/cf-bigip-ctlr/logger"
	"github.com/cf-bigip-ctlr/metrics"
	"github.com/cf-bigip-ctlr/metrics/fakes"
	"github.com/cf-bigip-ctlr/proxy"
	"github.com/cf-bigip-ctlr/proxy/test_helpers"
	"github.com/cf-bigip-ctlr/proxy/utils"
	"github.com/cf-bigip-ctlr/registry"
	"github.com/cf-bigip-ctlr/route"
	"github.com/cf-bigip-ctlr/routeservice"
	"github.com/cf-bigip-ctlr/test_util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
)

var _ = Describe("Proxy Unit tests", func() {
	var (
		proxyObj         proxy.Proxy
		fakeAccessLogger *fakelogger.FakeAccessLogger
		logger           logger.Logger
		resp             utils.ProxyResponseWriter
		combinedReporter metrics.CombinedReporter
	)

	Context("ServeHTTP", func() {
		BeforeEach(func() {
			tlsConfig := &tls.Config{
				CipherSuites:       conf.CipherSuites,
				InsecureSkipVerify: conf.SkipSSLValidation,
			}

			fakeAccessLogger = &fakelogger.FakeAccessLogger{}

			logger = test_util.NewTestZapLogger("test")
			r = registry.NewRouteRegistry(logger, conf, new(fakes.FakeRouteRegistryReporter), "")

			routeServiceConfig := routeservice.NewRouteServiceConfig(
				logger,
				conf.RouteServiceEnabled,
				conf.RouteServiceTimeout,
				crypto,
				cryptoPrev,
				false,
			)
			varz := test_helpers.NullVarz{}
			sender := new(fakes.MetricSender)
			batcher := new(fakes.MetricBatcher)
			proxyReporter := metrics.NewMetricsReporter(sender, batcher)
			combinedReporter = metrics.NewCompositeReporter(varz, proxyReporter)

			conf.HealthCheckUserAgent = "HTTP-Monitor/1.1"
			proxyObj = proxy.NewProxy(logger, fakeAccessLogger, conf, r, combinedReporter,
				routeServiceConfig, tlsConfig, nil)

			r.Register(route.Uri("some-app"), &route.Endpoint{})

			resp = utils.NewProxyResponseWriter(httptest.NewRecorder())
		})

		Context("when backend fails to respond", func() {
			It("logs the error and associated endpoint", func() {
				body := []byte("some body")
				req := test_util.NewRequest("GET", "some-app", "/", bytes.NewReader(body))

				proxyObj.ServeHTTP(resp, req)

				Eventually(logger).Should(Say("route-endpoint"))
				Eventually(logger).Should(Say("error"))
			})
		})

		Context("Log response time", func() {
			It("logs response time for HTTP connections", func() {
				body := []byte("some body")
				req := test_util.NewRequest("GET", "some-app", "/", bytes.NewReader(body))

				proxyObj.ServeHTTP(resp, req)
				Expect(fakeAccessLogger.LogCallCount()).To(Equal(1))
				Expect(fakeAccessLogger.LogArgsForCall(0).FinishedAt).NotTo(Equal(time.Time{}))
			})

			It("logs response time for TCP connections", func() {
				req := test_util.NewRequest("UPGRADE", "some-app", "/", nil)
				req.Header.Set("Upgrade", "tcp")
				req.Header.Set("Connection", "upgrade")

				proxyObj.ServeHTTP(resp, req)
				Expect(fakeAccessLogger.LogCallCount()).To(Equal(1))
				Expect(fakeAccessLogger.LogArgsForCall(0).FinishedAt).NotTo(Equal(time.Time{}))
			})

			It("logs response time for Web Socket connections", func() {
				req := test_util.NewRequest("UPGRADE", "some-app", "/", nil)
				req.Header.Set("Upgrade", "websocket")
				req.Header.Set("Connection", "upgrade")

				proxyObj.ServeHTTP(resp, req)
				Expect(fakeAccessLogger.LogCallCount()).To(Equal(1))
				Expect(fakeAccessLogger.LogArgsForCall(0).FinishedAt).NotTo(Equal(time.Time{}))
			})
		})
	})
})
