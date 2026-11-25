package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/cave/internal/core/history"
	"github.com/rm4n0s/cave/internal/core/hostkey"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"github.com/rm4n0s/cave/internal/core/storage/storagetest"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
	"sync/atomic"
	"syscall"
)

var ctx = context.Background()

// start is the time of the tests' clock.
var start = time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)

// waitFor is how long a test waits for the server's side of something the client did.
const waitFor = 10 * time.Second

// syncBuffer is a log buffer that the server's goroutines and the test share.
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

// fixture is a server on a loopback port with everything it needs, in the test's own folders.
type fixture struct {
	t        *testing.T
	server   *Server
	people   *people.Store
	attempts *attempts.List
	history  *history.Store
	settings *settings.Store
	clock    *clock.Fake
	hostKey  *hostkey.HostKey
	log      *syncBuffer
	// addr is the address the server listens on.
	addr string
}

// newStopped makes the server without starting it.
func newStopped(t *testing.T) *fixture {
	t.Helper()
	dirs := storagetest.Isolate(t)
	f := &fixture{t: t, clock: clock.NewFake(start), log: &syncBuffer{}}
	logger := slog.New(slog.NewJSONHandler(f.log, nil))
	var err error
	if f.people, err = people.Open(ctx, dirs, f.clock); err != nil {
		t.Fatal(err)
	}
	if f.attempts, err = attempts.Open(ctx, attempts.Options{Dirs: dirs, Clock: f.clock, Logger: logger}); err != nil {
		t.Fatal(err)
	}
	if f.settings, err = settings.Open(ctx, dirs); err != nil {
		t.Fatal(err)
	}
	if f.history, err = history.Open(ctx, history.Options{Dirs: dirs, Clock: f.clock, Logger: logger}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.history.Close(); err != nil {
			t.Errorf("history: %v", err)
		}
	})
	if f.hostKey, err = hostkey.LoadOrCreate(ctx, dirs); err != nil {
		t.Fatal(err)
	}
	// The keepalive is off unless a test is about it: a test that moves the fake clock far in
	// one step would otherwise ask a client several times in no time at all, and close it.
	if err := f.settings.Set(ctx, string(settings.ServerKeepAliveSeconds), 0); err != nil {
		t.Fatal(err)
	}
	f.server = New(Options{
		People: f.people, Attempts: f.attempts, History: f.history, HostKey: f.hostKey.Signer(), Settings: f.settings, Clock: f.clock, Logger: logger,
		Version: "test", Address: func(settings.Snapshot) string { return "127.0.0.1:0" },
	})
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(ctx, waitFor)
		defer cancel()
		if err := f.server.Stop(stopCtx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	return f
}

// newFixture makes the server and starts it.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := newStopped(t)
	f.start()
	return f
}

// start starts the server and notes its address.
func (f *fixture) start() {
	f.t.Helper()
	if err := f.server.Start(ctx); err != nil {
		f.t.Fatal(err)
	}
	f.addr = f.server.Addrs()[0].String()
}

// person adds a person with a new key.
func (f *fixture) person(name string) (people.Person, sshtest.Key) {
	f.t.Helper()
	p, err := f.people.Add(ctx, name)
	if err != nil {
		f.t.Fatal(err)
	}
	key := sshtest.NewKey(f.t, sshtest.Ed25519)
	if _, err := f.people.AddKeys(ctx, p.ID, key.AuthorizedKey); err != nil {
		f.t.Fatal(err)
	}
	return p, key
}

// total returns how many attempts the list has counted.
func (f *fixture) total() int64 {
	var n int64
	for _, e := range f.attempts.Entries() {
		n += e.Count
	}
	return n
}

// waitTotal waits until the list has counted n attempts, and returns its entries.
func (f *fixture) waitTotal(n int64) []attempts.Entry {
	f.t.Helper()
	deadline := time.Now().Add(waitFor)
	for f.total() < n {
		if time.Now().After(deadline) {
			f.t.Fatalf("the attempts list has %d attempts after %v, want %d", f.total(), waitFor, n)
		}
		time.Sleep(time.Millisecond)
	}
	if got := f.total(); got != n {
		f.t.Fatalf("the attempts list has %d attempts, want %d", got, n)
	}
	return f.attempts.Entries()
}

// dial connects a client inside the test with the configuration and returns its error.
func (f *fixture) dial(config *ssh.ClientConfig) (*ssh.Client, error) {
	f.t.Helper()
	return sshtest.DialErr(f.t, f.addr, config)
}

