/*-
 * Copyright (c) 2017, F5 Networks, Inc.
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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/cf-bigip-ctlr/test_util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func newPseudoFile(t *testing.T, failure int) *pseudoFile {
	f, err := ioutil.TempFile("/tmp", "config-writer-unit-test")
	require.NoError(t, err)
	require.NotNil(t, f)

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

func testFile(t *testing.T, f string, shouldExist bool) {
	_, err := os.Stat(f)

	if false == shouldExist {
		assert.NotNil(t, err)
		assert.True(t, os.IsNotExist(err))
	} else {
		assert.Nil(t, err)
		if nil != err {
			assert.False(t, os.IsNotExist(err))
		}
	}
}

func TestConfigWriterGetters(t *testing.T) {
	logger := test_util.NewTestZapLogger("router-test")
	cw, err := NewConfigWriter(logger)
	assert.Nil(t, err)
	require.NotNil(t, cw)
	defer func() {
		cw.Close()
	}()

	f := cw.GetOutputFilename()

	dir := filepath.Dir(f)
	_, err = os.Stat(dir)
	assert.NoError(t, err)
}

func TestConfigWriterSimpleWrite(t *testing.T) {
	logger := test_util.NewTestZapLogger("router-test")
	cw, err := NewConfigWriter(logger)
	assert.Nil(t, err)
	require.NotNil(t, cw)
	defer func() {
		cw.Close()
	}()

	f := cw.GetOutputFilename()
	fmt.Printf("%v\n", f)

	defer func() {
		_, err := os.Stat(f)
		assert.False(t, os.IsExist(err))
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

	testFile(t, f, false)

	d, err := json.Marshal(testData)
	assert.NoError(t, err)

	n, err := cw.Write(d)
	assert.Equal(t, len(d), n)
	assert.NoError(t, err)

	testFile(t, f, true)

	expected, err := json.Marshal(testData)
	assert.Nil(t, err)

	written, err := ioutil.ReadFile(f)
	assert.Nil(t, err)

	assert.EqualValues(t, expected, written)

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

	e, err := json.Marshal(empty)
	assert.NoError(t, err)

	n, err = cw.Write(e)
	assert.Equal(t, len(e), n)
	assert.NoError(t, err)

	testFile(t, f, true)

	expected, err = json.Marshal(empty)
	assert.Nil(t, err)

	written, err = ioutil.ReadFile(f)
	assert.Nil(t, err)

	assert.EqualValues(t, expected, written)

	// add section back
	n, err = cw.Write(d)
	assert.Equal(t, len(d), n)
	assert.NoError(t, err)

	testFile(t, f, true)

	expected, err = json.Marshal(testData)
	assert.Nil(t, err)

	written, err = ioutil.ReadFile(f)
	assert.Nil(t, err)

	assert.EqualValues(t, expected, written)
}

func TestConfigWriterWriteFailOpen(t *testing.T) {
	logger := test_util.NewTestZapLogger("router-test")
	cw := &ConfigWriter{
		configFile: "/this-file/really/probably/will/not/exist",
		logger:     logger,
	}

	var wrote int
	var err error
	require.NotPanics(t, func() {
		wrote, err = cw.Write([]byte("hello"))
	})
	assert.Zero(t, wrote)
	assert.Error(t, err)

	expected := "open /this-file/really/probably/will/not/exist: no such file or directory"
	assert.Equal(t, expected, err.Error())
}

func TestConfigWriterFailLock(t *testing.T) {
	logger := test_util.NewTestZapLogger("router-test")
	cw, err := NewConfigWriter(logger)
	assert.Nil(t, err)
	require.NotNil(t, cw)
	defer func() {
		cw.Close()
	}()

	// go does not have an idea of a File interface, doing the best
	// we can to try and create some negative behaviors
	mockFile := newPseudoFile(t, failLock)
	require.NotNil(t, mockFile)
	defer func() {
		err := mockFile.RealFile.Close()
		assert.NoError(t, err)

		os.Remove(mockFile.RealFile.Name())
	}()

	var wrote int
	require.NotPanics(t, func() {
		wrote, err = cw._write(mockFile, []byte("hello"))
	})
	assert.Zero(t, wrote)
	assert.Error(t, err)

	expected := "bad file descriptor"
	assert.Equal(t, expected, err.Error())
}

func TestConfigWriterFailUnlock(t *testing.T) {
	logger := test_util.NewTestZapLogger("router-test")
	cw, err := NewConfigWriter(logger)
	assert.Nil(t, err)
	require.NotNil(t, cw)
	defer func() {
		cw.Close()
	}()

	// go does not have an idea of a File interface, doing the best
	// we can to try and create some negative behaviors
	mockFile := newPseudoFile(t, failUnlock)
	require.NotNil(t, mockFile)
	defer func() {
		err := mockFile.RealFile.Close()
		assert.NoError(t, err)

		os.Remove(mockFile.RealFile.Name())
	}()

	var wrote int
	require.NotPanics(t, func() {
		wrote, err = cw._write(mockFile, []byte("hello"))
	})
	assert.Zero(t, wrote)
	assert.Error(t, err)

	expected := "bad file descriptor"
	assert.Equal(t, expected, err.Error())
}

func TestConfigWriterFailTrunc(t *testing.T) {
	logger := test_util.NewTestZapLogger("router-test")
	cw, err := NewConfigWriter(logger)
	assert.Nil(t, err)
	require.NotNil(t, cw)
	defer func() {
		cw.Close()
	}()

	// go does not have an idea of a File interface, doing the best
	// we can to try and create some negative behaviors
	mockFile := newPseudoFile(t, failTruncate)
	require.NotNil(t, mockFile)
	defer func() {
		err := mockFile.RealFile.Close()
		assert.NoError(t, err)

		os.Remove(mockFile.RealFile.Name())
	}()

	var wrote int
	require.NotPanics(t, func() {
		wrote, err = cw._write(mockFile, []byte("hello"))
	})
	assert.Zero(t, wrote)
	assert.Error(t, err)

	expected := "mock file truncate error"
	assert.Equal(t, expected, err.Error())
}

func TestConfigWriterFailWrite(t *testing.T) {
	logger := test_util.NewTestZapLogger("router-test")
	cw, err := NewConfigWriter(logger)
	assert.Nil(t, err)
	require.NotNil(t, cw)
	defer func() {
		cw.Close()
	}()

	// go does not have an idea of a File interface, doing the best
	// we can to try and create some negative behaviors
	mockFile := newPseudoFile(t, failWrite)
	require.NotNil(t, mockFile)
	defer func() {
		err := mockFile.RealFile.Close()
		assert.NoError(t, err)

		os.Remove(mockFile.RealFile.Name())
	}()

	var wrote int
	require.NotPanics(t, func() {
		wrote, err = cw._write(mockFile, []byte("hello"))
	})
	assert.Zero(t, wrote)
	assert.Error(t, err)

	expected := "mock file write error"
	assert.Equal(t, expected, err.Error())
}

func TestConfigWriterFailShortWrite(t *testing.T) {
	logger := test_util.NewTestZapLogger("router-test")
	cw, err := NewConfigWriter(logger)
	assert.Nil(t, err)
	require.NotNil(t, cw)
	defer func() {
		cw.Close()
	}()

	// go does not have an idea of a File interface, doing the best
	// we can to try and create some negative behaviors
	mockFile := newPseudoFile(t, failShortWrite)
	require.NotNil(t, mockFile)
	defer func() {
		err := mockFile.RealFile.Close()
		assert.NoError(t, err)

		os.Remove(mockFile.RealFile.Name())
	}()

	var wrote int
	require.NotPanics(t, func() {
		wrote, err = cw._write(mockFile, []byte("hello"))
	})
	assert.NotZero(t, wrote)
	assert.Error(t, err)

	expected := "mock file short write"
	assert.Equal(t, expected, err.Error())
}
