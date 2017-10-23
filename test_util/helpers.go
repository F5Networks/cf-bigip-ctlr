/*
 * Portions Copyright (c) 2017, F5 Networks, Inc.
 */

package test_util

import (
	"time"

	"github.com/F5Networks/cf-bigip-ctlr/config"
)

func SpecConfig(statusPort uint16, natsPorts ...uint16) *config.Config {
	return generateConfig(statusPort, natsPorts...)
}

func generateConfig(statusPort uint16, natsPorts ...uint16) *config.Config {
	c := config.DefaultConfig()

	c.Index = 2
	c.TraceKey = "my_trace_key"

	// Hardcode the IP to localhost to avoid leaving the machine while running tests
	c.Ip = "127.0.0.1"

	c.StartResponseDelayInterval = 1 * time.Second
	c.PublishStartMessageInterval = 10 * time.Second
	c.PruneStaleDropletsInterval = 0
	c.DropletStaleThreshold = 10 * time.Second
	c.PublishActiveAppsInterval = 0
	c.Zone = "z1"

	c.EndpointTimeout = 500 * time.Millisecond

	c.Status = config.StatusConfig{
		Port: statusPort,
		User: "user",
		Pass: "pass",
	}

	c.Nats = []config.NatsConfig{}
	for _, natsPort := range natsPorts {
		c.Nats = append(c.Nats, config.NatsConfig{
			Host: "localhost",
			Port: natsPort,
			User: "nats",
			Pass: "nats",
		})
	}

	c.Logging = config.LoggingConfig{
		Level:         "debug",
		MetronAddress: "localhost:3457",
		JobName:       "router_test_z1_0",
	}

	c.OAuth = config.OAuthConfig{
		TokenEndpoint:     "uaa.cf.service.internal",
		Port:              8443,
		SkipSSLValidation: true,
	}

	c.RouteServiceSecret = "kCvXxNMB0JO2vinxoru9Hg=="

	c.Tracing = config.Tracing{
		EnableZipkin: true,
	}

	return c
}
