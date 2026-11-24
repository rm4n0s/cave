package main

import (
	"os"
	"syscall"
)

// shutdownSignals are the signals that end tcave: it quits as if the user had. Windows has no
// SIGHUP.
func shutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
