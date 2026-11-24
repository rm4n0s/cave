package agentfwd

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/cave/internal/core/wine"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/sys/windows"
)

// These tests run under Wine (scripts/test-windows.sh), on real named pipes.

// wait is how long a test waits for the other side of a pipe.
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

// fixture is a pipe whose connections are carried to the opener's side.
type fixture struct {
	t      *testing.T
	socket *Socket
	log    *syncBuffer
}

// echoOpener returns an Opener whose connections send back what they get.
func echoOpener() Opener {
	return func() (io.ReadWriteCloser, error) {
		here, there := net.Pipe()
		go func() {
			_, _ = io.Copy(there, there)
			_ = there.Close()
		}()
		return here, nil
	}
}

// keyringOpener returns an Opener whose connections speak the agent protocol for the keyring.
func keyringOpener(keyring agent.Agent) Opener {
	return func() (io.ReadWriteCloser, error) {
		here, there := net.Pipe()
		go func() {
			_ = agent.ServeAgent(keyring, there)
			_ = there.Close()
		}()
		return here, nil
	}
}

func newFixture(t *testing.T, open Opener) *fixture {
	t.Helper()
	if open == nil {
		open = echoOpener()
	}
	f := &fixture{t: t, log: &syncBuffer{}}
	socket, err := Listen(context.Background(), Options{Open: open, Logger: slog.New(slog.NewJSONHandler(f.log, nil))})
	if err != nil {
		t.Fatal(err)
	}
	f.socket = socket
	t.Cleanup(socket.Close)
	return f
}

// openPipe opens the pipe as a program of the session would. A pipe whose instances are all busy
// is tried again for a moment, as Windows' OpenSSH does.
func openPipe(t testing.TB, name string) (*os.File, error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		file, err := os.OpenFile(name, os.O_RDWR, 0)
		if err == nil || !errors.Is(err, windows.ERROR_PIPE_BUSY) || time.Now().After(deadline) {
			return file, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// dial opens the pipe, closing the connection when the test ends or after the wait, whichever is
// first (a read of a pipe has no deadline).
func (f *fixture) dial() *os.File {
	f.t.Helper()
	file, err := openPipe(f.t, f.socket.Path())
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = file.Close() })
	timer := time.AfterFunc(wait, func() { _ = file.Close() })
	f.t.Cleanup(func() { timer.Stop() })
	return file
}

// echo sends a message through a connection and expects it back.
func echo(t *testing.T, conn io.ReadWriter, message string) {
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

func TestThePipeIsNamedAtRandom(t *testing.T) {
	a, b := newFixture(t, nil), newFixture(t, nil)
	if !strings.HasPrefix(a.socket.Path(), `\\.\pipe\cave-agent-`) || len(a.socket.Path()) != len(pipePrefix)+32 {
		t.Errorf("Path() = %q", a.socket.Path())
	}
	if a.socket.Path() == b.socket.Path() {
		t.Errorf("two sessions have the pipe %q", a.socket.Path())
	}
}

func TestConnectionsAreCarriedToTheClientsAgent(t *testing.T) {
	f := newFixture(t, nil)
	first, second := f.dial(), f.dial()
	echo(t, first, "first request")
	echo(t, second, "second request")
	echo(t, first, "first again")
	// A program that leaves ends its connection to the agent; the other goes on.
	_ = first.Close()
	echo(t, second, "second again")
}

func TestARealAgentThroughThePipe(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: private}); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, keyringOpener(keyring))

	keys, err := agent.NewClient(f.dial()).List()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || ssh.FingerprintSHA256(keys[0]) != ssh.FingerprintSHA256(signer.PublicKey()) {
		t.Fatalf("the agent behind the pipe lists %v, want the key %s", keys, ssh.FingerprintSHA256(signer.PublicKey()))
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
	if err := signer.PublicKey().Verify(data, signature); err != nil {
		t.Errorf("the signature does not verify: %v", err)
	}
}

