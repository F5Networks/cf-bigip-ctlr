/*
 * Portions Copyright (c) 2017,2018, F5 Networks, Inc.
 */

package routefetcher

import (
	"fmt"
	"reflect"

	"github.com/F5Networks/cf-bigip-ctlr/logger"
	"github.com/F5Networks/cf-bigip-ctlr/registry"
	"github.com/F5Networks/cf-bigip-ctlr/route"

	"code.cloudfoundry.org/routing-api"
	"code.cloudfoundry.org/routing-api/models"
	uaa_client "code.cloudfoundry.org/uaa-go-client"
	"github.com/cloudfoundry/dropsonde/metrics"
	"github.com/uber-go/zap"
)

// HTTPFetcher knows how to connect to the routing API and get tcp events and
// satisfies RouteClient interface
type HTTPFetcher struct {
	RouteRegistry registry.Registry
	logger        logger.Logger
	endpoints     []models.Route
	UaaClient     uaa_client.Client
	client        routing_api.Client
	protocol      string
}

// NewHTTPFetcher returns a http fetcher
func NewHTTPFetcher(
	logger logger.Logger,
	UaaClient uaa_client.Client,
	client routing_api.Client,
	RouteRegistry registry.Registry,
) *HTTPFetcher {
	return &HTTPFetcher{
		RouteRegistry: RouteRegistry,
		logger:        logger,
		UaaClient:     UaaClient,
		client:        client,
		protocol:      "http",
	}
}

func (httpFetcher *HTTPFetcher) ClientProtocol() string {
	return httpFetcher.protocol
}

func (httpFetcher *HTTPFetcher) HandleEvent(event interface{}) {
	e, ok := event.(routing_api.Event)
	if !ok {
		httpFetcher.logger.Warn("received-wrong-event-type",
			zap.String("event-type", fmt.Sprint(reflect.TypeOf(event))),
		)
	}
	eventRoute := e.Route
	uri := route.Uri(eventRoute.Route)
	endpoint := route.NewEndpoint(
		eventRoute.LogGuid,
		eventRoute.IP,
		uint16(eventRoute.Port),
		eventRoute.LogGuid,
		"",
		nil,
		eventRoute.GetTTL(),
		eventRoute.RouteServiceUrl,
		eventRoute.ModificationTag)
	switch e.Action {
	case "Delete":
		httpFetcher.RouteRegistry.Unregister(uri, endpoint)
	case "Upsert":
		httpFetcher.RouteRegistry.Register(uri, endpoint)
	}
}

func (httpFetcher *HTTPFetcher) FetchRoutes() error {
	httpFetcher.logger.Debug("HTTP-syncer-fetch-routes-started")

	defer httpFetcher.logger.Debug("HTTP-syncer-fetch-routes-completed")

	routes, err := httpFetcher.fetchRoutesWithTokenRefresh()
	if err != nil {
		return err
	}

	httpFetcher.logger.Debug("HTTP-syncer-refreshing-endpoints", zap.Int("number-of-routes", len(routes)))
	httpFetcher.refreshEndpoints(routes)
	return nil
}

func (httpFetcher *HTTPFetcher) fetchRoutesWithTokenRefresh() ([]models.Route, error) {
	forceUpdate := false
	var err error
	var routes []models.Route
	for count := 0; count < 2; count++ {
		httpFetcher.logger.Debug("HTTP-syncer-fetching-token")
		token, tokenErr := httpFetcher.UaaClient.FetchToken(forceUpdate)
		if tokenErr != nil {
			metrics.IncrementCounter(TokenFetchErrors)
			return []models.Route{}, tokenErr
		}
		httpFetcher.client.SetToken(token.AccessToken)
		httpFetcher.logger.Debug("HTTP-syncer-fetching-routes")
		routes, err = httpFetcher.client.Routes()
		if err != nil {
			if err.Error() == unauthorized {
				forceUpdate = true
			} else {
				return []models.Route{}, err
			}
		} else {
			break
		}
	}

	return routes, err
}

func (httpFetcher *HTTPFetcher) refreshEndpoints(validRoutes []models.Route) {
	httpFetcher.deleteEndpoints(validRoutes)

	httpFetcher.endpoints = validRoutes

	for _, aRoute := range httpFetcher.endpoints {
		httpFetcher.RouteRegistry.Register(
			route.Uri(aRoute.Route),
			route.NewEndpoint(
				aRoute.LogGuid,
				aRoute.IP,
				uint16(aRoute.Port),
				aRoute.LogGuid,
				"",
				nil,
				aRoute.GetTTL(),
				aRoute.RouteServiceUrl,
				aRoute.ModificationTag,
			))
	}
}

func (httpFetcher *HTTPFetcher) deleteEndpoints(validRoutes []models.Route) {
	var diff []models.Route

	for _, curRoute := range httpFetcher.endpoints {
		routeFound := false

		for _, validRoute := range validRoutes {
			if routeEquals(curRoute, validRoute) {
				routeFound = true
				break
			}
		}

		if !routeFound {
			diff = append(diff, curRoute)
		}
	}

	for _, aRoute := range diff {
		httpFetcher.RouteRegistry.Unregister(
			route.Uri(aRoute.Route),
			route.NewEndpoint(
				aRoute.LogGuid,
				aRoute.IP,
				uint16(aRoute.Port),
				aRoute.LogGuid,
				"",
				nil,
				aRoute.GetTTL(),
				aRoute.RouteServiceUrl,
				aRoute.ModificationTag,
			))
	}
}

func routeEquals(current, desired models.Route) bool {
	if current.Route == desired.Route && current.IP == desired.IP && current.Port == desired.Port {
		return true
	}

	return false
}
