package main

import (
	"os"

	"github.com/karust/openserp/cmd"
	"github.com/sirupsen/logrus"
)

func main() {
	defer recoverPanic()

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
