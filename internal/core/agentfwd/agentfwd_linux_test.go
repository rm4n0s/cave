package agentfwd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// wait is how long a test waits for the other side of a socket.
const wait = 10 * time.Second

// syncBuffer is a log that goroutines write and the test reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// fixture is a socket whose connections are carried to an echo: what a program sends to the
// "agent" comes back.
type fixture struct {
	t      *testing.T
	socket *Socket
	log    *syncBuffer
	// runtime is the runtime folder the socket's folder is in.
	runtime string
	// opened counts the connections to the client's agent that were opened.
	opened atomic.Int32
}

// echoOpener returns an Opener whose connections send back what they get.
func (f *fixture) echoOpener() Opener {
	return func() (io.ReadWriteCloser, error) {
		f.opened.Add(1)
		here, there := net.Pipe()
		go func() {
			_, _ = io.Copy(there, there)
			_ = there.Close()
		}()
		return here, nil
	}
}

// newFixture makes a socket in a runtime folder with a short path, with the opener (nil: an
// echo).
func newFixture(t *testing.T, open Opener) *fixture {
	t.Helper()
	f := &fixture{t: t, log: &syncBuffer{}, runtime: filepath.Join(sshtest.ShortDir(t), "cave")}
	if open == nil {
		open = f.echoOpener()
	}
	socket, err := Listen(context.Background(), Options{RuntimeDir: f.runtime, Open: open, Logger: slog.New(slog.NewJSONHandler(f.log, nil))})
	if err != nil {
		t.Fatal(err)
	}
	f.socket = socket
	t.Cleanup(socket.Close)
	return f
}

// dial connects to the socket as a program of the session would.
func (f *fixture) dial() net.Conn {
	f.t.Helper()
	conn, err := net.Dial("unix", f.socket.Path())
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(wait)); err != nil {
		f.t.Fatal(err)
	}
	return conn
}

// echo sends a message through a connection and expects it back.
func echo(t *testing.T, conn net.Conn, message string) {
	t.Helper()
	if _, err := io.WriteString(conn, message); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(message))
	if _, err := io.ReadFull(conn, got); err != nil || string(got) != message {
		t.Fatalf("the answer is %q, %v, want %q", got, err, message)
	}
}

