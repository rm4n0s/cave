// Package forward serves TCP port forwarding (docs/SPEC.md §9.8). A direct-tcpip channel
// ("ssh -L", "ssh -D") is a connection Cave makes for the client to a host and port the client
// names, with the bytes copied both ways.
package forward

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/sanitize"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// The Tags of this package's errors.
const (
	TagDialFailed   = "ForwardDialFailed"
	TagDialTimedOut = "ForwardDialTimedOut"
)

const (
	// DialTimeout is how long Cave tries to connect to what a client asked for.
	DialTimeout = 15 * time.Second
	// maxHost is the longest host a client may name: the longest name DNS has.
	maxHost = 255
	// maxPort is the highest port there is.
	maxPort = 65535
)

// Dialer connects to an address, as net.Dialer.DialContext does.
type Dialer func(ctx context.Context, network, address string) (net.Conn, error)

// Options are what the forwarding handlers need. Every field but Dial is required.
type Options struct {
	// Settings has forwarding.local.
	Settings *settings.Store
	// Clock times the limit of a connection attempt.
	Clock clock.Clock
	// Logger gets the forwards that were opened, closed, refused and failed.
	Logger *slog.Logger
	// Dial connects to what a client asked for; nil is the system's dialer.
	Dial Dialer
}

// handler serves the channels and requests of forwarding.
type handler struct {
	opts Options
}

// Register makes the server serve port forwarding.
func Register(srv *server.Server, opts Options) {
	if opts.Dial == nil {
		opts.Dial = (&net.Dialer{}).DialContext
	}
	h := &handler{opts: opts}
	srv.HandleChannel("direct-tcpip", h.direct)
}

// validHost says whether a client may name a host: a name or an address of printable ASCII
// without spaces, of a sane length. Whether it exists is the dialer's question.
func validHost(host string) bool {
	if host == "" || len(host) > maxHost {
		return false
	}
	for i := 0; i < len(host); i++ {
		if host[i] <= ' ' || host[i] > '~' {
			return false
		}
	}
	return true
}

// direct serves a direct-tcpip channel: it connects to the host and port asked, and copies both
// ways until both sides are done.
func (h *handler) direct(ctx context.Context, conn *server.Conn, newChannel ssh.NewChannel) {
	var request struct {
		Host       string
		Port       uint32
		OriginHost string
		OriginPort uint32
	}
	logger := h.opts.Logger.With("connection", conn.ID(), "person", conn.PersonName())
	// A rejection that cannot be sent means the connection is gone.
	if err := ssh.Unmarshal(newChannel.ExtraData(), &request); err != nil || !validHost(request.Host) || request.Port < 1 || request.Port > maxPort {
		logger.Info("forward refused", "reason", "the request is not valid")
		_ = newChannel.Reject(ssh.ConnectionFailed, "invalid forwarding request")
		return
	}
	target := net.JoinHostPort(request.Host, strconv.Itoa(int(request.Port)))
	logger = logger.With("target", target)
	if !h.opts.Settings.Snapshot().Bool(settings.ForwardingLocal) {
		logger.Info("forward refused", "reason", "forwarding.local is off")
		_ = newChannel.Reject(ssh.Prohibited, "port forwarding is turned off")
		return
	}
	remote, err := h.dial(ctx, target)
	if err != nil {
		logger.Warn("forward failed", "err", err)
		_ = newChannel.Reject(ssh.ConnectionFailed, "connect failed: "+reason(err))
		return
	}
	channel, requests, err := newChannel.Accept()
	if err != nil {
		_ = remote.Close() // the client is gone; nothing went through
		return
	}
	go ssh.DiscardRequests(requests)
	remove := conn.AddActivity(server.ActivityLocalForward, target)
	defer remove()
	logger.Info("forward opened", "origin", sanitize.Display(request.OriginHost, maxHost))
	sent, received := pipe(ctx, channel, remote)
	logger.Info("forward closed", "sent", sent, "received", received)
}

// dial connects to the target, for at most DialTimeout, and not after the connection ended.
func (h *handler) dial(ctx context.Context, target string) (net.Conn, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var timedOut atomic.Bool
	timer := h.opts.Clock.AfterFunc(DialTimeout, func() {
		timedOut.Store(true)
		cancel()
	})
	defer timer.Stop()
	remote, err := h.opts.Dial(ctx, "tcp", target)
	if err != nil {
		if timedOut.Load() {
			return nil, errors.NewErr(TagDialTimedOut, err).SetMetadata("target", target, "limit", DialTimeout.String())
		}
		return nil, errors.NewErr(TagDialFailed, err).SetMetadata("target", target)
	}
	return remote, nil
}

// reason is what a client is told about a connection that could not be made: the system's own
// word for it, without addresses.
func reason(err error) string {
	if appErr, ok := errors.FromError(err); ok && appErr.Tag == TagDialTimedOut {
		return "timed out"
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno.Error()
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return "no such host"
	}
	return "the connection could not be made"
}

// pipe copies between the client's channel and the connection Cave made, each way until its
// source ends, passing the end on so that either side can stop sending and still receive. It
// returns when both ways are done or the connection ended, with the bytes copied each way.
func pipe(ctx context.Context, channel ssh.Channel, remote net.Conn) (sent, received int64) {
	stop := context.AfterFunc(ctx, func() {
		// The connection is gone: end both copies.
		_ = remote.Close()
		_ = channel.Close()
	})
	defer stop()
	var copies sync.WaitGroup
	copies.Add(2)
	go func() {
		defer copies.Done()
		sent, _ = io.Copy(remote, channel) // an error is one of the two sides ending
		if half, ok := remote.(interface{ CloseWrite() error }); ok {
			_ = half.CloseWrite() // the other side sees the end and may still answer
		} else {
			_ = remote.Close()
		}
	}()
	go func() {
		defer copies.Done()
		received, _ = io.Copy(channel, remote) // as above
		_ = channel.CloseWrite()
	}()
	copies.Wait()
	_ = remote.Close() // closed twice is fine
	_ = channel.Close()
	return sent, received
}
