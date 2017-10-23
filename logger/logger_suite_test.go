/*
 * Portions Copyright (c) 2017, F5 Networks, Inc.
 */

package logger_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestLogger(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Logger Suite")
}
