package session

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/server/servertest"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// fixture is a server that serves sessions, with one person connected.
type fixture struct {
	*servertest.Fixture
	t      *testing.T
	client *ssh.Client
}

// caveEnv is the environment Cave is pretended to have: what a session may take from it, and
// things a session must never see.
func caveEnv(f *servertest.Fixture) []string {
	return []string{
		"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_TIME=en_GB.UTF-8", "XDG_RUNTIME_DIR=" + f.Dirs.Runtime,
		"SSH_AUTH_SOCK=/secret/agent.sock", "DISPLAY=:0", "SECRET_TOKEN=hunter2", "SHELL=/bin/false", "HOME=/somewhere/else",
	}
}

// options are the handler's options for a fixture: the isolated home, and /bin/sh as the shell.
func options(f *servertest.Fixture) Options {
	return Options{Settings: f.Settings, Logger: f.Logger, User: User{Name: "tester", Home: f.Home}, Environ: caveEnv(f), RuntimeDir: f.Dirs.Runtime}
}

// newFixture starts a server with the session handler and connects alice.
func newFixture(t *testing.T, register func(f *servertest.Fixture)) *fixture {
	t.Helper()
	f := servertest.New(t)
	// The tests' sessions run /bin/sh in the isolated home, never the user's own shell or home.
	f.Set(string(settings.SessionShell), "/bin/sh")
	if register != nil {
		register(f)
	} else {
		Register(f.Server, options(f))
	}
	f.Start()
	_, key := f.Person("alice")
	return &fixture{Fixture: f, t: t, client: f.Connect("alice", key)}
}

// result is how a command ended.
type result struct {
	stdout, stderr string
	status         int
	signal         string
}

// run runs a command in a new session with input, and returns what it wrote and how it ended.
func (f *fixture) run(command, stdin string) result {
	f.t.Helper()
	sess, err := f.client.NewSession()
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	return finish(f.t, sess, stdin, func() error { return sess.Run(command) })
}

// finish runs a session through start and collects its result.
func finish(t *testing.T, sess *ssh.Session, stdin string, start func() error) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	sess.Stdout, sess.Stderr = &stdout, &stderr
	// The input is written beside the session: a command that ends without reading it all
	// closes the channel under the writer, which is not a failure of the session.
	input, err := sess.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = io.WriteString(input, stdin)
		_ = input.Close()
	}()
	err = start()
	res := result{stdout: stdout.String(), stderr: stderr.String()}
	var exitErr *ssh.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.status, res.signal = exitErr.ExitStatus(), exitErr.Signal()
	default:
		t.Fatalf("the session failed: %v", err)
	}
	return res
}

// alive says whether a process exists.
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// waitDead waits until a process is gone.
func waitDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(servertest.WaitFor)
	for alive(pid) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL) // do not leave it behind
			t.Fatalf("the process %d is still running", pid)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestExec(t *testing.T) {
	f := newFixture(t, nil)
	tests := []struct {
		name    string
		command string
		stdin   string
		want    result
	}{
		{"output and success", "echo hello", "", result{stdout: "hello\n"}},
		{"both streams", "echo out; echo err >&2", "", result{stdout: "out\n", stderr: "err\n"}},
		{"an exit status", "echo before; exit 3", "", result{stdout: "before\n", status: 3}},
		{"a command that does not exist", "/nonexistent/command", "", result{stderr: "/bin/sh: line 1: /nonexistent/command: No such file or directory\n", status: 127}},
		{"input", "cat", "one\ntwo\n", result{stdout: "one\ntwo\n"}},
		{"the end of the input", "wc -l", "a\nb\nc\n", result{stdout: "3\n"}},
		{"input that is not read", "echo done", strings.Repeat("ignored\n", 10000), result{stdout: "done\n"}},
		{"no output at all", "true", "", result{}},
		{"quotes and special characters reach the shell as written", `printf '%s|' "a b" 'c;d' $((1+2))`, "", result{stdout: "a b|c;d|3|"}},
		// The client's library gives 128 plus the signal's number as the status of an exit-signal.
		{"killed by a signal", "kill -TERM $$", "", result{signal: "TERM", status: 143}},
		{"killed by another signal", "kill -KILL $$", "", result{signal: "KILL", status: 137}},
		{"killed by a signal the protocol has no name for", "kill -STKFLT $$", "", result{status: 128 + 16}},
		{"the working folder is the home", "pwd", "", result{stdout: "HOME\n"}},
		{"the shell is not a login shell", `case "$0" in -*) echo login;; *) echo "$0";; esac`, "", result{stdout: "/bin/sh\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := f.run(tt.command, tt.stdin)
			tt.want.stdout = strings.ReplaceAll(tt.want.stdout, "HOME", f.Home)
			if got != tt.want {
				t.Errorf("%q:\n got %+v\nwant %+v", tt.command, got, tt.want)
			}
		})
	}
}

