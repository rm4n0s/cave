// Package servertest starts Cave's SSH server for the tests of the packages that serve its
// channels (sessions, SFTP, forwarding): on a loopback port the system chooses, with people,
// settings, an attempts list and a host key in the test's own folders (docs/SPEC.md §14). It is
// used only by tests.
package servertest

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"encoding/json"
	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/history"
	"github.com/rm4n0s/cave/internal/core/hostkey"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/cave/internal/core/storage/storagetest"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
	"strings"
)

// WaitFor is how long a test waits for the server's side of something a client did.
const WaitFor = 10 * time.Second

// StartTime is the time the fixture's clock shows at first.
var StartTime = time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)

// Log is the server's log, which its goroutines write and the test reads.
type Log struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends to the log.
func (l *Log) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// String returns the log so far: JSON lines.
func (l *Log) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// Fixture is a server with everything it needs, in the test's own folders.
type Fixture struct {
	t testing.TB
	// Server is the server; a test registers its handlers on it before Start.
	Server *server.Server
	// People, Attempts, History and Settings are the server's stores.
	People   *people.Store
	Attempts *attempts.List
	History  *history.Store
	Settings *settings.Store
	// Clock is the fake clock everything runs on.
	Clock *clock.Fake
	// HostKey is the server's host key.
	HostKey *hostkey.HostKey
	// Dirs are Cave's folders for this test; Home is the isolated home folder.
	Dirs storage.Dirs
	Home string
	// Log is the server's log, and Logger writes to it.
	Log    *Log
	Logger *slog.Logger
	// Addr is the address the server listens on, after Start.
	Addr string
}

// New makes a server that is not started, and stops it when the test ends.
func New(t testing.TB) *Fixture {
	t.Helper()
	dirs := storagetest.Isolate(t)
	f := &Fixture{t: t, Clock: clock.NewFake(StartTime), Dirs: dirs, Log: &Log{}}
	f.Logger = slog.New(slog.NewJSONHandler(f.Log, nil))
	ctx := context.Background()
	var err error
	if f.People, err = people.Open(ctx, dirs, f.Clock); err != nil {
		t.Fatalf("servertest: %v", err)
	}
	if f.Attempts, err = attempts.Open(ctx, attempts.Options{Dirs: dirs, Clock: f.Clock, Logger: f.Logger}); err != nil {
		t.Fatalf("servertest: %v", err)
	}
	if f.Settings, err = settings.Open(ctx, dirs); err != nil {
		t.Fatalf("servertest: %v", err)
	}
	if f.History, err = history.Open(ctx, history.Options{Dirs: dirs, Clock: f.Clock, Logger: f.Logger}); err != nil {
		t.Fatalf("servertest: %v", err)
	}
	t.Cleanup(func() {
		if err := f.History.Close(); err != nil {
			t.Errorf("servertest: history: %v", err)
		}
	})
	if f.HostKey, err = hostkey.LoadOrCreate(ctx, dirs); err != nil {
		t.Fatalf("servertest: %v", err)
	}
	// The keepalive is off unless a test turns it on: a test that moves the fake clock far in
	// one step would otherwise ask a client several times in no time at all, and close it.
	if err := f.Settings.Set(ctx, string(settings.ServerKeepAliveSeconds), 0); err != nil {
		t.Fatalf("servertest: %v", err)
	}
	f.Home = homeDir(t)
	f.Server = server.New(server.Options{
		People: f.People, Attempts: f.Attempts, History: f.History, HostKey: f.HostKey.Signer(), Settings: f.Settings, Clock: f.Clock, Logger: f.Logger,
		Version: "test", Address: func(settings.Snapshot) string { return "127.0.0.1:0" },
	})
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(ctx, WaitFor)
		defer cancel()
		if err := f.Server.Stop(stopCtx); err != nil {
			t.Errorf("servertest: Stop: %v", err)
		}
	})
	return f
}

// Start starts the server and notes its address.
func (f *Fixture) Start() {
	f.t.Helper()
	if err := f.Server.Start(context.Background()); err != nil {
		f.t.Fatalf("servertest: %v", err)
	}
	f.Addr = f.Server.Addrs()[0].String()
}

// Set changes a setting.
func (f *Fixture) Set(key string, value any) {
	f.t.Helper()
	if err := f.Settings.Set(context.Background(), key, value); err != nil {
		f.t.Fatalf("servertest: %v", err)
	}
}

// Person adds a person with a new key.
func (f *Fixture) Person(name string) (people.Person, sshtest.Key) {
	f.t.Helper()
	ctx := context.Background()
	p, err := f.People.Add(ctx, name)
	if err != nil {
		f.t.Fatalf("servertest: %v", err)
	}
	key := sshtest.NewKey(f.t, sshtest.Ed25519)
	if _, err := f.People.AddKeys(ctx, p.ID, key.AuthorizedKey); err != nil {
		f.t.Fatalf("servertest: %v", err)
	}
	return p, key
}

// Config is the configuration of a client inside the test that logs in as user with the keys.
func (f *Fixture) Config(user string, keys ...sshtest.Key) *ssh.ClientConfig {
	return sshtest.ClientConfig(user, f.HostKey.Signer().PublicKey(), keys...)
}

// Connect logs a person in with a key, failing the test when the server refuses, and waits
// until the connection is in the registry: the client knows that it is in a moment before the
// server has noted it.
func (f *Fixture) Connect(name string, key sshtest.Key) *ssh.Client {
	f.t.Helper()
	before := len(f.Server.Connections())
	client, err := sshtest.DialErr(f.t, f.Addr, f.Config(name, key))
	if err != nil {
		f.t.Fatalf("servertest: %s was refused: %v", name, err)
	}
	f.WaitConnections(before + 1)
	return client
}

// WaitConnections waits until the registry has n connections, and returns them.
func (f *Fixture) WaitConnections(n int) []server.Connection {
	f.t.Helper()
	deadline := time.Now().Add(WaitFor)
	for {
		conns := f.Server.Connections()
		if len(conns) == n {
			return conns
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("servertest: the registry has %d connections after %v, want %d", len(conns), WaitFor, n)
		}
		time.Sleep(time.Millisecond)
	}
}

// OpenSSH prepares the real OpenSSH programs for this server; it skips the test when they are
// not installed.
func (f *Fixture) OpenSSH() *sshtest.OpenSSH {
	f.t.Helper()
	return sshtest.NewOpenSSH(f.t, f.Addr, f.HostKey.Signer().PublicKey())
}

// LoggedError returns the error of the last line of the log that has the message, as it was
// logged: its Tag, route and metadata can be asserted like those of the error itself. The test
// stops when the log has no such line.
func (f *Fixture) LoggedError(message string) errors.ErrorJson {
	f.t.Helper()
	lines := strings.Split(strings.TrimSpace(f.Log.String()), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		var entry struct {
			Msg string           `json:"msg"`
			Err errors.ErrorJson `json:"err"`
		}
		if err := json.Unmarshal([]byte(lines[i]), &entry); err == nil && entry.Msg == message {
			return entry.Err
		}
	}
	f.t.Fatalf("servertest: the log has no line %q:\n%s", message, f.Log.String())
	return errors.ErrorJson{}
}
