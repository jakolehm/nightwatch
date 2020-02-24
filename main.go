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
			watcher, err := fsnotify.NewWatcher()
			if err != nil {
				logrus.Fatal(err)
			}
			defer watcher.Close()

			done := make(chan os.Signal, 2)
			signal.Notify(done, os.Interrupt, syscall.SIGTERM)

			nightWatch := &NightWatch{
				cmdSignal:    make(chan *processSignal, 1),
				args:         c.Args(),
				watcher:      watcher,
				exitOnChange: c.Int("exit-on-change"),
				watchDirs:    c.Bool("dir"),
			}
			nightWatch.Run()
			startFileWatch(watcher)

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
				Usage:   "Track the directories of regular files provided as input and exit if a new file is added.",
			},
			&cli.IntFlag{
				Name:  "exit-on-change",
				Usage: "Exit on file change with a given code.",
				Value: 255,
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func startFileWatch(watcher *fsnotify.Watcher) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		file := scanner.Text()
		absFile, err := filepath.Abs(file)
		if err == nil {
			err = watcher.Add(absFile)
			if err != nil {
				logrus.Warningf("failed to watch file %s", absFile)
			}
		}
	}
}

type processSignal struct {
	signal os.Signal
}

type NightWatch struct {
	cmd          *exec.Cmd
	args         cli.Args
	cmdSignal    chan *processSignal
	watcher      *fsnotify.Watcher
	exitOnChange int
	watchDirs    bool
}

func (n *NightWatch) Run() {
	go n.handleWatchEvents()
	go n.runCommand()
}

func (n *NightWatch) Stop(exitSignal os.Signal) int {
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
				logrus.Debugf("modified file: %s", event.Name)
				signal = &processSignal{signal: syscall.SIGTERM}
			} else if event.Op == fsnotify.Create && n.watchDirs {
				logrus.Debugf("created: %s", event.Name)
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

func (n *NightWatch) runCommand() {
	for {
		changeDetected := false
		n.cmd = exec.Command(n.args.First(), n.args.Slice()[1:]...)
		n.cmd.Env = os.Environ()
		stdoutPipe, _ := n.cmd.StdoutPipe()
		stderrPipe, _ := n.cmd.StderrPipe()
		defer stdoutPipe.Close()
		defer stderrPipe.Close()

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
		if changeDetected {
			os.Exit(n.exitOnChange)
		} else if n.cmd.ProcessState.ExitCode() == 0 {
			os.Exit(0)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
