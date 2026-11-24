package forward

import "syscall"

// errnoRefused is the system's error for a port nobody listens on.
const errnoRefused = syscall.ECONNREFUSED
