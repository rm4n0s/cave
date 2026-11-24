//go:build linux || darwin

package session

import (
	"bufio"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/pty"
	"github.com/rm4n0s/cave/internal/core/server/servertest"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// ptyRequest is the payload of a pty-req.
type ptyRequest struct {
	Term                      string
	Cols, Rows, Width, Height uint32
	Modes                     string
}

// windowChange is the payload of a window-change.
type windowChange struct{ Cols, Rows, Width, Height uint32 }

// echoOff are the encoded terminal modes that turn the echo off.
const echoOff = "\x35\x00\x00\x00\x00\x00"

// ptySession opens a session with a terminal of 80 columns and 24 rows that does not echo.
func (f *fixture) ptySession() *ssh.Session {
	f.t.Helper()
	sess, err := f.client.NewSession()
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = sess.Close() })
	if err := sess.RequestPty("xterm-256color", 24, 80, ssh.TerminalModes{ssh.ECHO: 0}); err != nil {
		f.t.Fatal(err)
	}
	return sess
}

// terminals counts the pseudo terminals this process has open, either side.
func terminals(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if fdIsTerminal(entry.Name()) {
			count++
		}
	}
	return count
}

// waitTerminals waits until this process has as many pseudo terminals open as before.
func waitTerminals(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(servertest.WaitFor)
	for terminals(t) != want {
		if time.Now().After(deadline) {
			t.Fatalf("%d pseudo terminals are open, want %d", terminals(t), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// readLine reads one line of a terminal's output, without its end.
func readLine(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("the session's output ended after %q: %v", line, err)
	}
	return strings.TrimRight(line, "\r\n")
}

func TestASessionWithATerminal(t *testing.T) {
	before := terminals(t)
	f := newFixture(t, nil)
	sess := f.ptySession()
	got := finish(t, sess, "", func() error {
		return sess.Run(`tty; echo "$SSH_TTY"; echo "$TERM"; echo to the error stream >&2; exit 5`)
	})
	// A terminal has one stream, and ends its lines with a carriage return and a line feed.
	want := regexp.MustCompile(`^(` + ttyPattern + `)\r\n(` + ttyPattern + `)\r\nxterm-256color\r\nto the error stream\r\n$`)
	match := want.FindStringSubmatch(got.stdout)
	if match == nil || match[1] != match[2] || got.stderr != "" || got.status != 5 {
		t.Errorf("%+v", got)
	}
	waitTerminals(t, before)
}

func TestWithoutAPtyRequestThereIsNoTerminal(t *testing.T) {
	f := newFixture(t, nil)
	// TERM is not asked: a shell gives it a value of its own. The exact environment is tested
	// by TestTheEnvironmentIsExactlyWhatTheSpecLists.
	got := f.run(`tty; echo "tty=$SSH_TTY"`, "")
	if got.stdout != "not a tty\ntty=\n" {
		t.Errorf("%+v", got)
	}
}

func TestWindowChange(t *testing.T) {
	f := newFixture(t, nil)
	sess := f.ptySession()
	stdin, err := sess.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Start("stty size; read x; stty size; read x; stty size"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	if got := readLine(t, reader); got != "24 80" {
		t.Errorf("the size at the start is %q", got)
	}
	// The request is answered before the next line is typed, so the order is certain.
	ok, err := sess.SendRequest("window-change", true, ssh.Marshal(windowChange{Cols: 132, Rows: 43, Width: 1056, Height: 688}))
	if err != nil || !ok {
		t.Fatalf("window-change: %v, %v", ok, err)
	}
	if _, err := stdin.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	if got := readLine(t, reader); got != "43 132" {
		t.Errorf("the size after window-change is %q", got)
	}
	// Dimensions of 0 were not said: the request is granted and changes nothing.
	if ok, err := sess.SendRequest("window-change", true, ssh.Marshal(windowChange{})); err != nil || !ok {
		t.Errorf("a window-change that says no size = %v, %v", ok, err)
	}
	for name, size := range map[string]windowChange{
		"too many columns": {Cols: pty.MaxCells + 1, Rows: 24},
		"too many rows":    {Cols: 80, Rows: pty.MaxCells + 1},
	} {
		if ok, err := sess.SendRequest("window-change", true, ssh.Marshal(size)); err != nil || ok {
			t.Errorf("%s: window-change = %v, %v, want it refused", name, ok, err)
		}
	}
	if ok, err := sess.SendRequest("window-change", true, []byte{1, 2, 3}); err != nil || ok {
		t.Errorf("a window-change that does not parse = %v, %v, want it refused", ok, err)
	}
	if _, err := stdin.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	if got := readLine(t, reader); got != "43 132" {
		t.Errorf("the size after refused changes is %q", got)
	}
	if err := sess.Wait(); err != nil {
		t.Errorf("the session ended with %v", err)
	}
}

func TestATerminalWhoseSizeIsNotSaid(t *testing.T) {
	// What "ssh -tt" sends from a script, which has no terminal of its own (RFC 4254 §6.2: zero
	// dimensions are ignored).
	f := newFixture(t, nil)
	sess, err := f.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	if ok, err := sess.SendRequest("pty-req", true, ssh.Marshal(ptyRequest{Term: "dumb", Modes: echoOff})); err != nil || !ok {
		t.Fatalf("pty-req = %v, %v", ok, err)
	}
	got := finish(t, sess, "", func() error { return sess.Run("test -t 0 && stty size") })
	if got.stdout != "0 0\r\n" || got.status != 0 {
		t.Errorf("%+v", got)
	}
}

func TestWindowChangeWithoutATerminalIsRefused(t *testing.T) {
	f := newFixture(t, nil)
	sess, err := f.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	if ok, err := sess.SendRequest("window-change", true, ssh.Marshal(windowChange{Cols: 80, Rows: 24})); err != nil || ok {
		t.Errorf("window-change = %v, %v, want it refused", ok, err)
	}
}

func TestPtyRequestsThatAreRefused(t *testing.T) {
	before := terminals(t)
	tests := []struct {
		name    string
		payload []byte
		// wantTag is the Tag of the error the log has for the refusal; "" when none is logged.
		wantTag   string
		wantRoute string
	}{
		{"too many columns", ssh.Marshal(ptyRequest{Term: "xterm", Cols: pty.MaxCells + 1, Rows: 24}), pty.TagSizeInvalid, "Open->open->Size.Check.PtySizeInvalid"},
		{"too many rows", ssh.Marshal(ptyRequest{Term: "xterm", Cols: 80, Rows: pty.MaxCells + 1}), pty.TagSizeInvalid, "Open->open->Size.Check.PtySizeInvalid"},
		{
			"modes that end in the middle of a value", ssh.Marshal(ptyRequest{Term: "xterm", Cols: 80, Rows: 24, Modes: "\x35\x00\x00"}),
			pty.TagModesInvalid, "ParseModes.PtyModesInvalid",
		},
		{"an escape sequence as the terminal type", ssh.Marshal(ptyRequest{Term: "xterm\x1b[31m", Cols: 80, Rows: 24}), "", ""},
		{"a space in the terminal type", ssh.Marshal(ptyRequest{Term: "xterm color", Cols: 80, Rows: 24}), "", ""},
		{"a line feed in the terminal type", ssh.Marshal(ptyRequest{Term: "xterm\nPATH=/tmp", Cols: 80, Rows: 24}), "", ""},
		{"a terminal type that is not ASCII", ssh.Marshal(ptyRequest{Term: "xt\u00e9rm", Cols: 80, Rows: 24}), "", ""},
		{"a terminal type that is too long", ssh.Marshal(ptyRequest{Term: strings.Repeat("x", maxTermLength+1), Cols: 80, Rows: 24}), "", ""},
		{"a request that does not parse", []byte{0, 0, 0, 9, 'x'}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, nil)
			sess, err := f.client.NewSession()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = sess.Close() }()
			if ok, err := sess.SendRequest("pty-req", true, tt.payload); err != nil || ok {
				t.Fatalf("pty-req = %v, %v, want it refused", ok, err)
			}
			if tt.wantTag != "" {
				logged := loggedError(t, f.Log.String(), "pty-req refused")
				if logged.Tag != tt.wantTag || !logged.HasRoute(tt.wantRoute) {
					t.Errorf("logged %+v with the route %s", logged, logged.Route())
				}
			}
			// The session goes on without a terminal.
			got := finish(t, sess, "", func() error { return sess.Run("tty") })
			if got.stdout != "not a tty\n" || got.status != 1 {
				t.Errorf("%+v", got)
			}
		})
	}
	waitTerminals(t, before)
}

func TestOnlyOnePtyPerChannelAndOnlyBeforeTheStart(t *testing.T) {
	before := terminals(t)
	f := newFixture(t, nil)
	valid := ssh.Marshal(ptyRequest{Term: "xterm", Cols: 80, Rows: 24, Modes: echoOff})

	t.Run("a second pty-req", func(t *testing.T) {
		sess, err := f.client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sess.Close() }()
		if ok, err := sess.SendRequest("pty-req", true, valid); err != nil || !ok {
			t.Fatalf("the first pty-req = %v, %v", ok, err)
		}
		if ok, err := sess.SendRequest("pty-req", true, valid); err != nil || ok {
			t.Errorf("the second pty-req = %v, %v, want it refused", ok, err)
		}
		got := finish(t, sess, "", func() error { return sess.Run("stty size") })
		if got.stdout != "24 80\r\n" {
			t.Errorf("%+v", got)
		}
	})
	t.Run("a pty-req after the start", func(t *testing.T) {
		sess, err := f.client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sess.Close() }()
		stdin, err := sess.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := sess.Start("cat"); err != nil {
			t.Fatal(err)
		}
		if ok, err := sess.SendRequest("pty-req", true, valid); err != nil || ok {
			t.Errorf("pty-req = %v, %v, want it refused", ok, err)
		}
		_ = stdin.Close()
		if err := sess.Wait(); err != nil {
			t.Errorf("the session ended with %v", err)
		}
	})
	t.Run("a terminal that is never used", func(t *testing.T) {
		sess, err := f.client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := sess.SendRequest("pty-req", true, valid); err != nil || !ok {
			t.Fatalf("pty-req = %v, %v", ok, err)
		}
		_ = sess.Close()
	})
	waitTerminals(t, before)
}

