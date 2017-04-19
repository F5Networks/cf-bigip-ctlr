package handlers_test

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"github.com/cf-bigip-ctlr/access_log/fakes"
	"github.com/cf-bigip-ctlr/access_log/schema"
	"github.com/cf-bigip-ctlr/handlers"
	"github.com/cf-bigip-ctlr/proxy/utils"
	"github.com/cf-bigip-ctlr/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("AccessLog", func() {
	var (
		handler negroni.Handler

		resp        http.ResponseWriter
		proxyWriter utils.ProxyResponseWriter
		req         *http.Request

		accessLogger      *fakes.FakeAccessLogger
		extraHeadersToLog []string

		nextCalled bool

		reqChan chan *http.Request
	)

	nextHandler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		_, err := ioutil.ReadAll(req.Body)
		Expect(err).NotTo(HaveOccurred())

		rw.WriteHeader(http.StatusTeapot)
		rw.Write([]byte("I'm a little teapot, short and stout."))

		reqChan <- req
		nextCalled = true
	})

	BeforeEach(func() {
		body := bytes.NewBufferString("What are you?")
		req = test_util.NewRequest("GET", "example.com", "/", body)
		resp = httptest.NewRecorder()
		proxyWriter = utils.NewProxyResponseWriter(resp)

		extraHeadersToLog = []string{}

		accessLogger = &fakes.FakeAccessLogger{}

		handler = handlers.NewAccessLog(accessLogger, extraHeadersToLog)

		reqChan = make(chan *http.Request, 1)

		nextCalled = false
	})

	AfterEach(func() {
		Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		close(reqChan)
	})

	It("sets an access log record on the context", func() {
		handler.ServeHTTP(proxyWriter, req, nextHandler)
		var contextReq *http.Request
		Eventually(reqChan).Should(Receive(&contextReq))
		alr := contextReq.Context().Value("AccessLogRecord")
		Expect(alr).ToNot(BeNil())
		Expect(alr).To(BeAssignableToTypeOf(&schema.AccessLogRecord{}))
		accessLog, ok := alr.(*schema.AccessLogRecord)
		Expect(ok).To(BeTrue())
		Expect(accessLog.Request).To(Equal(req))
	})

	It("logs the access log record after all subsequent handlers have run", func() {
		handler.ServeHTTP(proxyWriter, req, nextHandler)

		Expect(accessLogger.LogCallCount()).To(Equal(1))

		alr := accessLogger.LogArgsForCall(0)

		Expect(alr.StartedAt).ToNot(BeZero())
		Expect(alr.Request).To(Equal(req))
		Expect(alr.ExtraHeadersToLog).To(Equal(extraHeadersToLog))
		Expect(alr.FinishedAt).ToNot(BeZero())
		Expect(alr.RequestBytesReceived).To(Equal(13))
		Expect(alr.BodyBytesSent).To(Equal(37))
		Expect(alr.StatusCode).To(Equal(http.StatusTeapot))
	})
})
