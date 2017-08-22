/*-
 * Copyright (c) 2017, F5 Networks, Inc.
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

package f5router

import (
	"errors"

	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/bigipResources"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/routeUpdate"
	"github.com/F5Networks/cf-bigip-ctlr/logger"
	"github.com/F5Networks/cf-bigip-ctlr/route"

	"github.com/uber-go/zap"
)

type updateHTTP struct {
	logger   logger.Logger
	op       routeUpdate.Operation
	uri      route.Uri
	endpoint *route.Endpoint
	name     string
	protocol string
}

func NewUpdate(
	logger logger.Logger,
	op routeUpdate.Operation,
	uri route.Uri,
	ep *route.Endpoint,
) (updateHTTP, error) {
	l := logger.Session("http-update")
	l.Debug("new-update", zap.String("URI", uri.String()))

	if len(uri) == 0 {
		return updateHTTP{}, errors.New("uri length of zero is not allowed")
	}

	return updateHTTP{
		logger:   logger,
		op:       op,
		uri:      uri,
		endpoint: ep,
		name:     makeObjectName(uri.String()),
		protocol: "http",
	}, nil
}

func (hu updateHTTP) CreateResources(c *config.Config) bigipResources.Resources {
	//  This will create the pool for the http update
	rs := bigipResources.Resources{}

	pool := makePool(
		c,
		hu.name,
		makeDescription(hu.uri.String(), hu.endpoint.ApplicationId),
		hu.endpoint.CanonicalAddr(),
	)
	rs.Pools = append(rs.Pools, pool)

	return rs
}

func (hu updateHTTP) Protocol() string {
	return hu.protocol
}

func (hu updateHTTP) Op() routeUpdate.Operation {
	return hu.op
}

func (hu updateHTTP) URI() route.Uri {
	return hu.uri
}

func (hu updateHTTP) Name() string {
	return hu.name
}

func (hu updateHTTP) AppID() string {
	return hu.endpoint.ApplicationId
}

func (hu updateHTTP) Route() string {
	return hu.uri.String()
}
