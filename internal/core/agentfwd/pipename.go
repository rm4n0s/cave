package agentfwd

import (
	"crypto/rand"
	"encoding/hex"
)

// pipePrefix is the start of the name of the named pipe that a session on Windows finds its agent
// in (docs/SPEC.md §13.5).
const pipePrefix = `\\.\pipe\cave-agent-`

// pipeName returns the name of a new pipe: the prefix and 32 hexadecimal digits from the system's
// random numbers, so that nobody can guess it before the session starts.
func pipeName() string {
	random := make([]byte, 16)
	_, _ = rand.Read(random) // never fails: it always fills the bytes
	return pipePrefix + hex.EncodeToString(random)
}

// pipeSDDL returns the security descriptor, as an SDDL string, of a pipe that only the user with
// the SID may open: a protected list (no entry is inherited) with one entry that gives all
// access to that SID.
func pipeSDDL(sid string) string {
	return "D:P(A;;GA;;;" + sid + ")"
}
