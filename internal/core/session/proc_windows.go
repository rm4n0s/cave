package session

import (
	"os/exec"

	"github.com/rm4n0s/errors"
)

// rootDir is where a session starts when the home folder is not there.
const rootDir = `C:\`

// newCommand returns the command of a session.
//
// TODO(step 7.1): sessions on Windows (docs/SPEC.md §13.2).
func newCommand(string, string, bool, bool) (*exec.Cmd, error) {
	return nil, errors.New(TagSessionUnsupported, "sessions are not built for Windows yet")
}

// wait waits for the process and returns its exit status; Windows has no signals to report.
func wait(cmd *exec.Cmd) (status int, signal string) {
	_ = cmd.Wait() // how it ended is in the process state
	return cmd.ProcessState.ExitCode(), ""
}

// signalProcess sends a signal a client asked for; none can be sent yet.
func signalProcess(*exec.Cmd, string) bool {
	return false
}

// hangUpProcess tells the process that its session is gone; nothing can be told yet.
func hangUpProcess(*exec.Cmd) {}