func TestTerminalModes(t *testing.T) {
	f := newFixture(t, nil)
	tests := []struct {
		name  string
		modes ssh.TerminalModes
		want  []string
	}{
		{"the defaults", ssh.TerminalModes{}, []string{" echo ", " icrnl ", "erase = ^?"}},
		{"no echo", ssh.TerminalModes{ssh.ECHO: 0}, []string{" -echo ", " icrnl "}},
		{"several", ssh.TerminalModes{ssh.ECHO: 0, ssh.ICRNL: 0, ssh.VERASE: 8, ssh.TTY_OP_ISPEED: 38400}, []string{" -echo ", "-icrnl ", "erase = ^H"}},
		{"an opcode the system does not have", ssh.TerminalModes{17: 1, ssh.ECHO: 0}, []string{" -echo "}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess, err := f.client.NewSession()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = sess.Close() }()
			if err := sess.RequestPty("vt100", 24, 80, tt.modes); err != nil {
				t.Fatal(err)
			}
			got := finish(t, sess, "", func() error { return sess.Run("stty -a") })
			settings := " " + strings.Join(strings.Fields(strings.NewReplacer(";", " ").Replace(got.stdout)), " ") + " "
			for _, want := range tt.want {
				if !strings.Contains(settings, strings.Join(strings.Fields(want), " ")) && !strings.Contains(settings, want) {
					t.Errorf("stty -a has no %q:\n%s", want, got.stdout)
				}
			}
		})
	}
}

