// Package server is Cave's SSH server: it listens, limits and times the connections that are not
// yet authenticated, authenticates each one by a public key of the person whose name it logs in
// as, and records how every connection ended in the attempts list (docs/SPEC.md §7.3, §8.1,
// §9.1–9.3).
package server

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// The Tags of this package's errors.
const (
	TagStartCancelled = "ServerStartCancelled"
	TagListenFailed   = "ListenFailed"
	TagRelistenFailed = "ServerRelistenFailed"
	TagStopTimedOut   = "ServerStopTimedOut"
	TagAcceptFailed   = "ServerAcceptFailed"
)

// acceptRetryDelay is how long the server waits after an accept that failed without the listener
// being closed, before it accepts again.
const acceptRetryDelay = 100 * time.Millisecond

// The limits of docs/SPEC.md §9.2.
const (
	// GraceTime is how long a connection has, from accept, to be authenticated.
	GraceTime = 30 * time.Second
	// MaxUnauthenticated is how many connections may be waiting to be authenticated at once.
	MaxUnauthenticated = 64
	// MaxUnauthenticatedPerAddress is how many of them may come from one address.
	MaxUnauthenticatedPerAddress = 8
	// MaxAuthTries is how many authentication requests a connection may fail.
	MaxAuthTries = 6
	// maxKeysTracked is how many offered keys a connection's attempt remembers.
	maxKeysTracked = 16
)

// publicKeyAlgorithms are the signature algorithms a client may authenticate with: every
// accepted key type, and RSA only with SHA-2 (docs/SPEC.md §7.3).
var publicKeyAlgorithms = []string{
	ssh.KeyAlgoED25519,
	ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521,
	ssh.KeyAlgoSKED25519, ssh.KeyAlgoSKECDSA256,
	ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512,
}

// State is what the server is doing.
type State int

// The states of the server (docs/SPEC.md §9.1).
const (
	// Stopped: not listening, no connection.
	Stopped State = iota
	// Starting: about to listen.
	Starting
	// Running: listening.
	Running
	// Stopping: closing the listener and the connections.
	Stopping
)

// String names the state.
func (s State) String() string {
	switch s {
	case Stopped:
		return "stopped"
	case Starting:
		return "starting"
	case Running:
		return "running"
	case Stopping:
		return "stopping"
	}
	return "unknown"
}

// Options are what a Server needs. Every field but Address is required.
type Options struct {
	// People answers who may log in.
	People *people.Store
	// Attempts gets one record for every connection.
	Attempts *attempts.List
	// HostKey is the key the server proves itself with; SetHostKey replaces it.
	HostKey ssh.Signer
	// Settings has the address, the port and what clients may do.
	Settings *settings.Store
	// Clock times the limits.
	Clock clock.Clock
	// Logger gets the server's log.
	Logger *slog.Logger
	// Version is Cave's version, for the identification string "SSH-2.0-Cave_<version>".
	Version string
	// Address, when not nil, decides the "host:port" to listen on in place of the settings
	// server.address and server.port: the programs' --port, and the tests' "127.0.0.1:0".
	Address func(settings.Snapshot) string
}

// Server is the SSH server. It is safe for concurrent use.
type Server struct {
	opts Options
	// listen opens the listener; tests replace it to make accepting fail.
	listen func(ctx context.Context, address string) (net.Listener, error)

	mu       sync.Mutex
	state    State
	listener net.Listener
	hostKey  ssh.Signer
	// conns are the open connections, authenticated or not.
	conns map[net.Conn]struct{}
	// connections is the registry: the authenticated connections by their ids.
	connections map[string]*Conn
	// channelHandlers and requestHandlers serve what authenticated clients ask for, by type.
	// They are replaced, never changed, so that a connection can keep the maps it started with.
	channelHandlers map[string]ChannelHandler
	requestHandlers map[string]RequestHandler
	// pending counts the connections that are not yet authenticated, in all and by address.
	pending          int
	pendingByAddress map[string]int
	// running counts the goroutines of the listener and the connections.
	running sync.WaitGroup

	listenersMu sync.Mutex
	listeners   []subscription
	nextID      int
}

// subscription is one listener of a Server.
type subscription struct {
	id int
	fn func(Change)
}