func TestLargeAndBinaryOutput(t *testing.T) {
	f := newFixture(t, nil)
	// A megabyte of every byte value, through cat: nothing is lost, added or changed.
	data := make([]byte, 1<<20)
	for i := range data {
		data[i] = byte(i * 7)
	}
	got := f.run("cat", string(data))
	if got.status != 0 || got.stdout != string(data) {
		t.Errorf("%d bytes came back with status %d, want the %d that were sent", len(got.stdout), got.status, len(data))
	}
	// Much output on both streams at once.
	got = f.run("head -c 300000 /dev/zero | tr '\\0' o; head -c 300000 /dev/zero | tr '\\0' e >&2", "")
	if len(got.stdout) != 300000 || len(got.stderr) != 300000 || strings.Trim(got.stdout, "o") != "" || strings.Trim(got.stderr, "e") != "" {
		t.Errorf("stdout %d bytes, stderr %d bytes", len(got.stdout), len(got.stderr))
	}
}

func TestTheEnvironmentIsExactlyWhatTheSpecLists(t *testing.T) {
	// Things in the test's own environment must not reach the session either.
	t.Setenv("CAVE_TEST_LEAK", "leaked")
	f := newFixture(t, nil)
	got := f.run("env", "")
	if got.status != 0 {
		t.Fatalf("%+v", got)
	}
	local := f.client.LocalAddr().(*net.TCPAddr)
	remotePort := strconv.Itoa(local.Port)
	serverPort := f.Addr[strings.LastIndex(f.Addr, ":")+1:]
	want := []string{
		"CAVE_PERSON=alice",
		"HOME=" + f.Home,
		"LANG=C.UTF-8",
		"LC_TIME=en_GB.UTF-8",
		"LOGNAME=tester",
		"PATH=/usr/bin:/bin",
		"SHELL=/bin/sh",
		"SSH_CLIENT=127.0.0.1 " + remotePort + " " + serverPort,
		"SSH_CONNECTION=127.0.0.1 " + remotePort + " 127.0.0.1 " + serverPort,
		"USER=tester",
		"XDG_RUNTIME_DIR=" + f.Dirs.Runtime,
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(got.stdout), "\n") {
		// The shell adds these three by itself.
		if strings.HasPrefix(line, "PWD=") || strings.HasPrefix(line, "SHLVL=") || strings.HasPrefix(line, "_=") || strings.HasPrefix(line, "OLDPWD=") {
			continue
		}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("the session's environment is\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestEnvRequests(t *testing.T) {
	f := newFixture(t, nil)
	f.Set(string(settings.SessionAcceptEnv), []string{"LANG", "LC_*", "APP_*"})
	sess, err := f.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	tests := []struct {
		name, value string
		wantOK      bool
	}{
		{"LANG", "el_GR.UTF-8", true},
		{"LC_ALL", "C", true},
		{"APP_MODE", "test", true},
		{"APP_EMPTY", "", true},
		{"APP_MODE", "changed", true}, // setting one again is not one more
		{"EDITOR", "vi", false},
		{"PATH", "/evil", false},
		{"LD_PRELOAD", "/evil.so", false},
		{"lang", "x", false},
		{"", "x", false},
		{"APP_A=B", "x", false},
		{"APP_BIG", strings.Repeat("v", MaxEnvBytes), false}, // name and value together are over the limit
		{"APP_FITS", strings.Repeat("v", MaxEnvBytes-len("APP_FITS")), true}, // exactly at it
	}
	for _, tt := range tests {
		err := sess.Setenv(tt.name, tt.value)
		if (err == nil) != tt.wantOK {
			t.Errorf("Setenv(%q): %v, want accepted = %v", tt.name, err, tt.wantOK)
		}
	}
	got := finish(t, sess, "", func() error {
		return sess.Run(`echo "$LANG|$LC_ALL|$APP_MODE|${APP_EMPTY-unset}|${EDITOR-unset}|$PATH|${LD_PRELOAD-unset}|${#APP_FITS}"`)
	})
	want := "el_GR.UTF-8|C|changed||unset|/usr/bin:/bin|unset|" + strconv.Itoa(MaxEnvBytes-len("APP_FITS")) + "\n"
	if got.stdout != want {
		t.Errorf("the session saw %q, want %q", got.stdout, want)
	}

	t.Run("at most 64 variables", func(t *testing.T) {
		sess, err := f.client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sess.Close() }()
		for i := 0; i < MaxEnvVars; i++ {
			if err := sess.Setenv("APP_"+strconv.Itoa(i), "x"); err != nil {
				t.Fatalf("variable %d was refused: %v", i+1, err)
			}
		}
		if err := sess.Setenv("APP_ONE_MORE", "x"); err == nil {
			t.Error("the 65th variable was accepted")
		}
		if err := sess.Setenv("APP_0", "again"); err != nil {
			t.Errorf("setting one of the 64 again was refused: %v", err)
		}
	})
	t.Run("a setting change applies to the next request", func(t *testing.T) {
		sess, err := f.client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sess.Close() }()
		f.Set(string(settings.SessionAcceptEnv), []string{})
		if err := sess.Setenv("LANG", "C"); err == nil {
			t.Error("a variable was accepted although none is allowed")
		}
	})
}

