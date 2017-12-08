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

package schema_test

import (
	"os"

	"github.com/F5Networks/cf-bigip-ctlr/schema"
	"github.com/F5Networks/cf-bigip-ctlr/test_util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("Schema", func() {
	var (
		logger           *test_util.TestZapLogger
		validConfig      string
		validLargeConfig string
		invalidConfig    string
	)

	BeforeEach(func() {
		os.Setenv("TEST_MODE", "true")
		logger = test_util.NewTestZapLogger("schema-test")

		validConfig = `{"plans":[{"description":"arggg","name":"test","virtualServer":{"policies":["potato"]}}]}`
		invalidConfig = `{"plans":[{"description":"arggg","name":"test","virtualServer":{"policies":[]}}]}`
		validLargeConfig = `{
      "plans": [{
        "description": "arggg",
        "name": "test",
        "virtualServer": {
          "policies": ["potato"]
        }
      }, {
        "name": "test2",
        "description": "more argggg",
        "virtualServer": {
          "policies": ["potato", "eggs"],
          "profiles": ["bacon"],
          "sslProfiles": ["foo"]
        },
        "pool": {
          "balance": "ratio-member",
          "healthMonitors": [{
            "name": "0",
            "interval": 1,
            "protocol": "tcp",
            "send": "hello",
            "timeout": 60
          }]
        }
      }, {
        "name": "test3",
        "description":"again argg",
        "pool": {
          "healthMonitors": [{"name": "/Common/http"}]
        }
      }]
    }`
	})

	AfterEach(func() {
		if nil != logger {
			logger.Close()
		}
		os.Unsetenv("SERVICE_BROKER_CONFIG")
		os.Unsetenv("TEST_MODE")
	})

	It("validates a valid config", func() {
		val, err := schema.VerifySchema(validConfig, logger)
		Expect(val).To(BeTrue())
		Expect(err).To(BeNil())
	})

	It("validates a large config", func() {
		val, err := schema.VerifySchema(validLargeConfig, logger)
		Expect(val).To(BeTrue())
		Expect(err).To(BeNil())
	})

	It("fails against an invalid config", func() {
		val, err := schema.VerifySchema(invalidConfig, logger)
		Expect(val).To(BeFalse())
		Expect(err).To(BeNil())
		Eventually(logger).Should(gbytes.Say("schema-not-valid"))
	})

})
