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

package routeUpdate

import (
	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/f5router/bigipResources"
)

// Listener optional listener for route registry updates
//go:generate counterfeiter -o fakes/fake_listener.go . Listener
type Listener interface {
	UpdateRoute(RouteUpdate)
}

// RouteUpdate interface to wrap different protocols
type RouteUpdate interface {
	CreateResources(*config.Config) (bigipResources.Resources, error)
	Protocol() string
	Op() Operation
	Name() string
	Route() string
}

// Operation of registry change
type Operation int

const (
	// Add operation
	Add Operation = iota
	// Remove operation
	Remove
)

func (op Operation) String() string {
	switch op {
	case Add:
		return "Add"
	case Remove:
		return "Remove"
	}
	return "Unknown"
}
