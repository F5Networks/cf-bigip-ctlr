/*-
 * Copyright (c) 2016,2017, F5 Networks, Inc.
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
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSort(t *testing.T) {
	routes := routeConfigs{}

	expectedList := make(routeConfigs, 10)

	vs := routeConfig{}
	vs.routeItem.Backend.ServiceName = "bar"
	vs.routeItem.Backend.ServicePort = 80
	routes = append(routes, &vs)
	expectedList[1] = &vs

	vs = routeConfig{}
	vs.routeItem.Backend.ServiceName = "foo"
	vs.routeItem.Backend.ServicePort = 2
	routes = append(routes, &vs)
	expectedList[5] = &vs

	vs = routeConfig{}
	vs.routeItem.Backend.ServiceName = "foo"
	vs.routeItem.Backend.ServicePort = 8080
	routes = append(routes, &vs)
	expectedList[7] = &vs

	vs = routeConfig{}
	vs.routeItem.Backend.ServiceName = "baz"
	vs.routeItem.Backend.ServicePort = 1
	routes = append(routes, &vs)
	expectedList[2] = &vs

	vs = routeConfig{}
	vs.routeItem.Backend.ServiceName = "foo"
	vs.routeItem.Backend.ServicePort = 80
	routes = append(routes, &vs)
	expectedList[6] = &vs

	vs = routeConfig{}
	vs.routeItem.Backend.ServiceName = "foo"
	vs.routeItem.Backend.ServicePort = 9090
	routes = append(routes, &vs)
	expectedList[9] = &vs

	vs = routeConfig{}
	vs.routeItem.Backend.ServiceName = "baz"
	vs.routeItem.Backend.ServicePort = 1000
	routes = append(routes, &vs)
	expectedList[3] = &vs

	vs = routeConfig{}
	vs.routeItem.Backend.ServiceName = "foo"
	vs.routeItem.Backend.ServicePort = 8080
	routes = append(routes, &vs)
	expectedList[8] = &vs

	vs = routeConfig{}
	vs.routeItem.Backend.ServiceName = "foo"
	vs.routeItem.Backend.ServicePort = 1
	routes = append(routes, &vs)
	expectedList[4] = &vs

	vs = routeConfig{}
	vs.routeItem.Backend.ServiceName = "bar"
	vs.routeItem.Backend.ServicePort = 1
	routes = append(routes, &vs)
	expectedList[0] = &vs

	sort.Sort(routes)

	for i := range expectedList {
		require.EqualValues(t, expectedList[i], routes[i],
			"Sorted list elements should be equal")
	}
}
