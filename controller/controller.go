/*-
 * Copyright (c) 2017,2018, F5 Networks, Inc.
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

package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/F5Networks/cf-bigip-ctlr/common"
	"github.com/F5Networks/cf-bigip-ctlr/common/health"
	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/handlers"
	"github.com/F5Networks/cf-bigip-ctlr/logger"
	"github.com/F5Networks/cf-bigip-ctlr/registry"
	"github.com/F5Networks/cf-bigip-ctlr/routingtable"
	"github.com/F5Networks/cf-bigip-ctlr/varz"

	"github.com/nats-io/nats"
	"github.com/uber-go/zap"
)

// Controller varz / common component server for cf-bigip-ctlr
type Controller struct {
	config       *config.Config
	component    *common.VcapComponent
	mbusClient   *nats.Conn
	registry     *registry.RouteRegistry
	routingTable *routingtable.RoutingTable
	heartbeatOK  *int32
	logger       logger.Logger
}

// NewController create new controller instance
func NewController(
	logger logger.Logger,
	cfg *config.Config,
	mbusClient *nats.Conn,
	r *registry.RouteRegistry,
	routingTable *routingtable.RoutingTable,
	v varz.Varz,
	brokerHandler http.Handler,
) (*Controller, error) {
	var host string

	var port uint64
	var err error
	portStr, ok := os.LookupEnv("PORT")
	if true == ok {
		port, err = strconv.ParseUint(portStr, 10, 16)
		if nil != err {
			logger.Warn("controller-env-port-not-uint16", zap.Error(err))
			port = uint64(cfg.Status.Port)
		}
	} else {
		port = uint64(cfg.Status.Port)
	}
	logger.Debug("controller-configured-port", zap.Uint64("port", port))
	host = fmt.Sprintf("%s:%d", cfg.Status.Host, port)

	varz := &health.Varz{
		GenericVarz: health.GenericVarz{
			Type:        "Controller",
			Index:       cfg.Index,
			Host:        host,
			Credentials: []string{cfg.Status.User, cfg.Status.Pass},
			LogCounts:   nil,
		},
	}

	if cfg.RoutingMode != config.TCP {
		varz.UniqueVarz = v
	}

	var heartbeatOK int32
	health := handlers.NewHealthcheck(&heartbeatOK, logger)
	component := &common.VcapComponent{
		Config: cfg,
		Varz:   varz,
		Health: health,
		InfoRoutes: map[string]json.Marshaler{
			"/routes": r,
		},
		Logger: logger,
	}

	if err := component.Start(brokerHandler); err != nil {
		return nil, err
	}

	return &Controller{
		config:       cfg,
		component:    component,
		mbusClient:   mbusClient,
		registry:     r,
		routingTable: routingTable,
		heartbeatOK:  &heartbeatOK,
		logger:       logger,
	}, nil
}

// Run start the cf-bigip-ctlr component server
func (c *Controller) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	if c.config.RoutingMode != config.TCP {
		c.registry.StartPruningCycle()
	}

	if c.config.RoutingMode != config.HTTP {
		c.routingTable.StartPruningCycle()
	}

	c.component.Register(c.mbusClient)

	c.logger.Info("controller-waiting-for-route-registration",
		zap.Float64("route-registration-interval-seconds",
			c.config.StartResponseDelayInterval.Seconds()),
	)

	if 0 < c.config.StartResponseDelayInterval {
		c.logger.Debug("sleeping-before-health-enabled",
			zap.Float64("sleep-time-seconds",
				c.config.StartResponseDelayInterval.Seconds()),
		)
		time.Sleep(c.config.StartResponseDelayInterval)
	}

	c.logger.Info("controller-wait-completed")

	atomic.StoreInt32(c.heartbeatOK, 1)
	c.logger.Debug("controller-reporting-healthy")

	c.logger.Info("controller-started")
	close(ready)

	<-signals

	c.Stop()
	c.logger.Info("controller-exiting")

	return nil
}

// Stop the cf-bigip-ctlr component server
func (c *Controller) Stop() {
	stoppingAt := time.Now()

	c.logger.Info("controller-stop-called")

	if c.config.RoutingMode != config.TCP {
		c.registry.StopPruningCycle()
	}

	if c.config.RoutingMode != config.HTTP {
		c.routingTable.StopPruningCycle()
	}

	c.component.Stop()
	c.logger.Info(
		"controller-stopped",
		zap.Duration("took", time.Since(stoppingAt)),
	)
}