// New returns a stopped server. It follows the people from now on: a change that makes a
// connection invalid closes it.
func New(opts Options) *Server {
	s := &Server{
		opts: opts,
		listen: func(ctx context.Context, address string) (net.Listener, error) {
			var config net.ListenConfig
			return config.Listen(ctx, "tcp", address)
		},
		hostKey:          opts.HostKey,
		conns:            map[net.Conn]struct{}{},
		connections:      map[string]*Conn{},
		channelHandlers:  map[string]ChannelHandler{},
		requestHandlers:  map[string]RequestHandler{},
		pendingByAddress: map[string]int{},
	}
	opts.People.Subscribe(s.peopleChanged)
	return s
}

// State returns what the server is doing.
func (s *Server) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Addrs returns the addresses the server listens on; none when it is not running.
func (s *Server) Addrs() []net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return []net.Addr{s.listener.Addr()}
}

// SetHostKey replaces the host key for the connections that come after.
func (s *Server) SetHostKey(key ssh.Signer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hostKey = key
}

// setState changes the state and tells the listeners.
func (s *Server) setState(state State) {
	s.mu.Lock()
	s.state = state
	s.mu.Unlock()
	s.notify(StateChanged)
}

// address returns the "host:port" to listen on.
func (s *Server) address() string {
	snap := s.opts.Settings.Snapshot()
	if s.opts.Address != nil {
		return s.opts.Address(snap)
	}
	return net.JoinHostPort(snap.String(settings.ServerAddress), strconv.Itoa(snap.Int(settings.ServerPort)))
}

// Start makes the server listen. On a server that is not stopped it does nothing. When it
// cannot listen (ListenFailed, with the address in the metadata), the server stays stopped.
func (s *Server) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errors.NewErr(TagStartCancelled, err)
	}
	s.mu.Lock()
	if s.state != Stopped {
		s.mu.Unlock()
		return nil
	}
	s.state = Starting
	s.mu.Unlock()
	s.notify(StateChanged)

	address := s.address()
	listener, err := s.listen(ctx, address)
	if err != nil {
		s.setState(Stopped)
		return errors.NewErr(TagListenFailed, err).SetMetadata("address", address)
	}
	s.mu.Lock()
	s.listener = listener
	s.state = Running
	s.running.Add(1)
	s.mu.Unlock()
	s.opts.Logger.Info("listening", "address", listener.Addr().String())
	go s.accept(listener)
	s.notify(StateChanged)
	return nil
}

// Stop closes the listener and every connection, and waits until their goroutines are done or
// ctx ends (ServerStopTimedOut). On a server that is not running it does nothing.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if s.state != Running {
		s.mu.Unlock()
		return nil
	}
	s.state = Stopping
	listener := s.listener
	s.listener = nil
	open := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		open = append(open, conn)
	}
	s.mu.Unlock()
	s.notify(StateChanged)

	// Closing is how the goroutines are told to end; a close that fails leaves nothing to do.
	if listener != nil { // nil when a new address could not be listened on (Relisten)
		_ = listener.Close()
	}
	for _, conn := range open {
		_ = conn.Close()
	}
	done := make(chan struct{})
	go func() {
		s.running.Wait()
		close(done)
	}()
	var err error
	select {
	case <-done:
	case <-ctx.Done():
		err = errors.NewErr(TagStopTimedOut, ctx.Err())
	}
	s.opts.Logger.Info("stopped")
	s.setState(Stopped)
	return err
}

// Relisten makes a running server listen on the address the settings have now, and keeps the
// connections that are established: it is what a change of server.port or server.address does
// (docs/SPEC.md §6). On a server that is not running it does nothing. The old listener is closed
// first, because the new address may be one it holds. When the new address cannot be listened
// on (ServerRelistenFailed, with the address in the metadata), the server is stopped, as when it
// cannot start.
func (s *Server) Relisten(ctx context.Context) error {
	s.mu.Lock()
	if s.state != Running {
		s.mu.Unlock()
		return nil
	}
	old := s.listener
	s.listener = nil // the loop that accepts from it sees that it is not the listener any more
	s.mu.Unlock()
	_ = old.Close() // a listener that is closed already is fine

	address := s.address()
	listener, err := s.listen(ctx, address)
	if err != nil {
		s.opts.Logger.Warn("the new address cannot be listened on", "address", address)
		_ = s.Stop(ctx) // its error would be about the wait; the one that matters is this one
		return errors.NewErr(TagRelistenFailed, err).SetMetadata("address", address)
	}
	s.mu.Lock()
	s.listener = listener
	s.running.Add(1)
	s.mu.Unlock()
	s.opts.Logger.Info("listening", "address", listener.Addr().String())
	go s.accept(listener)
	s.notify(StateChanged)
	return nil
}

