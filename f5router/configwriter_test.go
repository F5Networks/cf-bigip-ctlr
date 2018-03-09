/*-
 * Copyright (c) 2017,2018, F5 Networks, Inc.
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
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/F5Networks/cf-bigip-ctlr/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Configwriter", func() {
	It("should be true", func() {
		Expect(true).To(BeTrue())
	})
	Describe("running", func() {

		var (
			logger *test_util.TestZapLogger
			cw     *ConfigWriter
			err    error
		)

		BeforeEach(func() {
			logger = test_util.NewTestZapLogger("router-test")
			cw, err = NewConfigWriter(logger)

			Expect(cw).NotTo(BeNil())
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if nil != cw {
				cw.Close()
			}
			if nil != logger {
				logger.Close()
			}
		})

		It("should locate the file", func() {
			f := cw.GetOutputFilename()

			dir := filepath.Dir(f)
			_, err = os.Stat(dir)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should write a simple config", func() {
			var d []byte
			var e []byte
			var n int
			var expected []byte
			var written []byte

			f := cw.GetOutputFilename()

			defer func() {
				Expect(f).To(BeARegularFile())
			}()

			testData := simpleTest{
				Test: testSection{
					Field1: "test-field1",
					Field2: 121232343,
					Field3: &testSubSection{
						SubField1: "test-sub-field1",
						SubField2: 42,
					},
				},
			}

			testFile(f, false)

			d, err = json.Marshal(testData)
			Expect(err).NotTo(HaveOccurred())

			n, err = cw.Write(d)
			Expect(n).To(Equal(len(d)))
			Expect(err).NotTo(HaveOccurred())

			testFile(f, true)

			expected, err = json.Marshal(testData)
			Expect(err).NotTo(HaveOccurred())

			written, err = ioutil.ReadFile(f)
			Expect(err).NotTo(HaveOccurred())

			Expect(written).To(Equal(expected))

			// test empty section and overwrite
			empty := struct {
				Section struct {
					Field string `json:"field,omitempty"`
				} `json:"simple-test"`
			}{Section: struct {
				Field string `json:"field,omitempty"`
			}{
				Field: "",
			},
			}

			e, err = json.Marshal(empty)
			Expect(err).NotTo(HaveOccurred())

			n, err = cw.Write(e)
			Expect(n).To(Equal(len(e)))
			Expect(err).NotTo(HaveOccurred())

			testFile(f, true)

			expected, err = json.Marshal(empty)
			Expect(err).NotTo(HaveOccurred())

			written, err = ioutil.ReadFile(f)
			Expect(err).NotTo(HaveOccurred())
			Expect(written).To(Equal(expected))

			// add section back
			n, err = cw.Write(d)
			Expect(n).To(Equal(len(d)))
			Expect(err).NotTo(HaveOccurred())

			testFile(f, true)

			expected, err = json.Marshal(testData)
			Expect(err).NotTo(HaveOccurred())

			written, err = ioutil.ReadFile(f)
			Expect(err).NotTo(HaveOccurred())

			Expect(written).To(Equal(expected))
		})

		Context("fail cases", func() {
			It("should error when encountering a bad FD", func() {
				// go does not have an idea of a File interface, doing the best
				// we can to try and create some negative behaviors
				mockFile := newPseudoFile(failLock)
				Expect(mockFile).NotTo(BeNil())
				defer func() {
					err = mockFile.RealFile.Close()
					Expect(err).NotTo(HaveOccurred())

					os.Remove(mockFile.RealFile.Name())
				}()

				var wrote int
				Expect(func() {
					wrote, err = cw._write(mockFile, []byte("hello"))
				}).NotTo(Panic())
				Expect(wrote).To(BeZero())
				Expect(err).To(HaveOccurred())

				expected := "bad file descriptor"
				Expect(err).To(MatchError(expected))
			})

			It("should error on a failed truncate", func() {
				// go does not have an idea of a File interface, doing the best
				// we can to try and create some negative behaviors
				mockFile := newPseudoFile(failTruncate)
				Expect(mockFile).NotTo(BeNil())
				defer func() {
					err = mockFile.RealFile.Close()
					Expect(err).NotTo(HaveOccurred())

					os.Remove(mockFile.RealFile.Name())
				}()

				var wrote int
				Expect(func() {
					wrote, err = cw._write(mockFile, []byte("hello"))
				}).NotTo(Panic())
				Expect(wrote).To(BeZero())
				Expect(err).To(HaveOccurred())

				expected := "mock file truncate error"
				Expect(err).To(MatchError(expected))
			})

			It("should error on a failed write", func() {
				// go does not have an idea of a File interface, doing the best
				// we can to try and create some negative behaviors
				mockFile := newPseudoFile(failWrite)
				Expect(mockFile).NotTo(BeNil())
				defer func() {
					err = mockFile.RealFile.Close()
					Expect(err).NotTo(HaveOccurred())

					os.Remove(mockFile.RealFile.Name())
				}()

				var wrote int
				Expect(func() {
					wrote, err = cw._write(mockFile, []byte("hello"))
				}).NotTo(Panic())
				Expect(wrote).To(BeZero())
				Expect(err).To(HaveOccurred())

				expected := "mock file write error"
				Expect(err).To(MatchError(expected))
			})

			It("should error on a short write", func() {
				// go does not have an idea of a File interface, doing the best
				// we can to try and create some negative behaviors
				mockFile := newPseudoFile(failShortWrite)
				Expect(mockFile).NotTo(BeNil())
				defer func() {
					err = mockFile.RealFile.Close()
					Expect(err).NotTo(HaveOccurred())

					os.Remove(mockFile.RealFile.Name())
				}()

				var wrote int
				Expect(func() {
					wrote, err = cw._write(mockFile, []byte("hello"))
				}).NotTo(Panic())
				Expect(wrote).NotTo(BeZero())
				Expect(err).To(HaveOccurred())

				expected := "mock file short write"
				Expect(err).To(MatchError(expected))
			})

			It("should error if config file doesn't exist", func() {
				logger := test_util.NewTestZapLogger("router-test")
				cw = &ConfigWriter{
					configFile: "/this-file/really/probably/will/not/exist",
					logger:     logger,
				}

				var wrote int
				Expect(func() {
					wrote, err = cw.Write([]byte("hello"))
				}).NotTo(Panic())
				Expect(wrote).To(BeZero())
				Expect(err).To(HaveOccurred())

				expected := "open /this-file/really/probably/will/not/exist: no such file or directory"
				Expect(err).To(MatchError(expected))
			})

			It("should error when encountering a bad unlock", func() {
				// go does not have an idea of a File interface, doing the best
				// we can to try and create some negative behaviors
				mockFile := newPseudoFile(failUnlock)
				Expect(mockFile).NotTo(BeNil())
				defer func() {
					err = mockFile.RealFile.Close()
					Expect(err).NotTo(HaveOccurred())

					os.Remove(mockFile.RealFile.Name())
				}()

				var wrote int
				Expect(func() {
					wrote, err = cw._write(mockFile, []byte("hello"))
				}).NotTo(Panic())
				Expect(wrote).To(BeZero())
				Expect(err).To(HaveOccurred())

				expected := "bad file descriptor"
				Expect(err).To(MatchError(expected))
			})
		})
	})
})

const (
	failLock = iota
	failUnlock
	failTruncate
	failWrite
	failShortWrite
)

type pseudoFile struct {
	FailStyle int
	RealFile  *os.File
	BadFd     uintptr
}

func newPseudoFile(failure int) *pseudoFile {
	f, err := ioutil.TempFile("/tmp", "config-writer-unit-test")
	ExpectWithOffset(1, f).NotTo(BeNil())
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	pf := &pseudoFile{
		FailStyle: failure,
		RealFile:  f,
		BadFd:     uintptr(10101001),
	}
	return pf
}

func (pf *pseudoFile) Close() error {
	return nil
}

func (pf *pseudoFile) Fd() uintptr {
	switch pf.FailStyle {
	case failLock:
		return pf.BadFd
	case failUnlock:
		return pf.BadFd
	default:
		return pf.RealFile.Fd()
	}
}

func (pf *pseudoFile) Truncate(size int64) error {
	switch pf.FailStyle {
	case failTruncate:
		return errors.New("mock file truncate error")
	default:
		return nil
	}
}

func (pf *pseudoFile) Write(b []byte) (n int, err error) {
	switch pf.FailStyle {
	case failWrite:
		n = 0
		err = errors.New("mock file write error")
	case failShortWrite:
		n = 1
		err = errors.New("mock file short write")
	default:
		n = 100
		err = nil
	}
	return n, err
}

type testSubSection struct {
	SubField1 string `json:"sub-field1-str,omitempty"`
	SubField2 int    `json:"sub-field2-int,omitempty"`
}
type testSection struct {
	Field1 string          `json:"field1-str,omitempty"`
	Field2 int             `json:"field2-int,omitempty"`
	Field3 *testSubSection `json:"field3-struct,omitempty"`
}

type simpleTest struct {
	Test testSection `json:"simple-test"`
}

func testFile(f string, shouldExist bool) {
	if false == shouldExist {
		ExpectWithOffset(1, f).NotTo(BeAnExistingFile(), "The file should not exist")
	} else {
		ExpectWithOffset(1, f).To(BeARegularFile(), "The file should exist")
	}
}
