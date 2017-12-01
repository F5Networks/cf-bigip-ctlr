/*
 * Portions Copyright (c) 2017, F5 Networks, Inc.
 */
package main // import "github.com/F5Networks/cf-bigip-ctlr"

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/F5Networks/cf-bigip-ctlr/common/uuid"
	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/controller"
	"github.com/F5Networks/cf-bigip-ctlr/f5router"
	cfLogger "github.com/F5Networks/cf-bigip-ctlr/logger"
	"github.com/F5Networks/cf-bigip-ctlr/mbus"
	"github.com/F5Networks/cf-bigip-ctlr/metrics"
	rregistry "github.com/F5Networks/cf-bigip-ctlr/registry"
	"github.com/F5Networks/cf-bigip-ctlr/routefetcher"
	"github.com/F5Networks/cf-bigip-ctlr/routingtable"
	rvarz "github.com/F5Networks/cf-bigip-ctlr/varz"

	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/debugserver"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/routing-api"
	uaa_client "code.cloudfoundry.org/uaa-go-client"
	uaa_config "code.cloudfoundry.org/uaa-go-client/config"
	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/dropsonde/metric_sender"
	"github.com/cloudfoundry/dropsonde/metricbatcher"
	"github.com/nats-io/nats"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
	"github.com/tedsuo/ifrit/sigmon"
	"github.com/uber-go/zap"
)

var (
	// To be set by build
	version       string
	buildInfo     string
	pythonBaseDir string
	configFile    string
)

