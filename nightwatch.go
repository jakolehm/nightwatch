package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

type processSignal struct {
	signal syscall.Signal
}

type NightWatch struct {
	cmd           *exec.Cmd
	watchCmd      string
	filesList     []string
	args          cli.Args
	cmdSignal     chan *processSignal
	watcher       *fsnotify.Watcher
	exitOnChange  int
	exitOnError   bool
	exitOnSuccess bool
	stopped       bool
}

func (n *NightWatch) Run() {
	n.cmdSignal = make(chan *processSignal, 1)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logrus.Fatal(err)
	}
	n.watcher = watcher

	files := []string{}
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		logrus.Debugln("reading files from stdin")
		files = n.watchFromStdin()
	} else if len(n.filesList) > 0 {
		logrus.Debugf("Reading files from static list: %s", strings.Join(n.filesList, ", "))
		files = n.filesList
	} else {
		logrus.Debugf("Reading files from command: %s", n.watchCmd)
		files = n.watchFromCmd()
	}
	n.watchFiles(files)
	go n.handleWatchEvents()
	n.runCommand()
}

func (n *NightWatch) Stop(exitSignal syscall.Signal) int {
	n.stopped = true
	if n.cmd == nil {
		return 0
	}
	if n.cmd.ProcessState != nil && n.cmd.ProcessState.Exited() {
		return n.cmd.ProcessState.ExitCode()
	}

	logrus.Debugf("stop requested: %s", exitSignal)
	n.cmdSignal <- &processSignal{signal: exitSignal}
	n.cmd.Wait()
	return n.cmd.ProcessState.ExitCode()
}

func (n *NightWatch) Cleanup() {
	if n.watcher != nil {
		n.watcher.Close()
	}
}

func (n *NightWatch) handleWatchEvents() {
	for {
		select {
		case event, ok := <-n.watcher.Events:
			if !ok {
				return
			}
			var signal *processSignal
			if event.Op == fsnotify.Write || event.Op == fsnotify.Chmod {
				logrus.Debugf("modified (%s): %s", event.Op.String(), event.Name)
				signal = &processSignal{signal: syscall.SIGTERM}
			} else if event.Op == fsnotify.Create {
				logrus.Debugf("created: %s", event.Name)
				signal = &processSignal{signal: syscall.SIGTERM}
			} else if event.Op == fsnotify.Remove {
				logrus.Debugf("removed: %s", event.Name)
				n.watcher.Remove(event.Name)
				signal = &processSignal{signal: syscall.SIGTERM}
			}
			if signal == nil {
				return
			}
			select {
			case n.cmdSignal <- signal:
			default:
				logrus.Debugln("restart already scheduled, ignoring change.")
			}
		case err, ok := <-n.watcher.Errors:
			if !ok {
				return
			}
			logrus.Warnf("error: %s", err.Error())
		}
	}
}

func (n *NightWatch) watchFromStdin() []string {
	files := []string{}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		file := scanner.Text()
		files = append(files, file)
	}

	return files
}

func (n *NightWatch) watchFromCmd() []string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell, _ = exec.LookPath("sh")
	}
	cmd := exec.Command(shell, "-c", n.watchCmd)
	cmd.Env = os.Environ()
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		logrus.Errorln(err)
		os.Exit(1)
	}
	defer func() {
		if stdoutPipe != nil {
			stdoutPipe.Close()
		}
	}()
	cmd.Start()
	files := []string{}
	scanner := bufio.NewScanner(stdoutPipe)
	for scanner.Scan() {
		file := scanner.Text()
		files = append(files, file)
	}
	cmd.Wait()
	if cmd.ProcessState.ExitCode() != 0 {
		os.Exit(cmd.ProcessState.ExitCode())
	}

	return files
}

func (n *NightWatch) watchFiles(files []string) {
	watchedPaths := []string{}
	for _, file := range files {
		absFile, err := filepath.Abs(file)
		if err == nil {
			fileInfo, _ := os.Stat(absFile)
			shouldWatch := true
			for _, path := range watchedPaths {
				var dirName string
				if fileInfo.IsDir() {
					dirName = absFile
				} else {
					dirName = filepath.Dir(absFile)
				}
				if dirName == path {
					shouldWatch = false
				}
			}
			if shouldWatch {
				logrus.Debugf("watching file %s", absFile)
				err = n.watcher.Add(absFile)
				if err != nil {
					logrus.Warningf("failed to watch file %s: %s", absFile, err.Error())
					os.Exit(1)
				} else if fileInfo.IsDir() {
					watchedPaths = append(watchedPaths, absFile)
				}
			}
		}
	}
}
