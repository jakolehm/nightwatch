package main

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
			exitCode := nightWatch.Stop(exitSignal.(syscall.Signal))
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
