/*
 * Portions Copyright (c) 2017, F5 Networks, Inc.
 */

package config_test

import (
	"testing"

	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var (
	logger lager.Logger
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Suite")
}

var _ = BeforeEach(func() {
	logger = lagertest.NewTestLogger("test")
})