// accept takes connections from the listener until it is closed.
func (s *Server) accept(listener net.Listener) {
	defer s.running.Done()
	for {
		conn, err := listener.Accept()
		if err != nil {
			s.mu.Lock()
			stopping := s.state != Running || s.listener != listener
			s.mu.Unlock()
			if stopping {
				return
			}
			// A failure that is not the end (too many open files, for one): wait, and go on.
			s.opts.Logger.Warn("accept failed", "err", errors.NewErr(TagAcceptFailed, err))
			time.Sleep(acceptRetryDelay)
			continue
		}
		if !s.admit(conn) {
			continue
		}
		go s.serve(conn)
	}
}

// remoteAddress returns the address of the other side of a connection, without the port.
func remoteAddress(conn net.Conn) string {
	if tcp, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		if addr, ok := netip.AddrFromSlice(tcp.IP); ok {
			return addr.Unmap().WithZone("").String()
		}
	}
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return conn.RemoteAddr().String()
	}
	return host
}

// admit counts a new connection as waiting to be authenticated, or closes it at once and
// records it as refused when the limits of docs/SPEC.md §9.2 are reached or the server is
// stopping. It reports whether the connection was admitted.
func (s *Server) admit(conn net.Conn) bool {
	address := remoteAddress(conn)
	s.mu.Lock()
	if s.state != Running {
		s.mu.Unlock()
		_ = conn.Close() // the server is stopping: nothing more to do with it
		return false
	}
	if s.pending >= MaxUnauthenticated || s.pendingByAddress[address] >= MaxUnauthenticatedPerAddress {
		s.mu.Unlock()
		_ = conn.Close() // refused: the close is all there is to say
		s.record(attempts.Attempt{Outcome: attempts.Refused, Address: address})
		return false
	}
	s.pending++
	s.pendingByAddress[address]++
	s.conns[conn] = struct{}{}
	s.running.Add(1)
	s.mu.Unlock()
	return true
}

// settled takes a connection out of the count of those waiting to be authenticated, however its
// handshake ended.
func (s *Server) settled(address string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending--
	if s.pendingByAddress[address]--; s.pendingByAddress[address] <= 0 {
		delete(s.pendingByAddress, address)
	}
}

// forget takes a connection out of the open ones.
func (s *Server) forget(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, conn)
}

// record puts an attempt in the list and in the log.
func (s *Server) record(a attempts.Attempt) {
	entry := s.opts.Attempts.Record(a)
	s.opts.Logger.Info("attempt", "outcome", string(entry.Outcome), "loginName", entry.LoginName, "address", entry.Address,
		"person", entry.PersonName, "clientVersion", entry.ClientVersion, "count", entry.Count)
}

// Subscribe registers a function that is called after each change of the server's state and of
// its connections, with which of the two it was. The returned function ends the subscription.
func (s *Server) Subscribe(listener func(Change)) (cancel func()) {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	id := s.nextID
	s.nextID++
	s.listeners = append(s.listeners, subscription{id: id, fn: listener})
	return func() {
		s.listenersMu.Lock()
		defer s.listenersMu.Unlock()
		for i, sub := range s.listeners {
			if sub.id == id {
				// A new slice, so that a notify that is under way keeps its own list.
				s.listeners = append(append([]subscription(nil), s.listeners[:i]...), s.listeners[i+1:]...)
				return
			}
		}
	}
}

// notify calls the listeners, in the order they subscribed, outside the locks.
func (s *Server) notify(change Change) {
	s.listenersMu.Lock()
	listeners := s.listeners
	s.listenersMu.Unlock()
	for _, sub := range listeners {
		sub.fn(change)
	}
}
