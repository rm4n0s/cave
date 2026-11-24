package pty

import "regexp"

// fdDir lists the files this process has open.
const fdDir = "/proc/self/fd"

// slaveName is the form of the path of a slave side.
var slaveName = regexp.MustCompile(`^/dev/pts/\d+$`)