func main() {
	versionFlag := flag.Bool("version", false, "Print version and exit")

	val, ok := os.LookupEnv("BIGIP_CTLR_CFG")
	if !ok {
		flag.StringVar(&configFile, "c", "", "Configuration File")
	}
	flag.Parse()

	if *versionFlag {
		fmt.Printf("Version: %s\nBuild: %s\n", version, buildInfo)
		os.Exit(0)
	}

	c := config.DefaultConfig()

	if configFile != "" {
		c = config.InitConfigFromFile(configFile)
	} else {
		e := c.Initialize([]byte(val))
		if e != nil {
			panic(e.Error())
		}

		c.Process()
	}

	prefix := "cf-bigip-ctlr.stdout"
	if c.Logging.Syslog != "" {
		prefix = c.Logging.Syslog
	}
	logger, minLagerLogLevel := createLogger(prefix, c.Logging.Level)

	logger.Info("starting",
		zap.String("version", version),
		zap.String("buildInfo", buildInfo),
	)

	err := dropsonde.Initialize(c.Logging.MetronAddress, c.Logging.JobName)
	if err != nil {
		logger.Fatal("dropsonde-initialize-error", zap.Error(err))
	}

	// setup number of procs
	if c.GoMaxProcs != 0 {
		runtime.GOMAXPROCS(c.GoMaxProcs)
	}

	if c.DebugAddr != "" {
		reconfigurableSink := lager.NewReconfigurableSink(
			lager.NewWriterSink(os.Stdout, lager.DEBUG),
			minLagerLogLevel,
		)
		debugserver.Run(c.DebugAddr, reconfigurableSink)
	}

	logger.Info("setting-up-nats-connection")
	startMsgChan := make(chan struct{})
	natsClient := connectToNatsServer(logger.Session("nats"), c, startMsgChan)

	sender := metric_sender.NewMetricSender(dropsonde.AutowiredEmitter())
	// 5 sec is dropsonde default batching interval
	batcher := metricbatcher.New(sender, 5*time.Second)
	metricsReporter := metrics.NewMetricsReporter(sender, batcher)

	var (
		routerGroupGUID  string
		routingAPIClient routing_api.Client
	)
	if c.RoutingApiEnabled() {
		logger.Info("setting-up-routing-api")

		routingAPIClient, err = setupRoutingAPIClient(logger, c)
		if err != nil {
			logger.Fatal("routing-api-connection-failed", zap.Error(err))
		}

		routerGroupGUID = fetchRoutingGroupGUID(logger, c, routingAPIClient)
	}

	writer, err := f5router.NewConfigWriter(logger.Session("f5writer"))
	if nil != err {
		logger.Fatal("writer-failed-initialization", zap.Error(err))
	}

	defer func() {
		writer.Close()
	}()
	f5Router, err := f5router.NewF5Router(logger.Session("f5router"), c, writer)
	if nil != err {
		logger.Fatal("f5router-failed-initialization", zap.Error(err))
	}

	var dp string
	if 0 == len(c.BigIP.DriverCmd) {
		folderPath, err := os.Getwd()
		if err != nil {
			logger.Fatal("file-get-error", zap.Error(err))
		}

		dp = fmt.Sprintf("%v/%v", folderPath, f5router.DefaultCmd)
	} else {
		dp = c.BigIP.DriverCmd
	}
	_, err = os.Stat(dp)
	if os.IsNotExist(err) {
		logger.Fatal("driver-file-does-not-exist", zap.Error(err))
	}

	driver := f5router.NewDriver(
		writer.GetOutputFilename(),
		dp,
		logger.Session("python-driver"),
	)

	var members grouper.Members

	// registry is for http routing routes - if not in tcp only mode
	// create the registry, subsribe to the routing api and listen to nats
	var registry *rregistry.RouteRegistry
	if c.RoutingMode != config.TCP {
		registry = rregistry.NewRouteRegistry(
			logger.Session("registry"),
			c,
			f5Router,
			metricsReporter,
			routerGroupGUID,
		)
		if c.SuspendPruningIfNatsUnavailable {
			registry.SuspendPruning(func() bool {
				return !(natsClient.Status() == nats.CONNECTED)
			})
		}
		if c.RoutingApiEnabled() {
			httpFetcher := setupRouteFetcher(logger.Session("http-route-fetcher"), c, registry, routingAPIClient)
			members = append(members, grouper.Member{Name: "http-route-fetcher", Runner: httpFetcher})
		}
		// Subscribe to the nats client
		subscriber := createSubscriber(logger, c, natsClient, registry, startMsgChan, routerGroupGUID)
		members = append(members, grouper.Member{Name: "subscriber", Runner: subscriber})
	}

	// routingTable is for tcp routing routes - if not in http only mode
	// setup the connection to the routing api
	var routingTable *routingtable.RoutingTable
	if c.RoutingMode != config.HTTP {
		routingTable = routingtable.NewRoutingTable(logger.Session("tcp-routing-table"), c, f5Router)
		tcpFetcher := setupTCPRouteFetcher(logger.Session("tcp-route-fetcher"), c, routingTable, routingAPIClient)
		members = append(members, grouper.Member{Name: "tcp-route-fetcher", Runner: tcpFetcher})
	}

	varz := rvarz.NewVarz(registry)
	controller, err := controller.NewController(
		logger.Session("controller"),
		c,
		natsClient,
		registry,
		routingTable,
		varz,
	)
	if nil != err {
		logger.Fatal("failed-starting-controller", zap.Error(err))
	}

	// controller handles StartResponseDelayInterval - start it before configuration ops
	members = append(members, grouper.Member{Name: "controller", Runner: controller})
	members = append(members, grouper.Member{Name: "f5router", Runner: f5Router})
	members = append(members, grouper.Member{Name: "f5driver", Runner: driver})

	group := grouper.NewOrdered(os.Interrupt, members)

	monitor := ifrit.Invoke(sigmon.New(group, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1))

	err = <-monitor.Wait()
	if err != nil {
		logger.Error("cf-bigip-ctlr.exited-with-failure", zap.Error(err))
		os.Exit(1)
	}

	os.Exit(0)
}

func fetchRoutingGroupGUID(
	logger cfLogger.Logger,
	c *config.Config,
	routingAPIClient routing_api.Client,
) (routerGroupGUID string) {
	if c.RouterGroupName == "" {
		logger.Info(
			"retrieved-router-group",
			[]zap.Field{zap.String("router-group", "-"),
				zap.String("router-group-guid", "-")}...,
		)
		return
	}

	rg, err := routingAPIClient.RouterGroupWithName(c.RouterGroupName)
	if err != nil {
		logger.Fatal("fetching-router-group-failed", zap.Error(err))
	}
	logger.Info("starting-to-fetch-router-groups")

	if rg.Type != "http" {
		logger.Fatal(
			"expected-router-group-type-http",
			zap.Error(fmt.Errorf("Router Group '%s' is not of type http", c.RouterGroupName)),
		)
	}
	routerGroupGUID = rg.Guid

	logger.Info(
		"retrieved-router-group",
		zap.String("router-group", c.RouterGroupName),
		zap.String("router-group-guid", routerGroupGUID),
	)

	return
}

