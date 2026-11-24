package agentfwd

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/rm4n0s/errors"
)

// Socket is the agent socket of one session: a Unix socket of mode 0600 in a folder of its own
// of mode 0700.
type Socket struct {
	opts     Options
	dir      string
	listener net.Listener

	mu sync.Mutex
	// conns are the connections that are open, so that Close can end them.
	conns  map[net.Conn]struct{}
	closed bool
	// done is closed when the socket stopped accepting.
	done chan struct{}
}

// Listen makes the socket of a session and carries every connection to it to the client's
// agent, until Close is called or ctx ends.
func Listen(ctx context.Context, opts Options) (*Socket, error) {
	return listen(ctx, opts, os.Chmod)
}

// listen is Listen with the function that sets the socket's mode, so that tests can make it
// fail.
func listen(ctx context.Context, opts Options, chmod func(string, os.FileMode) error) (*Socket, error) {
	// The runtime folder is made when Cave starts; it is made again when something cleaned it
	// away since.
	if err := os.MkdirAll(opts.RuntimeDir, 0o700); err != nil {
		return nil, errors.NewErr(TagRuntimeDirFailed, err).SetMetadata("dir", opts.RuntimeDir)
	}
	// A new folder of mode 0700: nobody but the user gets to the socket, whatever its own mode
	// is for the moment between its making and the chmod below.
	dir, err := os.MkdirTemp(opts.RuntimeDir, "agent-")
	if err != nil {
		return nil, errors.NewErr(TagDirFailed, err).SetMetadata("dir", opts.RuntimeDir)
	}
	path := filepath.Join(dir, SocketName)
	listener, err := net.Listen("unix", path)
	if err != nil {
		_ = os.RemoveAll(dir) // the folder is empty
		return nil, errors.NewErr(TagListenFailed, err).SetMetadata("path", path)
	}
	if err := chmod(path, 0o600); err != nil {
		_ = listener.Close() // this also removes the socket
		_ = os.RemoveAll(dir)
		return nil, errors.NewErr(TagModeFailed, err).SetMetadata("path", path)
	}
	s := &Socket{opts: opts, dir: dir, listener: listener, conns: map[net.Conn]struct{}{}, done: make(chan struct{})}
	go s.serve()
	context.AfterFunc(ctx, s.Close)
	return s, nil
}

// Path returns the path of the socket: the session's SSH_AUTH_SOCK.
func (s *Socket) Path() string {
	return filepath.Join(s.dir, SocketName)
}

// Close stops the socket: it closes the connections that are open and removes the socket and
// its folder. A second call does nothing.
func (s *Socket) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	conns := s.conns
	s.conns = nil
	s.mu.Unlock()
	_ = s.listener.Close() // this also removes the socket
	for conn := range conns {
		_ = conn.Close() // ends its copies
	}
	<-s.done
	_ = os.RemoveAll(s.dir) // what is left of it: the folder
}

// serve carries every connection that arrives to the client's agent, until the listener is
// closed or fails.
func (s *Socket) serve() {
	defer close(s.done)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if !closed {
				s.opts.Logger.Warn("agent forwarding failed", "err", errors.NewErr(TagAcceptFailed, err).SetMetadata("path", s.Path()))
			}
			return
		}
		if !s.add(conn) {
			_ = conn.Close() // too many at once, or the socket is closed
			continue
		}
		go func() {
			defer s.remove(conn)
			s.carry(conn)
		}()
	}
}

// add keeps a connection that arrived, unless the session has as many as it may.
func (s *Socket) add(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || len(s.conns) >= MaxConns {
		return false
	}
	s.conns[conn] = struct{}{}
	return true
}

// remove closes a connection and forgets it.
func (s *Socket) remove(conn net.Conn) {
	_ = conn.Close() // closed already when its copies ended
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, conn)
}

// carry copies between a program of the session and the client's agent until either is done.
func (s *Socket) carry(conn net.Conn) {
	agent, err := s.opts.Open()
	if err != nil {
		// The client has no agent to offer, or is gone: the program sees the socket close.
		s.opts.Logger.Info("the client did not open its agent", "err", errors.NewErr(TagOpenFailed, err))
		return
	}
	var copies sync.WaitGroup
	copies.Add(2)
	go func() {
		defer copies.Done()
		_, _ = io.Copy(agent, conn) // an error is one of the two sides ending
		_ = agent.Close()           // the agent's protocol has no half: one end is the end
	}()
	go func() {
		defer copies.Done()
		_, _ = io.Copy(conn, agent) // as above
		_ = conn.Close()
	}()
	copies.Wait()
}
