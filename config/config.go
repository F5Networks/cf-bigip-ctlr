/*
 * Portions Copyright (c) 2017, F5 Networks, Inc.
 */

package config

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/url"
	"runtime"
	"strings"
	"time"

	"code.cloudfoundry.org/localip"
	"gopkg.in/yaml.v2"
)

// RoutingMode of controller
type RoutingMode int

const (
	// TCP only routing mode
	TCP RoutingMode = iota
	// HTTP only routing mode
	HTTP
	// all for TCP and HTTP
	all
)

func (rm RoutingMode) String() string {
	switch rm {
	case TCP:
		return "tcp"
	case HTTP:
		return "http"
	case all:
		return "all"
	}
	return "Unknown"
}

const (
	LOAD_BALANCE_RR string = "round-robin"
	LOAD_BALANCE_LC string = "least-connection"
)

var LoadBalancingStrategies = []string{LOAD_BALANCE_RR, LOAD_BALANCE_LC}

type StatusConfig struct {
	Host string `yaml:"host"`
	Port uint16 `yaml:"port"`
	User string `yaml:"user"`
	Pass string `yaml:"pass"`
}

// BigIPConfig configuration parameters for bigip integration
type BigIPConfig struct {
	URL               string   `yaml:"url" json:"url"`
	User              string   `yaml:"user" json:"username"`
	Pass              string   `yaml:"pass" json:"password"`
	Partitions        []string `yaml:"partition" json:"partitions"`
	LoadBalancingMode string   `yaml:"load_balancing_mode" json:"-"`
	VerifyInterval    int      `yaml:"verify_interval" json:"-"`
	ExternalAddr      string   `yaml:"external_addr" json:"-"`
	SSLProfiles       []string `yaml:"ssl_profiles" json:"-"`
	Policies          []string `yaml:"policies" json:"-"`
	Profiles          []string `yaml:"profiles" json:"-"`
	HealthMonitors    []string `yaml:"health_monitors" json:"-"`
	DriverCmd         string   `yaml:"driver_path" json:"-"`
}

var defaultBigIPConfig = BigIPConfig{
	URL:               "",
	User:              "",
	Pass:              "",
	Partitions:        []string{},
	LoadBalancingMode: "round-robin",
	VerifyInterval:    30,
	ExternalAddr:      "",
	SSLProfiles:       []string{},
	Policies:          []string{},
	Profiles:          []string{},
	DriverCmd:         "",
}

var defaultStatusConfig = StatusConfig{
	Host: "0.0.0.0",
	Port: 8080,
	User: "",
	Pass: "",
}

type NatsConfig struct {
	Host string `yaml:"host"`
	Port uint16 `yaml:"port"`
	User string `yaml:"user"`
	Pass string `yaml:"pass"`
}

type RoutingApiConfig struct {
	Uri          string `yaml:"uri"`
	Port         int    `yaml:"port"`
	AuthDisabled bool   `yaml:"auth_disabled"`
}

var defaultNatsConfig = NatsConfig{
	Host: "localhost",
	Port: 4222,
	User: "",
	Pass: "",
}

type OAuthConfig struct {
	TokenEndpoint     string `yaml:"token_endpoint"`
	Port              int    `yaml:"port"`
	SkipSSLValidation bool   `yaml:"skip_ssl_validation"`
	ClientName        string `yaml:"client_name"`
	ClientSecret      string `yaml:"client_secret"`
	CACerts           string `yaml:"ca_certs"`
}

type LoggingConfig struct {
	Syslog             string `yaml:"syslog"`
	Level              string `yaml:"level"`
	LoggregatorEnabled bool   `yaml:"loggregator_enabled"`
	MetronAddress      string `yaml:"metron_address"`

	// This field is populated by the `Process` function.
	JobName string `yaml:"-"`
}

type AccessLog struct {
	File            string `yaml:"file"`
	EnableStreaming bool   `yaml:"enable_streaming"`
}

