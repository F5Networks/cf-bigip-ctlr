/*-
 * Copyright (c) 2016-2018, F5 Networks, Inc.
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
	"os"

	"github.com/F5Networks/cf-bigip-ctlr/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
)

var _ = Describe("Python Driver", func() {
	Describe("running driver", func() {
		var logger *test_util.TestZapLogger
		var driver *Driver
		var signals chan os.Signal
		var ready chan struct{}

		BeforeEach(func() {
			signals = make(chan os.Signal)
			ready = make(chan struct{})
			driverCmd := "../testdata/fake_driver.py"
			fileName := "fake.json"
			logger = test_util.NewTestZapLogger("driver-test")
			driver = NewDriver(fileName, driverCmd, logger)
		})

		AfterEach(func() {
			if nil != logger {
				logger.Close()
			}
		})

		It("should start and exit properly", func() {
			go func() {
				defer GinkgoRecover()
				Expect(func() {
					driver.Run(signals, ready)
				}).NotTo(Panic())
			}()

			Eventually(ready).Should(BeClosed())
			Eventually(logger).Should(Say("f5router-driver-started"))
			signals <- os.Interrupt
			Eventually(logger).Should(SatisfyAll(Say("f5router-driver-exited-normally"), Say("f5router-driver-stopped")))
		})

		It("should stop when signaled", func() {
			go func() {
				defer GinkgoRecover()
				Expect(func() {
					driver.Run(signals, ready)
				}).NotTo(Panic())
			}()

			Eventually(ready).Should(BeClosed())
			Eventually(logger).Should(Say("f5router-driver-started"))
			signals <- os.Kill
			Eventually(logger).Should(SatisfyAll(Say("f5router-driver-signaled-to-stop"), Say("f5router-driver-stopped")))
		})

	})
})
