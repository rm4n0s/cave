package app

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/forward"
	"github.com/rm4n0s/cave/internal/core/history"
	"github.com/rm4n0s/cave/internal/core/hostkey"
	"github.com/rm4n0s/cave/internal/core/logging"
	"github.com/rm4n0s/cave/internal/core/netinfo"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/session"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/errors"
)

// The Tags of this package's errors.
const (
	TagRunningAsRoot = "RunningAsRoot"
)

const (
	// LogFile is the name of the log in the state folder.
	LogFile = "cave.log"
	// policyCheck is how often the attempts list's clearing is looked at while Cave runs.
	policyCheck = time.Minute
	// defaultSSHPort is the port a client uses when it is given none.
	defaultSSHPort = 22
)

// Options are what Open needs. Program and Dirs are required; the zero value of every other
// field is the real thing, and tests give their own.
type Options struct {
	// Program is the program's name, "cave" or "tcave": it is written into the lock, so that a
	// second instance can say who holds it.
	Program string
	// Dirs are Cave's folders (storage.Resolve).
	Dirs storage.Dirs
	// Clock is the time; nil is the system's.
	Clock clock.Clock
	// Logger gets the log; nil is the log file of the state folder, at LogLevel.
	Logger *slog.Logger
	// LogLevel is --log-level.
	LogLevel slog.Level
	// Version is Cave's version; "" is Version().
	Version string
	// Port is --port: the port for this run, in place of the setting server.port until that
	// setting is changed. 0 means the setting.
	Port int
	// NoStart is --no-start: the server is not started at launch, whatever the setting says.
	NoStart bool
	// AllowRoot is --allow-root: run although the user is root.
	AllowRoot bool
	// SessionHome is CAVE_SESSION_HOME: the home folder of the sessions in place of the user's
	// own; "" is the user's.
	SessionHome string

	// IsRoot says whether the user is root; nil asks the system.
	IsRoot func() bool
	// Addresses lists this computer's addresses; nil is netinfo.Addresses.
	Addresses func() ([]string, error)
	// User is the OS user the sessions run as; the zero value is the user who runs Cave.
	User session.User
	// Environ is Cave's own environment; nil is os.Environ().
	Environ []string
}

// App is the application behind both user interfaces: it owns the stores and the server. It is
// safe for concurrent use.
type App struct {
	opts     Options
	clock    clock.Clock
	logger   *slog.Logger
	lock     *storage.Lock
	settings *settings.Store
	people   *people.Store
	attempts *attempts.List
	history  *history.Store
	server   *server.Server
	// logCloser closes the log file; nil when the logger was given.
	logCloser io.Closer
	// unsubscribe ends the watches of the stores and the server.
	unsubscribe []func()
	// notifier makes the Accepted and Rejected events.
	notifier *notifier

	// serverMu makes the commands that start, stop and move the server take turns.
	serverMu sync.Mutex

	mu sync.Mutex
	// hostKey is the server's host key.
	hostKey *hostkey.HostKey
	// serverErr is why the server is not running although it was asked to; nil otherwise.
	serverErr error
	// port is --port, until the setting server.port is changed; 0 afterwards and without it.
	port int
	// policyTimer is the next look at the attempts list's clearing.
	policyTimer clock.Timer
	// listeners are the subscribers of the events, and pending the topics that changed since
	// the last Changed event; flush is the timer that sends the next one.
	listeners []listener
	nextID    int
	pending   Topics
	flush     clock.Timer
	closed    bool
}

// isRoot says whether the process runs as root. On Windows nobody is.
func isRoot() bool {
	return os.Geteuid() == 0
}

// Open opens the application (docs/SPEC.md §10): it takes the lock of the folders, opens the
// log, loads the settings, the people, the attempts list and the host key, clears the list when
// attempts.clear says so, and starts the server when server.startOnLaunch is on and NoStart is
// not. A server that cannot start leaves the application open, with the reason in the Server
// snapshot. Every other failure is returned, and nothing is left open.
func Open(ctx context.Context, opts Options) (*App, error) {
	root := opts.IsRoot
	if root == nil {
		root = isRoot
	}
	if root() && !opts.AllowRoot {
		return nil, errors.New(TagRunningAsRoot, "Cave gives every person it lets in a shell of the user who runs it; run as root, that is a root shell. Start it as an ordinary user, or with --allow-root")
	}
	a := &App{opts: opts, clock: opts.Clock, logger: opts.Logger, port: opts.Port}
	if a.clock == nil {
		a.clock = clock.System()
	}
	if a.opts.Version == "" {
		a.opts.Version = Version()
	}
	if a.opts.Addresses == nil {
		a.opts.Addresses = netinfo.Addresses
	}
	if a.opts.Environ == nil {
		a.opts.Environ = os.Environ()
	}
	if a.opts.User == (session.User{}) {
		a.opts.User = session.CurrentUser(a.opts.Environ)
	}
	if a.opts.SessionHome != "" {
		a.opts.User.Home = a.opts.SessionHome
	}
	if err := a.open(ctx); err != nil {
		_ = a.release() // what failed to open is the error that matters, not what failed to close
		return nil, err
	}
	return a, nil
}

