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

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"github.com/cf-bigip-ctlr/config"
	"github.com/cf-bigip-ctlr/f5router"
	"github.com/cf-bigip-ctlr/logger"

	"github.com/uber-go/zap"
)

func initializeDriverConfig(
	configWriter *f5router.ConfigWriter,
	global config.GlobalSection,
	bigIP config.BigIPConfig,
	logger logger.Logger,
) error {
	if nil == configWriter {
		return fmt.Errorf("config writer argument cannot be nil")
	}

	sections := make(map[string]interface{})
	sections["global"] = global
	sections["bigip"] = bigIP

	output, err := json.Marshal(sections)
	if nil != err {
		return fmt.Errorf("failed marshaling config: %v", err)
	}
	n, err := configWriter.Write(output)
	if nil != err {
		return fmt.Errorf("failed writing config: %v", err)
	} else if len(output) != n {
		return fmt.Errorf("short write from initial config")
	}

	return nil
}

func createDriverCmd(
	configFilename string,
	pyCmd string,
) *exec.Cmd {
	cmdName := "python"

	cmdArgs := []string{
		pyCmd,
		"--config-file", configFilename}

	cmd := exec.Command(cmdName, cmdArgs...)

	return cmd
}

func runBigIPDriver(pid chan<- int, cmd *exec.Cmd, logger logger.Logger) {
	defer close(pid)

	// the config driver python logging goes to stderr by default
	cmdOut, err := cmd.StderrPipe()
	scanOut := bufio.NewScanner(cmdOut)
	go func() {
		for true {
			if scanOut.Scan() {
				if strings.Contains(scanOut.Text(), "DEBUG]") {
					logger.Debug(scanOut.Text())
				} else if strings.Contains(scanOut.Text(), "Warn]") {
					logger.Warn(scanOut.Text())
				} else if strings.Contains(scanOut.Text(), "ERROR]") {
					logger.Error(scanOut.Text())
				} else if strings.Contains(scanOut.Text(), "CRITICAL]") {
					logger.Error(scanOut.Text())
				} else {
					logger.Info(scanOut.Text())
				}
			} else {
				break
			}
		}
	}()

	err = cmd.Start()
	if nil != err {
		logger.Fatal("failed-to-start-config-driver", zap.Error(err))
	}
	logger.Info("config-driver-sub-process-pid", zap.Int("pid", cmd.Process.Pid))

	pid <- cmd.Process.Pid

	err = cmd.Wait()
	var waitStatus syscall.WaitStatus
	if exitError, ok := err.(*exec.ExitError); ok {
		waitStatus = exitError.Sys().(syscall.WaitStatus)
		if waitStatus.Signaled() {
			logger.Fatal("config-driver-signaled-to-stop", zap.String("signal",
				fmt.Sprintf("%d - %s", waitStatus.Signal(), waitStatus.Signal())))
		} else {
			logger.Fatal("config-driver-exited", zap.Int("exit-status", waitStatus.ExitStatus()))
		}
	} else if nil != err {
		logger.Fatal("config-driver-exited", zap.Error(err))
	} else {
		waitStatus = cmd.ProcessState.Sys().(syscall.WaitStatus)
		logger.Warn("config-driver-exited-normally", zap.Int("exit-status", waitStatus.ExitStatus()))
	}
}

// Start called to run the python driver
func startPythonDriver(
	configWriter *f5router.ConfigWriter,
	global config.GlobalSection,
	bigIP config.BigIPConfig,
	pythonBaseDir string,
	logger logger.Logger,
) (<-chan int, error) {
	err := initializeDriverConfig(configWriter, global, bigIP, logger)
	if nil != err {
		return nil, err
	}

	subPidCh := make(chan int)
	pyCmd := fmt.Sprintf("%s/bigipconfigdriver.py", pythonBaseDir)
	cmd := createDriverCmd(
		configWriter.GetOutputFilename(),
		pyCmd,
	)
	go runBigIPDriver(subPidCh, cmd, logger)

	return subPidCh, nil
}
