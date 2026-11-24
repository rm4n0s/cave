package forward

import (
	"context"
	"log/slog"
	"net"
	"strconv"
	"sync"

	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// The loopback addresses a forward is bound to when it may not be reached from elsewhere.
const (
	loopback4 = "127.0.0.1"
	loopback6 = "::1"
)

// forwardRequest is the payload of tcpip-forward and of cancel-tcpip-forward: where the client
// asks Cave to listen.
type forwardRequest struct {
	Host string
	Port uint32
}

// forwardKey names a forward as the client knows it: the host it asked for, as it wrote it, and
// the port Cave listens on.
type forwardKey struct {
	host string
	port uint32
}

// remote is what Cave listens on for one connection.
type remote struct {
	mu sync.Mutex
	// listeners are the open listeners, each with the function that takes it out of the
	// registry.
	listeners map[forwardKey]*remoteListener
	// forwarded counts the connections that arrived and are still open.
	forwarded int
	// ended says that the connection is gone: nothing more may be listened on.
	ended bool
}

// remoteListener is one port Cave listens on for a client.
type remoteListener struct {
	listener net.Listener
	remove   func()
}

// bindHost returns the host Cave listens on when a client asks for a host (docs/SPEC.md §9.8),
// and false for a host Cave does not listen on. With gateway ports off, a forward is only
// reachable from this computer whatever was asked: "::1" when an IPv6 address was asked, else
// "127.0.0.1". With them on, it is the address asked: the empty host and "*" mean every
// interface, "localhost" the loopback address, and any other host must be an IP address.
func bindHost(asked string, gatewayPorts bool) (host string, ok bool) {
	ip := net.ParseIP(asked)
	switch {
	case asked == "" || asked == "*":
		if gatewayPorts {
			return "", true
		}
		return loopback4, true
	case asked == "localhost":
		return loopback4, true
	case ip == nil:
		return "", false
	case !gatewayPorts && ip.To4() == nil:
		return loopback6, true
	case !gatewayPorts:
		return loopback4, true
	}
	return ip.String(), true
}

// remoteOf returns what Cave listens on for a connection, made at its first forward. When the
// connection ends, everything it listens on is closed.
func (h *handler) remoteOf(ctx context.Context, conn *server.Conn) *remote {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.remotes[conn.ID()]
	if ok {
		return r
	}
	r = &remote{listeners: map[forwardKey]*remoteListener{}}
	h.remotes[conn.ID()] = r
	context.AfterFunc(ctx, func() {
		h.mu.Lock()
		delete(h.remotes, conn.ID())
		h.mu.Unlock()
		r.mu.Lock()
		defer r.mu.Unlock()
		r.ended = true
		for key, l := range r.listeners {
			_ = l.listener.Close() // nobody is left to tell
			l.remove()
			delete(r.listeners, key)
		}
	})
	return r
}

// listen answers tcpip-forward: Cave listens where the client asks, as far as the settings
// allow, and tells the port when the client left its choice to the system.
func (h *handler) listen(ctx context.Context, conn *server.Conn, request *ssh.Request) (bool, []byte) {
	var req forwardRequest
	logger := h.opts.Logger.With("connection", conn.ID(), "person", conn.PersonName())
	// The host may be empty, which means every interface; else it is text a host can be.
	if err := ssh.Unmarshal(request.Payload, &req); err != nil || req.Host != "" && !validHost(req.Host) || req.Port > maxPort {
		logger.Info("remote forward refused", "reason", "the request is not valid")
		return false, nil
	}
	logger = logger.With("asked", net.JoinHostPort(req.Host, strconv.Itoa(int(req.Port))))
	snap := h.opts.Settings.Snapshot()
	if !snap.Bool(settings.ForwardingRemote) {
		logger.Info("remote forward refused", "reason", "forwarding.remote is off")
		return false, nil
	}
	host, ok := bindHost(req.Host, snap.Bool(settings.ForwardingGatewayPorts))
	if !ok {
		logger.Info("remote forward refused", "reason", "the host is not an address")
		return false, nil
	}
	r := h.remoteOf(ctx, conn)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ended {
		return false, nil
	}
	if len(r.listeners) >= MaxListeners {
		logger.Info("remote forward refused", "reason", "the connection has as many as it may")
		return false, nil
	}
	listener, err := h.opts.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(int(req.Port))))
	if err != nil {
		logger.Warn("remote forward failed", "err", errors.NewErr(TagListenFailed, err).SetMetadata("host", host, "port", req.Port))
		return false, nil
	}
	port := req.Port
	if tcp, isTCP := listener.Addr().(*net.TCPAddr); isTCP {
		port = uint32(tcp.Port)
	}
	key := forwardKey{host: req.Host, port: port}
	address := listener.Addr().String()
	r.listeners[key] = &remoteListener{listener: listener, remove: conn.AddActivity(server.ActivityRemoteForward, address)}
	logger = logger.With("address", address)
	logger.Info("remote forward opened")
	go h.accept(ctx, conn, r, key, listener, logger)
	if req.Port == 0 {
		return true, ssh.Marshal(struct{ Port uint32 }{port})
	}
	return true, nil
}

