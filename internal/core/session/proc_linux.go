package session

import (
	"os/exec"
	"syscall"
)

// rootDir is where a session starts when the home folder is not there.
const rootDir = "/"

// signals are the signals a client may send, by their names in the protocol (docs/SPEC.md
// §9.4).
var signals = map[string]syscall.Signal{
	"HUP": syscall.SIGHUP, "INT": syscall.SIGINT, "QUIT": syscall.SIGQUIT, "TERM": syscall.SIGTERM,
	"KILL": syscall.SIGKILL, "USR1": syscall.SIGUSR1, "USR2": syscall.SIGUSR2,
}

// signalNames are the names the protocol has for the signals that may kill a process.
var signalNames = map[syscall.Signal]string{
	syscall.SIGABRT: "ABRT", syscall.SIGALRM: "ALRM", syscall.SIGFPE: "FPE", syscall.SIGHUP: "HUP", syscall.SIGILL: "ILL",
	syscall.SIGINT: "INT", syscall.SIGKILL: "KILL", syscall.SIGPIPE: "PIPE", syscall.SIGQUIT: "QUIT", syscall.SIGSEGV: "SEGV",
	syscall.SIGTERM: "TERM", syscall.SIGUSR1: "USR1", syscall.SIGUSR2: "USR2",
}

// processGroup is what the platform keeps of the process's group: on Linux the group is the
// process's own and needs nothing kept.
type processGroup struct{}

// startGroup puts the process that has just started in the group that the platform ends it
// with: on Linux it leads one (newCommand asked for it), so there is nothing to do.
func startGroup(*exec.Cmd) (processGroup, error) { return processGroup{}, nil }

// endGroup is called when the session is over: on Linux what a process still runs is left to
// the system, so there is nothing to do.
func endGroup(*process) {}

// defaultShell decides the shell of a session (docs/SPEC.md §9.5).
func defaultShell(setting string, user User, caveEnv []string) string {
	return PickShell(setting, user, caveEnv)
}

// sessionEnv builds the environment of a session (docs/SPEC.md §9.5).
func sessionEnv(in EnvInput) []string { return BuildEnv(in) }

// newCommand returns the command of a session: the shell as a login shell, or the shell running
// the command with -c. Without a terminal, the process leads a process group of its own, so that
// signals reach everything it started and nothing else. With one, it leads a new session whose
// controlling terminal is its standard input.
func newCommand(shell, command string, login, tty bool) (*exec.Cmd, error) {
	cmd := &exec.Cmd{Path: shell, SysProcAttr: &syscall.SysProcAttr{Setpgid: true}}
	if tty {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	}
	if login {
		cmd.Args = []string{loginArgv0(shell)}
	} else {
		cmd.Args = []string{shell, "-c", command}
	}
	return cmd, nil
}

// wait waits for the process and returns its exit status, or the protocol's name of the signal
// that killed it. A signal the protocol has no name for is reported as the status a shell gives
// it: 128 plus its number.
func wait(proc *process) (status int, signal string) {
	cmd := proc.cmd
	_ = cmd.Wait() // how it ended is in the process state
	state, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		return cmd.ProcessState.ExitCode(), ""
	}
	if state.Signaled() {
		if name, known := signalNames[state.Signal()]; known {
			return 0, name
		}
		return 128 + int(state.Signal()), ""
	}
	return state.ExitStatus(), ""
}

// signalProcess sends a signal a client asked for to the process's group, and reports whether
// the signal is one a client may send.
func signalProcess(proc *process, name string) bool {
	sig, ok := signals[name]
	if !ok {
		return false
	}
	// The group may be gone already: then there is nobody to signal.
	_ = syscall.Kill(-proc.cmd.Process.Pid, sig)
	return true
}

// hangUpProcess sends SIGHUP to the process's group.
func hangUpProcess(proc *process) {
	_ = syscall.Kill(-proc.cmd.Process.Pid, syscall.SIGHUP) // as in signalProcess
}