// open does the work of Open; what it opened before it failed is released by the caller.
func (a *App) open(ctx context.Context) error {
	dirs := a.opts.Dirs
	if err := dirs.Ensure(ctx); err != nil {
		return err
	}
	var err error
	if a.lock, err = storage.Acquire(ctx, dirs, a.opts.Program); err != nil {
		return err
	}
	if a.logger == nil {
		if a.logger, a.logCloser, err = logging.Open(ctx, filepath.Join(dirs.State, LogFile), a.opts.LogLevel); err != nil {
			return err
		}
	}
	if a.settings, err = settings.Open(ctx, dirs); err != nil {
		return err
	}
	if a.people, err = people.Open(ctx, dirs, a.clock); err != nil {
		return err
	}
	if a.attempts, err = attempts.Open(ctx, attempts.Options{Dirs: dirs, Clock: a.clock, Logger: a.logger}); err != nil {
		return err
	}
	if a.history, err = history.Open(ctx, history.Options{Dirs: dirs, Clock: a.clock, Logger: a.logger}); err != nil {
		return err
	}
	if a.hostKey, err = hostkey.LoadOrCreate(ctx, dirs); err != nil {
		return err
	}
	a.logger.Info("cave started", "program", a.opts.Program, "version", a.opts.Version)

	// The list is cleared before anybody can connect (docs/SPEC.md §8.4).
	a.attempts.ApplyPolicy(a.policy(), true)
	a.history.ApplyPolicy(a.historyPolicy(), true)

	a.server = server.New(server.Options{
		People: a.people, Attempts: a.attempts, History: a.history, HostKey: a.hostKey.Signer(), Settings: a.settings, Clock: a.clock, Logger: a.logger,
		Version: a.opts.Version, Address: a.listenAddress,
	})
	session.Register(a.server, session.Options{
		Settings: a.settings, Logger: a.logger, User: a.opts.User, Environ: a.opts.Environ, RuntimeDir: dirs.Runtime,
		History: a.history, Clock: a.clock,
	})
	forward.Register(a.server, forward.Options{Settings: a.settings, Clock: a.clock, Logger: a.logger})

	a.unsubscribe = []func(){
		a.settings.Subscribe(func(string) { a.changed(TopicSettings) }),
		a.people.Subscribe(func(people.Change) { a.changed(TopicPeople) }),
		a.attempts.Subscribe(func() { a.changed(TopicAttempts) }),
		a.history.Subscribe(func() { a.changed(TopicHistory) }),
		a.server.Subscribe(func(change server.Change) {
			if change == server.ConnectionsChanged {
				a.changed(TopicConnections)
			} else {
				a.changed(TopicServer)
			}
		}),
	}
	a.startNotifier()
	a.mu.Lock()
	a.armPolicyCheck()
	a.mu.Unlock()

	if a.settings.Snapshot().Bool(settings.ServerStartOnLaunch) && !a.opts.NoStart {
		// A server that cannot start is not a reason not to open: the user sees why and can
		// change the port. StartServer keeps the error for the snapshot.
		if err := a.StartServer(ctx); err != nil {
			a.logger.Error("the server did not start", "err", err)
		}
	}
	return nil
}

// release lets go of everything the application has open, in the reverse order of its opening.
// It returns the first failure.
func (a *App) release() error {
	var first error
	keep := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	for _, cancel := range a.unsubscribe {
		cancel()
	}
	a.unsubscribe = nil
	if a.history != nil {
		// Kept after its Close, which makes it write nothing more: a snapshot may still read it.
		keep(a.history.Close())
	}
	if a.logCloser != nil {
		keep(a.logCloser.Close())
		a.logCloser = nil
	}
	if a.lock != nil {
		keep(a.lock.Release())
		a.lock = nil
	}
	return first
}