func TestTheShellRequest(t *testing.T) {
	f := newFixture(t, nil)
	sess, err := f.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	// Without a PTY, a shell reads its commands from its input.
	got := finish(t, sess, "echo \"argv0=$0\"\necho hello from the shell\nexit 4\n", func() error {
		if err := sess.Shell(); err != nil {
			return err
		}
		return sess.Wait()
	})
	if got.stdout != "argv0=-sh\nhello from the shell\n" || got.status != 4 {
		t.Errorf("%+v", got)
	}
}

func TestOnlyOneStartPerChannel(t *testing.T) {
	f := newFixture(t, nil)
	channel, requests, err := f.client.OpenChannel("session", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = channel.Close() }()
	go ssh.DiscardRequests(requests)
	exec := func(command string) bool {
		ok, err := channel.SendRequest("exec", true, ssh.Marshal(struct{ Command string }{command}))
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !exec("cat") {
		t.Fatal("the first exec was refused")
	}
	if exec("echo second") {
		t.Error("a second exec on the channel was granted")
	}
	if ok, err := channel.SendRequest("shell", true, nil); err != nil || ok {
		t.Errorf("a shell after an exec: ok = %v, err = %v", ok, err)
	}
	// An env request after the start is refused too: it could not apply.
	if ok, err := channel.SendRequest("env", true, ssh.Marshal(struct{ Name, Value string }{"LANG", "C"})); err != nil || ok {
		t.Errorf("env after the start: ok = %v, err = %v", ok, err)
	}
	// Requests the session does not know, or does not have yet, are refused; "break" is taken.
	for request, want := range map[string]bool{"nonsense": false, "x11-req": false, "break": true} {
		if ok, err := channel.SendRequest(request, true, nil); err != nil || ok != want {
			t.Errorf("%s: ok = %v, err = %v, want %v", request, ok, err, want)
		}
	}
	// A malformed exec payload on a fresh channel is refused.
	fresh, freshRequests, err := f.client.OpenChannel("session", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fresh.Close() }()
	go ssh.DiscardRequests(freshRequests)
	if ok, err := fresh.SendRequest("exec", true, []byte{0, 0}); err != nil || ok {
		t.Errorf("a malformed exec: ok = %v, err = %v", ok, err)
	}
	if ok, err := fresh.SendRequest("env", true, []byte{1}); err != nil || ok {
		t.Errorf("a malformed env: ok = %v, err = %v", ok, err)
	}
}

func TestSignalRequests(t *testing.T) {
	f := newFixture(t, nil)
	tests := []struct {
		signal ssh.Signal
		want   string // the signal the process is killed by; "" when the request is refused
	}{
		{ssh.SIGTERM, "TERM"}, {ssh.SIGINT, "INT"}, {ssh.SIGHUP, "HUP"}, {ssh.SIGKILL, "KILL"}, {ssh.SIGQUIT, "QUIT"},
		{ssh.SIGUSR1, "USR1"}, {ssh.SIGUSR2, "USR2"},
		{ssh.SIGSEGV, ""}, {ssh.SIGABRT, ""}, {ssh.Signal("NONSENSE"), ""},
	}
	for _, tt := range tests {
		t.Run(string(tt.signal), func(t *testing.T) {
			sess, err := f.client.NewSession()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = sess.Close() }()
			stdout, err := sess.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			// "exec" replaces the shell, so that the signal's effect is the process's own.
			if err := sess.Start("echo ready; exec sleep 600"); err != nil {
				t.Fatal(err)
			}
			ready := make([]byte, len("ready\n"))
			if _, err := io.ReadFull(stdout, ready); err != nil {
				t.Fatal(err)
			}
			if err := sess.Signal(tt.signal); err != nil {
				t.Fatal(err)
			}
			if tt.want == "" {
				// Refused: the process runs on. End it with a signal that is taken.
				time.Sleep(50 * time.Millisecond)
				if err := sess.Signal(ssh.SIGKILL); err != nil {
					t.Fatal(err)
				}
			}
			err = sess.Wait()
			var exitErr *ssh.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("the session ended with %v", err)
			}
			want := tt.want
			if want == "" {
				want = "KILL"
			}
			if exitErr.Signal() != want {
				t.Errorf("killed by %q, want %q", exitErr.Signal(), want)
			}
		})
	}
}

