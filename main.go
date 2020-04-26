package main

import (
	"bufio"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

// Version gets overridden at build time using -X main.Version=$VERSION
var (
	Version = "dev"
)

func init() {
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.InfoLevel)
}

func main() {
	app := &cli.App{
		Name:    "nightwatch",
		Version: Version,
		Usage:   "A utility for running arbitrary commands when files change",
		Before: func(ctx *cli.Context) error {
			if ctx.Bool("debug") {
				logrus.SetLevel(logrus.DebugLevel)
			}

			return nil
		},
		Action: func(c *cli.Context) error {
			if !c.Args().Present() {
				logrus.Fatal("No command specified")
			}

			done := make(chan os.Signal, 2)
			signal.Notify(done, os.Interrupt, syscall.SIGTERM)

			exitOnChange := -1
			if c.IsSet("exit-on-change") {
				exitOnChange = c.Int("exit-on-change")
			}

			nightWatch := &NightWatch{
				cmdSignal:     make(chan *processSignal, 1),
				args:          c.Args(),
				watchDirs:     c.Bool("dir"),
				exitOnChange:  exitOnChange,
				exitOnError:   c.Bool("exit-on-error"),
				exitOnSuccess: c.Bool("exit-on-success"),
				watchCmd:      c.String("find-cmd"),
			}
			go nightWatch.Run()

			exitSignal := <-done
			exitCode := nightWatch.Stop(exitSignal)
			time.Sleep(10 * time.Second)
			os.Exit(exitCode)
			return nil
		},
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "debug",
				Usage: "Debug logging.",
			},
			&cli.BoolFlag{
				Name:    "dir",
				Aliases: []string{"d"},
				Usage:   "Track the directories of regular files returned from --find-cmd",
				Value:   true,
			},
			&cli.StringFlag{
				Name:  "find-cmd",
				Usage: "Command to list files to watch",
				Value: "find .",
			},
			&cli.IntFlag{
				Name:  "exit-on-error",
				Usage: "Exit if process returns an error code.",
			},
			&cli.IntFlag{
				Name:  "exit-on-success",
				Usage: "Exit if process returns with code 0.",
			},
			&cli.IntFlag{
				Name:  "exit-on-change",
				Usage: "Exit on file change with a given code.",
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

type processSignal struct {
	signal os.Signal
}

type NightWatch struct {
	cmd           *exec.Cmd
	watchCmd      string
	args          cli.Args
	cmdSignal     chan *processSignal
	watcher       *fsnotify.Watcher
	exitOnChange  int
	exitOnError   bool
	exitOnSuccess bool
	watchDirs     bool
	stopped       bool
}

func (n *NightWatch) Run() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logrus.Fatal(err)
	}
	n.watcher = watcher
	defer n.watcher.Close()

	n.reloadWatch()
	go n.handleWatchEvents()
	n.runCommand()
}

func (n *NightWatch) Stop(exitSignal os.Signal) int {
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

func (n *NightWatch) handleWatchEvents() {
	for {
		select {
		case event, ok := <-n.watcher.Events:
			if !ok {
				return
			}
			var signal *processSignal
			if event.Op == fsnotify.Write || event.Op == fsnotify.Chmod {
				logrus.Debugf("modified: %s", event.Name)
				signal = &processSignal{signal: syscall.SIGTERM}
			} else if event.Op == fsnotify.Create && n.watchDirs {
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

func (n *NightWatch) reloadWatch() {
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
		return
	}
	cmd.Start()
	watchedPaths := []string{}
	scanner := bufio.NewScanner(stdoutPipe)
	for scanner.Scan() {
		file := scanner.Text()
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
				if dirName == path || (!n.watchDirs && fileInfo.IsDir()) {
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
	cmd.Wait()
	if cmd.ProcessState.ExitCode() != 0 {
		os.Exit(cmd.ProcessState.ExitCode())
	}
}

func (n *NightWatch) runCommand() {
	for {
		changeDetected := false
		n.cmd = exec.Command(n.args.First(), n.args.Slice()[1:]...)
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
		go func() {
			signal := <-n.cmdSignal
			changeDetected = true
			logrus.Debugf("got signal %+v", signal)
			n.cmd.Process.Signal(signal.signal)
			n.cmd.Wait()
		}()
		logrus.Debugln("process started")
		n.cmd.Wait()
		logrus.Debugln("process killed")

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
		n.Run()
	}
}
