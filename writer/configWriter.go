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

package writer

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cf-bigip-ctlr/logger"

	"github.com/uber-go/zap"
)

// Writer writes out the config for the python driver
type Writer interface {
	GetOutputFilename() string
	Stop()
	SendSection(string, interface{}) (<-chan struct{}, <-chan error, error)
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

type configWriter struct {
	configFile string
	stopCh     chan struct{}
	dataCh     chan configSection
	sectionMap map[string]interface{}
	logger     logger.Logger
}

type configSection struct {
	name    string
	data    interface{}
	doneCh  chan struct{}
	errorCh chan error
}

type doneCall func(chan<- struct{})
type errCall func(chan<- error, error)

// NewConfigWriter creates and returns a config writer
func NewConfigWriter(logger logger.Logger) (Writer, error) {
	dir, err := ioutil.TempDir("", "k8s-bigip-ctlr.config")
	if nil != err {
		return nil, fmt.Errorf("could not create unique config directory: %v", err)
	}

	tmpfn := filepath.Join(dir, "config.json")

	cw := &configWriter{
		configFile: tmpfn,
		stopCh:     make(chan struct{}),
		dataCh:     make(chan configSection),
		sectionMap: make(map[string]interface{}),
		logger:     logger,
	}

	go cw.waitData()

	logger.Info("configwriter-started", zap.String("configwriter", fmt.Sprintf("%p", cw)))
	return cw, nil
}

func (cw *configWriter) GetOutputFilename() string {
	return cw.configFile
}

func (cw *configWriter) Stop() {
	defer func() {
		if r := recover(); r != nil {
			cw.logger.Warn("configwriter-stop-called-after-stop", zap.String("configwriter", fmt.Sprintf("%p", cw)))
		}
	}()

	cw.stopCh <- struct{}{}
	close(cw.stopCh)
	close(cw.dataCh)
	os.RemoveAll(filepath.Dir(cw.configFile))

	cw.logger.Info("configwriter-stopped", zap.String("configwriter", fmt.Sprintf("%p", cw)))
}

func (cw *configWriter) SendSection(
	name string,
	obj interface{},
) (<-chan struct{}, <-chan error, error) {
	if 0 == len(name) {
		return nil, nil, fmt.Errorf("cannot marshal section without name")
	}
	defer func() {
		if r := recover(); r != nil {
			cw.logger.Warn("configwriter-sendsection-called-after-stop", zap.String("configwriter", fmt.Sprintf("%p", cw)))
		}
	}()

	cw.logger.Debug("configwriter-writing-section", zap.String("configwriter", fmt.Sprintf("%p", cw)), zap.String("section", name))

	done := make(chan struct{})
	err := make(chan error)
	cw.dataCh <- configSection{
		name:    name,
		data:    obj,
		doneCh:  done,
		errorCh: err,
	}

	return done, err, nil
}

func (cw *configWriter) _lockAndWrite(
	f pseudoFileInterface,
	output []byte,
) (bool, error) {
	var wroteSome bool
	var err error

	flock := syscall.Flock_t{
		Type:   syscall.F_WRLCK,
		Start:  0,
		Len:    0,
		Whence: int16(os.SEEK_SET),
	}
	err = syscall.FcntlFlock(uintptr(f.Fd()), syscall.F_SETLKW, &flock)
	if nil != err {
		return wroteSome, err
	}

	err = f.Truncate(0)
	if nil != err {
		return wroteSome, err
	}
	n, err := f.Write(output)
	if nil == err {
		wroteSome = true
	} else {
		if 0 == n {
			return wroteSome, err
		}
		wroteSome = true
		return wroteSome, err
	}

	flock = syscall.Flock_t{
		Type:   syscall.F_UNLCK,
		Start:  0,
		Len:    0,
		Whence: int16(os.SEEK_SET),
	}
	err = syscall.FcntlFlock(uintptr(f.Fd()), syscall.F_SETLKW, &flock)
	if nil != err {
		return wroteSome, err
	}

	return wroteSome, err
}

func (cw *configWriter) lockAndWrite(output []byte) (wroteSome bool, err error) {
	f, err := os.OpenFile(cw.configFile, os.O_WRONLY|os.O_CREATE, 0644)
	if nil != err {
		return wroteSome, err
	}

	defer func() {
		if err != nil {
			f.Close()
		} else {
			err = f.Close()
		}
	}()

	wroteSome, err = cw._lockAndWrite(f, output)

	return wroteSome, err
}

func (cw *configWriter) waitData() {
	respondDone := func(d chan<- struct{}) {
		select {
		case d <- struct{}{}:
		case <-time.After(time.Second):
		}
	}
	respondErr := func(e chan<- error, err error) {
		select {
		case e <- err:
		case <-time.After(time.Second):
		}
	}
	for {
		select {
		case <-cw.stopCh:
			cw.logger.Debug("configwriter-recieved-stop", zap.String("configwriter", fmt.Sprintf("%p", cw)))
			return
		case cs := <-cw.dataCh:
			// check if this section will marshal
			_, err := json.Marshal(cs.data)
			if nil != err {
				cw.logger.Warn(
					"configwriter-bad-json",
					zap.String("configwriter", fmt.Sprintf("%p", cw)),
					zap.String("section", cs.name),
					zap.Error(err),
				)
				go respondErr(cs.errorCh, err)
			} else {
				cw.sectionMap[cs.name] = cs.data

				output, err := json.Marshal(cw.sectionMap)
				if nil != err {
					cw.logger.Warn(
						"configwriter-marshal-error",
						zap.String("configwriter", fmt.Sprintf("%p", cw)),
						zap.String("section", cs.name),
						zap.Error(err),
					)
					go respondErr(cs.errorCh, err)
				}

				wrote, err := cw.lockAndWrite(output)
				if nil != err {
					if wrote {
						cw.logger.Warn(
							"configwriter-error-during-write",
							zap.String("configwriter", fmt.Sprintf("%p", cw)),
							zap.String("section", cs.name),
							zap.Error(err),
						)
					} else {
						cw.logger.Warn(
							"configwriter-failed-to-write",
							zap.String("configwriter", fmt.Sprintf("%p", cw)),
							zap.String("section", cs.name),
							zap.Error(err),
						)
					}
					go respondErr(cs.errorCh, err)
				} else {
					cw.logger.Debug(
						"configwriter-successfully wrote",
						zap.String("configwriter", fmt.Sprintf("%p", cw)),
						zap.String("section", cs.name),
					)
					go respondDone(cs.doneCh)
				}
			}
		}
	}
}