// cancel answers cancel-tcpip-forward: Cave stops listening. The connections that arrived
// before go on.
func (h *handler) cancel(ctx context.Context, conn *server.Conn, request *ssh.Request) (bool, []byte) {
	var req forwardRequest
	if err := ssh.Unmarshal(request.Payload, &req); err != nil {
		return false, nil
	}
	if !h.remoteOf(ctx, conn).close(forwardKey{host: req.Host, port: req.Port}) {
		return false, nil
	}
	h.opts.Logger.Info("remote forward closed", "connection", conn.ID(), "person", conn.PersonName(), "port", req.Port)
	return true, nil
}

// close stops listening for one forward, and says whether there was one.
func (r *remote) close(key forwardKey) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.listeners[key]
	if !ok {
		return false
	}
	_ = l.listener.Close() // a listener that is closed already is fine
	l.remove()
	delete(r.listeners, key)
	return true
}

// take counts one more connection that arrived, unless the connection has as many as it may.
func (r *remote) take() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.forwarded >= MaxForwarded {
		return false
	}
	r.forwarded++
	return true
}

// release counts one connection less.
func (r *remote) release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forwarded--
}

// accept carries every connection that arrives at a listener to the client, until the listener
// is closed. A listener that fails is closed: the client asks again if it wants it back.
func (h *handler) accept(ctx context.Context, conn *server.Conn, r *remote, key forwardKey, listener net.Listener, logger *slog.Logger) {
	for {
		arrived, err := listener.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				logger.Warn("remote forward failed", "err", errors.NewErr(TagAcceptFailed, err).SetMetadata("address", listener.Addr().String()))
			}
			if r.close(key) {
				logger.Info("remote forward closed")
			}
			return
		}
		if !r.take() {
			_ = arrived.Close() // too many at once; nothing was read from it
			continue
		}
		go func() {
			defer r.release()
			h.carry(ctx, conn, key, arrived, logger)
		}()
	}
}

// carry opens a forwarded-tcpip channel for a connection that arrived and copies both ways.
func (h *handler) carry(ctx context.Context, conn *server.Conn, key forwardKey, arrived net.Conn, logger *slog.Logger) {
	originHost, originPort := "", uint32(0)
	if origin, ok := arrived.RemoteAddr().(*net.TCPAddr); ok {
		originHost, originPort = origin.IP.String(), uint32(origin.Port)
	}
	// The host is the one the client asked for, as it wrote it: that is how a client finds the
	// forward the channel belongs to.
	channel, requests, err := conn.SSH().OpenChannel("forwarded-tcpip", ssh.Marshal(struct {
		Host       string
		Port       uint32
		OriginHost string
		OriginPort uint32
	}{key.host, key.port, originHost, originPort}))
	if err != nil {
		_ = arrived.Close() // the client refused the channel, or is gone
		return
	}
	go ssh.DiscardRequests(requests)
	sent, received := pipe(ctx, channel, arrived)
	logger.Info("forwarded connection closed", "origin", originHost, "sent", sent, "received", received)
}
