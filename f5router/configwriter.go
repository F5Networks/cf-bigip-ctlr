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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"

	"github.com/cf-bigip-ctlr/logger"

	"github.com/uber-go/zap"
)

// Writer interface to support unit testing
type Writer interface {
	GetOutputFilename() string
	Write(input []byte) (n int, err error)
}

// ConfigWriter Writer instance to output configuration
type ConfigWriter struct {
	configFile string
	logger     logger.Logger
}

// Without a File interface unit testing becomes difficult,
// use an internal Interface which describes what we need
// from the file and which we can then mock in _test
type pseudoFileInterface interface {
	Close() error
	Fd() uintptr
	Truncate(size int64) error
	Write(b []byte) (n int, err error)
}

// NewConfigWriter creates and returns a config writer
func NewConfigWriter(logger logger.Logger) (*ConfigWriter, error) {
	dir, err := ioutil.TempDir("", "cf-bigip-ctlr.config")
	if nil != err {
		return nil, fmt.Errorf("could not create unique config directory: %v", err)
	}

	tmpfn := filepath.Join(dir, "config.json")

	cw := &ConfigWriter{
		configFile: tmpfn,
		logger:     logger,
	}

	logger.Info("f5router-configwriter-started",
		zap.String("configwriter", fmt.Sprintf("%p", cw)))

	return cw, nil
}

// Close close file and delete temp file
func (cw *ConfigWriter) Close() {
	os.RemoveAll(filepath.Dir(cw.configFile))

	cw.logger.Info("f5router-configwriter-file-closed")
}

// GetOutputFilename return config filename
func (cw *ConfigWriter) GetOutputFilename() string {
	return cw.configFile
}

// Write creates file lock and outputs byte slice
func (cw *ConfigWriter) Write(input []byte) (n int, err error) {
	f, err := os.OpenFile(cw.configFile, os.O_WRONLY|os.O_CREATE, 0644)
	if nil != err {
		return n, err
	}

	defer func() {
		if nil != err {
			f.Close()
		} else {
			err = f.Close()
		}
	}()

	return cw._write(f, input)
}

func (cw *ConfigWriter) _write(
	f pseudoFileInterface,
	input []byte,
) (n int, err error) {
	flock := syscall.Flock_t{
		Type:   syscall.F_WRLCK,
		Start:  0,
		Len:    0,
		Whence: int16(os.SEEK_SET),
	}
	err = syscall.FcntlFlock(uintptr(f.Fd()), syscall.F_SETLKW, &flock)
	if nil != err {
		return n, err
	}

	err = f.Truncate(0)
	if nil != err {
		return n, err
	}
	n, err = f.Write(input)
	if nil != err {
		return n, err
	}

	flock = syscall.Flock_t{
		Type:   syscall.F_UNLCK,
		Start:  0,
		Len:    0,
		Whence: int16(os.SEEK_SET),
	}
	err = syscall.FcntlFlock(uintptr(f.Fd()), syscall.F_SETLKW, &flock)
	if nil != err {
		return n, err
	}

	return n, err
}
