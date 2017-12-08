/*
 * Portions Copyright (c) 2017, F5 Networks, Inc.
 */

package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/F5Networks/cf-bigip-ctlr/common/health"
	. "github.com/F5Networks/cf-bigip-ctlr/common/http"
	"github.com/F5Networks/cf-bigip-ctlr/common/schema"
	"github.com/F5Networks/cf-bigip-ctlr/common/uuid"
	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/logger"
	"github.com/F5Networks/cf-bigip-ctlr/servicebroker"

	"code.cloudfoundry.org/localip"
	"github.com/nats-io/nats"
	"github.com/uber-go/zap"
)

const RefreshInterval time.Duration = time.Second * 1

var log logger.Logger

type ProcessStatus struct {
	sync.RWMutex
	rusage      *syscall.Rusage
	lastCpuTime int64
	stopSignal  chan bool
	stopped     bool

	CpuUsage float64
	MemRss   int64
}

func NewProcessStatus() *ProcessStatus {
	p := new(ProcessStatus)
	p.rusage = new(syscall.Rusage)

	go func() {
		timer := time.Tick(RefreshInterval)
		for {
			select {
			case <-timer:
				p.Update()
			case <-p.stopSignal:
				return
			}
		}
	}()

	return p
}

func (p *ProcessStatus) Update() {
	e := syscall.Getrusage(syscall.RUSAGE_SELF, p.rusage)
	if e != nil {
		log.Fatal("failed-to-get-rusage", zap.Error(e))
	}

	p.Lock()
	defer p.Unlock()
	p.MemRss = int64(p.rusage.Maxrss)

	t := p.rusage.Utime.Nano() + p.rusage.Stime.Nano()
	p.CpuUsage = float64(t-p.lastCpuTime) / float64(RefreshInterval.Nanoseconds())
	p.lastCpuTime = t
}

func (p *ProcessStatus) StopUpdate() {
	p.Lock()
	defer p.Unlock()
	if !p.stopped {
		p.stopped = true
		p.stopSignal <- true
		p.stopSignal = nil
	}
}

var procStat *ProcessStatus

type VcapComponent struct {
	Config     interface{}  `json:"-"`
	Varz       *health.Varz `json:"-"`
	Health     http.Handler
	InfoRoutes map[string]json.Marshaler `json:"-"`
	Logger     logger.Logger             `json:"-"`

	listener net.Listener
	statusCh chan error
	quitCh   chan struct{}
}

type RouterStart struct {
	Id                               string   `json:"id"`
	Hosts                            []string `json:"hosts"`
	MinimumRegisterIntervalInSeconds int      `json:"minimumRegisterIntervalInSeconds"`
	PruneThresholdInSeconds          int      `json:"pruneThresholdInSeconds"`
}

func (c *VcapComponent) UpdateVarz() {
	c.Varz.Lock()
	defer c.Varz.Unlock()

	procStat.RLock()
	c.Varz.MemStat = procStat.MemRss
	c.Varz.Cpu = procStat.CpuUsage
	procStat.RUnlock()
	c.Varz.Uptime = c.Varz.StartTime.Elapsed()
}

func (c *VcapComponent) Start() error {
	if c.Varz.Type == "" {
		err := errors.New("type is required")
		log.Error("Component type is required", zap.Error(err))
		return err
	}

	c.quitCh = make(chan struct{}, 1)
	c.Varz.StartTime = schema.Time(time.Now())
	guid, err := uuid.GenerateUUID()
	if err != nil {
		return err
	}
	c.Varz.UUID = fmt.Sprintf("%d-%s", c.Varz.Index, guid)

	if c.Varz.Host == "" {
		host, err := localip.LocalIP()
		if err != nil {
			log.Error("error-getting-localIP", zap.Error(err))
			return err
		}

		port, err := localip.LocalPort()
		if err != nil {
			log.Error("error-getting-localPort", zap.Error(err))
			return err
		}

		c.Varz.Host = fmt.Sprintf("%s:%d", host, port)
	}

	if c.Varz.Credentials == nil || len(c.Varz.Credentials) != 2 {
		c.Logger.Warn("status user and/or pass not provided, controller will not run" +
			"with broker features. If broker features are required please specify user" +
			"and pass and restart controller.")
		user, err := uuid.GenerateUUID()
		if err != nil {
			return err
		}
		password, err := uuid.GenerateUUID()
		if err != nil {
			return err
		}

		c.Varz.Credentials = []string{user, password}
	}

	if c.Logger != nil {
		log = c.Logger
	}

	c.Varz.NumCores = runtime.NumCPU()

	procStat = NewProcessStatus()

	c.ListenAndServe()
	return nil
}

func (c *VcapComponent) Register(mbusClient *nats.Conn) error {
	mbusClient.Subscribe("vcap.component.discover", func(msg *nats.Msg) {
		if msg.Reply == "" {
			log.Info("Received message with empty reply", zap.String("nats-msg-subject", msg.Subject))
			return
		}

		c.Varz.Uptime = c.Varz.StartTime.Elapsed()
		b, e := json.Marshal(c.Varz)
		if e != nil {
			log.Error("error-json-marshaling", zap.Error(e))
			return
		}

		mbusClient.Publish(msg.Reply, b)
	})

	b, e := json.Marshal(c.Varz)
	if e != nil {
		log.Error("error-json-marshaling", zap.Error(e))
		return e
	}

	mbusClient.Publish("vcap.component.announce", b)

	log.Info(fmt.Sprintf("Component %s registered successfully", c.Varz.Type))
	return nil
}

func (c *VcapComponent) Stop() {
	close(c.quitCh)
	if c.listener != nil {
		c.listener.Close()
		<-c.statusCh
	}
}

func (c *VcapComponent) ListenAndServe() {
	hs := http.NewServeMux()

	mainConfig := c.Config.(*config.Config)
	if mainConfig.BrokerMode {
		sb, err := servicebroker.NewServiceBroker(mainConfig, c.Logger)
		if nil != err {
			c.Logger.Warn("create-new-broker-error", zap.Error(err))
		} else {
			err = sb.ProcessPlans()
			if nil != err {
				c.Logger.Warn("process-broker-plan-error", zap.Error(err))
			} else {
				hs.Handle("/", sb.Handler)
			}
		}
	}

	hs.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) {
		c.Health.ServeHTTP(w, req)
	})

	for path, marshaler := range c.InfoRoutes {
		m := marshaler
		hs.HandleFunc(path, func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Connection", "close")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			enc := json.NewEncoder(w)
			enc.Encode(m)
		})
	}

	f := func(user, password string) bool {
		return user == c.Varz.Credentials[0] && password == c.Varz.Credentials[1]
	}

	s := &http.Server{
		Addr:         c.Varz.Host,
		Handler:      &BasicAuth{Handler: hs, Authenticator: f},
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	c.statusCh = make(chan error, 1)
	l, err := net.Listen("tcp", c.Varz.Host)
	if err != nil {
		c.statusCh <- err
		return
	}
	c.listener = l

	go func() {
		err = s.Serve(l)
		select {
		case <-c.quitCh:
			c.statusCh <- nil

		default:
			c.statusCh <- err
		}
	}()
}