type Tracing struct {
	EnableZipkin bool `yaml:"enable_zipkin"`
}

var defaultLoggingConfig = LoggingConfig{
	Level:         "debug",
	MetronAddress: "localhost:3457",
}

type Config struct {
	BigIP                    BigIPConfig   `yaml:"bigip"`
	Status                   StatusConfig  `yaml:"status"`
	Nats                     []NatsConfig  `yaml:"nats"`
	Logging                  LoggingConfig `yaml:"logging"`
	Port                     uint16        `yaml:"port"`
	Index                    uint          `yaml:"index"`
	Zone                     string        `yaml:"zone"`
	GoMaxProcs               int           `yaml:"go_max_procs,omitempty"`
	Tracing                  Tracing       `yaml:"tracing"`
	TraceKey                 string        `yaml:"trace_key"`
	AccessLog                AccessLog     `yaml:"access_log"`
	EnableAccessLogStreaming bool          `yaml:"enable_access_log_streaming"`
	DebugAddr                string        `yaml:"debug_addr"`
	EnablePROXY              bool          `yaml:"enable_proxy"`
	EnableSSL                bool          `yaml:"enable_ssl"`
	SSLPort                  uint16        `yaml:"ssl_port"`
	SSLCertPath              string        `yaml:"ssl_cert_path"`
	SSLKeyPath               string        `yaml:"ssl_key_path"`
	SSLCertificate           tls.Certificate
	SkipSSLValidation        bool `yaml:"skip_ssl_validation"`
	ForceForwardedProtoHttps bool `yaml:"force_forwarded_proto_https"`

	CipherString string `yaml:"cipher_suites"`
	CipherSuites []uint16

	LoadBalancerHealthyThreshold    time.Duration `yaml:"load_balancer_healthy_threshold"`
	PublishStartMessageInterval     time.Duration `yaml:"publish_start_message_interval"`
	SuspendPruningIfNatsUnavailable bool          `yaml:"suspend_pruning_if_nats_unavailable"`
	PruneStaleDropletsInterval      time.Duration `yaml:"prune_stale_droplets_interval"`
	DropletStaleThreshold           time.Duration `yaml:"droplet_stale_threshold"`
	PublishActiveAppsInterval       time.Duration `yaml:"publish_active_apps_interval"`
	StartResponseDelayInterval      time.Duration `yaml:"start_response_delay_interval"`
	EndpointTimeout                 time.Duration `yaml:"endpoint_timeout"`
	RouteServiceTimeout             time.Duration `yaml:"route_services_timeout"`
	RouteMode                       string        `yaml:"route_mode"`
	RoutingMode                     RoutingMode

	DrainWait          time.Duration `yaml:"drain_wait,omitempty"`
	DrainTimeout       time.Duration `yaml:"drain_timeout,omitempty"`
	SecureCookies      bool          `yaml:"secure_cookies"`
	RouterGroupName    string        `yaml:"router_group"`
	TCPRouterGroupName string        `yaml:"tcp_router_group"`

	OAuth                      OAuthConfig      `yaml:"oauth"`
	RoutingApi                 RoutingApiConfig `yaml:"routing_api"`
	RouteServiceSecret         string           `yaml:"route_services_secret"`
	RouteServiceSecretPrev     string           `yaml:"route_services_secret_decrypt_only"`
	RouteServiceRecommendHttps bool             `yaml:"route_services_recommend_https"`
	// These fields are populated by the `Process` function.
	Ip                     string        `yaml:"-"`
	RouteServiceEnabled    bool          `yaml:"-"`
	NatsClientPingInterval time.Duration `yaml:"-"`

	ExtraHeadersToLog []string `yaml:"extra_headers_to_log"`

	TokenFetcherMaxRetries                    uint32        `yaml:"token_fetcher_max_retries"`
	TokenFetcherRetryInterval                 time.Duration `yaml:"token_fetcher_retry_interval"`
	TokenFetcherExpirationBufferTimeInSeconds int64         `yaml:"token_fetcher_expiration_buffer_time"`

	PidFile     string `yaml:"pid_file"`
	LoadBalance string `yaml:"balancing_algorithm"`

	SessionPersistence bool `yaml:"session_persistence"`

	DisableKeepAlives   bool `yaml:"disable_keep_alives"`
	MaxIdleConns        int  `yaml:"max_idle_conns"`
	MaxIdleConnsPerHost int  `yaml:"max_idle_conns_per_host"`
}

