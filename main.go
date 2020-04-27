package main

import (
	"bufio"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
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
			filesList := []string{}
			if c.IsSet("files") {
				filesList = strings.Split(c.String("files"), ",")
			}
			nightWatch := &NightWatch{
				cmdSignal:     make(chan *processSignal, 1),
				args:          c.Args(),
				exitOnChange:  exitOnChange,
				exitOnError:   c.Bool("exit-on-error"),
				exitOnSuccess: c.Bool("exit-on-success"),
				watchCmd:      c.String("find-cmd"),
				filesList:     filesList,
			}
			go nightWatch.Run()

			exitSignal := <-done
			exitCode := nightWatch.Stop(exitSignal)
			time.Sleep(10 * time.Second)
			nightWatch.Cleanup()
			os.Exit(exitCode)
			return nil
		},
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "debug",
				Usage: "Debug logging.",
			},
			&cli.StringFlag{
				Name:  "find-cmd",
				Usage: "Command to list files (or dirs) to watch",
				Value: "find . -type f -not -path '*/\\.git/*'",
			},
			&cli.StringFlag{
				Name:  "files",
				Usage: "Files (or dirs) to watch (comma separated list)",
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
				n.cmd.Process.Signal(signal.signal)
				n.cmd.Wait()
			}
		}
	}()
	for {
		changeDetected = false
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