// loggedError returns the error of the log line with the message.
func (f *fixture) loggedError(message string) errors.ErrorJson {
	f.t.Helper()
	deadline := time.Now().Add(wait)
	for {
		for _, line := range strings.Split(strings.TrimSpace(f.log.String()), "\n") {
			var entry struct {
				Msg string           `json:"msg"`
				Err errors.ErrorJson `json:"err"`
			}
			if err := json.Unmarshal([]byte(line), &entry); err == nil && entry.Msg == message {
				return entry.Err
			}
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("the log has no line %q:\n%s", message, f.log.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTheSocketIsPrivate(t *testing.T) {
	f := newFixture(t, nil)
	path := f.socket.Path()
	if filepath.Base(path) != SocketName || filepath.Dir(filepath.Dir(path)) != f.runtime {
		t.Errorf("the socket is %s, want %s in a folder of its own under %s", path, SocketName, f.runtime)
	}
	for name, want := range map[string]os.FileMode{
		f.runtime:          os.ModeDir | 0o700,
		filepath.Dir(path): os.ModeDir | 0o700,
		path:               os.ModeSocket | 0o600,
	} {
		info, err := os.Lstat(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode() != want {
			t.Errorf("%s has the mode %v, want %v", name, info.Mode(), want)
		}
	}
	// Every session has a folder and a socket of its own.
	other := newFixture(t, nil)
	if other.socket.Path() == path {
		t.Error("two sockets have the same path")
	}
}

func TestConnectionsAreCarriedToTheClientsAgent(t *testing.T) {
	f := newFixture(t, nil)
	first, second := f.dial(), f.dial()
	echo(t, first, "first request")
	echo(t, second, "second request")
	echo(t, first, "first again")
	if got := f.opened.Load(); got != 2 {
		t.Errorf("%d connections to the client's agent were opened, want one for each program", got)
	}
	// A program that leaves ends its connection to the agent; the other goes on.
	_ = first.Close()
	echo(t, second, "second again")
}

func TestARealAgentThroughTheSocket(t *testing.T) {
	sshtest.Require(t)
	key := sshtest.NewKey(t, sshtest.Ed25519)
	clientAgent := sshtest.StartAgent(t, key)
	// What a client does with an auth-agent@openssh.com channel: it connects it to its agent.
	f := newFixture(t, func() (io.ReadWriteCloser, error) { return net.Dial("unix", clientAgent) })

	keys, err := agent.NewClient(f.dial()).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || ssh.FingerprintSHA256(keys[0]) != ssh.FingerprintSHA256(key.Public) {
		t.Errorf("the agent behind the socket lists %v, want the key %s", keys, ssh.FingerprintSHA256(key.Public))
	}
	// The agent signs for a program of the session; the key itself never leaves the client.
	signers, err := agent.NewClient(f.dial()).Signers()
	if err != nil || len(signers) != 1 {
		t.Fatalf("signers: %v, %v", signers, err)
	}
	data := []byte("something to sign")
	signature, err := signers[0].Sign(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := key.Public.Verify(data, signature); err != nil {
		t.Errorf("the signature does not verify: %v", err)
	}
}

func TestCloseRemovesTheSocketAndEndsItsConnections(t *testing.T) {
	f := newFixture(t, nil)
	conn := f.dial()
	echo(t, conn, "hello")
	path := f.socket.Path()
	f.socket.Close()
	f.socket.Close() // twice is fine
	for _, name := range []string{path, filepath.Dir(path)} {
		if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is still there: %v", name, err)
		}
	}
	if _, err := os.Stat(f.runtime); err != nil {
		t.Errorf("the runtime folder is gone: %v", err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Error("a connection is still open after Close")
	}
	if _, err := net.Dial("unix", path); err == nil {
		t.Error("the socket still takes connections")
	}
	if strings.Contains(f.log.String(), "agent forwarding failed") {
		t.Errorf("closing the socket was logged as a failure:\n%s", f.log.String())
	}
}

func TestTheEndOfTheContextClosesTheSocket(t *testing.T) {
	f := &fixture{t: t, log: &syncBuffer{}, runtime: filepath.Join(sshtest.ShortDir(t), "cave")}
	ctx, cancel := context.WithCancel(context.Background())
	socket, err := Listen(ctx, Options{RuntimeDir: f.runtime, Open: f.echoOpener(), Logger: slog.New(slog.NewJSONHandler(f.log, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	cancel()
	deadline := time.Now().Add(wait)
	for {
		if _, err := os.Lstat(socket.Path()); errors.Is(err, os.ErrNotExist) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the socket is still there after the context ended")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTooManyConnectionsAtOnce(t *testing.T) {
	f := newFixture(t, nil)
	var conns []net.Conn
	for i := 0; i < MaxConns; i++ {
		conns = append(conns, f.dial())
	}
	echo(t, conns[len(conns)-1], "the last one that fits")
	// One more is closed as it arrives.
	extra := f.dial()
	if got, err := io.ReadAll(extra); len(got) != 0 || err != nil {
		t.Errorf("one connection more than the limit read %q, %v", got, err)
	}
	// Room again when one of them leaves.
	_ = conns[0].Close()
	deadline := time.Now().Add(wait)
	for {
		conn := f.dial()
		if _, err := io.WriteString(conn, "x"); err == nil {
			if _, err := io.ReadFull(conn, make([]byte, 1)); err == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no room after a connection left")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAClientThatDoesNotOpenItsAgent(t *testing.T) {
	f := newFixture(t, func() (io.ReadWriteCloser, error) { return nil, io.ErrClosedPipe })
	conn := f.dial()
	// The program sees the socket close, as when there is no agent.
	if got, err := io.ReadAll(conn); len(got) != 0 || err != nil {
		t.Errorf("the program read %q, %v", got, err)
	}
	logged := f.loggedError("the client did not open its agent")
	if logged.Tag != TagOpenFailed || !logged.HasRoute("Socket.carry.AgentOpenFailed") || !strings.Contains(logged.OriginalErr, "closed pipe") {
		t.Errorf("logged %+v with the route %s", logged, logged.Route())
	}
	// The socket goes on: the next program is tried again.
	_ = f.dial()
	f.socket.Close()
}

func TestListenErrors(t *testing.T) {
	open := func() (io.ReadWriteCloser, error) { return nil, io.EOF }
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	t.Run("a runtime folder that cannot be made", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Listen(context.Background(), Options{RuntimeDir: filepath.Join(file, "cave"), Open: open, Logger: logger})
		meta := errtest.Route(t, err, "Listen->listen.AgentRuntimeDirFailed")
		if meta["dir"] != filepath.Join(file, "cave") || !errors.Is(err, syscall.ENOTDIR) {
			t.Errorf("metadata = %v, cause %v", meta, errors.Unwrap(err))
		}
	})
	t.Run("a runtime folder that may not be written", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root is not refused by file permissions")
		}
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		_, err := Listen(context.Background(), Options{RuntimeDir: dir, Open: open, Logger: logger})
		errtest.Route(t, err, "Listen->listen.AgentDirFailed")
		if !errors.Is(err, os.ErrPermission) {
			t.Errorf("the cause is %v", errors.Unwrap(err))
		}
	})
	t.Run("a path too long for a socket", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), strings.Repeat("long", 30))
		_, err := Listen(context.Background(), Options{RuntimeDir: dir, Open: open, Logger: logger})
		meta := errtest.Route(t, err, "Listen->listen.AgentListenFailed")
		if path, _ := meta["path"].(string); !strings.HasSuffix(path, SocketName) {
			t.Errorf("metadata = %v", meta)
		}
		// The folder that was made for the socket is not left behind.
		if entries, readErr := os.ReadDir(dir); readErr != nil || len(entries) != 0 {
			t.Errorf("the runtime folder has %v, %v", entries, readErr)
		}
	})
	t.Run("a mode that cannot be set", func(t *testing.T) {
		dir := filepath.Join(sshtest.ShortDir(t), "cave")
		_, err := listen(context.Background(), Options{RuntimeDir: dir, Open: open, Logger: logger}, func(string, os.FileMode) error { return syscall.EPERM })
		errtest.Route(t, err, "listen.AgentModeFailed")
		if !errors.Is(err, syscall.EPERM) {
			t.Errorf("the cause is %v", errors.Unwrap(err))
		}
		// Neither the socket nor its folder is left behind: a socket of the wrong mode is not
		// used.
		if entries, readErr := os.ReadDir(dir); readErr != nil || len(entries) != 0 {
			t.Errorf("the runtime folder has %v, %v", entries, readErr)
		}
	})
}

// brokenListener is a listener whose Accept fails.
type brokenListener struct {
	net.Listener
}

func (brokenListener) Accept() (net.Conn, error) { return nil, syscall.EMFILE }

func TestAListenerThatFails(t *testing.T) {
	log := &syncBuffer{}
	dir := sshtest.ShortDir(t)
	listener, err := net.Listen("unix", filepath.Join(dir, SocketName))
	if err != nil {
		t.Fatal(err)
	}
	s := &Socket{
		opts: Options{Logger: slog.New(slog.NewJSONHandler(log, nil))}, dir: dir, listener: brokenListener{listener},
		conns: map[net.Conn]struct{}{}, done: make(chan struct{}),
	}
	s.serve() // returns: a listener that fails is not tried again
	f := &fixture{t: t, log: log}
	logged := f.loggedError("agent forwarding failed")
	if logged.Tag != TagAcceptFailed || !logged.HasRoute("Socket.serve.AgentAcceptFailed") || !strings.Contains(logged.OriginalErr, "too many open files") {
		t.Errorf("logged %+v with the route %s", logged, logged.Route())
	}
	s.Close()
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the folder is still there: %v", err)
	}
}