func setupRoutingAPIClient(
	logger cfLogger.Logger,
	c *config.Config,
) (routing_api.Client, error) {
	routingAPIURI := fmt.Sprintf("%s:%d", c.RoutingApi.Uri, c.RoutingApi.Port)
	client := routing_api.NewClient(routingAPIURI, false)

	logger.Debug("fetching-token")
	clock := clock.NewClock()

	uaaClient := newUaaClient(logger, clock, c)

	if !c.RoutingApi.AuthDisabled {
		token, err := uaaClient.FetchToken(true)
		if err != nil {
			return nil, fmt.Errorf("unable-to-fetch-token: %s", err.Error())
		}
		if token.AccessToken == "" {
			return nil, fmt.Errorf("empty token fetched")
		}
		client.SetToken(token.AccessToken)
	}
	// Test connectivity
	_, err := client.Routes()
	if err != nil {
		return nil, err
	}

	return client, nil
}

func setupRouteFetcher(
	logger cfLogger.Logger,
	c *config.Config,
	registry rregistry.Registry,
	routingAPIClient routing_api.Client,
) *routefetcher.RouteFetcher {
	clock := clock.NewClock()

	uaaClient := newUaaClient(logger, clock, c)

	_, err := uaaClient.FetchToken(true)
	if err != nil {
		logger.Fatal("unable-to-fetch-token", zap.Error(err))
	}

	httpClient := routefetcher.NewHTTPFetcher(logger, uaaClient, routingAPIClient, registry)

	routeFetcher := routefetcher.NewRouteFetcher(
		logger,
		uaaClient,
		c,
		routingAPIClient,
		1,
		clock,
		httpClient,
	)
	return routeFetcher
}

func setupTCPRouteFetcher(
	logger cfLogger.Logger,
	c *config.Config,
	routingTable *routingtable.RoutingTable,
	routingAPIClient routing_api.Client,
) *routefetcher.RouteFetcher {
	clock := clock.NewClock()

	uaaClient := newUaaClient(logger, clock, c)

	_, err := uaaClient.FetchToken(true)
	if err != nil {
		logger.Fatal("unable-to-fetch-token", zap.Error(err))
	}

	tcpClient := routefetcher.NewTCPFetcher(
		logger,
		routingTable,
		routingAPIClient,
		uaaClient,
	)

	routeFetcher := routefetcher.NewRouteFetcher(
		logger,
		uaaClient,
		c,
		routingAPIClient,
		1,
		clock,
		tcpClient,
	)
	return routeFetcher
}

func newUaaClient(
	logger cfLogger.Logger,
	clock clock.Clock,
	c *config.Config,
) uaa_client.Client {
	if c.RoutingApi.AuthDisabled {
		logger.Info("using-noop-token-fetcher")
		return uaa_client.NewNoOpUaaClient()
	}

	if c.OAuth.Port == -1 {
		logger.Fatal(
			"tls-not-enabled",
			zap.Error(errors.New("cf-bigip-ctlr requires TLS enabled to get OAuth token")),
			zap.String("token-endpoint", c.OAuth.TokenEndpoint),
			zap.Int("port", c.OAuth.Port),
		)
	}

	tokenURL := fmt.Sprintf("https://%s:%d", c.OAuth.TokenEndpoint, c.OAuth.Port)

	cfg := &uaa_config.Config{
		UaaEndpoint:           tokenURL,
		SkipVerification:      c.OAuth.SkipSSLValidation,
		ClientName:            c.OAuth.ClientName,
		ClientSecret:          c.OAuth.ClientSecret,
		CACerts:               c.OAuth.CACerts,
		MaxNumberOfRetries:    c.TokenFetcherMaxRetries,
		RetryInterval:         c.TokenFetcherRetryInterval,
		ExpirationBufferInSec: c.TokenFetcherExpirationBufferTimeInSeconds,
	}

	uaaClient, err := uaa_client.NewClient(cfLogger.NewLagerAdapter(logger), cfg, clock)
	if err != nil {
		logger.Fatal("initialize-token-fetcher-error", zap.Error(err))
	}
	return uaaClient
}

