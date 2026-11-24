package app

import "golang.org/x/sys/windows"

// errnoAddrInUse is the system's error for a port that is taken: Windows has its own number
// (WSAEADDRINUSE), not the constant of package syscall.
const errnoAddrInUse = windows.WSAEADDRINUSE

// addrInUseWords is how the text of a failed listen says that the port is taken: the system's own
// words (Wine has none and says "winapi error #10048").
var addrInUseWords = windows.WSAEADDRINUSE.Error()

// modesMean says whether the mode bits of a file mean anything: not on Windows, where the access
// is the profile's ACLs (docs/SPEC.md §13.1).
const modesMean = false
