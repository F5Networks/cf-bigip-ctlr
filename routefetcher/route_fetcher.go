/*
 * Portions Copyright (c) 2017, F5 Networks, Inc.
 */

package routefetcher

import (
	"errors"
	"os"
	"sync/atomic"
	"time"

	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/logger"

	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/routing-api"
	"code.cloudfoundry.org/routing-api/models"
	uaa_client "code.cloudfoundry.org/uaa-go-client"
	"code.cloudfoundry.org/uaa-go-client/schema"
	"github.com/cloudfoundry/dropsonde/metrics"
	"github.com/uber-go/zap"
)

type RouteClient interface {
	FetchRoutes() error
	HandleEvent(interface{})
	ClientProtocol() string
}

type RouteFetcher struct {
	UaaClient                          uaa_client.Client
	FetchRoutesInterval                time.Duration
	SubscriptionRetryIntervalInSeconds int

	logger          logger.Logger
	endpoints       []models.Route
	client          routing_api.Client
	stopEventSource int32
	eventSource     atomic.Value
	eventChannel    chan interface{}
	routeClient     RouteClient

	clock clock.Clock
}

const (
	TokenFetchErrors      = "token_fetch_errors"
	SubscribeEventsErrors = "subscribe_events_errors"
	maxRetries            = 3
	unauthorized          = "unauthorized"
)

// NewRouteFetcher knows how to subscribe to the routing api
func NewRouteFetcher(
	logger logger.Logger,
	uaaClient uaa_client.Client,
	cfg *config.Config,
	client routing_api.Client,
	subscriptionRetryInterval int,
	clock clock.Clock,
	rc RouteClient,
) *RouteFetcher {
	return &RouteFetcher{
		UaaClient:                          uaaClient,
		FetchRoutesInterval:                cfg.PruneStaleDropletsInterval / 2,
		SubscriptionRetryIntervalInSeconds: subscriptionRetryInterval,

		client:       client,
		logger:       logger,
		eventChannel: make(chan interface{}, 1024),
		clock:        clock,
		routeClient:  rc,
	}
}

func (r *RouteFetcher) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	r.startEventCycle()

	ticker := r.clock.NewTicker(r.FetchRoutesInterval)
	r.logger.Debug("created-ticker", zap.Duration("interval", r.FetchRoutesInterval))
	r.logger.Info("syncer-started")

	close(ready)
	for {
		select {
		case <-ticker.C():
			err := r.routeClient.FetchRoutes()
			if err != nil {
				r.logger.Error("failed-to-fetch-routes", zap.Error(err))
			}
		case e := <-r.eventChannel:
			r.routeClient.HandleEvent(e)

		case <-signals:
			r.logger.Info("stopping")
			atomic.StoreInt32(&r.stopEventSource, 1)
			if es := r.eventSource.Load(); es != nil {
				var err error
				switch es.(type) {
				case routing_api.EventSource:
					err = es.(routing_api.EventSource).Close()
				case routing_api.TcpEventSource:
					err = es.(routing_api.TcpEventSource).Close()
				}
				if err != nil {
					r.logger.Error("failed-closing-routing-api-event-source", zap.Error(err))
				} else {
					r.logger.Info("closed-routing-api-event-source", zap.String("protocol", r.routeClient.ClientProtocol()))
				}
			}
			ticker.Stop()
			return nil
		}
	}
}

func (r *RouteFetcher) startEventCycle() {
	go func() {
		forceUpdate := false
		for {
			r.logger.Debug("fetching-token")
			token, err := r.UaaClient.FetchToken(forceUpdate)
			if err != nil {
				metrics.IncrementCounter(TokenFetchErrors)
				r.logger.Error("failed-to-fetch-token", zap.Error(err))
			} else {
				r.logger.Debug("token-fetched-successfully")
				if atomic.LoadInt32(&r.stopEventSource) == 1 {
					return
				}
				err = r.subscribeToEvents(token)
				if err != nil && err.Error() == unauthorized {
					forceUpdate = true
				} else {
					forceUpdate = false
				}
				if atomic.LoadInt32(&r.stopEventSource) == 1 {
					return
				}
				time.Sleep(time.Duration(r.SubscriptionRetryIntervalInSeconds) * time.Second)
			}
		}
	}()
}

func (r *RouteFetcher) subscribeToEvents(token *schema.Token) error {
	r.client.SetToken(token.AccessToken)
	protocol := r.routeClient.ClientProtocol()

	if protocol == "http" {
		return r.subscribeToHTTP()
	} else if protocol == "tcp" {
		return r.subscribeToTCP()
	}
	return errors.New("Unkown protocol")
}

func (r *RouteFetcher) subscribeToHTTP() error {
	r.logger.Info("subscribing-to-routing-api-HTTP-event-stream")
	source, err := r.client.SubscribeToEventsWithMaxRetries(maxRetries)
	if err != nil {
		metrics.IncrementCounter(SubscribeEventsErrors)
		r.logger.Error("failed-subscribing-to-routing-api-event-stream-HTTP", zap.Error(err))
		return err
	}
	r.logger.Info("successfully-subscribed-to-routing-api-event-stream-HTTP")

	err = r.routeClient.FetchRoutes()
	if err != nil {
		r.logger.Error("failed-to-refresh-HTTP-routes", zap.Error(err))
	}

	r.eventSource.Store(source)
	var event routing_api.Event

	for {
		event, err = source.Next()
		if err != nil {
			metrics.IncrementCounter(SubscribeEventsErrors)
			r.logger.Error("failed-getting-next-HTTP-event: ", zap.Error(err))

			closeErr := source.Close()
			if closeErr != nil {
				r.logger.Error("failed-closing-HTTP-event-source", zap.Error(closeErr))
			}
			break
		}
		r.logger.Debug("received-HTTP-event", zap.Object("event", event))
		r.eventChannel <- event
	}
	return err
}

func (r *RouteFetcher) subscribeToTCP() error {
	r.logger.Info("subscribing-to-routing-api-TCP-event-stream")
	source, err := r.client.SubscribeToTcpEventsWithMaxRetries(maxRetries)
	if err != nil {
		metrics.IncrementCounter(SubscribeEventsErrors)
		r.logger.Error("failed-subscribing-to-routing-api-event-stream-TCP", zap.Error(err))
		return err
	}
	r.logger.Info("successfully-subscribed-to-routing-api-event-stream-TCP")

	err = r.routeClient.FetchRoutes()
	if err != nil {
		r.logger.Error("failed-to-refresh-TCP-routes", zap.Error(err))
	}

	r.eventSource.Store(source)
	var event routing_api.TcpEvent

	for {
		event, err = source.Next()
		if err != nil {
			metrics.IncrementCounter(SubscribeEventsErrors)
			r.logger.Error("failed-getting-next-TCP-event: ", zap.Error(err))

			closeErr := source.Close()
			if closeErr != nil {
				r.logger.Error("failed-closing-TCP-event-source", zap.Error(closeErr))
			}
			break
		}
		r.logger.Debug("received-TCP-event", zap.Object("event", event))
		r.eventChannel <- event
	}
	return err
}
