package f5router

import (
	"io/ioutil"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var expectedConfigs [][]byte
var baseDir = "../testdata/resources/"

//NOTE: (rtalley): Expected file name format config[num].json
type SortFiles []os.FileInfo

func (sf SortFiles) Len() int      { return len(sf) }
func (sf SortFiles) Swap(i, j int) { sf[i], sf[j] = sf[j], sf[i] }
func (sf SortFiles) Less(i, j int) bool {
	piece1 := strings.Split(strings.Split(sf[i].Name(), "config")[1], ".")[0]
	piece2 := strings.Split(strings.Split(sf[j].Name(), "config")[1], ".")[0]
	val1, _ := strconv.Atoi(piece1)
	val2, _ := strconv.Atoi(piece2)
	return val1 < val2
}

func TestF5router(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "F5router Suite")
}

var _ = BeforeSuite(func() {
	SetDefaultEventuallyTimeout(5 * time.Second)
	SetDefaultEventuallyPollingInterval(100 * time.Millisecond)

	files, err := ioutil.ReadDir(baseDir)
	Expect(err).ToNot(HaveOccurred())
	sort.Sort(SortFiles(files))

	for _, file := range files {
		f, fileErr := ioutil.ReadFile(baseDir + file.Name())
		Expect(fileErr).ToNot(HaveOccurred())
		expectedConfigs = append(expectedConfigs, f)
	}
})