func TestALoginShellOnATerminal(t *testing.T) {
	f := newFixture(t, nil)
	sess := f.ptySession()
	got := finish(t, sess, "echo \"argv0=$0\"; test -t 0 && echo on a terminal; exit 3\n", func() error {
		if err := sess.Shell(); err != nil {
			return err
		}
		return sess.Wait()
	})
	// The shell is interactive: its prompt is in the output too.
	if !strings.Contains(got.stdout, "argv0=-sh\r\n") || !strings.Contains(got.stdout, "on a terminal\r\n") || got.status != 3 {
		t.Errorf("%+v", got)
	}
}

func TestTheTerminalIsTheControllingTerminal(t *testing.T) {
	f := newFixture(t, nil)
	sess := f.ptySession()
	stdin, err := sess.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Start("echo ready; exec sleep 600"); err != nil {
		t.Fatal(err)
	}
	if got := readLine(t, bufio.NewReader(stdout)); got != "ready" {
		t.Fatalf("the session printed %q", got)
	}
	// The interrupt character reaches the process as SIGINT only through a controlling terminal.
	if _, err := stdin.Write([]byte{3}); err != nil {
		t.Fatal(err)
	}
	err = sess.Wait()
	var exitErr *ssh.ExitError
	if !errors.As(err, &exitErr) || exitErr.Signal() != "INT" {
		t.Errorf("the session ended with %v, want the signal INT", err)
	}
}

