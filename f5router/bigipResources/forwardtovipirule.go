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
 * WITHOUT WARRANTIES OR conditionS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package bigipResources

//ForwardToVIPiRule forwards traffic from a tier1 vip to a tier2
const (
	// HTTPForwardingiRuleName on BIG-IP
	HTTPForwardingiRuleName = "forward-to-vip"
	// ForwardToVIPiRule irule used to forward to the tier2 vips
	ForwardToVIPiRule = `
when HTTP_REQUEST {
  if {[info exists target_vip] && [string length $target_vip] != 0} {
    if { [catch { virtual $target_vip } ] } {
      log local0. "ERROR: Attempting to assign traffic to non-existent virtual $target_vip"
      reject
    }
  }
}`
)