// config is the configuration of a client that logs in as user with the keys.
func (f *fixture) config(user string, keys ...sshtest.Key) *ssh.ClientConfig {
	return sshtest.ClientConfig(user, f.hostKey.Signer().PublicKey(), keys...)
}

// raw opens a TCP connection to the server, closed when the test ends.
func (f *fixture) raw(local string) net.Conn {
	f.t.Helper()
	dialer := net.Dialer{Timeout: waitFor}
	if local != "" {
		dialer.LocalAddr = &net.TCPAddr{IP: net.ParseIP(local)}
	}
	conn, err := dialer.Dial("tcp", f.addr)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// banner reads the server's identification line from a raw connection: "" when the server
// closed the connection without one.
func banner(t *testing.T, conn net.Conn) string {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(waitFor)); err != nil {
		t.Fatal(err)
	}
	var line []byte
	buf := make([]byte, 1)
	for {
		n, err := conn.Read(buf)
		if n == 1 {
			line = append(line, buf[0])
			if buf[0] == '\n' {
				return string(line)
			}
		}
		if err == io.EOF {
			return string(line)
		}
		if err != nil {
			t.Fatalf("reading the banner: %v", err)
		}
	}
}

// isOpen says whether the server still holds a raw connection open: a read gets a timeout, not
// the end.
func isOpen(t *testing.T, conn net.Conn) bool {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	_, err := conn.Read(make([]byte, 1))
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return true
	}
	return false
}

// waitClosed waits until the server closed a raw connection.
func waitClosed(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(waitFor)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, conn); err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			t.Fatal("the server did not close the connection")
		}
	}
}

// failingSigner offers its key and then cannot sign.
type failingSigner struct{ ssh.Signer }

func (failingSigner) Sign(io.Reader, []byte) (*ssh.Signature, error) {
	return nil, io.ErrClosedPipe
}

// lyingSigner offers its key and signs something else.
type lyingSigner struct{ ssh.Signer }

func (s lyingSigner) Sign(rand io.Reader, _ []byte) (*ssh.Signature, error) {
	return s.Signer.Sign(rand, []byte("not what the server asked to be signed"))
}

// withSigner returns a client configuration that authenticates with a signer.
func (f *fixture) withSigner(user string, signer ssh.Signer) *ssh.ClientConfig {
	config := f.config(user)
	config.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	return config
}

