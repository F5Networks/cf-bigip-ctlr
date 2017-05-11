package main_test

import (
	"crypto/tls"
	"strconv"

	"github.com/cf-bigip-ctlr/access_log"
	"github.com/cf-bigip-ctlr/config"
	"github.com/cf-bigip-ctlr/metrics"
	"github.com/cf-bigip-ctlr/metrics/fakes"
	"github.com/cf-bigip-ctlr/proxy"
	"github.com/cf-bigip-ctlr/registry"
	"github.com/cf-bigip-ctlr/route"
	"github.com/cf-bigip-ctlr/routeservice"
	"github.com/cf-bigip-ctlr/test_util"
	"github.com/cf-bigip-ctlr/varz"

	"code.cloudfoundry.org/routing-api/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("AccessLogRecord", func() {
	Measure("Register", func(b Benchmarker) {
		sender := new(fakes.MetricSender)
		batcher := new(fakes.MetricBatcher)
		metricsReporter := metrics.NewMetricsReporter(sender, batcher)
		logger := test_util.NewTestZapLogger("test")
		c := config.DefaultConfig()
		r := registry.NewRouteRegistry(logger, c, new(fakes.FakeRouteRegistryReporter), "")
		combinedReporter := metrics.NewCompositeReporter(varz.NewVarz(r), metricsReporter)
		accesslog, err := access_log.CreateRunningAccessLogger(logger, c)
		Expect(err).ToNot(HaveOccurred())

		proxy.NewProxy(logger, accesslog, c, r, combinedReporter, &routeservice.RouteServiceConfig{},
			&tls.Config{}, nil)

		b.Time("RegisterTime", func() {
			for i := 0; i < 1000; i++ {
				str := strconv.Itoa(i)
				r.Register(
					route.Uri("bench.vcap.me."+str),
					route.NewEndpoint("", "localhost", uint16(i), "", "", nil, -1, "", models.ModificationTag{}),
				)
			}
		})
	}, 10)

})
