package app

import "syscall"

// errnoAddrInUse is the system's error for a port that is taken.
const errnoAddrInUse = syscall.EADDRINUSE

// addrInUseWords is how the text of a failed listen says that the port is taken.
const addrInUseWords = "address already in use"

// modesMean says whether the mode bits of a file mean anything: not on Windows, where the access
// is the profile's ACLs (docs/SPEC.md §13.1).
const modesMean = true
