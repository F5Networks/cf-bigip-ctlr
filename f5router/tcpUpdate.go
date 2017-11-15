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
	"fmt"
	"strconv"

	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/bigipResources"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/routeUpdate"
	"github.com/F5Networks/cf-bigip-ctlr/logger"
)

type updateTCP struct {
	c         *config.Config
	logger    logger.Logger
	op        routeUpdate.Operation
	routePort uint16
	member    bigipResources.Member
	name      string
	protocol  string
}

// NewTCPUpdate satisfies the interface of a routeUpdate
func NewTCPUpdate(
	c *config.Config,
	logger logger.Logger,
	op routeUpdate.Operation,
	routePort uint16,
	member bigipResources.Member,
) (updateTCP, error) {

	return updateTCP{
		c:         c,
		logger:    logger,
		op:        op,
		routePort: routePort,
		member:    member,
		name:      createTCPObjectName(c, routePort),
		protocol:  "tcp",
	}, nil
}

func (tu updateTCP) CreateResources(c *config.Config) (bigipResources.Resources, error) {
	rs := bigipResources.Resources{}
	va := &bigipResources.VirtualAddress{
		BindAddr: tu.c.BigIP.ExternalAddr,
		Port:     int32(tu.routePort),
	}

	dest, err := verifyDestAddress(va, tu.c.BigIP.Partitions[0])
	if err != nil {
		return rs, err
	}

	// FIXME need to handle multiple tcp router groups
	poolDescrip := fmt.Sprintf("route-port: %d, router-group: %s", tu.routePort, c.TCPRouterGroupName)
	pool := makePool(c, tu.name, poolDescrip, tu.member)
	rs.Pools = append(rs.Pools, pool)

	vs := &bigipResources.Virtual{
		VirtualServerName:     tu.name,
		PoolName:              tu.name,
		Mode:                  "tcp",
		Enabled:               true,
		Destination:           dest,
		SourceAddrTranslation: bigipResources.SourceAddrTranslation{Type: "automap"},
	}

	if nil != vs {
		rs.Virtuals = append(rs.Virtuals, vs)
	}
	return rs, nil
}

func (tu updateTCP) Protocol() string {
	return tu.protocol
}

func (tu updateTCP) Op() routeUpdate.Operation {
	return tu.op
}

func (tu updateTCP) Name() string {
	return tu.name
}

func (tu updateTCP) Route() string {
	return strconv.Itoa(int(tu.routePort))
}

func createTCPObjectName(c *config.Config, port uint16) string {
	name := fmt.Sprintf("cf-tcp-route-%s-%s", c.TCPRouterGroupName, strconv.Itoa(int(port)))
	return name
}
