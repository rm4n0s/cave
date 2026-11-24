//go:build linux || darwin

package main

import (
	"os"
	"syscall"
)

// shutdownSignals are the signals that end cave: it quits as if the user had.
func shutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}
