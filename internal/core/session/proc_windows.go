package session

import (
	"os/exec"
	"strings"
	"syscall"
)

// rootDir is where a session starts when the home folder is not there.
const rootDir = `C:\`

// defaultShell decides the shell of a session (docs/SPEC.md §13.2): the setting session.shell,
// else the ComSpec of Cave's own environment, else cmd.exe.
func defaultShell(setting string, _ User, caveEnv []string) string {
	if setting != "" {
		return setting
	}
	for _, entry := range caveEnv {
		if name, value, ok := strings.Cut(entry, "="); ok && strings.EqualFold(name, "ComSpec") && value != "" {
			return value
		}
	}
	return "cmd.exe"
}

// sessionEnv builds the environment of a session (docs/SPEC.md §13.2).
func sessionEnv(in EnvInput) []string { return BuildWindowsEnv(in) }

// newCommand returns the command of a session. Go would quote the arguments again, so the whole
// command line is given as it is built by windowsCommandLine; the process starts in a process
// group of its own, and startGroup puts it in its job right after.
func newCommand(shell, command string, login, _ bool) (*exec.Cmd, error) {
	path, err := exec.LookPath(shell)
	if err != nil {
		// The start reports the same failure, with the shell's name; the path is tried as given.
		path = shell
	}
	return &exec.Cmd{
		Path: path,
		SysProcAttr: &syscall.SysProcAttr{
			CmdLine:       windowsCommandLine(shell, command, login),
			CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		},
	}, nil
}

// wait waits for the process and returns its exit status; Windows has no signals to report. A
// process that the job ended has the status the job gave.
func wait(proc *process) (status int, signal string) {
	_ = proc.cmd.Wait() // how it ended is in the process state
	return proc.cmd.ProcessState.ExitCode(), ""
}

// signalProcess handles a signal a client asked for: KILL and TERM end the job, the others are
// refused (docs/SPEC.md §13.2). The job may be ended already: then there is nobody to end.
func signalProcess(proc *process, name string) bool {
	if name != "KILL" && name != "TERM" {
		return false
	}
	proc.group.job.terminate()
	return true
}

// hangUpProcess ends the job: Windows has no SIGHUP, and the session is gone.
func hangUpProcess(proc *process) { proc.group.job.close() }

// endGroup ends what the session still runs when it is over.
func endGroup(proc *process) { proc.group.job.close() }
