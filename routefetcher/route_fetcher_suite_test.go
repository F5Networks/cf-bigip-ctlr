/*
 * Portions Copyright (c) 2017,2018, F5 Networks, Inc.
 */

package routefetcher_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestRouteFetcher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RouteFetcher Suite")
}
