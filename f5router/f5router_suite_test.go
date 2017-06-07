package f5router

import (
	"io/ioutil"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var expectedConfigs [][]byte
var baseDir = "../testdata/resources/"

func TestF5router(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "F5router Suite")
}

var _ = BeforeSuite(func() {
	SetDefaultEventuallyTimeout(5 * time.Second)
	SetDefaultEventuallyPollingInterval(100 * time.Millisecond)

	files, err := ioutil.ReadDir(baseDir)
	Expect(err).ToNot(HaveOccurred())

	for _, file := range files {
		f, fileErr := ioutil.ReadFile(baseDir + file.Name())
		Expect(fileErr).ToNot(HaveOccurred())
		expectedConfigs = append(expectedConfigs, f)
	}
})
