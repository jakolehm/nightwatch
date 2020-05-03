// +build linux darwin

package main

import (
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

func (n *NightWatch) runCommand() {
	changeDetected := false
	go func() {
		for {
			signal := <-n.cmdSignal
			if changeDetected {
				continue
			}
			changeDetected = true
			logrus.Debugf("got signal %+v", signal)
			if n.cmd != nil {
				syscall.Kill(-1*n.cmd.Process.Pid, signal.signal)
				n.cmd.Wait()
			}
		}
	}()
	for {
		changeDetected = false
		n.cmd = exec.Command(n.args.First(), n.args.Slice()[1:]...)
		n.cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true,
		}
		n.cmd.Env = os.Environ()
		stdoutPipe, _ := n.cmd.StdoutPipe()
		stderrPipe, _ := n.cmd.StderrPipe()
		defer func() {
			if stdoutPipe != nil {
				stdoutPipe.Close()
			}
			if stderrPipe != nil {
				stderrPipe.Close()
			}
		}()

		err := n.cmd.Start()
		if err != nil {
			logrus.Fatal(err.Error())
		}
		go func() {
			io.Copy(os.Stdout, stdoutPipe)
		}()
		go func() {
			io.Copy(os.Stderr, stderrPipe)
		}()
		logrus.Debugf("process (pid: %d) started", n.cmd.Process.Pid)
		n.cmd.Wait()
		logrus.Debugf("process (pid: %d) killed", n.cmd.Process.Pid)

		exitCode := n.cmd.ProcessState.ExitCode()
		if n.stopped {
			os.Exit(exitCode)
		}
		if changeDetected {
			if n.exitOnChange > -1 {
				os.Exit(n.exitOnChange)
			}
		} else {
			if n.exitOnError && exitCode > 0 {
				os.Exit(exitCode)
			} else if n.exitOnSuccess && exitCode == 0 {
				os.Exit(exitCode)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}
