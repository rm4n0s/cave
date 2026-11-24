package apptest

import "os"

// shell is the shell that the sessions of a test run: cmd.exe, which Wine has too.
const shell = "cmd.exe"

// environ is Cave's own environment, as the real application of a test is given it: the one of
// the test, from which a session takes the variables Windows programs need (docs/SPEC.md §13.2)
// and nothing else.
func environ(string) []string { return os.Environ() }
