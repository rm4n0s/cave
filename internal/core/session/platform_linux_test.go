package session

import (
	"os"
	"strings"
)

// fdDir lists the files this process has open.
const fdDir = "/proc/self/fd"

// ttyPattern is the form of the path of a terminal's slave side, as a regular expression.
const ttyPattern = `/dev/pts/\d+`

// fdIsTerminal says whether the descriptor of fdDir with the name is a pseudo terminal, either
// side.
func fdIsTerminal(name string) bool {
	target, err := os.Readlink(fdDir + "/" + name)
	if err != nil {
		return false // the descriptor of the listing itself
	}
	return target == "/dev/ptmx" || strings.HasPrefix(target, "/dev/pts/")
}

// fdIsFile says whether the descriptor of fdDir with the name is open on the file at path.
func fdIsFile(name, path string) bool {
	target, err := os.Readlink(fdDir + "/" + name)
	return err == nil && target == path
}

// notFoundPrefix starts the message of /bin/sh for a command that is not there.
const notFoundPrefix = "/bin/sh: line 1: "

// unnamedSignal is a signal that ends a process and that the SSH protocol has no name for.
const (
	unnamedSignal       = "STKFLT"
	unnamedSignalNumber = 16
)

// linkMode is how ls shows the mode of a symbolic link.
const linkMode = "lrwxrwxrwx"
