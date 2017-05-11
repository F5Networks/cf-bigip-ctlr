package routeservice_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestRouteService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RouteService Suite")
}