// Close stops the server, saves the attempts list, closes the log and releases the lock
// (docs/SPEC.md §10). It returns the first failure and goes on with the rest. A second call does
// nothing.
func (a *App) Close(ctx context.Context) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	if a.policyTimer != nil {
		a.policyTimer.Stop()
		a.policyTimer = nil
	}
	if a.flush != nil {
		a.flush.Stop()
		a.flush = nil
	}
	a.listeners = nil
	a.mu.Unlock()
	a.stopNotifier()

	a.serverMu.Lock()
	first := a.server.Stop(ctx)
	a.serverMu.Unlock()
	if err := a.attempts.Flush(ctx); err != nil && first == nil {
		first = err
	}
	a.logger.Info("cave stopped")
	if err := a.release(); err != nil && first == nil {
		first = err
	}
	return first
}

// policy returns the setting attempts.clear.
func (a *App) policy() attempts.Policy {
	return attempts.Policy(a.settings.Snapshot().String(settings.AttemptsClear))
}

// historyPolicy returns the setting history.clear, which has the choices of attempts.clear.
func (a *App) historyPolicy() attempts.Policy {
	return attempts.Policy(a.settings.Snapshot().String(settings.HistoryClear))
}

// armPolicyCheck sets the next look at the attempts list's clearing. The caller holds the lock.
func (a *App) armPolicyCheck() {
	if a.closed {
		return
	}
	a.policyTimer = a.clock.AfterFunc(policyCheck, func() {
		a.attempts.ApplyPolicy(a.policy(), false)
		a.history.ApplyPolicy(a.historyPolicy(), false)
		a.mu.Lock()
		defer a.mu.Unlock()
		a.armPolicyCheck()
	})
}

// listenAddress is the "host:port" the server listens on: the settings, with --port in place of
// server.port until that setting is changed.
func (a *App) listenAddress(snap settings.Snapshot) string {
	return net.JoinHostPort(snap.String(settings.ServerAddress), strconv.Itoa(a.listenPort(snap)))
}

// listenPort is the port the server listens on, or would.
func (a *App) listenPort(snap settings.Snapshot) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.port != 0 {
		return a.port
	}
	return snap.Int(settings.ServerPort)
}

// StartServer makes the server listen. On a server that runs it does nothing. A failure
// (ListenFailed: the port is taken, or needs rights Cave does not have) is returned and kept for
// the Server snapshot.
func (a *App) StartServer(ctx context.Context) error {
	a.serverMu.Lock()
	defer a.serverMu.Unlock()
	err := a.server.Start(ctx)
	a.setServerErr(err)
	return err
}

// StopServer closes the listener and every connection. On a server that does not run it does
// nothing.
func (a *App) StopServer(ctx context.Context) error {
	a.serverMu.Lock()
	defer a.serverMu.Unlock()
	a.setServerErr(nil)
	return a.server.Stop(ctx)
}

// setServerErr keeps why the server is not running, and tells the subscribers.
func (a *App) setServerErr(err error) {
	a.mu.Lock()
	a.serverErr = err
	a.mu.Unlock()
	a.changed(TopicServer)
}

// Set changes one setting (docs/SPEC.md §6) and does what the change asks for at once: a new
// server.port or server.address moves the listener of a running server and keeps its
// connections, and a new attempts.clear starts its period. A setting that is saved but whose
// effect failed returns that failure: a port that cannot be listened on stops the server
// (ServerRelistenFailed), and the Server snapshot keeps the reason.
func (a *App) Set(ctx context.Context, key string, value any) error {
	before := a.settings.Snapshot()
	if err := a.settings.Set(ctx, key, value); err != nil {
		return err
	}
	after := a.settings.Snapshot()
	switch key {
	case string(settings.ServerPort):
		if before.Int(settings.ServerPort) == after.Int(settings.ServerPort) {
			return nil
		}
		// The user chose a port: --port has had its run.
		a.mu.Lock()
		a.port = 0
		a.mu.Unlock()
		return a.relisten(ctx)
	case string(settings.ServerAddress):
		if before.String(settings.ServerAddress) == after.String(settings.ServerAddress) {
			return nil
		}
		return a.relisten(ctx)
	case string(settings.AttemptsClear):
		if before.String(settings.AttemptsClear) != after.String(settings.AttemptsClear) {
			a.attempts.PolicyChanged(a.policy())
		}
	case string(settings.HistoryClear):
		if before.String(settings.HistoryClear) != after.String(settings.HistoryClear) {
			a.history.PolicyChanged(a.historyPolicy())
		}
	}
	return nil
}

