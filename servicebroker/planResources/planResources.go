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

package planResources

import (
	"github.com/F5Networks/cf-bigip-ctlr/f5router/bigipResources"
)

type (
	// Plans holds our plans
	Plans struct {
		Plans []Plan `json:"plans"`
	}

	// Plan used for per route config
	Plan struct {
		Name          string      `json:"name,omitempty"`
		Description   string      `json:"description,omitempty"`
		Pool          PoolType    `json:"pool,omitempty"`
		VirtualServer VirtualType `json:"virtualServer,omitempty"`
		ID            string
	}

	// PoolType holds pool info
	PoolType struct {
		Balance        string                   `json:"balance,omitempty"`
		HealthMonitors []bigipResources.Monitor `json:"healthMonitors,omitempty"`
	}

	// VirtualType holds virtual info
	VirtualType struct {
		Policies    []string `json:"policies,omitempty"`
		Profiles    []string `json:"profiles,omitempty"`
		SslProfiles []string `json:"sslProfiles,omitempty"`
	}
)
