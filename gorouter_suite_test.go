package main_test

import (
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	"github.com/onsi/gomega/ghttp"
)

var (
	gorouterPath string
	oauthServer  *ghttp.Server
)

func TestGorouter(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CF BigIP Controller Suite")
}

var _ = BeforeSuite(func() {
	var path string
	var err error

	variant, ok := os.LookupEnv("BUILD_VARIANT")
	if !ok || variant == "release" {
		path, err = gexec.Build("github.com/F5Networks/cf-bigip-ctlr")
	} else if variant == "debug" {
		path, err = gexec.Build("github.com/F5Networks/cf-bigip-ctlr", "-race")
	} else {
		Expect(variant).To(
			Or(
				Equal("debug"),
				Equal("release"),
				Equal(""),
			),
		)
	}

	Expect(err).ToNot(HaveOccurred())
	gorouterPath = path
	SetDefaultEventuallyTimeout(15 * time.Second)
	SetDefaultEventuallyPollingInterval(100 * time.Millisecond)
	SetDefaultConsistentlyDuration(1 * time.Second)
	SetDefaultConsistentlyPollingInterval(10 * time.Millisecond)
	oauthServer = setupTlsServer()
	oauthServer.HTTPTestServer.StartTLS()
})

var _ = AfterSuite(func() {
	if oauthServer != nil {
		oauthServer.Close()
	}
	gexec.CleanupBuildArtifacts()
})