func TestOutcomes(t *testing.T) {
	f := newFixture(t)
	alice, aliceKey := f.person("alice")
	bob, bobKey := f.person("bob")
	carol, carolKey := f.person("carol")
	if err := f.people.SetEnabled(ctx, carol.ID, false); err != nil {
		t.Fatal(err)
	}
	dave, err := f.people.Add(ctx, "dave")
	if err != nil {
		t.Fatal(err)
	}
	rsaKey := sshtest.NewKey(t, sshtest.RSA)
	if _, err := f.people.AddKeys(ctx, dave.ID, rsaKey.AuthorizedKey); err != nil {
		t.Fatal(err)
	}
	stranger, stranger2 := sshtest.NewKey(t, sshtest.Ed25519), sshtest.NewKey(t, sshtest.Ed25519)
	fp := func(k sshtest.Key) string { return ssh.FingerprintSHA256(k.Public) }
	sha1Only, err := ssh.NewSignerWithAlgorithms(rsaKey.Signer.(ssh.AlgorithmSigner), []string{ssh.KeyAlgoRSA})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		// connect makes the attempt and reports whether the client was let in.
		connect func() bool
		want    attempts.Entry
		// wantIn says whether the client must have been let in.
		wantIn bool
	}{
		{
			name:    "the person's key",
			connect: func() bool { _, err := f.dial(f.config("alice", aliceKey)); return err == nil },
			want: attempts.Entry{Outcome: attempts.Accepted, PersonID: alice.ID, PersonName: "alice", LoginName: "alice",
				ClientVersion: "SSH-2.0-Go", Keys: []string{fp(aliceKey)}},
			wantIn: true,
		},
		{
			name:    "a wrong key first, then the person's",
			connect: func() bool { _, err := f.dial(f.config("bob", stranger, bobKey)); return err == nil },
			want: attempts.Entry{Outcome: attempts.Accepted, PersonID: bob.ID, PersonName: "bob", LoginName: "bob",
				ClientVersion: "SSH-2.0-Go", Keys: []string{fp(bobKey)}},
			wantIn: true,
		},
		{
			name:    "an RSA key signing with SHA-2",
			connect: func() bool { _, err := f.dial(f.config("dave", rsaKey)); return err == nil },
			want: attempts.Entry{Outcome: attempts.Accepted, PersonID: dave.ID, PersonName: "dave", LoginName: "dave",
				ClientVersion: "SSH-2.0-Go", Keys: []string{fp(rsaKey)}},
			wantIn: true,
		},
		{
			name:    "an RSA key signing with SHA-1",
			connect: func() bool { _, err := f.dial(f.withSigner("dave", sha1Only)); return err == nil },
			// The algorithm is refused before the key is looked at: no key was offered in a way Cave takes.
			want: attempts.Entry{Outcome: attempts.NoKey, PersonID: dave.ID, PersonName: "dave", LoginName: "dave", ClientVersion: "SSH-2.0-Go"},
		},
		{
			name:    "a key nobody has",
			connect: func() bool { _, err := f.dial(f.config("alice", stranger)); return err == nil },
			want: attempts.Entry{Outcome: attempts.WrongKey, PersonID: alice.ID, PersonName: "alice", LoginName: "alice",
				ClientVersion: "SSH-2.0-Go", Keys: []string{fp(stranger)}},
		},
		{
			name:    "two keys nobody has",
			connect: func() bool { _, err := f.dial(f.config("bob", stranger, stranger2)); return err == nil },
			want: attempts.Entry{Outcome: attempts.WrongKey, PersonID: bob.ID, PersonName: "bob", LoginName: "bob",
				ClientVersion: "SSH-2.0-Go", Keys: []string{fp(stranger), fp(stranger2)}},
		},
		{
			name:    "somebody else's key",
			connect: func() bool { _, err := f.dial(f.config("alice", bobKey)); return err == nil },
			want: attempts.Entry{Outcome: attempts.WrongKey, PersonID: alice.ID, PersonName: "alice", LoginName: "alice",
				ClientVersion: "SSH-2.0-Go", Keys: []string{fp(bobKey)}, KeyOwner: "bob"},
		},
		{
			name:    "a name nobody has",
			connect: func() bool { _, err := f.dial(f.config("root", stranger)); return err == nil },
			want:    attempts.Entry{Outcome: attempts.UnknownName, LoginName: "root", ClientVersion: "SSH-2.0-Go", Keys: []string{fp(stranger)}},
		},
		{
			name:    "a name nobody has, with a person's key",
			connect: func() bool { _, err := f.dial(f.config("Alice", aliceKey)); return err == nil },
			want: attempts.Entry{Outcome: attempts.UnknownName, LoginName: "Alice", ClientVersion: "SSH-2.0-Go",
				Keys: []string{fp(aliceKey)}, KeyOwner: "alice"},
		},
		{
			name:    "a hostile name",
			connect: func() bool { _, err := f.dial(f.config("\x1b[2Jroot\n", stranger)); return err == nil },
			want: attempts.Entry{Outcome: attempts.UnknownName, LoginName: "\uFFFD[2Jroot\uFFFD", ClientVersion: "SSH-2.0-Go",
				Keys: []string{fp(stranger)}},
		},
		{
			name:    "a disabled person with their key",
			connect: func() bool { _, err := f.dial(f.config("carol", carolKey)); return err == nil },
			want: attempts.Entry{Outcome: attempts.Disabled, PersonID: carol.ID, PersonName: "carol", LoginName: "carol",
				ClientVersion: "SSH-2.0-Go", Keys: []string{fp(carolKey)}},
		},
		{
			name: "no key offered, a person's name",
			connect: func() bool {
				config := f.config("alice")
				config.Auth = nil
				_, err := f.dial(config)
				return err == nil
			},
			want: attempts.Entry{Outcome: attempts.NoKey, PersonID: alice.ID, PersonName: "alice", LoginName: "alice", ClientVersion: "SSH-2.0-Go"},
		},
		{
			name: "a password instead of a key",
			connect: func() bool {
				config := f.config("nobody")
				config.Auth = []ssh.AuthMethod{ssh.Password("hunter2")}
				_, err := f.dial(config)
				return err == nil
			},
			want: attempts.Entry{Outcome: attempts.NoKey, LoginName: "nobody", ClientVersion: "SSH-2.0-Go"},
		},
		{
			name: "the person's key, offered and not signed for",
			connect: func() bool {
				_, err := f.dial(f.withSigner("alice", failingSigner{aliceKey.Signer}))
				return err == nil
			},
			want: attempts.Entry{Outcome: attempts.NotProven, PersonID: alice.ID, PersonName: "alice", LoginName: "alice",
				ClientVersion: "SSH-2.0-Go", Keys: []string{fp(aliceKey)}},
		},
		{
			name:    "the person's key with a signature of something else",
			connect: func() bool { _, err := f.dial(f.withSigner("bob", lyingSigner{bobKey.Signer})); return err == nil },
			want: attempts.Entry{Outcome: attempts.NotProven, PersonID: bob.ID, PersonName: "bob", LoginName: "bob",
				ClientVersion: "SSH-2.0-Go", Keys: []string{fp(bobKey)}},
		},
		{
			name: "not SSH",
			connect: func() bool {
				conn := f.raw("")
				banner(t, conn)
				if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: example\r\n\r\n")); err != nil {
					t.Fatal(err)
				}
				// Lines before an identification line are allowed, so the server waits for one:
				// it is the time limit that ends a client that never sends it.
				if !isOpen(t, conn) {
					t.Fatal("the connection was closed before the time limit")
				}
				f.clock.Advance(GraceTime)
				waitClosed(t, conn)
				return false
			},
			want: attempts.Entry{Outcome: attempts.HandshakeFailed},
		},
		{
			name: "a flood of bytes that is not SSH",
			connect: func() bool {
				conn := f.raw("")
				banner(t, conn)
				// The server may close while this is written; then the write fails, which is fine.
				_, _ = conn.Write(bytes.Repeat([]byte("A"), 4096))
				waitClosed(t, conn)
				return false
			},
			want: attempts.Entry{Outcome: attempts.HandshakeFailed},
		},
		{
			name: "a connection that says nothing and leaves",
			connect: func() bool {
				conn := f.raw("")
				banner(t, conn)
				if err := conn.Close(); err != nil {
					t.Fatal(err)
				}
				return false
			},
			want: attempts.Entry{Outcome: attempts.HandshakeFailed},
		},
	}
	var count int64
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A later time for each case, so that its entry is the newest of the list.
			f.clock.Advance(time.Second)
			if in := tt.connect(); in != tt.wantIn {
				t.Fatalf("the client was let in: %v, want %v", in, tt.wantIn)
			}
			count++
			newest := f.waitTotal(count)[0]
			got := newest
			got.ID, got.Count, got.FirstAt, got.LastAt, got.Address = "", 0, time.Time{}, time.Time{}, ""
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("the entry is\n%+v\nwant\n%+v", got, tt.want)
			}
			if newest.Address != "127.0.0.1" {
				t.Errorf("the address is %q, want 127.0.0.1 without a port", newest.Address)
			}
		})
	}
	if !strings.Contains(f.log.String(), `"outcome":"wrongKey"`) || strings.Contains(f.log.String(), "\x1b") {
		t.Error("the log lacks an attempt, or holds an escape character")
	}
}