func natsOptions(
	logger cfLogger.Logger,
	c *config.Config,
	natsHost *atomic.Value,
	startMsg chan<- struct{},
) nats.Options {
	natsServers := c.NatsServers()

	options := nats.DefaultOptions
	options.Servers = natsServers
	options.PingInterval = c.NatsClientPingInterval
	options.MaxReconnect = -1
	connectedChan := make(chan struct{})

	options.ClosedCB = func(conn *nats.Conn) {
		logger.Fatal(
			"nats-connection-closed",
			zap.Error(errors.New("unexpected close")),
			zap.Object("last_error", conn.LastError()),
		)
	}

	options.DisconnectedCB = func(conn *nats.Conn) {
		hostStr := natsHost.Load().(string)
		logger.Info("nats-connection-disconnected", zap.String("nats-host", hostStr))

		go func() {
			ticker := time.NewTicker(c.NatsClientPingInterval)

			for {
				select {
				case <-connectedChan:
					return
				case <-ticker.C:
					logger.Info("nats-connection-still-disconnected")
				}
			}
		}()
	}

	options.ReconnectedCB = func(conn *nats.Conn) {
		connectedChan <- struct{}{}

		natsURL, err := url.Parse(conn.ConnectedUrl())
		natsHostStr := ""
		if err != nil {
			logger.Error("nats-url-parse-error", zap.Error(err))
		} else {
			natsHostStr = natsURL.Host
		}
		natsHost.Store(natsHostStr)

		logger.Info("nats-connection-reconnected", zap.String("nats-host", natsHostStr))
		startMsg <- struct{}{}
	}

	return options
}

func connectToNatsServer(
	logger cfLogger.Logger,
	c *config.Config,
	startMsg chan<- struct{},
) *nats.Conn {
	var natsClient *nats.Conn
	var natsHost atomic.Value
	var err error

	options := natsOptions(logger, c, &natsHost, startMsg)
	attempts := 3
	for attempts > 0 {
		natsClient, err = options.Connect()
		if err == nil {
			break
		} else {
			attempts--
			time.Sleep(100 * time.Millisecond)
		}
	}

	if err != nil {
		logger.Fatal("nats-connection-error", zap.Error(err))
	}

	var natsHostStr string
	natsURL, err := url.Parse(natsClient.ConnectedUrl())
	if err == nil {
		natsHostStr = natsURL.Host
	}

	logger.Info("Successfully-connected-to-nats", zap.String("host", natsHostStr))

	natsHost.Store(natsHostStr)
	return natsClient
}

func createSubscriber(
	logger cfLogger.Logger,
	c *config.Config,
	natsClient *nats.Conn,
	registry rregistry.Registry,
	startMsgChan chan struct{},
	routerGroupGUID string,
) ifrit.Runner {

	guid, err := uuid.GenerateUUID()
	if err != nil {
		logger.Fatal("failed-to-generate-uuid", zap.Error(err))
	}

	opts := &mbus.SubscriberOpts{
		ID: fmt.Sprintf("%d-%s", c.Index, guid),
		MinimumRegisterIntervalInSeconds: int(c.StartResponseDelayInterval.Seconds()),
		PruneThresholdInSeconds:          int(c.DropletStaleThreshold.Seconds()),
	}
	return mbus.NewSubscriber(
		logger.Session("subscriber"),
		natsClient,
		registry,
		startMsgChan,
		opts,
		routerGroupGUID,
	)
}

func createLogger(component string, level string) (cfLogger.Logger, lager.LogLevel) {
	var logLevel zap.Level
	logLevel.UnmarshalText([]byte(level))

	var minLagerLogLevel lager.LogLevel
	switch minLagerLogLevel {
	case lager.DEBUG:
		minLagerLogLevel = lager.DEBUG
	case lager.INFO:
		minLagerLogLevel = lager.INFO
	case lager.ERROR:
		minLagerLogLevel = lager.ERROR
	case lager.FATAL:
		minLagerLogLevel = lager.FATAL
	default:
		panic(fmt.Errorf("unknown log level: %s", level))
	}

	lggr := cfLogger.NewLogger(component, logLevel, zap.Output(os.Stdout))
	return lggr, minLagerLogLevel
}