var defaultConfig = Config{
	BigIP:   defaultBigIPConfig,
	Status:  defaultStatusConfig,
	Nats:    []NatsConfig{defaultNatsConfig},
	Logging: defaultLoggingConfig,

	Port:        8081,
	Index:       0,
	GoMaxProcs:  -1,
	EnablePROXY: false,
	EnableSSL:   false,
	SSLPort:     443,

	EndpointTimeout:     60 * time.Second,
	RouteServiceTimeout: 60 * time.Second,

	PublishStartMessageInterval:               30 * time.Second,
	PruneStaleDropletsInterval:                30 * time.Second,
	DropletStaleThreshold:                     120 * time.Second,
	PublishActiveAppsInterval:                 0 * time.Second,
	StartResponseDelayInterval:                5 * time.Second,
	TokenFetcherMaxRetries:                    3,
	TokenFetcherRetryInterval:                 5 * time.Second,
	TokenFetcherExpirationBufferTimeInSeconds: 30,
	RouteMode: HTTP.String(),

	LoadBalance: LOAD_BALANCE_RR,

	SessionPersistence: true,

	DisableKeepAlives:   true,
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 2,
}

func DefaultConfig() *Config {
	c := defaultConfig
	c.Process()

	return &c
}

func (c *Config) Process() {
	var err error

	if c.GoMaxProcs == -1 {
		c.GoMaxProcs = runtime.NumCPU()
	}

	c.Logging.JobName = "cf-bigip-ctlr"
	if c.StartResponseDelayInterval > c.DropletStaleThreshold {
		c.DropletStaleThreshold = c.StartResponseDelayInterval
	}

	// To avoid routes getting purged because of unresponsive NATS server
	// we need to set the ping interval of nats client such that it fails over
	// to next NATS server before dropletstalethreshold is hit. We are hardcoding the ping interval
	// to 20 sec because the operators cannot set the value of DropletStaleThreshold and StartResponseDelayInterval
	// ping_interval = ((DropletStaleThreshold- StartResponseDelayInterval)-minimumRegistrationInterval+(2 * number_of_nats_servers))/3
	c.NatsClientPingInterval = 20 * time.Second

	if c.DrainTimeout == 0 || c.DrainTimeout == defaultConfig.EndpointTimeout {
		c.DrainTimeout = c.EndpointTimeout
	}

	c.Ip, err = localip.LocalIP()
	if err != nil {
		panic(err)
	}

	if c.EnableSSL {
		c.CipherSuites = c.processCipherSuites()
		cert, err := tls.LoadX509KeyPair(c.SSLCertPath, c.SSLKeyPath)
		if err != nil {
			panic(err)
		}
		c.SSLCertificate = cert
	}

	if c.RouteServiceSecret != "" {
		c.RouteServiceEnabled = true
	}

	// check if valid load balancing strategy
	validLb := false
	for _, lb := range LoadBalancingStrategies {
		if c.LoadBalance == lb {
			validLb = true
			break
		}
	}
	if !validLb {
		errMsg := fmt.Sprintf("Invalid load balancing algorithm %s. Allowed values are %s", c.LoadBalance, LoadBalancingStrategies)
		panic(errMsg)
	}

	if c.RouterGroupName != "" && !c.RoutingApiEnabled() {
		errMsg := fmt.Sprintf("Routing API must be enabled to assign Router Group")
		panic(errMsg)
	}

	if len(c.TCPRouterGroupName) == 0 {
		c.TCPRouterGroupName = "default-tcp"
	}

	switch c.RouteMode {
	case "http":
		c.RoutingMode = HTTP
	case "tcp":
		c.RoutingMode = TCP
	case "all":
		c.RoutingMode = all
	default:
		errMsg := fmt.Sprintf("Invalid Router Mode set %s. Allowed values are 'tcp', 'http', and 'all'.", c.RouteMode)
		panic(errMsg)
	}

	if (c.RoutingMode == all) && !c.RoutingApiEnabled() {
		c.RoutingMode = HTTP
		fmt.Print("Route mode changed from 'all' to 'HTTP', Routing API is disabled")
	}

	if (c.RoutingMode == TCP) && !c.RoutingApiEnabled() {
		errMsg := fmt.Sprintf("Routing API must be enable for TCP only route mode")
		panic(errMsg)
	}
}

