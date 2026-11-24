package forward

import (
	"strings"

	"golang.org/x/sys/windows"
)

// errnoRefused is the system's error for a port nobody listens on. Windows has its own number
// (WSAECONNREFUSED): the constant of package syscall for it is not what a connection gets.
const errnoRefused = windows.WSAECONNREFUSED

// refusedMessage is what a client is told when the port it asked for is not listened on: the
// system's own word, without the full stop it ends with.
var refusedMessage = strings.TrimRight(errnoRefused.Error(), ". ")

// addressInUse says whether the text of an error is the system's "address already in use": WSAEADDRINUSE.
func addressInUse(text string) bool {
	return strings.Contains(text, strings.TrimRight(windows.WSAEADDRINUSE.Error(), ". "))
}