func TestAProcessOnATerminalThatOutlivesItsSessionGetsSIGHUP(t *testing.T) {
	before := terminals(t)
	for name, end := range map[string]func(f *fixture, sess *ssh.Session){
		"the client closes the channel": func(_ *fixture, sess *ssh.Session) { _ = sess.Close() },
		"the connection ends":           func(f *fixture, _ *ssh.Session) { _ = f.client.Close() },
		"the user disconnects it":       func(f *fixture, _ *ssh.Session) { f.Server.DisconnectAll() },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			sess := f.ptySession()
			stdout, err := sess.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := sess.Start("sleep 600 & echo $! $$; wait"); err != nil {
				t.Fatal(err)
			}
			fields := strings.Fields(readLine(t, bufio.NewReader(stdout)))
			if len(fields) != 2 {
				t.Fatalf("the session printed %q", fields)
			}
			child, _ := strconv.Atoi(fields[0])
			shell, _ := strconv.Atoi(fields[1])
			if !alive(child) || !alive(shell) {
				t.Fatalf("the processes %d and %d are not running", child, shell)
			}
			// Closing the terminal is all Cave does: the kernel tells the processes on it.
			end(f, sess)
			waitDead(t, shell)
			waitDead(t, child)
			waitTerminals(t, before)
		})
	}
}

func TestAJobOnATerminalThatIgnoresSIGHUPGoesOn(t *testing.T) {
	before := terminals(t)
	f := newFixture(t, nil)
	sess := f.ptySession()
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Start("trap '' HUP; echo $$; exec sleep 600"); err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(readLine(t, bufio.NewReader(stdout)))
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	_ = sess.Close()
	// Cave's side of the terminal is closed, the job is left alone.
	waitTerminals(t, before)
	time.Sleep(300 * time.Millisecond)
	if !alive(pid) {
		t.Error("a job that ignores SIGHUP was killed")
	}
}

func TestABackgroundChildThatHoldsTheTerminalDoesNotHangTheSession(t *testing.T) {
	before := terminals(t)
	f := newFixture(t, nil)
	sess := f.ptySession()
	begin := time.Now()
	// The child ignores the hang-up of the terminal, keeps it open and outlives the shell.
	got := finish(t, sess, "", func() error { return sess.Run("trap '' HUP; sleep 600 & echo $!") })
	took := time.Since(begin)
	pid, _ := strconv.Atoi(strings.TrimSpace(got.stdout))
	if pid <= 0 {
		t.Fatalf("%+v", got)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if got.status != 0 || took > DrainTime+1500*time.Millisecond {
		t.Errorf("the session ended after %v with %+v", took, got)
	}
	if !alive(pid) {
		t.Error("the child was killed")
	}
	waitTerminals(t, before)
}

func TestManyTerminalsLeaveNothingOpen(t *testing.T) {
	before := terminals(t)
	f := newFixture(t, nil)
	for i := 0; i < 20; i++ {
		sess, err := f.client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		if err := sess.RequestPty("xterm", 24, 80, ssh.TerminalModes{}); err != nil {
			t.Fatal(err)
		}
		got := finish(t, sess, "", func() error { return sess.Run("echo " + strconv.Itoa(i)) })
		if got.stdout != strconv.Itoa(i)+"\r\n" {
			t.Errorf("session %d: %+v", i, got)
		}
		_ = sess.Close()
	}
	waitTerminals(t, before)
}
