//go:build linux || darwin

package forward

import (
	"strings"
	"syscall"
)

// errnoRefused is the system's error for a port nobody listens on.
const errnoRefused = syscall.ECONNREFUSED

// refusedMessage is what a client is told when the port it asked for is not listened on.
const refusedMessage = "connection refused"

// addressInUse says whether the text of an error is the system's "address already in use".
func addressInUse(text string) bool {
	return strings.Contains(text, "address already in use")
}