func TestAProcessThatOutlivesItsSessionGetsSIGHUP(t *testing.T) {
	for name, end := range map[string]func(f *fixture, sess *ssh.Session){
		"the client closes the channel": func(_ *fixture, sess *ssh.Session) { _ = sess.Close() },
		"the connection ends":           func(f *fixture, _ *ssh.Session) { _ = f.client.Close() },
		"the user disconnects it":       func(f *fixture, _ *ssh.Session) { f.Server.DisconnectAll() },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			sess, err := f.client.NewSession()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := sess.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			// The shell starts a child and waits: both are in the session's process group.
			if err := sess.Start("sleep 600 & echo $! $$; wait"); err != nil {
				t.Fatal(err)
			}
			var child, shell int
			line := make([]byte, 64)
			n, err := stdout.Read(line)
			if err != nil {
				t.Fatal(err)
			}
			fields := strings.Fields(string(line[:n]))
			if len(fields) != 2 {
				t.Fatalf("the session printed %q", line[:n])
			}
			child, _ = strconv.Atoi(fields[0])
			shell, _ = strconv.Atoi(fields[1])
			if !alive(child) || !alive(shell) {
				t.Fatalf("the processes %d and %d are not running", child, shell)
			}
			end(f, sess)
			waitDead(t, shell)
			waitDead(t, child)
		})
	}
}

func TestAJobThatIgnoresSIGHUPGoesOn(t *testing.T) {
	f := newFixture(t, nil)
	sess, err := f.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	// As with sshd, Cave tells the job once and never kills it.
	if err := sess.Start("trap '' HUP; echo $$; exec sleep 600"); err != nil {
		t.Fatal(err)
	}
	line := make([]byte, 32)
	n, err := stdout.Read(line)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(line[:n])))
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	_ = sess.Close()
	f.WaitConnections(1)
	time.Sleep(300 * time.Millisecond)
	if !alive(pid) {
		t.Error("a job that ignores SIGHUP was killed")
	}
}

func TestABackgroundChildThatHoldsThePipeDoesNotHangTheSession(t *testing.T) {
	f := newFixture(t, nil)
	begin := time.Now()
	// The child inherits the output pipe and outlives the shell: sshd would wait for it.
	got := f.run("sleep 4 & echo done", "")
	took := time.Since(begin)
	if got.stdout != "done\n" || got.status != 0 {
		t.Errorf("%+v", got)
	}
	if took < DrainTime || took > DrainTime+1500*time.Millisecond {
		t.Errorf("the session ended after %v, want the %v the output is waited for", took, DrainTime)
	}
}

func TestTheRegistryShowsTheSession(t *testing.T) {
	f := newFixture(t, nil)
	activities := func() []server.Activity { return f.WaitConnections(1)[0].Activities }
	waitActivities := func(n int) []server.Activity {
		t.Helper()
		deadline := time.Now().Add(servertest.WaitFor)
		for len(activities()) != n {
			if time.Now().After(deadline) {
				t.Fatalf("the registry shows %d activities, want %d", len(activities()), n)
			}
			time.Sleep(time.Millisecond)
		}
		return activities()
	}
	execSess, err := f.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := execSess.Start("sleep 600 # \x1b[31mred"); err != nil {
		t.Fatal(err)
	}
	shellSess, err := f.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := shellSess.Shell(); err != nil {
		t.Fatal(err)
	}
	got := waitActivities(2)
	want := []server.Activity{{Kind: server.ActivityExec, Detail: "sleep 600 # \uFFFD[31mred"}, {Kind: server.ActivityShell}}
	if got[0] != want[0] || got[1] != want[1] {
		t.Errorf("the activities are %+v, want %+v", got, want)
	}
	_ = execSess.Close()
	_ = shellSess.Close()
	waitActivities(0)

	// The log has the start and the end, with the command made safe.
	log := f.Log.String()
	if !strings.Contains(log, `"msg":"session started"`) || !strings.Contains(log, `"msg":"session ended"`) || strings.Contains(log, "\x1b") {
		t.Errorf("the log:\n%s", log)
	}
}

