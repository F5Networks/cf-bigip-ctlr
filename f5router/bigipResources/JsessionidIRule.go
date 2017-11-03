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

package bigipResources

const (
	// JSESSIONIDiRuleName on BIG-IP
	JsessionidIRuleName = "jsessionid-persistence"

	// JSESSIONID iRule on the BIG-IP
	JsessionidIRule = `
when HTTP_RESPONSE {
  set jsessionid [lsearch -inline -regexp [HTTP::cookie names] (?i)^jsessionid$]
  set cookieVal [HTTP::cookie value $jsessionid]
  if { $jsessionid ne "" } {
    set maxAge [HTTP::cookie maxage $jsessionid]
    if { $maxAge < 0 } {
      persist add uie $cookieVal 3600
    } elseif { $maxAge == 0 } {
      if { [persist lookup uie $cookieVal] } {
        persist delete uie $cookieVal
      }
    } else {
      persist add uie $cookieVal $maxAge
    }
  }
}
when HTTP_REQUEST {
  set jsessionid [lsearch -inline -regexp [HTTP::cookie names] (?i)^jsessionid$]
  set cookieVal [HTTP::cookie value $jsessionid]
  if { $jsessionid ne "" } {
    set forwardNode [persist lookup uie $cookieVal node]
    set forwardPort [persist lookup uie $cookieVal port]
    set forwardIP $forwardNode:$forwardPort
    if { $forwardNode ne "" && $forwardPort ne "" } {
      node $forwardIP
    } else {
      log local0. "Could not find endpoint for persistence record: $cookieVal. \
      Check to see if this record still exists (check Statistics -> Module Statistics -> Local \
      Traffic -> Persistence Records) or the status of the records endpoint."
    }
  }
}`
)