func TestEveryRefusalLooksTheSameToTheClient(t *testing.T) {
	f := newFixture(t)
	_, aliceKey := f.person("alice")
	carol, carolKey := f.person("carol")
	if err := f.people.SetEnabled(ctx, carol.ID, false); err != nil {
		t.Fatal(err)
	}
	stranger := sshtest.NewKey(t, sshtest.Ed25519)
	refusals := map[string]*ssh.ClientConfig{
		"an unknown name":              f.config("mallory", stranger),
		"an unknown name, a known key": f.config("mallory", aliceKey),
		"a disabled person":            f.config("carol", carolKey),
		"a wrong key":                  f.config("alice", stranger),
		"somebody else's key":          f.config("alice", carolKey),
	}
	var first string
	for name, config := range refusals {
		_, err := f.dial(config)
		if err == nil {
			t.Fatalf("%s was let in", name)
		}
		if first == "" {
			first = err.Error()
		}
		if err.Error() != first {
			t.Errorf("%s: the client sees %q, another refusal shows %q", name, err.Error(), first)
		}
	}
	if !strings.Contains(first, "unable to authenticate") || strings.Contains(first, "denied") {
		t.Errorf("the refusal is %q", first)
	}
}

func TestAnEditAppliesToTheNextAttempt(t *testing.T) {
	f := newFixture(t)
	alice, key := f.person("alice")
	if _, err := f.dial(f.config("alice", key)); err != nil {
		t.Fatal(err)
	}
	f.waitTotal(1)
	f.clock.Advance(time.Second)
	if err := f.people.SetEnabled(ctx, alice.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.dial(f.config("alice", key)); err == nil {
		t.Error("a disabled person was let in")
	}
	f.waitTotal(2)
	f.clock.Advance(time.Second)
	if err := f.people.SetEnabled(ctx, alice.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.people.Rename(ctx, alice.ID, "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.dial(f.config("alice", key)); err == nil {
		t.Error("the old name was let in")
	}
	f.waitTotal(3)
	f.clock.Advance(time.Second)
	if _, err := f.dial(f.config("ada", key)); err != nil {
		t.Errorf("the new name was refused: %v", err)
	}
	f.waitTotal(4)
	f.clock.Advance(time.Second)
	if err := f.people.Remove(ctx, alice.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.dial(f.config("ada", key)); err == nil {
		t.Error("a removed person was let in")
	}
	var outcomes []string
	for _, e := range f.waitTotal(5) {
		outcomes = append(outcomes, string(e.Outcome)+":"+e.LoginName)
	}
	// Newest first; the two accepted attempts are under two names.
	want := []string{"unknownName:ada", "accepted:ada", "unknownName:alice", "disabled:alice", "accepted:alice"}
	if !reflect.DeepEqual(outcomes, want) {
		t.Errorf("outcomes = %v, want %v", outcomes, want)
	}
}

func TestTooManyKeysEndTheConnection(t *testing.T) {
	f := newFixture(t)
	f.person("alice")
	var keys []sshtest.Key
	for i := 0; i < 30; i++ {
		keys = append(keys, sshtest.NewKey(t, sshtest.Ed25519))
	}
	_, err := f.dial(f.config("alice", keys...))
	if err == nil {
		t.Fatal("let in")
	}
	entries := f.waitTotal(1)
	if entries[0].Outcome != attempts.WrongKey || len(entries[0].Keys) != attempts.MaxKeys {
		t.Errorf("the entry is %+v", entries[0])
	}
	// The server ended it before the client ran out of keys.
	if !strings.Contains(err.Error(), "too many authentication failures") && !strings.Contains(err.Error(), "disconnect") &&
		!strings.Contains(err.Error(), "EOF") && !strings.Contains(err.Error(), "reset") {
		t.Errorf("the client's error is %q, want a disconnect by the server", err)
	}
}

func TestIdentificationAndHostKey(t *testing.T) {
	f := newFixture(t)
	_, key := f.person("alice")
	if got := banner(t, f.raw("")); got != "SSH-2.0-Cave_test\r\n" {
		t.Errorf("the identification line is %q", got)
	}
	// The client inside the test accepts only the fixture's host key: this proves the server has it.
	if _, err := f.dial(f.config("alice", key)); err != nil {
		t.Fatal(err)
	}
	// A new host key is what the next connection sees.
	other, err := hostkey.Create(ctx, storagetest.Isolate(t))
	if err != nil {
		t.Fatal(err)
	}
	f.server.SetHostKey(other.Signer())
	if _, err := f.dial(f.config("alice", key)); err == nil || !strings.Contains(err.Error(), "host key mismatch") {
		t.Errorf("with the old host key expected: %v", err)
	}
	if _, err := sshtest.DialErr(t, f.addr, sshtest.ClientConfig("alice", other.Signer().PublicKey(), key)); err != nil {
		t.Errorf("with the new host key expected: %v", err)
	}
}

func TestLimits(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the test connects from several loopback addresses, which Linux has without setup")
	}
	f := newFixture(t)
	_, key := f.person("alice")

	// Eight connections from one address wait to be authenticated; the ninth is closed at once.
	var waiting []net.Conn
	for i := 0; i < MaxUnauthenticatedPerAddress; i++ {
		conn := f.raw("127.0.0.1")
		if banner(t, conn) == "" {
			t.Fatalf("connection %d from one address was refused", i+1)
		}
		waiting = append(waiting, conn)
	}
	if got := banner(t, f.raw("127.0.0.1")); got != "" {
		t.Errorf("the ninth connection of one address got %q, want to be closed", got)
	}
	entries := f.waitTotal(1)
	if entries[0].Outcome != attempts.Refused || entries[0].Address != "127.0.0.1" || entries[0].LoginName != "" {
		t.Errorf("the entry is %+v", entries[0])
	}
	// A real client from that address is refused too, while the eight wait.
	if _, err := f.dial(f.config("alice", key)); err == nil {
		t.Error("a client was let in past the limit of its address")
	}
	f.waitTotal(2)

	// Other addresses are not held back by it, up to the limit of all: 64.
	for a := 2; a <= MaxUnauthenticated/MaxUnauthenticatedPerAddress; a++ {
		address := "127.0.0." + string(rune('0'+a))
		for i := 0; i < MaxUnauthenticatedPerAddress; i++ {
			conn := f.raw(address)
			if banner(t, conn) == "" {
				t.Fatalf("connection %d from %s was refused", i+1, address)
			}
			waiting = append(waiting, conn)
		}
	}
	f.clock.Advance(time.Second) // a later time, so that the next entry is the newest
	if got := banner(t, f.raw("127.0.0.9")); got != "" {
		t.Errorf("the 65th connection got %q, want to be closed", got)
	}
	entries = f.waitTotal(3)
	if entries[0].Outcome != attempts.Refused || entries[0].Address != "127.0.0.9" {
		t.Errorf("the entry is %+v", entries[0])
	}

	// When the waiting ones leave, there is room again, and authenticated clients do not count.
	for _, conn := range waiting {
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	}
	f.waitTotal(3 + int64(len(waiting)))
	for i := 0; i < 2*MaxUnauthenticatedPerAddress; i++ {
		if _, err := f.dial(f.config("alice", key)); err != nil {
			t.Fatalf("client %d was refused although nobody is waiting: %v", i+1, err)
		}
	}
	if got := banner(t, f.raw("127.0.0.1")); got == "" {
		t.Error("a new connection was refused although only authenticated clients are connected")
	}
}

func TestTheTimeLimitOfTheHandshake(t *testing.T) {
	f := newFixture(t)
	_, key := f.person("alice")
	silent := f.raw("")
	banner(t, silent)

	f.clock.Advance(GraceTime - time.Second)
	if !isOpen(t, silent) {
		t.Fatal("the connection was closed before the time limit")
	}
	f.clock.Advance(time.Second)
	waitClosed(t, silent)
	entries := f.waitTotal(1)
	if entries[0].Outcome != attempts.HandshakeFailed {
		t.Errorf("the entry is %+v", entries[0])
	}

	// An authenticated connection has no time limit.
	client, err := f.dial(f.config("alice", key))
	if err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(10 * GraceTime)
	if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
		t.Errorf("the authenticated connection was closed by the time limit: %v", err)
	}
}

func TestStartAndStop(t *testing.T) {
	f := newStopped(t)
	_, key := f.person("alice")
	var states []string
	var mu sync.Mutex
	f.server.Subscribe(func(change Change) {
		if change != StateChanged {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		states = append(states, f.server.State().String())
	})

	if f.server.State() != Stopped || len(f.server.Addrs()) != 0 {
		t.Fatalf("a new server is %v with the addresses %v", f.server.State(), f.server.Addrs())
	}
	if err := f.server.Stop(ctx); err != nil {
		t.Errorf("Stop of a stopped server: %v", err)
	}
	f.start()
	if f.server.State() != Running || len(f.server.Addrs()) != 1 {
		t.Fatalf("after Start: %v, %v", f.server.State(), f.server.Addrs())
	}
	first := f.addr
	if err := f.server.Start(ctx); err != nil { // nothing to do
		t.Errorf("Start of a running server: %v", err)
	}
	if f.server.Addrs()[0].String() != first {
		t.Error("a second Start listened again")
	}

	client, err := f.dial(f.config("alice", key))
	if err != nil {
		t.Fatal(err)
	}
	waiting := f.raw("")
	banner(t, waiting)
	gone := make(chan struct{})
	go func() {
		// Wait returns when the connection ends; how it ended is not what is tested.
		_ = client.Wait()
		close(gone)
	}()

	if err := f.server.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if f.server.State() != Stopped || len(f.server.Addrs()) != 0 {
		t.Errorf("after Stop: %v, %v", f.server.State(), f.server.Addrs())
	}
	select {
	case <-gone:
	case <-time.After(waitFor):
		t.Error("Stop did not close the authenticated connection")
	}
	waitClosed(t, waiting)
	if _, err := net.DialTimeout("tcp", first, time.Second); err == nil {
		t.Error("the port still accepts connections after Stop")
	}
	if err := f.server.Stop(ctx); err != nil {
		t.Errorf("a second Stop: %v", err)
	}

	// It starts again.
	f.start()
	if _, err := f.dial(f.config("alice", key)); err != nil {
		t.Errorf("after a restart: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"starting", "running", "stopping", "stopped", "starting", "running"}
	if !reflect.DeepEqual(states, want) {
		t.Errorf("states = %v, want %v", states, want)
	}
}

func TestStartErrors(t *testing.T) {
	t.Run("the port is taken", func(t *testing.T) {
		taken, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = taken.Close() }()
		f := newStopped(t)
		f.server.opts.Address = func(settings.Snapshot) string { return taken.Addr().String() }
		var states []string
		f.server.Subscribe(func(Change) { states = append(states, f.server.State().String()) })
		meta := errtest.Route(t, f.server.Start(ctx), "Server.Start.ListenFailed")
		if meta["address"] != taken.Addr().String() {
			t.Errorf("metadata = %v", meta)
		}
		if f.server.State() != Stopped || !reflect.DeepEqual(states, []string{"starting", "stopped"}) {
			t.Errorf("state %v, states %v", f.server.State(), states)
		}
	})
	t.Run("a cancelled context", func(t *testing.T) {
		f := newStopped(t)
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		errtest.Route(t, f.server.Start(cancelled), "Server.Start.ServerStartCancelled")
		if f.server.State() != Stopped {
			t.Errorf("state %v", f.server.State())
		}
	})
	t.Run("the address of the settings", func(t *testing.T) {
		f := newStopped(t)
		f.server.opts.Address = nil
		if err := f.settings.Set(ctx, string(settings.ServerAddress), "127.0.0.1"); err != nil {
			t.Fatal(err)
		}
		// A port that is free now: ask the system for one, and give it back.
		probe, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := probe.Addr().(*net.TCPAddr).Port
		if err := probe.Close(); err != nil {
			t.Fatal(err)
		}
		if err := f.settings.Set(ctx, string(settings.ServerPort), port); err != nil {
			t.Fatal(err)
		}
		if err := f.server.Start(ctx); err != nil {
			t.Fatal(err)
		}
		if got := f.server.Addrs()[0].(*net.TCPAddr); got.Port != port || !got.IP.IsLoopback() {
			t.Errorf("listening on %v, want 127.0.0.1:%d", got, port)
		}
	})
}

// stubbornListener is a listener whose Accept fails a number of times, and which can refuse to
// end when it is closed.
type stubbornListener struct {
	net.Listener
	mu       sync.Mutex
	failures int
	// stuck, when not nil, keeps Accept from returning after Close until it is closed.
	stuck chan struct{}
}

func (l *stubbornListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if l.failures > 0 {
		l.failures--
		l.mu.Unlock()
		return nil, io.ErrNoProgress
	}
	l.mu.Unlock()
	conn, err := l.Listener.Accept()
	if err != nil && l.stuck != nil {
		<-l.stuck
	}
	return conn, err
}

func TestAnAcceptThatFailsIsLoggedAndTheServerGoesOn(t *testing.T) {
	f := newStopped(t)
	_, key := f.person("alice")
	f.server.listen = func(ctx context.Context, address string) (net.Listener, error) {
		var config net.ListenConfig
		inner, err := config.Listen(ctx, "tcp", address)
		if err != nil {
			return nil, err
		}
		return &stubbornListener{Listener: inner, failures: 2}, nil
	}
	f.start()
	if _, err := f.dial(f.config("alice", key)); err != nil {
		t.Fatalf("after the failed accepts: %v", err)
	}
	var warnings int
	for _, line := range strings.Split(strings.TrimSpace(f.log.String()), "\n") {
		var entry struct {
			Level string           `json:"level"`
			Err   errors.ErrorJson `json:"err"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("a log line is not JSON: %v\n%s", err, line)
		}
		if entry.Level == "WARN" && entry.Err.Tag == TagAcceptFailed && entry.Err.HasRoute("Server.accept.ServerAcceptFailed") {
			warnings++
		}
	}
	if warnings != 2 {
		t.Errorf("%d warnings of a failed accept, want 2:\n%s", warnings, f.log.String())
	}
}

func TestStopGivesUpWhenTheContextEnds(t *testing.T) {
	f := newStopped(t)
	stuck := make(chan struct{})
	f.server.listen = func(ctx context.Context, address string) (net.Listener, error) {
		var config net.ListenConfig
		inner, err := config.Listen(ctx, "tcp", address)
		if err != nil {
			return nil, err
		}
		return &stubbornListener{Listener: inner, stuck: stuck}, nil
	}
	f.start()
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	err := f.server.Stop(short)
	errtest.Route(t, err, "Server.Stop.ServerStopTimedOut")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the cause is %v", errors.Unwrap(err))
	}
	if f.server.State() != Stopped {
		t.Errorf("state %v", f.server.State())
	}
	close(stuck)
}

func TestOpenSSHIsRefusedAndAccepted(t *testing.T) {
	f := newFixture(t)
	_, key := f.person("alice")
	stranger := sshtest.NewKey(t, sshtest.Ed25519)
	o := sshtest.NewOpenSSH(t, f.addr, f.hostKey.Signer().PublicKey())

	res := o.SSH("", "alice", stranger, nil, "true")
	if res.ExitCode != 255 || !strings.Contains(res.Stderr, "Permission denied (publickey)") {
		t.Errorf("a wrong key: %+v", res)
	}
	res = o.SSH("", "mallory", key, nil, "true")
	if res.ExitCode != 255 || !strings.Contains(res.Stderr, "Permission denied (publickey)") {
		t.Errorf("an unknown name: %+v", res)
	}
	// The right key is let in; there is nothing to run yet, so ssh ends without having been denied.
	res = o.SSH("", "alice", key, nil, "true")
	if strings.Contains(res.Stderr, "Permission denied") || res.ExitCode == 0 {
		t.Errorf("the right key: %+v", res)
	}
	var outcomes []string
	entries := f.waitTotal(3)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Outcome < entries[j].Outcome })
	for _, e := range entries {
		outcomes = append(outcomes, string(e.Outcome)+":"+e.LoginName)
		if !strings.HasPrefix(e.ClientVersion, "SSH-2.0-OpenSSH_") {
			t.Errorf("the client version is %q", e.ClientVersion)
		}
	}
	if want := []string{"accepted:alice", "unknownName:mallory", "wrongKey:alice"}; !reflect.DeepEqual(outcomes, want) {
		t.Errorf("outcomes = %v, want %v", outcomes, want)
	}
}

func TestRelistenKeepsTheConnections(t *testing.T) {
	f := newStopped(t)
	// The address follows a variable of the test, as it follows the settings in the app.
	address := "127.0.0.1:0"
	f.server.opts.Address = func(settings.Snapshot) string { return address }
	// On a server that is not running, nothing happens.
	if err := f.server.Relisten(ctx); err != nil || f.server.State() != Stopped {
		t.Fatalf("Relisten on a stopped server: %v, state %v", err, f.server.State())
	}
	f.start()
	_, key := f.person("alice")
	client := f.connect("alice", key)
	oldAddr := f.addr
	var changes atomic.Int64
	f.server.Subscribe(func(change Change) {
		if change == StateChanged {
			changes.Add(1)
		}
	})

	if err := f.server.Relisten(ctx); err != nil {
		t.Fatal(err)
	}
	newAddr := f.server.Addrs()[0].String()
	if newAddr == oldAddr || f.server.State() != Running {
		t.Fatalf("after Relisten the server listens on %s (before: %s), state %v", newAddr, oldAddr, f.server.State())
	}
	if changes.Load() == 0 {
		t.Error("the listeners were not told")
	}
	// The connection that was established is still there.
	if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
		t.Errorf("the established connection was closed: %v", err)
	}
	if conns := f.server.Connections(); len(conns) != 1 {
		t.Errorf("the registry has %d connections", len(conns))
	}
	// The old address takes nobody, the new one does.
	if conn, err := net.DialTimeout("tcp", oldAddr, time.Second); err == nil {
		_ = conn.Close()
		t.Error("the old address still takes connections")
	}
	f.addr = newAddr
	second := f.connect("alice", key)
	if _, _, err := second.SendRequest("keepalive@openssh.com", true, nil); err != nil {
		t.Errorf("the new address: %v", err)
	}
	// Stop ends them all, old and new.
	stopCtx, cancel := context.WithTimeout(ctx, waitFor)
	defer cancel()
	if err := f.server.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	waitGone(t, client)
	waitGone(t, second)
}

func TestRelistenOnAnAddressThatIsTakenStopsTheServer(t *testing.T) {
	f := newStopped(t)
	address := "127.0.0.1:0"
	f.server.opts.Address = func(settings.Snapshot) string { return address }
	f.start()
	_, key := f.person("alice")
	client := f.connect("alice", key)

	// Somebody else has the address the settings now name.
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }()
	address = taken.Addr().String()

	err = f.server.Relisten(ctx)
	meta := errtest.Route(t, err, "Server.Relisten.ServerRelistenFailed")
	if meta["address"] != address || !errors.Is(err, syscall.EADDRINUSE) {
		t.Errorf("metadata = %v, cause %v", meta, errors.Unwrap(err))
	}
	// The server is stopped, as when it cannot start: no listener, no connection.
	if f.server.State() != Stopped || len(f.server.Addrs()) != 0 {
		t.Errorf("state %v, addresses %v", f.server.State(), f.server.Addrs())
	}
	waitGone(t, client)
	f.waitConnections(0)
	// It starts again when the address is free.
	address = "127.0.0.1:0"
	if err := f.server.Start(ctx); err != nil || f.server.State() != Running {
		t.Errorf("Start afterwards: %v, state %v", err, f.server.State())
	}
}
