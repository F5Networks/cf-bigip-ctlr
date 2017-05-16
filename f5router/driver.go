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
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/cf-bigip-ctlr/config"
	"github.com/cf-bigip-ctlr/logger"

	"github.com/uber-go/zap"
)

// Driver type which provides ifrit process interface
type Driver struct {
	writer  *ConfigWriter
	global  config.GlobalSection
	bigIP   config.BigIPConfig
	baseDir string
	logger  logger.Logger
}

const (
	command string = "bigipconfigdriver.py"
)

// NewDriver create ifrit process instance
func NewDriver(
	writer *ConfigWriter,
	pythonBaseDir string,
	logger logger.Logger,
) *Driver {
	return &Driver{
		writer:  writer,
		baseDir: pythonBaseDir,
		logger:  logger,
	}
}

func (d *Driver) createDriverCmd() *exec.Cmd {
	cmdName := "python"

	cmdArgs := []string{
		fmt.Sprintf("%s/%s", d.baseDir, command),
		"--config-file", d.writer.GetOutputFilename()}

	cmd := exec.Command(cmdName, cmdArgs...)

	return cmd
}

func (d *Driver) runBigIPDriver(pid chan<- int, cmd *exec.Cmd) {
	defer close(pid)

	// the config driver python logging goes to stderr by default
	cmdOut, err := cmd.StderrPipe()
	scanOut := bufio.NewScanner(cmdOut)
	go func() {
		for true {
			if scanOut.Scan() {
				if strings.Contains(scanOut.Text(), "DEBUG]") {
					d.logger.Debug(scanOut.Text())
				} else if strings.Contains(scanOut.Text(), "Warn]") {
					d.logger.Warn(scanOut.Text())
				} else if strings.Contains(scanOut.Text(), "ERROR]") {
					d.logger.Error(scanOut.Text())
				} else if strings.Contains(scanOut.Text(), "CRITICAL]") {
					d.logger.Error(scanOut.Text())
				} else {
					d.logger.Info(scanOut.Text())
				}
			} else {
				break
			}
		}
	}()

	err = cmd.Start()
	if nil != err {
		d.logger.Fatal("f5router-driver-failed-start", zap.Error(err))
	}
	d.logger.Info("f5router-driver-process-pid", zap.Int("pid", cmd.Process.Pid))

	pid <- cmd.Process.Pid

	err = cmd.Wait()
	var waitStatus syscall.WaitStatus
	if exitError, ok := err.(*exec.ExitError); ok {
		waitStatus = exitError.Sys().(syscall.WaitStatus)
		if waitStatus.Signaled() {
			d.logger.Fatal("f5router-driver-signaled-to-stop", zap.String("signal",
				fmt.Sprintf("%d - %s", waitStatus.Signal(), waitStatus.Signal())))
		} else {
			d.logger.Fatal("f5router-driver-exited", zap.Int("exit-status", waitStatus.ExitStatus()))
		}
	} else if nil != err {
		d.logger.Fatal("f5router-driver-exited", zap.Error(err))
	} else {
		waitStatus = cmd.ProcessState.Sys().(syscall.WaitStatus)
		d.logger.Warn("f5router-driver-exited-normally", zap.Int("exit-status", waitStatus.ExitStatus()))
	}
}

// Run start the F5Router configuration driver
func (d *Driver) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	d.logger.Info("f5router-driver-starting")

	pidCh := make(chan int)
	go d.runBigIPDriver(pidCh, d.createDriverCmd())

	pid := <-pidCh
	close(ready)
	d.logger.Info("f5router-driver-started")

	sig := <-signals

	proc, err := os.FindProcess(pid)
	if nil != err {
		d.logger.Warn("f5router-driver-failed-finding-process", zap.Error(err))
		return err
	}
	err = proc.Signal(sig)
	if nil != err {
		d.logger.Warn("f5router-driver-failed-signalling",
			zap.Int("pid", pid),
			zap.String("signal", sig.String()),
			zap.Error(err),
		)
		return err
	}
	d.logger.Info("f5router-driver-stopped")

	return nil
}
