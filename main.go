package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/karust/openserp/cmd"
	"github.com/sirupsen/logrus"
)

// reapZombies installs a SIGCHLD reaper so that any child that is killed or
// orphaned outside rod's own cmd.Wait() (e.g. Chromium/headless-shell or its
// crashpad handler after an aborted captcha/search request) is reclaimed
// instead of accumulating as a zombie for the lifetime of the server.
func reapZombies() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGCHLD)
	for range ch {
		for {
			var ws syscall.WaitStatus
			pid, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, nil)
			if err != nil || pid <= 0 {
				break
			}
		}
	}
}

func main() {
	defer recoverPanic()
	go reapZombies()

	if err := cmd.RootCmd.Execute(); err != nil {
		// Cobra already prints the error to stderr; just set the exit code.
		os.Exit(1)
	}
}

func recoverPanic() {
	if r := recover(); r != nil {
		logrus.Fatalf("Error: %v\n", r)
	}
}
