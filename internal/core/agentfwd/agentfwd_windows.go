package agentfwd

import (
	"context"
	"io"
	"sync"

	"github.com/rm4n0s/errors"
)

// Socket is the agent's place of one session on Windows: a named pipe, `\\.\pipe\cave-agent-<random>`,
// that only the current user may open (docs/SPEC.md §13.5).
type Socket struct {
	opts     Options
	name     string
	listener *pipeListener

	mu sync.Mutex
	// conns are the connections that are open, so that Close can end them.
	conns  map[*pipeConn]struct{}
	closed bool
	// done is closed when the socket stopped accepting.
	done chan struct{}
}

// Listen makes the pipe of a session and carries every connection to it to the client's agent,
// until Close is called or ctx ends.
func Listen(ctx context.Context, opts Options) (*Socket, error) {
	return listenOn(ctx, opts, pipeName())
}

// listenOn is Listen with the name of the pipe, so that a test can name one that is taken.
func listenOn(ctx context.Context, opts Options, name string) (*Socket, error) {
	listener, err := listenPipe(name)
	if err != nil {
		return nil, err
	}
	s := &Socket{opts: opts, name: name, listener: listener, conns: map[*pipeConn]struct{}{}, done: make(chan struct{})}
	go s.serve()
	context.AfterFunc(ctx, s.Close)
	return s, nil
}

// Path returns the name of the pipe: the session's SSH_AUTH_SOCK.
func (s *Socket) Path() string {
	return s.name
}

// Close stops the pipe: it closes the connections that are open. A second call does nothing.
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
	s.listener.Close() // ends the wait of serve
	for conn := range conns {
		_ = conn.Close() // ends its copies
	}
	<-s.done
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
				s.opts.Logger.Warn("agent forwarding failed", "err", errors.NewErr(TagAcceptFailed, err).SetMetadata("path", s.name))
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
func (s *Socket) add(conn *pipeConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || len(s.conns) >= MaxConns {
		return false
	}
	s.conns[conn] = struct{}{}
	return true
}

// remove closes a connection and forgets it.
func (s *Socket) remove(conn *pipeConn) {
	_ = conn.Close() // closed already when its copies ended
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, conn)
}

// carry copies between a program of the session and the client's agent until either is done.
func (s *Socket) carry(conn *pipeConn) {
	agent, err := s.opts.Open()
	if err != nil {
		// The client has no agent to offer, or is gone: the program sees the pipe close.
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
