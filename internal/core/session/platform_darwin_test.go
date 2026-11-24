package session

import (
	"os"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

// fdDir lists the files this process has open.
const fdDir = "/dev/fd"

// ttyPattern is the form of the path of a terminal's slave side, as a regular expression.
const ttyPattern = `/dev/ttys\d+`

// fdIsTerminal says whether the descriptor of fdDir with the name is a terminal: macOS has no
// links to say which file a descriptor is, so it asks the descriptor for a terminal's settings,
// which both sides of a pseudo terminal have.
func fdIsTerminal(name string) bool {
	fd, err := strconv.Atoi(name)
	if err != nil {
		return false
	}
	_, err = unix.IoctlGetTermios(fd, unix.TIOCGETA)
	return err == nil
}

// fdIsFile says whether the descriptor of fdDir with the name is open on the file at path: the
// same device and inode.
func fdIsFile(name, path string) bool {
	fd, err := strconv.Atoi(name)
	if err != nil {
		return false
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	want, ok := info.Sys().(*syscall.Stat_t)
	return ok && want.Dev == stat.Dev && want.Ino == stat.Ino
}

// notFoundPrefix starts the message of /bin/sh for a command that is not there.
const notFoundPrefix = "/bin/sh: "

// unnamedSignal is a signal that ends a process and that the SSH protocol has no name for.
const (
	unnamedSignal       = "VTALRM"
	unnamedSignalNumber = 26
)

// linkMode is how ls shows the mode of a symbolic link: macOS gives a link the mode the umask
// leaves.
const linkMode = "lrwxr-xr-x"
