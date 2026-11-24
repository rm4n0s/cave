package apptest

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/hostkey"
	"github.com/rm4n0s/cave/internal/core/session"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/cave/internal/core/storage/storagetest"
	"golang.org/x/crypto/ssh"
)

// waitFor is how long a test waits for the other side of a connection.
const waitFor = 5 * time.Second

// Real is a real application in a temporary folder, with its server on a loopback port that the
// system chose.
type Real struct {
	// App is the application. It is closed when the test ends.
	App *app.App
	// Clock is the application's fake clock.
	Clock *clock.Fake
	// Dirs are its folders, all under the test's temporary folder.
	Dirs storage.Dirs
	// Port is the port the server listens on.
	Port int

	t   testing.TB
	log *syncBuffer
}

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

// Open opens a real application for a test. Before it opens, it saves the settings every test
// needs: the server listens only on 127.0.0.1, on a port the system gave out, sessions run the
// shell of the platform (shell_linux.go, shell_windows.go), and keepalive is off (one big move of
// the fake clock would otherwise ask a client several times at once and close it). The settings
// of more are saved after them. The application's folders, home and runtime folder are all under
// the test's temporary folder; it is closed when the test ends.
func Open(t testing.TB, more map[string]any) *Real {
	t.Helper()
	dirs := storagetest.Isolate(t)
	port := freePort(t)
	store, err := settings.Open(context.Background(), dirs)
	if err != nil {
		t.Fatal(err)
	}
	seed := map[string]any{
		string(settings.ServerAddress): "127.0.0.1", string(settings.ServerPort): port,
		string(settings.SessionShell): shell, string(settings.ServerKeepAliveSeconds): 0,
	}
	for _, values := range []map[string]any{seed, more} {
		for key, value := range values {
			if err := store.Set(context.Background(), key, value); err != nil {
				t.Fatalf("%s: %v", key, err)
			}
		}
	}
	home := filepath.Join(filepath.Dir(dirs.Config), "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	r := &Real{Clock: clock.NewFake(Now), Dirs: dirs, Port: port, t: t, log: &syncBuffer{}}
	r.App, err = app.Open(context.Background(), app.Options{
		Program: "apptest", Dirs: dirs, Clock: r.Clock, Logger: slog.New(slog.NewJSONHandler(r.log, nil)), Version: "test",
		IsRoot:    func() bool { return false },
		Addresses: func() ([]string, error) { return []string{"192.0.2.10", "2001:db8::10", "127.0.0.1"}, nil },
		User:      session.User{Name: "tester", Home: home},
		Environ:   environ(home),
	})
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := r.App.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return r
}

// freePort returns a port of 127.0.0.1 that the system gave out a moment ago and that is free
// again: a test never chooses a port itself.
func freePort(t testing.TB) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

// Addr is the address the server listens on, "127.0.0.1:<port>".
func (r *Real) Addr() string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(r.Port))
}

// HostKey reads the server's host public key from its file, so that it is the current one after
// a new host key was made.
func (r *Real) HostKey() ssh.PublicKey {
	r.t.Helper()
	data, err := os.ReadFile(filepath.Join(r.Dirs.Config, hostkey.PublicFileName))
	if err != nil {
		r.t.Fatal(err)
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		r.t.Fatal(err)
	}
	return key
}

// Person adds a person with a new key through the commands, and returns the key.
func (r *Real) Person(name string) sshtest.Key {
	r.t.Helper()
	ctx := context.Background()
	p, err := r.App.AddPerson(ctx, name)
	if err != nil {
		r.t.Fatal(err)
	}
	key := sshtest.NewKey(r.t, sshtest.Ed25519)
	if _, err := r.App.AddKeys(ctx, p.ID, key.AuthorizedKey); err != nil {
		r.t.Fatal(err)
	}
	return key
}

// Connect connects a client as the person with the key and waits until the application shows
// the connection. The client is closed when the test ends.
func (r *Real) Connect(name string, key sshtest.Key) *ssh.Client {
	r.t.Helper()
	before := len(r.App.Connections())
	client := sshtest.Dial(r.t, r.Addr(), name, key, r.HostKey())
	r.Eventually("the application does not show the connection", func() bool {
		return len(r.App.Connections()) == before+1
	})
	return client
}

// Refuse makes an attempt that the server must refuse (a name nobody has, or a key that is not
// the person's) and returns when it has been refused; it leaves an entry in the attempts list.
func (r *Real) Refuse(name string, key sshtest.Key) {
	r.t.Helper()
	if _, err := sshtest.DialErr(r.t, r.Addr(), sshtest.ClientConfig(name, r.HostKey(), key)); err == nil {
		r.t.Fatalf("the server let %q in, and should have refused", name)
	}
}

// Eventually waits for up to five seconds of real time until cond is true, and fails the test
// with what if it never is.
func (r *Real) Eventually(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(waitFor)
	for !cond() {
		if time.Now().After(deadline) {
			r.t.Fatalf("after %v: %s", waitFor, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
