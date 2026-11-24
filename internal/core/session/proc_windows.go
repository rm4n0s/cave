package session

import (
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"github.com/rm4n0s/cave/internal/core/pty"
	"golang.org/x/sys/windows"
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

// process is the process of a session. pid is its id; cmd is the command that started it; nil for a process that started on a pseudo console, which os/exec cannot do; exited
// is closed when it ended; group is what the platform keeps to end it and what it started.
type process struct {
	pid    int
	cmd    *exec.Cmd
	exited chan struct{}
	group  processGroup

	// handle and terminal are those of a process that started on a pseudo console: its handle
	// and the console. Both are zero for a process that os/exec started.
	handle   windows.Handle
	terminal *pty.Pty
}

// startCommand starts the command. A process on a terminal cannot be started by os/exec: it is
// started with CreateProcess, which can give it the pseudo console.
func startCommand(cmd *exec.Cmd, terminal *pty.Pty) (*process, error) {
	if terminal == nil {
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return &process{pid: cmd.Process.Pid, cmd: cmd}, nil
	}
	return startOnConsole(cmd, terminal)
}

// startOnConsole starts the command on the pseudo console, with the command line, environment and
// folder that newCommand and the caller gave the command.
func startOnConsole(cmd *exec.Cmd, terminal *pty.Pty) (*process, error) {
	console := terminal.Console()
	if console == 0 {
		return nil, windows.ERROR_INVALID_HANDLE // the terminal was closed before the process started
	}
	commandLine, err := windows.UTF16PtrFromString(cmd.SysProcAttr.CmdLine)
	if err != nil {
		return nil, err
	}
	var appName, dir *uint16
	if cmd.Path != "" {
		if appName, err = windows.UTF16PtrFromString(cmd.Path); err != nil {
			return nil, err
		}
	}
	if cmd.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(cmd.Dir); err != nil {
			return nil, err
		}
	}
	block, err := windowsEnvBlock(cmd.Env)
	if err != nil {
		return nil, err
	}
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, err
	}
	defer attributes.Delete()
	// The value of the attribute is the console's handle itself, not a pointer to it.
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, *(*unsafe.Pointer)(unsafe.Pointer(&console)), unsafe.Sizeof(console)); err != nil {
		return nil, err
	}
	startup := windows.StartupInfoEx{ProcThreadAttributeList: attributes.List()}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	// A new process group would make the process ignore Ctrl-C, which the user types on a terminal.
	flags := cmd.SysProcAttr.CreationFlags&^windows.CREATE_NEW_PROCESS_GROUP | windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT
	var info windows.ProcessInformation
	if err := windows.CreateProcess(appName, commandLine, nil, nil, false, flags, &block[0], dir, &startup.StartupInfo, &info); err != nil {
		return nil, err
	}
	_ = windows.CloseHandle(info.Thread) // only the process is needed
	return &process{pid: int(info.ProcessId), handle: info.Process, terminal: terminal}, nil
}

// abandon ends a process that started but cannot be used, and collects it.
func abandon(proc *process) {
	if proc.cmd != nil {
		_ = proc.cmd.Process.Kill() // it may have ended already
		_ = proc.cmd.Wait()         // how it ended does not matter
		return
	}
	_ = windows.TerminateProcess(proc.handle, 1) // as above
	_ = windows.CloseHandle(proc.handle)
	proc.terminal.CloseConsole()
}

// hangUpTerminal tells a process on a terminal that its session is gone: Windows has no SIGHUP,
// so the console is closed and the job ended.
func hangUpTerminal(proc *process, terminal *pty.Pty) {
	terminal.CloseConsole()
	proc.group.job.close()
}

// wait waits for the process and returns its exit status; Windows has no signals to report. A
// process that the job ended has the status the job gave. When the process of a pseudo console
// ends, the console is closed: only that makes the console's output end.
func wait(proc *process) (status int, signal string) {
	if proc.cmd != nil {
		_ = proc.cmd.Wait() // how it ended is in the process state
		return proc.cmd.ProcessState.ExitCode(), ""
	}
	_, _ = windows.WaitForSingleObject(proc.handle, windows.INFINITE) // an error leaves the exit code unknown: 0 below is wrong only then
	var code uint32
	_ = windows.GetExitCodeProcess(proc.handle, &code) // as above
	_ = windows.CloseHandle(proc.handle)
	proc.terminal.CloseConsole()
	return int(code), ""
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