func TestCloseEndsTheConnectionsAndTheListener(t *testing.T) {
	f := newFixture(t, nil)
	conn := f.dial()
	echo(t, conn, "hello")
	name := f.socket.Path()
	read := make(chan error, 1)
	go func() {
		_, err := conn.Read(make([]byte, 1)) // waits: nothing is sent
		read <- err
	}()
	time.Sleep(50 * time.Millisecond)
	f.socket.Close()
	f.socket.Close() // twice is fine
	select {
	case err := <-read:
		if err == nil {
			t.Error("a read of the connection got data after Close")
		}
	case <-time.After(wait):
		t.Fatal("a read that waited did not end when the pipe was closed")
	}
	if file, err := os.OpenFile(name, os.O_RDWR, 0); err == nil {
		_ = file.Close()
		t.Error("the pipe still takes connections after Close")
	}
	if strings.Contains(f.log.String(), "agent forwarding failed") {
		t.Errorf("closing the pipe was logged as a failure:\n%s", f.log.String())
	}
}

func TestTheEndOfTheContextClosesThePipe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	socket, err := Listen(ctx, Options{Open: echoOpener(), Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	cancel()
	deadline := time.Now().Add(wait)
	for {
		if file, err := os.OpenFile(socket.Path(), os.O_RDWR, 0); err != nil {
			break
		} else {
			_ = file.Close()
		}
		if time.Now().After(deadline) {
			t.Fatal("the pipe still takes connections after the context ended")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTooManyConnectionsAtOnce(t *testing.T) {
	f := newFixture(t, nil)
	var conns []*os.File
	for i := 0; i < MaxConns; i++ {
		conns = append(conns, f.dial())
	}
	echo(t, conns[len(conns)-1], "the last one that fits")
	// One more is closed as it arrives.
	extra := f.dial()
	if got, err := io.ReadAll(extra); len(got) != 0 {
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
	// The program sees the pipe close, as when there is no agent.
	if got, err := io.ReadAll(conn); len(got) != 0 {
		t.Errorf("the program read %q, %v", got, err)
	}
	logged := f.loggedError("the client did not open its agent")
	if logged.Tag != TagOpenFailed || !logged.HasRoute("Socket.carry.AgentOpenFailed") || !strings.Contains(logged.OriginalErr, "closed pipe") {
		t.Errorf("logged %+v with the route %s", logged, logged.Route())
	}
	// The pipe goes on: the next program is tried again.
	_ = f.dial()
}

func TestAPipeThatIsTakenCannotBeMadeAgain(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	name := pipeName()
	first, err := listenOn(context.Background(), Options{Open: echoOpener(), Logger: logger}, name)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	// FILE_FLAG_FIRST_PIPE_INSTANCE: nobody can have made a pipe of that name before.
	second, err := listenOn(context.Background(), Options{Open: echoOpener(), Logger: logger}, name)
	if second != nil {
		second.Close()
		if wine.Running() {
			t.Skip("Wine ignores FILE_FLAG_FIRST_PIPE_INSTANCE: a second pipe of the name is made")
		}
		t.Fatal("a second pipe was made with the name of the first")
	}
	meta := errtest.Route(t, err, "listenOn.listenPipe.AgentPipeCreateFailed")
	if meta["name"] != name {
		t.Errorf("metadata %v", meta)
	}
}

func TestOnlyTheCurrentUserHasAccessToThePipe(t *testing.T) {
	f := newFixture(t, nil)
	handle, err := windows.CreateFile(windows.StringToUTF16Ptr(f.socket.Path()), windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatalf("the pipe does not open for reading its descriptor: %v", err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_KERNEL_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Skipf("the system does not tell the descriptor of a pipe (Wine?): %v", err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		t.Skipf("the pipe's descriptor has no list to look at: %v", err)
	}
	if dacl.AceCount != 1 {
		t.Fatalf("the list has %d entries, want 1 (descriptor %s)", dacl.AceCount, descriptor.String())
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	if got := (*windows.SID)(unsafePointer(&ace.SidStart)); !got.Equals(user.User.Sid) {
		t.Errorf("the only entry is for %s, want %s", got, user.User.Sid)
	}
}

func TestNothingIsLeftRunning(t *testing.T) {
	before := runtime.NumGoroutine()
	for i := 0; i < 5; i++ {
		f := newFixture(t, nil)
		conn := f.dial()
		echo(t, conn, "hello")
		_ = conn.Close()
		f.socket.Close()
	}
	deadline := time.Now().Add(wait)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("%d goroutines before, %d after:\n%s", before, runtime.NumGoroutine(), buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// unsafePointer is the address of a field as a pointer, for reading the SID that follows the
// header of an access control entry.
func unsafePointer(field *uint32) unsafe.Pointer { return unsafe.Pointer(field) }
