/*
 * Portions Copyright (c) 2017, F5 Networks, Inc.
 */

package common_test

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	. "github.com/F5Networks/cf-bigip-ctlr/common"
	"github.com/F5Networks/cf-bigip-ctlr/common/health"
	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/handlers"
	"github.com/F5Networks/cf-bigip-ctlr/test_util"

	"code.cloudfoundry.org/localip"
	"github.com/nats-io/nats"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

type MarshalableValue struct {
	Value map[string]string
}

func (m *MarshalableValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.Value)
}

var _ = Describe("Component", func() {
	var (
		component   *VcapComponent
		varz        *health.Varz
		heartbeatOK int32
		logger      *test_util.TestZapLogger
	)

	BeforeEach(func() {
		port, err := localip.LocalPort()
		Expect(err).ToNot(HaveOccurred())

		atomic.StoreInt32(&heartbeatOK, 0)
		logger = test_util.NewTestZapLogger("router-test")
		hc := handlers.NewHealthcheck(&heartbeatOK, logger)

		varz = &health.Varz{
			GenericVarz: health.GenericVarz{
				Host: fmt.Sprintf("127.0.0.1:%d", port),
				Credentials: []string{"username", "password"},
			},
		}
		component = &VcapComponent{
			Config: config.DefaultConfig(),
			Varz:   varz,
			Health: hc,
		}
	})

	AfterEach(func() {
		if nil != logger {
			logger.Close()
		}
	})

	It("prevents unauthorized access", func() {
		path := "/test"

		component.InfoRoutes = map[string]json.Marshaler{
			path: &MarshalableValue{Value: map[string]string{"key": "value"}},
		}
		serveComponent(component)

		req := buildRequest(component, path, "GET", nil)
		code, _, _ := doRequest(req)
		Expect(code).To(Equal(401))

		req = buildRequest(component, path, "GET", nil)
		req.SetBasicAuth("username", "incorrect-password")
		code, _, _ = doRequest(req)
		Expect(code).To(Equal(401))

		req = buildRequest(component, path, "GET", nil)
		req.SetBasicAuth("incorrect-username", "password")
		code, _, _ = doRequest(req)
		Expect(code).To(Equal(401))
	})

	It("allows multiple info routes", func() {
		path1 := "/test1"
		path2 := "/test2"

		component.InfoRoutes = map[string]json.Marshaler{
			path1: &MarshalableValue{Value: map[string]string{"key": "value1"}},
			path2: &MarshalableValue{Value: map[string]string{"key": "value2"}},
		}
		serveComponent(component)

		//access path1
		req := buildRequest(component, path1, "GET", nil)
		req.SetBasicAuth("username", "password")

		code, header, body := doRequest(req)
		Expect(code).To(Equal(200))
		Expect(header.Get("Content-Type")).To(Equal("application/json"))
		Expect(body).To(Equal(`{"key":"value1"}` + "\n"))

		//access path2
		req = buildRequest(component, path2, "GET", nil)
		req.SetBasicAuth("username", "password")

		code, header, body = doRequest(req)
		Expect(code).To(Equal(200))
		Expect(header.Get("Content-Type")).To(Equal("application/json"))
		Expect(body).To(Equal(`{"key":"value2"}` + "\n"))
	})

	It("allows authorized access", func() {
		path := "/test"

		component.InfoRoutes = map[string]json.Marshaler{
			path: &MarshalableValue{Value: map[string]string{"key": "value"}},
		}
		serveComponent(component)

		req := buildRequest(component, path, "GET", nil)
		req.SetBasicAuth("username", "password")

		code, header, body := doRequest(req)
		Expect(code).To(Equal(200))
		Expect(header.Get("Content-Type")).To(Equal("application/json"))
		Expect(body).To(Equal(`{"key":"value"}` + "\n"))
	})

	It("does not allow unauthorized access to broker", func() {
		data := []string{
			"/v2/catalog" + ":" + "GET",
			"/v2/service_instances/test_instance" + ":" + "PUT",
			"/v2/service_instances/test_instance" + ":" + "DELETE",
			"/v2/service_instances/test_instance" + ":" + "PATCH",
			"/v2/service_instances/test_instance/last_operation" + ":" + "GET",
			"/v2/service_instances/test_instance/service_bindings/test_bind" + ":" + "GET",
			"/v2/service_instances/test_instance/service_bindings/test_bind" + ":" + "DELETE",
		}

		serveComponent(component)

		for idx := range data {
			splits := strings.Split(data[idx], ":")
			path := splits[0]
			method := splits[1]
			reader := (io.Reader)(nil)
			if method == "PATCH" || method == "PUT" {
				reader = strings.NewReader(`{}`)
			}

			req := buildRequest(component, path, method, reader)
			req.SetBasicAuth("incorrect-username", "password")
			code, _, _ := doRequest(req)
			Expect(code).To(Equal(401))

			req = buildRequest(component, path, method, reader)
			req.SetBasicAuth("username", "incorrect-password")
			code, _, _ = doRequest(req)
			Expect(code).To(Equal(401))
		}
	})

	It("allows authorized access to broker catalog", func() {
		path := "/v2/catalog"

		serveComponent(component)

		req := buildRequest(component, path, "GET", nil)
		req.SetBasicAuth("username", "password")

		code, header, body := doRequest(req)
		Expect(code).To(Equal(200))
		Expect(header.Get("Content-Type")).To(Equal("application/json"))
		Expect(body).To(Equal(`{"services":[{"id":"","name":"","description":"","bindable":true,"tags":["f5"],"plan_updateable":false,"plans":[],"metadata":{"providerDisplayName":"F5 Service Broker"}}]}` + "\n"))
	})

	It("allows authorized access to broker provision", func() {
		path := "/v2/service_instances/test_instance"

		serveComponent(component)

		req := buildRequest(component, path, "PUT", strings.NewReader(`{}`))
		req.SetBasicAuth("username", "password")

		code, header, body := doRequest(req)
		Expect(code).To(Equal(201))
		Expect(header.Get("Content-Type")).To(Equal("application/json"))
		Expect(body).To(Equal(`{}` + "\n"))
	})

	It("allows authorized access to broker deprovision", func() {
		path := "/v2/service_instances/test_instance"

		serveComponent(component)

		req := buildRequest(component, path, "DELETE", nil)
		req.SetBasicAuth("username", "password")

		code, header, body := doRequest(req)
		Expect(code).To(Equal(200))
		Expect(header.Get("Content-Type")).To(Equal("application/json"))
		Expect(body).To(Equal(`{}` + "\n"))
	})

	It("allows authorized access to broker update", func() {
		path := "/v2/service_instances/test_instance"

		serveComponent(component)

		req := buildRequest(component, path, "PATCH", strings.NewReader(`{}`))
		req.SetBasicAuth("username", "password")

		code, header, body := doRequest(req)
		Expect(code).To(Equal(200))
		Expect(header.Get("Content-Type")).To(Equal("application/json"))
		Expect(body).To(Equal(`{}` + "\n"))
	})

	It("allows authorized access to broker last operation", func() {
		path := "/v2/service_instances/test_instance/last_operation"

		serveComponent(component)

		req := buildRequest(component, path, "GET", nil)
		req.SetBasicAuth("username", "password")

		code, header, body := doRequest(req)
		Expect(code).To(Equal(200))
		Expect(header.Get("Content-Type")).To(Equal("application/json"))
		Expect(body).To(Equal(`{"state":""}` + "\n"))
	})

	It("allows authorized access to broker bind", func() {
		path := "/v2/service_instances/test_instance/service_bindings/test_bind"

		serveComponent(component)

		req := buildRequest(component, path, "PUT", strings.NewReader(`{}`))
		req.SetBasicAuth("username", "password")

		code, header, body := doRequest(req)
		Expect(code).To(Equal(201))
		Expect(header.Get("Content-Type")).To(Equal("application/json"))
		Expect(body).To(Equal(`{"credentials":null}` + "\n"))
	})

	It("allows authorized access to broker unbind", func() {
		path := "/v2/service_instances/test_instance/service_bindings/test_bind"

		serveComponent(component)

		req := buildRequest(component, path, "DELETE", nil)
		req.SetBasicAuth("username", "password")

		code, header, body := doRequest(req)
		Expect(code).To(Equal(200))
		Expect(header.Get("Content-Type")).To(Equal("application/json"))
		Expect(body).To(Equal(`{}` + "\n"))
	})

	It("returns 404 for non existent paths", func() {
		serveComponent(component)

		req := buildRequest(component, "/non-existent-path", "GET", nil)
		req.SetBasicAuth("username", "password")

		code, _, _ := doRequest(req)
		Expect(code).To(Equal(404))
	})

	It("returns 200 for good health checks", func() {
		component.Varz.Type = "Router"
		startComponent(component)

		time.Sleep(2 * time.Second)
		atomic.StoreInt32(&heartbeatOK, 1)

		req := buildRequest(component, "/health", "GET", nil)

		code, _, _ := doRequest(req)
		Expect(code).To(Equal(200))
	})

	It("returns 503 for bad health checks", func() {
		component.Varz.Type = "Router"
		startComponent(component)

		time.Sleep(2 * time.Second)

		req := buildRequest(component, "/health", "GET", nil)

		code, _, _ := doRequest(req)
		Expect(code).To(Equal(503))
	})

	Describe("Register", func() {
		var mbusClient *nats.Conn
		var natsRunner *test_util.NATSRunner

		BeforeEach(func() {
			natsPort := test_util.NextAvailPort()
			natsRunner = test_util.NewNATSRunner(int(natsPort))
			natsRunner.Start()
			mbusClient = natsRunner.MessageBus
		})

		AfterEach(func() {
			natsRunner.Stop()
		})

		It("subscribes to vcap.component.discover", func() {
			done := make(chan struct{})
			members := []string{
				"type",
				"index",
				"host",
				"credentials",
				"start",
				"uuid",
				"uptime",
				"num_cores",
				"mem",
				"cpu",
				"log_counts",
			}

			component.Varz.Type = "TestType"
			component.Logger = logger

			err := component.Start()
			Expect(err).ToNot(HaveOccurred())

			err = component.Register(mbusClient)
			Expect(err).ToNot(HaveOccurred())

			_, err = mbusClient.Subscribe("subject", func(msg *nats.Msg) {
				defer GinkgoRecover()
				data := make(map[string]interface{})
				jsonErr := json.Unmarshal(msg.Data, &data)
				Expect(jsonErr).ToNot(HaveOccurred())

				for _, key := range members {
					_, ok := data[key]
					Expect(ok).To(BeTrue())
				}

				close(done)
			})
			Expect(err).ToNot(HaveOccurred())

			err = mbusClient.PublishRequest("vcap.component.discover", "subject", []byte(""))
			Expect(err).ToNot(HaveOccurred())

			Eventually(done).Should(BeClosed())
		})

		It("publishes to vcap.component.announce on start-up", func() {
			done := make(chan struct{})
			members := []string{
				"type",
				"index",
				"host",
				"credentials",
				"start",
				"uuid",
				"uptime",
				"num_cores",
				"mem",
				"cpu",
				"log_counts",
			}

			component.Varz.Type = "TestType"
			component.Logger = logger

			err := component.Start()
			Expect(err).ToNot(HaveOccurred())

			_, err = mbusClient.Subscribe("vcap.component.announce", func(msg *nats.Msg) {
				defer GinkgoRecover()
				data := make(map[string]interface{})
				jsonErr := json.Unmarshal(msg.Data, &data)
				Expect(jsonErr).ToNot(HaveOccurred())

				for _, key := range members {
					_, ok := data[key]
					Expect(ok).To(BeTrue())
				}

				close(done)
			})
			Expect(err).ToNot(HaveOccurred())

			err = component.Register(mbusClient)
			Expect(err).ToNot(HaveOccurred())

			Eventually(done).Should(BeClosed())
		})

		It("can handle an empty reply in the subject", func() {
			component.Varz.Type = "TestType"
			component.Logger = logger

			err := component.Start()
			Expect(err).ToNot(HaveOccurred())

			err = component.Register(mbusClient)
			Expect(err).ToNot(HaveOccurred())

			err = mbusClient.PublishRequest("vcap.component.discover", "", []byte(""))
			Expect(err).ToNot(HaveOccurred())

			Eventually(logger).Should(gbytes.Say("Received message with empty reply"))

			err = mbusClient.PublishRequest("vcap.component.discover", "reply", []byte("hi"))
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

func startComponent(component *VcapComponent) {
	err := component.Start()
	Expect(err).ToNot(HaveOccurred())

	for i := 0; i < 5; i++ {
		conn, err := net.DialTimeout("tcp", component.Varz.Host, 1*time.Second)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	Expect(true).ToNot(BeTrue(), "Could not connect to vcap.Component")
}

func serveComponent(component *VcapComponent) {
	component.ListenAndServe()

	for i := 0; i < 5; i++ {
		conn, err := net.DialTimeout("tcp", component.Varz.Host, 1*time.Second)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	Expect(true).ToNot(BeTrue(), "Could not connect to vcap.Component")
}

func buildRequest(component *VcapComponent, path string, method string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, "http://"+component.Varz.Host+path, body)
	Expect(err).ToNot(HaveOccurred())
	return req
}

func doRequest(req *http.Request) (int, http.Header, string) {
	var client http.Client
	var resp *http.Response
	var err error

	resp, err = client.Do(req)
	Expect(err).ToNot(HaveOccurred())
	Expect(resp).ToNot(BeNil())

	body, err := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()
	Expect(err).ToNot(HaveOccurred())

	return resp.StatusCode, resp.Header, string(body)
}