func TestManySessionsAtOnce(t *testing.T) {
	f := newFixture(t, nil)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess, err := f.client.NewSession()
			if err != nil {
				t.Errorf("session %d: %v", i, err)
				return
			}
			defer func() { _ = sess.Close() }()
			out, err := sess.Output("echo " + strconv.Itoa(i) + "; exit 0")
			if err != nil || string(out) != strconv.Itoa(i)+"\n" {
				t.Errorf("session %d: %q, %v", i, out, err)
			}
		}()
	}
	wg.Wait()
}

func TestTheHomeFolderIsMissing(t *testing.T) {
	f := newFixture(t, func(f *servertest.Fixture) {
		opts := options(f)
		opts.User.Home = filepath.Join(f.Home, "does-not-exist")
		Register(f.Server, opts)
	})
	if got := f.run("pwd", ""); got.stdout != "/\n" || got.status != 0 {
		t.Errorf("%+v", got)
	}
}

// loggedError finds the error of the log line with the message.
func loggedError(t *testing.T, log, message string) errors.ErrorJson {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		var entry struct {
			Msg string           `json:"msg"`
			Err errors.ErrorJson `json:"err"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err == nil && entry.Msg == message {
			return entry.Err
		}
	}
	t.Fatalf("the log has no line %q:\n%s", message, log)
	return errors.ErrorJson{}
}

func TestAShellThatCannotStart(t *testing.T) {
	f := newFixture(t, nil)
	f.Set(string(settings.SessionShell), "/nonexistent/shell")
	for name, start := range map[string]func(sess *ssh.Session) error{
		"exec":  func(sess *ssh.Session) error { return sess.Run("echo hello") },
		"shell": func(sess *ssh.Session) error { _ = sess.Shell(); return sess.Wait() },
	} {
		t.Run(name, func(t *testing.T) {
			sess, err := f.client.NewSession()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = sess.Close() }()
			got := finish(t, sess, "", func() error { return start(sess) })
			if got.status != 255 || got.stdout != "" || !strings.Contains(got.stderr, "the session could not start") {
				t.Errorf("%+v", got)
			}
		})
	}
	logged := loggedError(t, f.Log.String(), "session could not start")
	if logged.Tag != TagSessionStartFailed || !logged.HasRoute("handler.spawn.SessionStartFailed") || logged.Metadata["shell"] != "/nonexistent/shell" {
		t.Errorf("logged %+v with the route %s", logged, logged.Route())
	}
	// The registry shows nothing for a session that did not start.
	if activities := f.WaitConnections(1)[0].Activities; len(activities) != 0 {
		t.Errorf("activities = %+v", activities)
	}
}

func TestPipesThatCannotBeMade(t *testing.T) {
	for failAt := 1; failAt <= 3; failAt++ {
		t.Run("pipe "+strconv.Itoa(failAt), func(t *testing.T) {
			var mu sync.Mutex
			var made []*os.File
			calls := 0
			f := newFixture(t, func(f *servertest.Fixture) {
				h := &handler{opts: options(f), pipe: func() (*os.File, *os.File, error) {
					mu.Lock()
					defer mu.Unlock()
					calls++
					if calls == failAt {
						return nil, nil, io.ErrShortBuffer
					}
					r, w, err := os.Pipe()
					made = append(made, r, w)
					return r, w, err
				}}
				f.Server.HandleChannel("session", h.serve)
			})
			got := f.run("echo hello", "")
			if got.status != 255 || !strings.Contains(got.stderr, "the session could not start") {
				t.Errorf("%+v", got)
			}
			logged := loggedError(t, f.Log.String(), "session could not start")
			if !logged.HasRoute("handler.spawn.SessionPipeFailed") {
				t.Errorf("logged the route %s", logged.Route())
			}
			// The pipes made before the failure were closed again.
			mu.Lock()
			defer mu.Unlock()
			for _, file := range made {
				if err := file.Close(); err == nil {
					t.Error("a pipe was left open after the failure")
				}
			}
		})
	}
}

func TestStopEndsSessions(t *testing.T) {
	f := newFixture(t, nil)
	sess, err := f.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Start("echo $$; exec sleep 600"); err != nil {
		t.Fatal(err)
	}
	line := make([]byte, 32)
	n, err := stdout.Read(line)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(line[:n])))
	stopCtx, cancel := context.WithTimeout(context.Background(), servertest.WaitFor)
	defer cancel()
	if err := f.Server.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	waitDead(t, pid)
}
