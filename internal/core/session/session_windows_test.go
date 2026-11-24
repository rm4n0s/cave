package session

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/server/servertest"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/windows"
)

// These tests run under Wine (scripts/test-windows.sh). Their sessions run cmd.exe, which Wine has,
// in the isolated home, and a helper that is this test binary itself (TestHelperProcess).

// helperArg marks a run of the test binary as a helper process.
const helperArg = "--cave-session-helper"

// TestHelperProcess is not a test: it is the program the tests start inside a session. Run on its
// own it does nothing.
func TestHelperProcess(t *testing.T) {
	i := slices.Index(os.Args, helperArg)
	if i < 0 {
		return
	}
	switch os.Args[i+1] {
	case "cat":
		_, _ = io.Copy(os.Stdout, os.Stdin)
	case "sleep":
		time.Sleep(60 * time.Second)
	case "spawn":
		// Starts a second helper that sleeps, and says which process it is.
		child := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "--", helperArg, "sleep")
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		_, _ = os.Stdout.WriteString(strconv.Itoa(child.Process.Pid) + "\n")
		_ = child.Wait()
	}
	os.Exit(0)
}

// helperCommand is the command line that runs a helper mode in a session.
func helperCommand(mode string) string {
	return quoteWindowsArg(os.Args[0]) + " -test.run=^TestHelperProcess$ -- " + helperArg + " " + mode
}

// winFixture is a server that serves sessions with cmd.exe, with one person connected.
type winFixture struct {
	*servertest.Fixture
	t      *testing.T
	client *ssh.Client
}

func newWinFixture(t *testing.T) *winFixture {
	t.Helper()
	t.Setenv("SECRET_TOKEN", "hunter2") // Cave's own environment: a session must not see it
	f := servertest.New(t)
	f.Set(string(settings.SessionShell), "cmd.exe")
	Register(f.Server, Options{Settings: f.Settings, Logger: f.Logger, User: User{Name: "tester", Home: f.Home},
		Environ: os.Environ(), RuntimeDir: f.Dirs.Runtime})
	f.Start()
	_, key := f.Person("alice")
	return &winFixture{Fixture: f, t: t, client: f.Connect("alice", key)}
}

// winResult is how a command ended.
type winResult struct {
	stdout, stderr string
	status         int
}

// run runs a command in a new session with input.
func (f *winFixture) run(command, stdin string) winResult {
	f.t.Helper()
	sess, err := f.client.NewSession()
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	var stdout, stderr bytes.Buffer
	sess.Stdout, sess.Stderr = &stdout, &stderr
	input, err := sess.StdinPipe()
	if err != nil {
		f.t.Fatal(err)
	}
	go func() {
		_, _ = io.WriteString(input, stdin)
		_ = input.Close()
	}()
	res := winResult{}
	err = sess.Run(command)
	var exitErr *ssh.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.status = exitErr.ExitStatus()
	default:
		f.t.Fatalf("the session failed: %v", err)
	}
	res.stdout, res.stderr = stdout.String(), stderr.String()
	return res
}

// processAlive says whether a process is running.
func processAlive(pid uint32) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

func TestExecOnWindows(t *testing.T) {
	f := newWinFixture(t)
	tests := []struct {
		name    string
		command string
		want    winResult
		check   func(t *testing.T, got winResult)
	}{
		{name: "output and success", command: "echo hello", want: winResult{stdout: "hello\r\n"}},
		{name: "an exit status", command: "exit 3", want: winResult{status: 3}},
		{name: "the command reaches the shell as written", command: `echo "a b"   c`, want: winResult{stdout: `"a b"   c` + "\r\n"}},
		{name: "a command that does not exist", command: "cave-no-such-command-xyz", check: func(t *testing.T, got winResult) {
			if got.status == 0 || got.stderr == "" {
				t.Errorf("%+v", got)
			}
		}},
		{name: "the working folder is the home", command: "cd", want: winResult{stdout: "HOME\r\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := f.run(tt.command, "")
			if tt.check != nil {
				tt.check(t, got)
				return
			}
			tt.want.stdout = strings.ReplaceAll(tt.want.stdout, "HOME", f.Home)
			if got != tt.want {
				t.Errorf("%q:\n got %+v\nwant %+v", tt.command, got, tt.want)
			}
		})
	}
}

func TestTheEnvironmentOnWindows(t *testing.T) {
	f := newWinFixture(t)
	got := f.run("set", "")
	if got.status != 0 {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(got.stdout, "SECRET_TOKEN") || strings.Contains(got.stdout, "hunter2") {
		t.Errorf("Cave's own variable reached the session:\n%s", got.stdout)
	}
	for _, want := range []string{"CAVE_PERSON=alice", "SSH_CLIENT=127.0.0.1 ", "SSH_CONNECTION=127.0.0.1 "} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("the environment has no %q:\n%s", want, got.stdout)
		}
	}
}

func TestStandardInputOnWindows(t *testing.T) {
	f := newWinFixture(t)
	data := strings.Repeat("0123456789abcdef", 1<<16) // 1 MiB
	got := f.run(helperCommand("cat"), data)
	if got.status != 0 || got.stdout != data {
		t.Errorf("%d bytes came back with status %d and error %q, want the %d that were sent", len(got.stdout), got.status, got.stderr, len(data))
	}
}

func TestJobEndsWhatTheSessionStarted(t *testing.T) {
	f := newWinFixture(t)
	sess, err := f.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Start(helperCommand("spawn")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	pid := uint32(n)
	if !processAlive(pid) {
		t.Fatalf("the second helper %d is not running", pid)
	}
	_ = sess.Close() // the client closes the channel: Windows has no SIGHUP, the job is closed
	deadline := time.Now().Add(5 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			if h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid); err == nil {
				_ = windows.TerminateProcess(h, 1) // do not leave it behind
				_ = windows.CloseHandle(h)
			}
			t.Fatalf("the second helper %d is still running 5 seconds after its session ended", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSignalOnWindows(t *testing.T) {
	f := newWinFixture(t)
	sess, err := f.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	if err := sess.Start(helperCommand("sleep")); err != nil {
		t.Fatal(err)
	}
	ended := make(chan error, 1)
	go func() { ended <- sess.Wait() }()

	// signal INT is refused: the helper goes on.
	if err := sess.Signal(ssh.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-ended:
		t.Fatalf("the session ended after INT: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	// KILL ends the job; the client gets a status.
	if err := sess.Signal(ssh.SIGKILL); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-ended:
		var exitErr *ssh.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitStatus() != 1 {
			t.Errorf("the session ended with %v, want the exit status 1", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not end after KILL")
	}
}

func TestAShellThatCannotStartOnWindows(t *testing.T) {
	f := newWinFixture(t)
	f.Set(string(settings.SessionShell), `C:\nonexistent\shell.exe`)
	got := f.run("echo hello", "")
	if got.status != 255 || got.stdout != "" || !strings.Contains(got.stderr, "the session could not start") {
		t.Errorf("%+v", got)
	}
}