func (c *Config) processCipherSuites() []uint16 {
	cipherMap := map[string]uint16{
		"TLS_RSA_WITH_RC4_128_SHA":                0x0005,
		"TLS_RSA_WITH_3DES_EDE_CBC_SHA":           0x000a,
		"TLS_RSA_WITH_AES_128_CBC_SHA":            0x002f,
		"TLS_RSA_WITH_AES_256_CBC_SHA":            0x0035,
		"TLS_RSA_WITH_AES_128_GCM_SHA256":         0x009c,
		"TLS_RSA_WITH_AES_256_GCM_SHA384":         0x009d,
		"TLS_ECDHE_ECDSA_WITH_RC4_128_SHA":        0xc007,
		"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA":    0xc009,
		"TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA":    0xc00a,
		"TLS_ECDHE_RSA_WITH_RC4_128_SHA":          0xc011,
		"TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA":     0xc012,
		"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA":      0xc013,
		"TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA":      0xc014,
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":   0xc02f,
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256": 0xc02b,
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":   0xc030,
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384": 0xc02c}

	var ciphers []string

	if len(strings.TrimSpace(c.CipherString)) == 0 {
		panic("must specify list of cipher suite when ssl is enabled")
	} else {
		ciphers = strings.Split(c.CipherString, ":")
	}

	return convertCipherStringToInt(ciphers, cipherMap)
}

func convertCipherStringToInt(cipherStrs []string, cipherMap map[string]uint16) []uint16 {
	ciphers := []uint16{}
	for _, cipher := range cipherStrs {
		if val, ok := cipherMap[cipher]; ok {
			ciphers = append(ciphers, val)
		} else {
			var supportedCipherSuites = []string{}
			for key, _ := range cipherMap {
				supportedCipherSuites = append(supportedCipherSuites, key)
			}
			errMsg := fmt.Sprintf("Invalid cipher string configuration: %s, please choose from %v", cipher, supportedCipherSuites)
			panic(errMsg)
		}
	}

	return ciphers
}

func (c *Config) NatsServers() []string {
	var natsServers []string
	for _, info := range c.Nats {
		uri := url.URL{
			Scheme: "nats",
			User:   url.UserPassword(info.User, info.Pass),
			Host:   fmt.Sprintf("%s:%d", info.Host, info.Port),
		}
		natsServers = append(natsServers, uri.String())
	}

	return natsServers
}

func (c *Config) RoutingApiEnabled() bool {
	return (c.RoutingApi.Uri != "") && (c.RoutingApi.Port != 0)
}

func (c *Config) Initialize(configYAML []byte) error {
	c.Nats = []NatsConfig{}
	return yaml.Unmarshal(configYAML, &c)
}

func InitConfigFromFile(path string) *Config {
	var c *Config = DefaultConfig()
	var e error

	b, e := ioutil.ReadFile(path)
	if e != nil {
		panic(e.Error())
	}

	e = c.Initialize(b)
	if e != nil {
		panic(e.Error())
	}

	c.Process()

	return c
}