// relisten moves the listener of a running server to the address the settings have now.
func (a *App) relisten(ctx context.Context) error {
	a.serverMu.Lock()
	defer a.serverMu.Unlock()
	if a.server.State() != server.Running {
		return nil
	}
	err := a.server.Relisten(ctx)
	a.setServerErr(err)
	return err
}

// ServerSnapshot is what the server is doing.
type ServerSnapshot struct {
	// State is stopped, starting, running or stopping.
	State server.State
	// Listening are the addresses the server listens on, such as "[::]:2222"; none when it does
	// not run.
	Listening []string
	// Port is the port a client connects to: the listener's, or the one the server would
	// listen on.
	Port int
	// Err is why the server is not running although it was asked to; nil otherwise.
	Err error
	// HostKeyFingerprint is the SHA256 fingerprint of the host key, which a person compares
	// with what their client shows at the first connection.
	HostKeyFingerprint string
	// Addresses are the addresses of this computer at which the server can be reached, in the
	// order a person would try them.
	Addresses []string
}

// Command returns the line a person types to connect from an address of Addresses, such as
// "ssh -p 2222 ada@192.168.1.20".
func (s ServerSnapshot) Command(person, address string) string {
	if s.Port == defaultSSHPort {
		return "ssh " + person + "@" + address
	}
	return "ssh -p " + strconv.Itoa(s.Port) + " " + person + "@" + address
}

// Server returns what the server is doing.
func (a *App) Server() ServerSnapshot {
	snap := a.settings.Snapshot()
	s := ServerSnapshot{State: a.server.State(), Port: a.listenPort(snap)}
	for _, addr := range a.server.Addrs() {
		s.Listening = append(s.Listening, addr.String())
		if tcp, ok := addr.(*net.TCPAddr); ok {
			s.Port = tcp.Port
		}
	}
	a.mu.Lock()
	s.Err, s.HostKeyFingerprint = a.serverErr, a.hostKey.Fingerprint()
	a.mu.Unlock()
	if only := snap.String(settings.ServerAddress); only != "" {
		// The server listens on one address: that is the only one to tell people.
		s.Addresses = []string{only}
		return s
	}
	addresses, err := a.opts.Addresses()
	if err != nil {
		// The list is a help for the user, not something Cave needs: without it, the user
		// knows the address some other way.
		a.logger.Warn("this computer's addresses are not known", "err", err)
	}
	s.Addresses = addresses
	return s
}

// People returns the people and their keys.
func (a *App) People() []people.Person {
	return a.people.List()
}

// AttemptsSnapshot is the attempts list.
type AttemptsSnapshot struct {
	// Entries are who tried to connect and how many times.
	Entries []attempts.Entry
	// LastClearedAt is when the list was last emptied; the zero time when it never was.
	LastClearedAt time.Time
	// NextClearAt is when attempts.clear empties it next; the zero time without a period.
	NextClearAt time.Time
}

// Attempts returns the attempts list.
func (a *App) Attempts() AttemptsSnapshot {
	return AttemptsSnapshot{Entries: a.attempts.Entries(), LastClearedAt: a.attempts.LastClearedAt(), NextClearAt: a.attempts.NextClearAt()}
}

// HistorySnapshot is the history of connections and transfers (docs/SPEC.md §9.11).
type HistorySnapshot struct {
	// Connections are the connections that were authenticated, the newest first.
	Connections []history.Connection
	// Transfers are the files moved over SFTP, the newest first.
	Transfers []history.Transfer
	// LastClearedAt is when the history was last emptied; the zero time when it never was.
	LastClearedAt time.Time
	// NextClearAt is when history.clear empties it next; the zero time without a period.
	NextClearAt time.Time
}

// History returns the history.
func (a *App) History() HistorySnapshot {
	return HistorySnapshot{
		Connections: a.history.Connections(), Transfers: a.history.Transfers(),
		LastClearedAt: a.history.LastClearedAt(), NextClearAt: a.history.NextClearAt(),
	}
}

// Connections returns the connections that are established, and what each has open.
func (a *App) Connections() []server.Connection {
	return a.server.Connections()
}

// Settings returns the settings.
func (a *App) Settings() settings.Snapshot {
	return a.settings.Snapshot()
}
