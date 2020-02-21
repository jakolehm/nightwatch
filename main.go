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

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

// Version gets overridden at build time using -X main.Version=$VERSION
var (
	Version = "dev"
)

type processSignal struct {
	signal      os.Signal
	exitProcess bool
}

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

			done := make(chan os.Signal, 1)
			signal.Notify(done, os.Interrupt)
			var cmd *exec.Cmd
			cmdSignal := make(chan *processSignal, 1)
			go runCommand(cmd, c.Args(), cmdSignal)
			go handleWatchEvents(watcher, cmdSignal, c.Bool("dir"))

			startFileWatch(watcher)

			exitSignal := <-done
			if cmd != nil {
				cmdSignal <- &processSignal{signal: exitSignal, exitProcess: true}
				cmd.Wait()
			}

			return nil
		},
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "debug",
				Usage: "Debug logging.",
			},
			&cli.BoolFlag{
				Name:  "dir,d",
				Usage: "Track the directories of regular files provided as input and exit if a new file is added.",
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

func handleWatchEvents(watcher *fsnotify.Watcher, cmdSignal chan *processSignal, watchDirs bool) {
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op == fsnotify.Write {
				logrus.Debugf("modified file: %s", event.Name)
				cmdSignal <- &processSignal{signal: syscall.SIGTERM, exitProcess: false}
			} else if event.Op == fsnotify.Create && watchDirs {
				logrus.Debugf("created: %s", event.Name)
				cmdSignal <- &processSignal{signal: syscall.SIGTERM, exitProcess: true}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logrus.Warnf("error: %s", err.Error())
		}
	}
}

func runCommand(cmd *exec.Cmd, args cli.Args, cmdSignal chan *processSignal) {
	for {
		running := true
		cmd = exec.Command(args.First(), args.Slice()[1:]...)
		cmd.Env = os.Environ()
		stdoutPipe, _ := cmd.StdoutPipe()
		stderrPipe, _ := cmd.StderrPipe()
		err := cmd.Start()
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
			signal := <-cmdSignal
			if !running {
				return
			}
			logrus.Debugf("got signal %+v", signal)
			if signal.exitProcess {
				running = false
			}
			cmd.Process.Signal(signal.signal)
			cmd.Wait()
		}()
		logrus.Debugln("process started")
		cmd.Wait()
		logrus.Debugln("process killed")
		if !running {
			os.Exit(cmd.ProcessState.ExitCode())
			break
		}
	}
}
