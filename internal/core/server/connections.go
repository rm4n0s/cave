package server

import (
	"context"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/rm4n0s/cave/internal/core/ids"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/cave/internal/core/sanitize"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// TagConnectionNotFound is the Tag of the error of disconnecting a connection that is not there.
const TagConnectionNotFound = "ConnectionNotFound"

// The limits of docs/SPEC.md §9.3.
const (
	// MaxChannels is how many channels one connection may have open at once.
	MaxChannels = 256
	// MaxActivityDetail is how many characters the detail of an activity keeps.
	MaxActivityDetail = 80
	// connectionPrefix starts the ids of connections.
	connectionPrefix = "c_"
)

// The names of the requests the server answers itself.
const (
	requestKeepAlive      = "keepalive@openssh.com"
	requestNoMoreSessions = "no-more-sessions@openssh.com"
	channelSession        = "session"
)

// ActivityKind says what a connection has open.
type ActivityKind string

// The kinds of activity (docs/SPEC.md §9.3).
const (
	// ActivityShell is a session with a shell.
	ActivityShell ActivityKind = "shell"
	// ActivityExec is a session that runs a command; the detail is the command.
	ActivityExec ActivityKind = "exec"
	// ActivitySFTP is a session with the SFTP subsystem.
	ActivitySFTP ActivityKind = "sftp"
	// ActivityLocalForward is a connection from this computer made for the client; the detail
	// is where it goes.
	ActivityLocalForward ActivityKind = "local-forward"
	// ActivityRemoteForward is a port this computer listens on for the client; the detail is
	// the address and the port.
	ActivityRemoteForward ActivityKind = "remote-forward"
	// ActivityAgent is agent forwarding.
	ActivityAgent ActivityKind = "agent"
)

// Activity is one thing a connection has open.
type Activity struct {
	// Kind says what it is.
	Kind ActivityKind
	// Detail says which: a command, a destination, an address. It is safe to show.
	Detail string
}

// Connection is an authenticated connection as the registry shows it (docs/SPEC.md §9.3).
type Connection struct {
	// ID names the connection for Disconnect.
	ID string
	// PersonID and PersonName say who it is.
	PersonID, PersonName string
	// KeyID and KeyFingerprint say which of the person's keys it was authenticated with.
	KeyID, KeyFingerprint string
	// Address is the client's IP address, without the port.
	Address string
	// ClientVersion is the client's version string, safe to show.
	ClientVersion string
	// Since is when it was authenticated.
	Since time.Time
	// Activities are what it has open, in the order it was opened.
	Activities []Activity
}

// Change says what changed in the server, for its listeners.
type Change int

// The kinds of change.
const (
	// StateChanged: the server started, stopped, or is on its way.
	StateChanged Change = iota
	// ConnectionsChanged: a connection came or went, or what one has open changed.
	ConnectionsChanged
)

// ChannelHandler serves one channel that a client asks to open: it accepts or rejects it, and
// returns when the channel is done. ctx ends when the connection ends.
type ChannelHandler func(ctx context.Context, conn *Conn, channel ssh.NewChannel)

// RequestHandler answers one global request of a client: whether it succeeded, and the reply's
// payload. ctx ends when the connection ends.
type RequestHandler func(ctx context.Context, conn *Conn, request *ssh.Request) (ok bool, payload []byte)

// Conn is an authenticated connection as the handlers of its channels and requests see it.
type Conn struct {
	server *Server
	ssh    *ssh.ServerConn
	info   Connection
	ctx    context.Context
	cancel context.CancelFunc
	// handlers counts the handlers that run for this connection.
	handlers sync.WaitGroup

	mu sync.Mutex
	// activities are what is open, by the order of their numbers.
	activities   map[int]Activity
	nextActivity int
	// channels counts the open channels.
	channels int
	// noMoreSessions says that the client asked for no further session.
	noMoreSessions bool
}

// ID returns the connection's id.
func (c *Conn) ID() string { return c.info.ID }

// PersonID returns the id of the person who is connected.
func (c *Conn) PersonID() string { return c.info.PersonID }

// PersonName returns the name of the person who is connected.
func (c *Conn) PersonName() string { return c.info.PersonName }

// RemoteAddr returns the client's address.
func (c *Conn) RemoteAddr() net.Addr { return c.ssh.RemoteAddr() }

// LocalAddr returns the address of this side of the connection.
func (c *Conn) LocalAddr() net.Addr { return c.ssh.LocalAddr() }

// SSH returns the connection itself, to open channels to the client and to send it requests.
func (c *Conn) SSH() *ssh.ServerConn { return c.ssh }

// AddActivity shows something the connection has open in the registry, until the returned
// function is called. The detail is made safe to show and cut.
func (c *Conn) AddActivity(kind ActivityKind, detail string) (remove func()) {
	c.mu.Lock()
	n := c.nextActivity
	c.nextActivity++
	c.activities[n] = Activity{Kind: kind, Detail: sanitize.Display(detail, MaxActivityDetail)}
	c.mu.Unlock()
	c.server.notify(ConnectionsChanged)
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			delete(c.activities, n)
			c.mu.Unlock()
			c.server.notify(ConnectionsChanged)
		})
	}
}

