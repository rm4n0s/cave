package session

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/pty"
	"github.com/rm4n0s/cave/internal/core/server/servertest"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/cave/internal/core/sftp"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"github.com/rm4n0s/cave/internal/core/wine"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
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
		pid := strconv.Itoa(child.Process.Pid) + "\n"
		if file := os.Args[i+2:]; len(file) > 0 {
			_ = os.WriteFile(file[0], []byte(pid), 0o600) // for a session on a terminal, which has no output to read it from
		} else {
			_, _ = os.Stdout.WriteString(pid)
		}
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
		Environ: os.Environ(), RuntimeDir: f.Dirs.Runtime, History: f.History, Clock: f.Clock})
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

// consoleOutput collects what a session on a terminal writes, and waits for a text in it.
type consoleOutput struct {
	mu   sync.Mutex
	text strings.Builder
}

func (c *consoleOutput) collect(r io.Reader) {
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			c.mu.Lock()
			c.text.Write(buf[:n])
			c.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
}

// waitFor waits up to ten seconds until the output holds the text, and returns the output.
func (c *consoleOutput) waitFor(t *testing.T, want string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		c.mu.Lock()
		got := c.text.String()
		c.mu.Unlock()
		if strings.Contains(got, want) {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("after 10 s the terminal output has no %q:\n%q", want, got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// terminalSession opens a session with a terminal that runs cmd.exe or a command. A test that
// needs to read the terminal's output (needsOutput) skips under Wine: Wine's ConPTY makes the
// pseudo console but does not connect the process's standard handles to it, so cmd.exe writes its
// prompt to the test's own output (docs/SPEC.md §13.6; docs/MANUAL-CHECKS.md has the check for a
// Windows PC). A Wine that cannot make a pseudo console skips every test.
func (f *winFixture) terminalSession(t *testing.T, command string, needsOutput bool) (*ssh.Session, *consoleOutput, io.WriteCloser) {
	t.Helper()
	if wine.Running() {
		p, err := pty.Open(pty.Size{Cols: 80, Rows: 24}, nil)
		if err != nil {
			t.Skipf("Wine's ConPTY: %v", err)
		}
		_ = p.Close()
		if needsOutput {
			t.Skip("Wine's ConPTY does not connect the standard handles of the process to the pseudo console")
		}
	}
	sess, err := f.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	if err := sess.RequestPty("xterm-256color", 24, 80, ssh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out := &consoleOutput{}
	out.collect(stdout)
	if command == "" {
		err = sess.Shell()
	} else {
		err = sess.Start(command)
	}
	if err != nil {
		t.Fatal(err)
	}
	return sess, out, stdin
}

// ended waits for the session to end and returns its exit status.
func ended(t *testing.T, sess *ssh.Session) int {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	select {
	case err := <-done:
		var exitErr *ssh.ExitError
		switch {
		case err == nil:
			return 0
		case errors.As(err, &exitErr):
			return exitErr.ExitStatus()
		}
		t.Fatalf("the session ended with %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not end")
	}
	return -1
}

func TestShellOnATerminalOnWindows(t *testing.T) {
	f := newWinFixture(t)
	sess, out, stdin := f.terminalSession(t, "", true)
	if _, err := io.WriteString(stdin, "echo marker-%CAVE_PERSON%\r"); err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, "marker-alice")
	if _, err := io.WriteString(stdin, "echo term-%TERM%\r"); err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, "term-xterm-256color")
	if _, err := io.WriteString(stdin, "exit 5\r"); err != nil {
		t.Fatal(err)
	}
	if status := ended(t, sess); status != 5 {
		t.Errorf("the exit status is %d, want 5", status)
	}
}

func TestExitStatusOnATerminalOnWindows(t *testing.T) {
	f := newWinFixture(t)
	sess, _, _ := f.terminalSession(t, "exit 5", false)
	if status := ended(t, sess); status != 5 {
		t.Errorf("the exit status is %d, want 5", status)
	}
}

func TestWindowChangeOnWindows(t *testing.T) {
	f := newWinFixture(t)
	sess, _, _ := f.terminalSession(t, helperCommand("sleep"), false)
	type size struct{ Cols, Rows, Width, Height uint32 }
	for _, tt := range []struct {
		size size
		want bool
	}{{size{132, 43, 0, 0}, true}, {size{100, 0, 0, 0}, true}, {size{0, 0, 0, 0}, true}, {size{5000, 24, 0, 0}, false}} {
		ok, err := sess.SendRequest("window-change", true, ssh.Marshal(tt.size))
		if err != nil || ok != tt.want {
			t.Errorf("window-change %+v: granted %v (error %v), want %v", tt.size, ok, err, tt.want)
		}
	}
	if err := sess.Signal(ssh.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if status := ended(t, sess); status != 1 {
		t.Errorf("the exit status is %d, want 1", status)
	}
}

func TestClosingTheChannelEndsTheProcessOnATerminal(t *testing.T) {
	f := newWinFixture(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	sess, _, _ := f.terminalSession(t, helperCommand("spawn")+" "+quoteWindowsArg(pidFile), false)
	var pid uint32
	deadline := time.Now().Add(10 * time.Second)
	for pid == 0 {
		if data, err := os.ReadFile(pidFile); err == nil && strings.HasSuffix(string(data), "\n") {
			n, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			pid = uint32(n)
		} else if time.Now().After(deadline) {
			t.Fatal("the helper did not write the process id of its child")
		} else {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !processAlive(pid) {
		t.Fatalf("the second helper %d is not running", pid)
	}
	_ = sess.Close()
	deadline = time.Now().Add(5 * time.Second)
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

func TestTheSftpSubsystemStartsOnWindows(t *testing.T) {
	f := newWinFixture(t)
	channel, requests, err := f.client.OpenChannel("session", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = channel.Close() })
	go ssh.DiscardRequests(requests)
	ok, err := channel.SendRequest("subsystem", true, ssh.Marshal(struct{ Name string }{"sftp"}))
	if err != nil || !ok {
		t.Fatalf("the sftp subsystem was refused (error %v)", err)
	}
	client := sshtest.NewSFTPClient(t, channel)
	if version, _ := client.Init(); version != sftp.Version {
		t.Errorf("the server speaks version %d, want %d", version, sftp.Version)
	}
	// The session starts in the home folder, in the form of SFTP.
	reply := client.Call(sshtest.SFTPRealpath, ".")
	if names := reply.Names(); len(names) != 1 || !strings.EqualFold(names[0].Name, fromSFTPHome(f.Home)) {
		t.Errorf("realpath . = %+v, want the home %s", names, f.Home)
	}
}

// fromSFTPHome is the home folder as an SFTP client sees it: "/C:/Users/ada".
func fromSFTPHome(home string) string {
	return "/" + strings.ReplaceAll(home, `\`, "/")
}

func TestAgentForwardingOnWindows(t *testing.T) {
	f := newWinFixture(t)
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: private}); err != nil {
		t.Fatal(err)
	}
	if err := agent.ForwardToAgent(f.client, keyring); err != nil {
		t.Fatal(err)
	}
	sess, err := f.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	if err := agent.RequestAgentForwarding(sess); err != nil {
		t.Fatalf("auth-agent-req: %v", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close() }()
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	// The session prints its environment, then waits for input that never comes.
	if err := sess.Start("set & " + helperCommand("cat")); err != nil {
		t.Fatal(err)
	}
	var pipe string
	reader := bufio.NewReader(stdout)
	for pipe == "" {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("the output ended without SSH_AUTH_SOCK: %v", err)
		}
		pipe = strings.TrimSpace(strings.TrimPrefix(line, "SSH_AUTH_SOCK="))
		if !strings.HasPrefix(line, "SSH_AUTH_SOCK=") {
			pipe = ""
		}
	}
	if !strings.HasPrefix(pipe, `\\.\pipe\cave-agent-`) {
		t.Fatalf("SSH_AUTH_SOCK is %q", pipe)
	}
	// The pipe leads to the client's agent: it lists the one key.
	conn, err := os.OpenFile(pipe, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	keys, err := agent.NewClient(conn).List()
	if err != nil || len(keys) != 1 {
		t.Fatalf("the agent behind SSH_AUTH_SOCK lists %v, %v", keys, err)
	}
	// Without a forwarded agent the variable is not there.
	if got := f.run("set", ""); strings.Contains(got.stdout, "SSH_AUTH_SOCK") {
		t.Errorf("a session that asked for no agent has SSH_AUTH_SOCK:\n%s", got.stdout)
	}
}
