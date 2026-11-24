package app

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/session"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/cave/internal/core/storage/storagetest"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

var (
	ctx = context.Background()
	// start is the time the fake clock shows at first.
	start = time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
)

// waitFor is how long a test waits for the other side of a connection.
const waitFor = 10 * time.Second

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

// fixture is what a test of the application has: isolated folders, a fake clock, a log, and the
// options to open the application with them.
type fixture struct {
	t     *testing.T
	dirs  storage.Dirs
	clock *clock.Fake
	log   *syncBuffer
	// port is a port of 127.0.0.1 that the system gave out for this test.
	port int
	app  *App
}

// freePort returns a port of 127.0.0.1 that the system gave out a moment ago and that is free
// again: a test never chooses a port itself.
func freePort(t *testing.T) int {
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

// newFixture isolates the folders and saves the settings every test needs before the
// application is opened: the server listens only on 127.0.0.1, on the test's port, and sessions
// would run /bin/sh. More settings are saved from the map.
func newFixture(t *testing.T, more map[string]any) *fixture {
	t.Helper()
	f := &fixture{t: t, dirs: storagetest.Isolate(t), clock: clock.NewFake(start), log: &syncBuffer{}, port: freePort(t)}
	f.save(map[string]any{
		string(settings.ServerAddress): "127.0.0.1", string(settings.ServerPort): f.port, string(settings.SessionShell): "/bin/sh",
		// Off, so that a test may move the fake clock far in one step (see servertest).
		string(settings.ServerKeepAliveSeconds): 0,
	})
	f.save(more)
	return f
}

// save writes settings to the file, as an earlier run of Cave would have.
func (f *fixture) save(values map[string]any) {
	f.t.Helper()
	store, err := settings.Open(ctx, f.dirs)
	if err != nil {
		f.t.Fatal(err)
	}
	for key, value := range values {
		if err := store.Set(ctx, key, value); err != nil {
			f.t.Fatalf("%s: %v", key, err)
		}
	}
}

// options are the options of the tests: nothing of the real user or computer.
func (f *fixture) options() Options {
	home := filepath.Join(filepath.Dir(f.dirs.Config), "home")
	return Options{
		Program: "tcave", Dirs: f.dirs, Clock: f.clock, Logger: slog.New(slog.NewJSONHandler(f.log, nil)), Version: "test",
		IsRoot:    func() bool { return false },
		Addresses: func() ([]string, error) { return []string{"192.0.2.10", "2001:db8::10", "127.0.0.1"}, nil },
		User:      session.User{Name: "tester", Home: home},
		Environ:   []string{"PATH=/usr/bin:/bin", "HOME=" + home},
	}
}

// open opens the application with the options, changed by the function when there is one, and
// closes it when the test ends.
func (f *fixture) open(change func(*Options)) *App {
	f.t.Helper()
	opts := f.options()
	if change != nil {
		change(&opts)
	}
	a, err := Open(ctx, opts)
	if err != nil {
		f.t.Fatalf("Open: %v", err)
	}
	f.app = a
	f.t.Cleanup(func() { f.close(a) })
	return a
}

// close closes an application.
func (f *fixture) close(a *App) {
	f.t.Helper()
	closeCtx, cancel := context.WithTimeout(ctx, waitFor)
	defer cancel()
	if err := a.Close(closeCtx); err != nil {
		f.t.Errorf("Close: %v", err)
	}
}

// person adds a person with a new key, through the commands.
func (f *fixture) person(a *App, name string) sshtest.Key {
	f.t.Helper()
	p, err := a.AddPerson(ctx, name)
	if err != nil {
		f.t.Fatal(err)
	}
	key := sshtest.NewKey(f.t, sshtest.Ed25519)
	if _, err := a.AddKeys(ctx, p.ID, key.AuthorizedKey); err != nil {
		f.t.Fatal(err)
	}
	return key
}

// connect connects a person to the port, and waits until the application shows the connection.
func (f *fixture) connect(a *App, name string, key sshtest.Key, port int) *ssh.Client {
	f.t.Helper()
	before := len(a.Connections())
	client := sshtest.Dial(f.t, "127.0.0.1:"+strconv.Itoa(port), name, key, a.hostKey.Signer().PublicKey())
	deadline := time.Now().Add(waitFor)
	for len(a.Connections()) != before+1 {
		if time.Now().After(deadline) {
			f.t.Fatalf("the application shows %d connections, want %d", len(a.Connections()), before+1)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return client
}

// alive says whether a client's connection still answers.
func alive(client *ssh.Client) bool {
	_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
	return err == nil
}

// waitGone waits until a client's connection has ended.
func waitGone(t *testing.T, client *ssh.Client) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		_ = client.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(waitFor):
		t.Fatal("the connection is still open")
	}
}

// taken says whether something listens on the port of 127.0.0.1.
func taken(port int) bool {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func TestOpenOnEmptyFolders(t *testing.T) {
	// Nothing was saved before: the settings are the defaults, which listen on every address.
	// A test never does that, so the server is not started here; the next test starts it.
	dirs := storagetest.Isolate(t)
	f := &fixture{t: t, dirs: dirs, clock: clock.NewFake(start), log: &syncBuffer{}}
	a := f.open(func(o *Options) {
		o.NoStart = true
		o.Logger = nil // the log file of the state folder
	})

	// The files of a first run, private to the user.
	for name, want := range map[string]os.FileMode{
		dirs.Config: os.ModeDir | 0o700,
		dirs.State:  os.ModeDir | 0o700,
		filepath.Join(dirs.Config, "ssh_host_ed25519_key"):     0o600,
		filepath.Join(dirs.Config, "ssh_host_ed25519_key.pub"): 0o600,
		filepath.Join(dirs.State, "lock"):                      0o600,
		filepath.Join(dirs.State, LogFile):                     0o600,
	} {
		info, err := os.Stat(name)
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		if modesMean && info.Mode() != want {
			t.Errorf("%s has the mode %v, want %v", name, info.Mode(), want)
		}
	}
	// The snapshots of an application nobody has used.
	srv := a.Server()
	if srv.State != server.Stopped || len(srv.Listening) != 0 || srv.Err != nil || srv.Port != 2222 {
		t.Errorf("the server is %+v", srv)
	}
	if !strings.HasPrefix(srv.HostKeyFingerprint, "SHA256:") {
		t.Errorf("the host key's fingerprint is %q", srv.HostKeyFingerprint)
	}
	if len(a.People()) != 0 || len(a.Connections()) != 0 {
		t.Errorf("people %v, connections %v", a.People(), a.Connections())
	}
	if got := a.Attempts(); len(got.Entries) != 0 || !got.LastClearedAt.IsZero() || !got.NextClearAt.IsZero() {
		t.Errorf("attempts = %+v", got)
	}
	if got := a.Settings(); !got.Bool(settings.SFTPEnabled) || got.Int(settings.ServerPort) != 2222 {
		t.Error("the settings are not the defaults")
	}
	f.close(a)
	log, err := os.ReadFile(filepath.Join(dirs.State, LogFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"msg":"cave started"`, `"program":"tcave"`, `"version":"test"`, `"msg":"cave stopped"`} {
		if !strings.Contains(string(log), want) {
			t.Errorf("the log has no %s:\n%s", want, log)
		}
	}
}

func TestOpenStartsTheServer(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(nil)
	srv := a.Server()
	want := "127.0.0.1:" + strconv.Itoa(f.port)
	if srv.State != server.Running || len(srv.Listening) != 1 || srv.Listening[0] != want || srv.Port != f.port || srv.Err != nil {
		t.Fatalf("the server is %+v, want it running on %s", srv, want)
	}
	// It listens on one address, and that is the one to tell people.
	if len(srv.Addresses) != 1 || srv.Addresses[0] != "127.0.0.1" {
		t.Errorf("the addresses are %v", srv.Addresses)
	}
	if got, want := srv.Command("alice", srv.Addresses[0]), "ssh -p "+strconv.Itoa(f.port)+" alice@127.0.0.1"; got != want {
		t.Errorf("the command is %q, want %q", got, want)
	}

	// Somebody connects, and the snapshots show it.
	key := f.person(a, "alice")
	client := f.connect(a, "alice", key, f.port)
	if conns := a.Connections(); conns[0].PersonName != "alice" {
		t.Errorf("connections = %+v", conns)
	}
	if entries := a.Attempts().Entries; len(entries) != 1 || entries[0].Outcome != attempts.Accepted {
		t.Errorf("attempts = %+v", entries)
	}
	if people := a.People(); len(people) != 1 || people[0].Name != "alice" || len(people[0].Keys) != 1 {
		t.Errorf("people = %+v", people)
	}

	// Stop, and start again.
	if err := a.StopServer(ctx); err != nil {
		t.Fatal(err)
	}
	waitGone(t, client)
	if srv := a.Server(); srv.State != server.Stopped || len(srv.Listening) != 0 || taken(f.port) {
		t.Errorf("after StopServer the server is %+v", srv)
	}
	if err := a.StopServer(ctx); err != nil {
		t.Errorf("a second StopServer: %v", err)
	}
	if err := a.StartServer(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.StartServer(ctx); err != nil {
		t.Errorf("a second StartServer: %v", err)
	}
	if srv := a.Server(); srv.State != server.Running || !taken(f.port) {
		t.Errorf("after StartServer the server is %+v", srv)
	}
}

func TestNoStart(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(func(o *Options) { o.NoStart = true })
	if srv := a.Server(); srv.State != server.Stopped || taken(f.port) || srv.Port != f.port {
		t.Fatalf("with --no-start the server is %+v", srv)
	}
	if err := a.StartServer(ctx); err != nil {
		t.Fatal(err)
	}
	if srv := a.Server(); srv.State != server.Running {
		t.Errorf("after StartServer the server is %+v", srv)
	}
}

func TestStartOnLaunchTurnedOff(t *testing.T) {
	f := newFixture(t, map[string]any{string(settings.ServerStartOnLaunch): false})
	a := f.open(nil)
	if srv := a.Server(); srv.State != server.Stopped || taken(f.port) {
		t.Fatalf("with server.startOnLaunch off the server is %+v", srv)
	}
}

func TestASecondOpenIsAlreadyRunning(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(nil)
	opts := f.options()
	opts.Program = "cave"
	second, err := Open(ctx, opts)
	if second != nil {
		t.Fatal("a second application was opened on the same folders")
	}
	meta := errtest.Route(t, err, "Open->App.open", storage.TagAlreadyRunning)
	if meta["program"] != "tcave" {
		t.Errorf("the error does not say who holds the lock: %v", meta)
	}
	// The first one is as it was.
	if srv := a.Server(); srv.State != server.Running {
		t.Errorf("the first application's server is %+v", srv)
	}
	// After it is closed, the folders are free.
	f.close(a)
	third, err := Open(ctx, opts)
	if err != nil {
		t.Fatalf("Open after Close: %v", err)
	}
	f.close(third)
}

func TestAPortInUseLeavesTheApplicationOpen(t *testing.T) {
	f := newFixture(t, nil)
	other, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(f.port))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()

	a := f.open(nil)
	srv := a.Server()
	if srv.State != server.Stopped || len(srv.Listening) != 0 {
		t.Fatalf("the server is %+v, want it stopped", srv)
	}
	meta := errtest.Route(t, srv.Err, "Server.Start.ListenFailed")
	if meta["address"] != "127.0.0.1:"+strconv.Itoa(f.port) || !errors.Is(srv.Err, errnoAddrInUse) {
		t.Errorf("the error is %v with %v", srv.Err, meta)
	}
	if log := f.log.String(); !strings.Contains(log, `"msg":"the server did not start"`) || !strings.Contains(log, "ListenFailed") {
		t.Errorf("the log does not have the reason:\n%s", log)
	}
	// The application works: a person can be added, a setting changed.
	f.person(a, "alice")
	if err := a.Set(ctx, string(settings.SFTPEnabled), false); err != nil {
		t.Errorf("Set: %v", err)
	}
	// A second try fails the same way, and the error is returned too.
	errtest.Route(t, a.StartServer(ctx), "Server.Start.ListenFailed")

	// The port is freed: the server starts, and the error is gone.
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.StartServer(ctx); err != nil {
		t.Fatal(err)
	}
	if srv := a.Server(); srv.State != server.Running || srv.Err != nil {
		t.Errorf("the server is %+v", srv)
	}
}

func TestChangingThePortKeepsTheConnections(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(nil)
	key := f.person(a, "alice")
	client := f.connect(a, "alice", key, f.port)

	// The same port again changes nothing.
	if err := a.Set(ctx, string(settings.ServerPort), f.port); err != nil {
		t.Fatal(err)
	}
	newPort := freePort(t)
	if err := a.Set(ctx, string(settings.ServerPort), newPort); err != nil {
		t.Fatal(err)
	}
	srv := a.Server()
	if srv.State != server.Running || srv.Port != newPort || srv.Listening[0] != "127.0.0.1:"+strconv.Itoa(newPort) {
		t.Fatalf("after the change the server is %+v", srv)
	}
	if !alive(client) {
		t.Error("the connection that was established was closed")
	}
	if taken(f.port) {
		t.Error("the old port still takes connections")
	}
	second := f.connect(a, "alice", key, newPort)
	if len(a.Connections()) != 2 || !alive(second) {
		t.Errorf("connections = %+v", a.Connections())
	}

	// A port somebody else has: the setting is saved, the server stops, and the reason is both
	// returned and kept.
	other, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	otherPort := other.Addr().(*net.TCPAddr).Port
	err = a.Set(ctx, string(settings.ServerPort), otherPort)
	errtest.Route(t, err, "App.Set->App.relisten->Server.Relisten.ServerRelistenFailed")
	srv = a.Server()
	if srv.State != server.Stopped || srv.Err == nil || srv.Port != otherPort || a.Settings().Int(settings.ServerPort) != otherPort {
		t.Errorf("after a port that is taken the server is %+v", srv)
	}
	if appErr, ok := errors.FromError(srv.Err); !ok || appErr.Tag != server.TagRelistenFailed {
		t.Errorf("the kept error is %v", srv.Err)
	}
	waitGone(t, client)
	waitGone(t, second)

	// A change while the server is stopped only saves the setting.
	if err := a.Set(ctx, string(settings.ServerPort), newPort); err != nil {
		t.Fatal(err)
	}
	if srv := a.Server(); srv.State != server.Stopped || srv.Port != newPort {
		t.Errorf("the server is %+v", srv)
	}
	if err := a.StartServer(ctx); err != nil {
		t.Fatal(err)
	}
	if srv := a.Server(); srv.State != server.Running || srv.Err != nil || !taken(newPort) {
		t.Errorf("the server is %+v", srv)
	}
}

func TestChangingTheAddressMovesTheListener(t *testing.T) {
	probe, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("this computer has no ::1: %v", err)
	}
	_ = probe.Close()
	f := newFixture(t, nil)
	a := f.open(nil)
	key := f.person(a, "alice")
	client := f.connect(a, "alice", key, f.port)
	if err := a.Set(ctx, string(settings.ServerAddress), "::1"); err != nil {
		t.Fatal(err)
	}
	srv := a.Server()
	if srv.State != server.Running || srv.Listening[0] != "[::1]:"+strconv.Itoa(f.port) || len(srv.Addresses) != 1 || srv.Addresses[0] != "::1" {
		t.Fatalf("after the change the server is %+v", srv)
	}
	if got, want := srv.Command("alice", "::1"), "ssh -p "+strconv.Itoa(f.port)+" alice@::1"; got != want {
		t.Errorf("the command is %q, want %q", got, want)
	}
	if !alive(client) || taken(f.port) {
		t.Error("the connection was closed, or 127.0.0.1 still takes connections")
	}
}

func TestThePortOfTheCommandLine(t *testing.T) {
	f := newFixture(t, nil)
	flagPort := freePort(t)
	a := f.open(func(o *Options) { o.Port = flagPort })
	// For this run the flag's port is used; the setting is as it was.
	if srv := a.Server(); srv.Port != flagPort || !taken(flagPort) || taken(f.port) || a.Settings().Int(settings.ServerPort) != f.port {
		t.Fatalf("with --port the server is %+v", srv)
	}
	// Another setting does not end it.
	if err := a.Set(ctx, string(settings.SFTPEnabled), false); err != nil {
		t.Fatal(err)
	}
	if srv := a.Server(); srv.Port != flagPort {
		t.Errorf("the server is %+v", srv)
	}
	// The user chooses a port: that one is used from now on.
	chosen := freePort(t)
	if err := a.Set(ctx, string(settings.ServerPort), chosen); err != nil {
		t.Fatal(err)
	}
	if srv := a.Server(); srv.Port != chosen || !taken(chosen) || taken(flagPort) {
		t.Errorf("after the user chose a port the server is %+v", srv)
	}
}

func TestTheAddressesOfThisComputer(t *testing.T) {
	// A server that listens on every address is not started by a test; its snapshot still says
	// where it would be reached.
	f := newFixture(t, map[string]any{string(settings.ServerAddress): "", string(settings.ServerPort): 22})
	a := f.open(func(o *Options) { o.NoStart = true })
	srv := a.Server()
	if want := []string{"192.0.2.10", "2001:db8::10", "127.0.0.1"}; strings.Join(srv.Addresses, " ") != strings.Join(want, " ") {
		t.Errorf("the addresses are %v, want %v", srv.Addresses, want)
	}
	// The port every client uses by itself is not written.
	if got := srv.Command("ada", "192.0.2.10"); got != "ssh ada@192.0.2.10" {
		t.Errorf("the command is %q", got)
	}
	f.close(a)

	// A system that does not tell its addresses: none are shown, and the log says so.
	b := f.open(func(o *Options) {
		o.NoStart = true
		o.Addresses = func() ([]string, error) { return nil, syscall.EPERM }
	})
	if srv := b.Server(); len(srv.Addresses) != 0 {
		t.Errorf("the addresses are %v", srv.Addresses)
	}
	if !strings.Contains(f.log.String(), "this computer's addresses are not known") {
		t.Errorf("the log does not say that the addresses are not known:\n%s", f.log.String())
	}
}

// events collects what a subscriber is told.
type events struct {
	mu  sync.Mutex
	got []Topics
}

func (e *events) add(event Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if changed, ok := event.(Changed); ok {
		e.got = append(e.got, changed.Topics)
	}
}

func (e *events) take() []Topics {
	e.mu.Lock()
	defer e.mu.Unlock()
	got := e.got
	e.got = nil
	return got
}

func TestEventsAreCoalesced(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(func(o *Options) { o.NoStart = true })
	f.clock.Advance(time.Second) // what opening changed is told and done with
	var first, second events
	a.Subscribe(first.add)
	cancelSecond := a.Subscribe(second.add)

	// Many changes at once: nothing is told before the time has passed, then one event with
	// all of them.
	f.person(a, "alice")
	f.person(a, "bob")
	if err := a.Set(ctx, string(settings.SFTPEnabled), false); err != nil {
		t.Fatal(err)
	}
	if err := a.Set(ctx, string(settings.ForwardingAgent), false); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(CoalesceTime - time.Millisecond)
	if got := first.take(); len(got) != 0 {
		t.Fatalf("events were sent before the time passed: %v", got)
	}
	f.clock.Advance(time.Millisecond)
	for name, e := range map[string]*events{"first": &first, "second": &second} {
		got := e.take()
		if len(got) != 1 || got[0] != TopicPeople|TopicSettings {
			t.Errorf("the %s subscriber got %v, want one event with the people and the settings", name, got)
		}
	}
	// Nothing changed since: nothing is told.
	f.clock.Advance(time.Second)
	if got := first.take(); len(got) != 0 {
		t.Errorf("events without a change: %v", got)
	}

	// A subscriber that left is not told; the other gets the next change, alone in its event.
	cancelSecond()
	cancelSecond() // twice is fine
	if err := a.StartServer(ctx); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(CoalesceTime)
	got := first.take()
	if len(got) != 1 || !got[0].Has(TopicServer) || got[0].Has(TopicPeople) {
		t.Errorf("after the server started the subscriber got %v", got)
	}
	if got := second.take(); len(got) != 0 {
		t.Errorf("a subscriber that left got %v", got)
	}

	// A connection and its attempt.
	key := sshtest.NewKey(t, sshtest.Ed25519)
	p, err := a.people.Add(ctx, "carol")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.people.AddKeys(ctx, p.ID, key.AuthorizedKey); err != nil {
		t.Fatal(err)
	}
	f.connect(a, "carol", key, f.port)
	// The registry shows a connection a moment before the server announces it, so the event with
	// the connections may come in the next round of the clock; what matters is that the subscriber
	// is told about the person, the attempt and the connection, and never twice about one change.
	var told Topics
	deadline := time.Now().Add(waitFor)
	for !told.Has(TopicConnections | TopicAttempts | TopicPeople) {
		if time.Now().After(deadline) {
			t.Fatalf("after a connection the subscriber was told only about %b", told)
		}
		f.clock.Advance(CoalesceTime)
		for _, topics := range first.take() {
			told |= topics
		}
		time.Sleep(5 * time.Millisecond)
	}

	// After Close nothing is told any more.
	f.close(a)
	f.clock.Advance(time.Second)
	if got := first.take(); len(got) != 0 {
		t.Errorf("events after Close: %v", got)
	}
}

func TestTopics(t *testing.T) {
	all := TopicServer | TopicPeople | TopicAttempts | TopicConnections | TopicSettings
	for _, topic := range []Topics{TopicServer, TopicPeople, TopicAttempts, TopicConnections, TopicSettings} {
		if !all.Has(topic) || topic.Has(all) || Topics(0).Has(topic) {
			t.Errorf("topic %d", topic)
		}
	}
	if !(TopicPeople | TopicSettings).Has(TopicPeople|TopicSettings) || (TopicPeople).Has(TopicPeople|TopicSettings) {
		t.Error("Has with two topics")
	}
}

// record puts an attempt in the list, as the server does when somebody tries to connect.
func record(a *App, name string) {
	a.attempts.Record(attempts.Attempt{Outcome: attempts.UnknownName, LoginName: name, Address: "192.0.2.99"})
}

func TestOnStartClearsTheList(t *testing.T) {
	f := newFixture(t, map[string]any{string(settings.AttemptsClear): string(attempts.OnStart)})
	a := f.open(func(o *Options) { o.NoStart = true })
	record(a, "mallory")
	record(a, "eve")
	if got := a.Attempts(); len(got.Entries) != 2 {
		t.Fatalf("attempts = %+v", got)
	}
	f.close(a)

	// The next start: the list of the last run is gone before anybody can connect.
	f.clock.Advance(time.Hour)
	b := f.open(func(o *Options) { o.NoStart = true })
	got := b.Attempts()
	if len(got.Entries) != 0 || !got.LastClearedAt.Equal(start.Add(time.Hour)) || !got.NextClearAt.IsZero() {
		t.Errorf("after a start with onStart the list is %+v", got)
	}
}

func TestWithoutOnStartTheListIsKept(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(func(o *Options) { o.NoStart = true })
	record(a, "mallory")
	f.close(a) // Close saves the list, although the delay of its saving has not passed
	b := f.open(func(o *Options) { o.NoStart = true })
	if got := b.Attempts(); len(got.Entries) != 1 || got.Entries[0].LoginName != "mallory" || !got.LastClearedAt.IsZero() {
		t.Errorf("after a restart the list is %+v", got)
	}
}

func TestAPeriodClearsTheListWhileRunning(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(func(o *Options) { o.NoStart = true })
	record(a, "mallory")
	// The period starts when the setting is changed to it.
	if err := a.Set(ctx, string(settings.AttemptsClear), string(attempts.Hourly)); err != nil {
		t.Fatal(err)
	}
	if got := a.Attempts(); !got.NextClearAt.Equal(start.Add(time.Hour)) || len(got.Entries) != 1 {
		t.Fatalf("after the change the list is %+v", got)
	}
	f.clock.Advance(59 * time.Minute)
	if got := a.Attempts(); len(got.Entries) != 1 {
		t.Fatalf("the list was cleared before the hour passed: %+v", got)
	}
	// The look of the minute in which the hour ends clears it, and the next hour begins.
	f.clock.Advance(time.Minute)
	got := a.Attempts()
	if len(got.Entries) != 0 || !got.LastClearedAt.Equal(start.Add(time.Hour)) || !got.NextClearAt.Equal(start.Add(2*time.Hour)) {
		t.Fatalf("after the hour the list is %+v", got)
	}
	record(a, "eve")
	f.clock.Advance(time.Hour)
	if got := a.Attempts(); len(got.Entries) != 0 {
		t.Errorf("the second hour did not clear the list: %+v", got)
	}

	// The time passes while Cave is closed: the next start clears at once.
	record(a, "trudy")
	f.close(a)
	f.clock.Advance(3 * time.Hour)
	b := f.open(func(o *Options) { o.NoStart = true })
	if got := b.Attempts(); len(got.Entries) != 0 {
		t.Errorf("a period that passed while Cave was closed did not clear the list: %+v", got)
	}
	// Back to never: no next time, and time clears nothing.
	if err := b.Set(ctx, string(settings.AttemptsClear), string(attempts.Never)); err != nil {
		t.Fatal(err)
	}
	record(b, "victor")
	f.clock.Advance(100 * 24 * time.Hour)
	if got := b.Attempts(); len(got.Entries) != 1 || !got.NextClearAt.IsZero() {
		t.Errorf("with never the list is %+v", got)
	}
}

func TestCloseSavesAndReleases(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(nil)
	key := f.person(a, "alice")
	client := f.connect(a, "alice", key, f.port)
	f.close(a)
	// The server is stopped and its connections are closed.
	waitGone(t, client)
	if taken(f.port) {
		t.Error("the port is still listened on after Close")
	}
	f.close(a) // twice is fine
	// The attempt of the connection is in the file, and the lock is free: a new run sees it.
	if _, err := os.Stat(filepath.Join(f.dirs.State, "attempts.json")); err != nil {
		t.Errorf("the attempts list was not saved: %v", err)
	}
	b := f.open(func(o *Options) { o.NoStart = true })
	if got := b.Attempts(); len(got.Entries) != 1 || got.Entries[0].PersonName != "alice" || got.Entries[0].Outcome != attempts.Accepted {
		t.Errorf("after a restart the list is %+v", got)
	}
	if people := b.People(); len(people) != 1 || people[0].Name != "alice" {
		t.Errorf("after a restart the people are %+v", people)
	}
	if !strings.Contains(f.log.String(), `"msg":"cave stopped"`) {
		t.Errorf("the log does not have the stop:\n%s", f.log.String())
	}
}

func TestRootIsRefused(t *testing.T) {
	f := newFixture(t, nil)
	asRoot := func(o *Options) { o.IsRoot = func() bool { return true } }
	opts := f.options()
	asRoot(&opts)
	a, err := Open(ctx, opts)
	if a != nil {
		t.Fatal("the application was opened as root")
	}
	errtest.Route(t, err, "Open.RunningAsRoot")
	// Nothing was touched: no lock, no host key, no listener.
	for _, name := range []string{filepath.Join(f.dirs.State, "lock"), filepath.Join(f.dirs.Config, "ssh_host_ed25519_key")} {
		if _, err := os.Stat(name); err == nil {
			t.Errorf("%s was made", name)
		}
	}
	if taken(f.port) {
		t.Error("the server listens")
	}
	// With --allow-root it opens.
	b := f.open(func(o *Options) {
		asRoot(o)
		o.AllowRoot = true
	})
	if srv := b.Server(); srv.State != server.Running {
		t.Errorf("with --allow-root the server is %+v", srv)
	}
}

func TestTheSystemIsAskedWhoTheUserIs(t *testing.T) {
	// The tests do not run as root; if they ever do, the refusal is what is expected.
	if got, want := isRoot(), os.Geteuid() == 0; got != want {
		t.Errorf("isRoot() = %v, the system says %v", got, want)
	}
}

func TestAFailedOpenLeavesNothingOpen(t *testing.T) {
	f := newFixture(t, nil)
	file := filepath.Join(f.dirs.Config, "settings.json")
	good, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("{ this is not JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := Open(ctx, f.options())
	if a != nil {
		t.Fatal("the application was opened over a settings file that does not parse")
	}
	errtest.Route(t, err, "Open->App.open", "SettingsParseFailed")
	// The file is as it was, and nothing listens.
	if got, _ := os.ReadFile(file); string(got) != "{ this is not JSON" {
		t.Errorf("the broken file was changed: %q", got)
	}
	if taken(f.port) {
		t.Error("the server listens")
	}
	// The lock was released: with the file repaired, the application opens.
	if err := os.WriteFile(file, good, 0o600); err != nil {
		t.Fatal(err)
	}
	b := f.open(nil)
	if srv := b.Server(); srv.State != server.Running {
		t.Errorf("the server is %+v", srv)
	}
}

func TestOpenWithAFolderThatCannotBeMade(t *testing.T) {
	f := newFixture(t, nil)
	opts := f.options()
	// A file where the state folder should be.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	opts.Dirs.State = filepath.Join(blocker, "state")
	a, err := Open(ctx, opts)
	if a != nil {
		t.Fatal("the application was opened without a state folder")
	}
	errtest.Route(t, err, "Open->App.open", "StorageDirCreateFailed")
}

func TestTheVersionOfTheBuild(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(func(o *Options) {
		o.NoStart = true
		o.Version = ""
	})
	if a.opts.Version != Version() {
		t.Errorf("the version is %q, want %q", a.opts.Version, Version())
	}
}