// snapshot returns the connection as the registry shows it now.
func (c *Conn) snapshot() Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.info
	numbers := make([]int, 0, len(c.activities))
	for n := range c.activities {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	for _, n := range numbers {
		out.Activities = append(out.Activities, c.activities[n])
	}
	return out
}

// HandleChannel makes handler serve the channels of a type ("session", "direct-tcpip"). A type
// without a handler is refused as an unknown channel type. It is called before Start.
func (s *Server) HandleChannel(channelType string, handler ChannelHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]ChannelHandler, len(s.channelHandlers)+1)
	for t, h := range s.channelHandlers {
		next[t] = h
	}
	next[channelType] = handler
	s.channelHandlers = next
}

// HandleRequest makes handler answer the global requests of a type ("tcpip-forward"). A type
// without a handler is answered with a failure. It is called before Start.
func (s *Server) HandleRequest(requestType string, handler RequestHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]RequestHandler, len(s.requestHandlers)+1)
	for t, h := range s.requestHandlers {
		next[t] = h
	}
	next[requestType] = handler
	s.requestHandlers = next
}

// Connections returns the authenticated connections, the oldest first. The result is the
// caller's own copy.
func (s *Server) Connections() []Connection {
	s.mu.Lock()
	conns := make([]*Conn, 0, len(s.connections))
	for _, c := range s.connections {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	out := make([]Connection, 0, len(conns))
	for _, c := range conns {
		out = append(out, c.snapshot())
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Since.Equal(out[j].Since) {
			return out[i].Since.Before(out[j].Since)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Disconnect closes one connection.
func (s *Server) Disconnect(id string) error {
	s.mu.Lock()
	c, ok := s.connections[id]
	s.mu.Unlock()
	if !ok {
		return errors.New(TagConnectionNotFound, "there is no such connection", "id", id)
	}
	s.opts.Logger.Info("disconnecting", "connection", id, "person", c.info.PersonName)
	_ = c.ssh.Close() // closing is the whole of it; a connection that is already gone is fine
	return nil
}

// DisconnectAll closes every authenticated connection.
func (s *Server) DisconnectAll() {
	s.disconnectWhere(func(*Conn) bool { return true })
}

// disconnectWhere closes the connections that match.
func (s *Server) disconnectWhere(match func(*Conn) bool) {
	s.mu.Lock()
	var closing []*Conn
	for _, c := range s.connections {
		if match(c) {
			closing = append(closing, c)
		}
	}
	s.mu.Unlock()
	for _, c := range closing {
		s.opts.Logger.Info("disconnecting", "connection", c.info.ID, "person", c.info.PersonName)
		_ = c.ssh.Close() // as in Disconnect
	}
}

// peopleChanged closes the connections that a change of the people makes invalid: those of a
// person who was renamed, disabled or removed, and those authenticated with a key that was
// removed (docs/SPEC.md §7.1).
func (s *Server) peopleChanged(change people.Change) {
	switch change.Kind {
	case people.PersonRenamed, people.PersonDisabled, people.PersonRemoved:
		s.disconnectWhere(func(c *Conn) bool { return c.info.PersonID == change.PersonID })
	case people.KeyRemoved:
		s.disconnectWhere(func(c *Conn) bool {
			for _, id := range change.KeyIDs {
				if c.info.KeyID == id {
					return true
				}
			}
			return false
		})
	}
}

// serveAuthenticated runs an authenticated connection until it ends: it puts it in the registry,
// gives its channels and requests to their handlers, and takes it out when the client, the user
// or the server ends it.
func (s *Server) serveAuthenticated(sshConn *ssh.ServerConn, channels <-chan ssh.NewChannel, requests <-chan *ssh.Request, address string) {
	perms := sshConn.Permissions.Extensions
	ctx, cancel := context.WithCancel(context.Background())
	c := &Conn{
		server: s,
		ssh:    sshConn,
		info: Connection{
			ID: ids.New(connectionPrefix), PersonID: perms[extPersonID], PersonName: perms[extPersonName],
			KeyID: perms[extKeyID], KeyFingerprint: perms[extKeyFingerprint], Address: address,
			ClientVersion: sanitize.Display(string(sshConn.ClientVersion()), 64), Since: s.opts.Clock.Now().UTC(),
		},
		ctx:        ctx,
		cancel:     cancel,
		activities: map[int]Activity{},
	}
	s.mu.Lock()
	s.connections[c.info.ID] = c
	channelHandlers, requestHandlers := s.channelHandlers, s.requestHandlers
	s.mu.Unlock()
	s.opts.Logger.Info("connected", "connection", c.info.ID, "person", c.info.PersonName, "address", address)
	s.notify(ConnectionsChanged)

	// The person may have been changed between the key's check and the registry: look again, so
	// that no connection outlives a change that should have ended it.
	if p, ok := s.opts.People.Get(c.info.PersonID); !ok || !p.Enabled || p.Name != c.info.PersonName || !hasKey(p, c.info.KeyID) {
		_ = sshConn.Close() // the loops below see the close
	}

	c.handlers.Add(1)
	go func() {
		defer c.handlers.Done()
		for request := range requests {
			c.request(requestHandlers, request)
		}
	}()
	for channel := range channels {
		c.channel(channelHandlers, channel)
	}

	// The connection ended. The handlers are told, and waited for.
	cancel()
	_ = sshConn.Close() // it is closed already unless the channels ended some other way
	c.handlers.Wait()
	s.mu.Lock()
	delete(s.connections, c.info.ID)
	s.mu.Unlock()
	s.opts.Logger.Info("disconnected", "connection", c.info.ID, "person", c.info.PersonName, "address", address)
	s.notify(ConnectionsChanged)
}

// hasKey says whether the person has the key.
func hasKey(p people.Person, keyID string) bool {
	for _, k := range p.Keys {
		if k.ID == keyID {
			return true
		}
	}
	return false
}

// request answers one global request.
func (c *Conn) request(handlers map[string]RequestHandler, request *ssh.Request) {
	ok, payload := false, []byte(nil)
	switch request.Type {
	case requestKeepAlive:
		ok = true
	case requestNoMoreSessions:
		c.mu.Lock()
		c.noMoreSessions = true
		c.mu.Unlock()
		ok = true
	default:
		if handler, found := handlers[request.Type]; found {
			ok, payload = handler(c.ctx, c, request)
		}
	}
	if request.WantReply {
		_ = request.Reply(ok, payload) // a reply that cannot be sent means the connection is gone
	}
}

// channel gives a channel the client asks for to its handler, on a goroutine of its own, or
// rejects it: an unknown type, a session after "no more sessions", or one channel too many.
func (c *Conn) channel(handlers map[string]ChannelHandler, channel ssh.NewChannel) {
	handler, found := handlers[channel.ChannelType()]
	c.mu.Lock()
	reason, message := ssh.RejectionReason(0), ""
	switch {
	case !found:
		reason, message = ssh.UnknownChannelType, "unknown channel type"
	case channel.ChannelType() == channelSession && c.noMoreSessions:
		reason, message = ssh.Prohibited, "no more sessions"
	case c.channels >= MaxChannels:
		reason, message = ssh.ResourceShortage, "too many channels"
	default:
		c.channels++
	}
	c.mu.Unlock()
	if reason != 0 {
		_ = channel.Reject(reason, message) // a rejection that cannot be sent means the connection is gone
		return
	}
	c.handlers.Add(1)
	go func() {
		defer c.handlers.Done()
		defer func() {
			c.mu.Lock()
			c.channels--
			c.mu.Unlock()
		}()
		handler(c.ctx, c, channel)
	}()
}
