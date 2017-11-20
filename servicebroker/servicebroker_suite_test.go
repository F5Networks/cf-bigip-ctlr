package servicebroker

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func Testservicebroker(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "servicebroker Suite")
}
