package health_test

import (
	"github.com/cf-bigip-ctlr/common/health"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Healthz", func() {
	It("has a Value", func() {
		healthz := &health.Healthz{}
		ok := healthz.Value()
		Expect(ok).To(Equal("ok"))
	})
})
