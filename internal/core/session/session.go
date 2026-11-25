package session

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/rm4n0s/cave/internal/core/agentfwd"
	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/history"
	"github.com/rm4n0s/cave/internal/core/pty"
	"github.com/rm4n0s/cave/internal/core/sanitize"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/cave/internal/core/sftp"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// The Tags of this package's errors.
const (
	TagSessionStartFailed = "SessionStartFailed"
	TagSessionPipeFailed  = "SessionPipeFailed"
)

// The limits of docs/SPEC.md §9.4.
const (
	// MaxEnvVars is how many variables a client may set on one session.
	MaxEnvVars = 64
	// MaxEnvBytes is how long the name and the value of one variable may be together.
	MaxEnvBytes = 4096
	// DrainTime is how long the output of a process that ended is still waited for, when its
	// descendants keep the pipes open.
	DrainTime = 2 * time.Second
	// startFailedStatus is the exit status reported for a process that could not start.
	startFailedStatus = 255
	// maxCommandLog bounds a command in the log and in the registry.
	maxCommandLog = 80
	// maxTermLength bounds the terminal type of a pty-req.
	maxTermLength = 128
)

// Options are what the session handler needs. Every field is required.
type Options struct {
	// Settings has session.shell, session.acceptEnv, sftp.enabled and forwarding.agent.
	Settings *settings.Store
	// Logger gets the starts and ends of sessions.
	Logger *slog.Logger
	// User is the OS user the sessions run as.
	User User
	// Environ is Cave's own environment, as os.Environ gives it.
	Environ []string
	// RuntimeDir is where the sockets of forwarded agents are made (storage.Dirs.Runtime).
	RuntimeDir string
	// History gets the files that SFTP sessions read and write (docs/SPEC.md §9.11).
	History *history.Store
	// Clock tells the times of those files.
	Clock clock.Clock
}

// handler serves session channels.
type handler struct {
	opts Options
	// pipe makes a pipe; tests replace it to make it fail.
	pipe func() (r, w *os.File, err error)
	// sftp serves the sftp subsystem; nil when the file system could not be made.
	sftp *sftp.Server
}

// Register makes the server serve session channels.
func Register(srv *server.Server, opts Options) {
	h := &handler{opts: opts, pipe: os.Pipe}
	fileSystem, err := sftp.NewFileSystem(opts.User.Home)
	if err != nil {
		opts.Logger.Warn("the sftp subsystem is not available", "err", err)
	} else {
		h.sftp = sftp.New(sftp.Options{FS: fileSystem, Logger: opts.Logger, Clock: opts.Clock})
	}
	srv.HandleChannel("session", h.serve)
}

// session is one session channel.
type session struct {
	h       *handler
	conn    *server.Conn
	channel ssh.Channel
	// env holds the variables the client set.
	env map[string]string
	// pty is the session's pseudo terminal, and term its type; nil and "" without a pty-req.
	pty  *pty.Pty
	term string
	// agent is where the session finds the client's agent; nil without agent forwarding.
	agent *agentfwd.Socket
	// agentShown takes the agent forwarding out of the registry.
	agentShown func()
	// ctx ends when the connection does.
	ctx context.Context
	// started says that the channel had its one start: a shell, a command or a subsystem,
	// whether it could start or not.
	started bool
	// subsystem says that what was started is a subsystem, which runs inside Cave.
	subsystem bool
	// proc is the session's process; nil before the start, when the start failed, and for a
	// subsystem.
	proc *process
	// done is closed when the process ended and its output and status were sent.
	done chan struct{}
}

// serve runs one session channel until the process it started has ended and reported, or the
// client or the connection ended it.
func (h *handler) serve(ctx context.Context, conn *server.Conn, newChannel ssh.NewChannel) {
	channel, requests, err := newChannel.Accept()
	if err != nil {
		return // the connection is gone
	}
	s := &session{h: h, ctx: ctx, conn: conn, channel: channel, env: map[string]string{}, done: make(chan struct{})}
	defer func() { _ = channel.Close() }() // a close that fails means the channel is closed already
	defer func() {
		// Closing the terminal is what tells a process that is still on it that the session is
		// gone: the system sends it SIGHUP.
		if s.pty != nil {
			_ = s.pty.Close()
		}
		// The agent was forwarded to this session, and goes with it.
		if s.agent != nil {
			s.agent.Close()
			s.agentShown()
		}
	}()
	for {
		select {
		case request, ok := <-requests:
			if !ok {
				// The channel ended before the process did: the client closed it, or the
				// connection is gone.
				s.hangUp()
				if s.subsystem {
					// A subsystem runs inside Cave: it ends when its channel is closed, and the
					// session is over when it has let go of what it had open.
					_ = channel.Close() // closed already when the connection is gone
					<-s.done
				}
				return
			}
			s.request(request)
		case <-s.done:
			return
		}
	}
}

// request handles one request of the session's channel.
func (s *session) request(request *ssh.Request) {
	ok := false
	switch request.Type {
	case "env":
		ok = s.setEnv(request.Payload)
	case "shell":
		ok = s.start("", true)
	case "exec":
		var payload struct{ Command string }
		if err := ssh.Unmarshal(request.Payload, &payload); err == nil {
			ok = s.start(payload.Command, false)
		}
	case "signal":
		var payload struct{ Signal string }
		if err := ssh.Unmarshal(request.Payload, &payload); err == nil && s.proc != nil {
			ok = signalProcess(s.proc, payload.Signal)
		}
	case "pty-req":
		ok = s.requestPty(request.Payload)
	case "window-change":
		var size struct{ Cols, Rows, Width, Height uint32 }
		if err := ssh.Unmarshal(request.Payload, &size); err == nil && s.pty != nil {
			ok = s.pty.Resize(pty.Size{Cols: size.Cols, Rows: size.Rows, Width: size.Width, Height: size.Height}) == nil
		}
	case "auth-agent-req@openssh.com":
		ok = s.requestAgent()
	case "subsystem":
		var payload struct{ Name string }
		if err := ssh.Unmarshal(request.Payload, &payload); err == nil {
			ok = s.startSubsystem(payload.Name)
		}
	case "break":
		ok = true
	}
	if request.WantReply {
		_ = request.Reply(ok, nil) // a reply that cannot be sent means the channel is gone
	}
}

// setEnv handles an env request: the variable is kept when its name is one the client may set
// and the limits allow it.
func (s *session) setEnv(payload []byte) bool {
	var v struct{ Name, Value string }
	if err := ssh.Unmarshal(payload, &v); err != nil {
		return false
	}
	if s.started || !validEnvName(v.Name) || len(v.Name)+len(v.Value) > MaxEnvBytes {
		return false
	}
	if _, set := s.env[v.Name]; !set && len(s.env) >= MaxEnvVars {
		return false
	}
	if !settings.MatchEnvName(s.h.opts.Settings.Snapshot().Strings(settings.SessionAcceptEnv), v.Name) {
		return false
	}
	s.env[v.Name] = v.Value
	return true
}

// requestPty handles a pty-req: a pseudo terminal of the size and with the modes asked, for the
// process to come. It is granted once per channel, before the process starts.
func (s *session) requestPty(payload []byte) bool {
	var req struct {
		Term                      string
		Cols, Rows, Width, Height uint32
		Modes                     string
	}
	if err := ssh.Unmarshal(payload, &req); err != nil {
		return false
	}
	if s.started || s.pty != nil || !validTerm(req.Term) {
		return false
	}
	logger := s.h.opts.Logger.With("connection", s.conn.ID(), "person", s.conn.PersonName())
	modes, err := pty.ParseModes([]byte(req.Modes))
	if err != nil {
		logger.Warn("pty-req refused", "err", err)
		return false
	}
	terminal, err := pty.Open(pty.Size{Cols: req.Cols, Rows: req.Rows, Width: req.Width, Height: req.Height}, modes)
	if err != nil {
		logger.Warn("pty-req refused", "err", err)
		return false
	}
	s.pty, s.term = terminal, req.Term
	return true
}

// requestAgent handles auth-agent-req@openssh.com: the session to come finds the client's agent
// at a socket of its own (docs/SPEC.md §9.8). It is granted once per channel, before the start,
// when forwarding.agent is on.
func (s *session) requestAgent() bool {
	if s.started || s.agent != nil {
		return false
	}
	logger := s.h.opts.Logger.With("connection", s.conn.ID(), "person", s.conn.PersonName())
	if !s.h.opts.Settings.Snapshot().Bool(settings.ForwardingAgent) {
		logger.Info("agent forwarding refused", "reason", "forwarding.agent is off")
		return false
	}
	socket, err := agentfwd.Listen(s.ctx, agentfwd.Options{
		RuntimeDir: s.h.opts.RuntimeDir, Logger: logger,
		Open: func() (io.ReadWriteCloser, error) {
			channel, requests, err := s.conn.SSH().OpenChannel("auth-agent@openssh.com", nil)
			if err != nil {
				return nil, err
			}
			go ssh.DiscardRequests(requests)
			return channel, nil
		},
	})
	if err != nil {
		logger.Warn("agent forwarding refused", "err", err)
		return false
	}
	s.agent, s.agentShown = socket, s.conn.AddActivity(server.ActivityAgent, "")
	return true
}

// validTerm says whether a terminal type may go into TERM: printable ASCII without spaces, of a
// sane length. An empty type is allowed, and sets no TERM.
func validTerm(term string) bool {
	if len(term) > maxTermLength {
		return false
	}
	for i := 0; i < len(term); i++ {
		if term[i] <= ' ' || term[i] > '~' {
			return false
		}
	}
	return true
}

// start starts the session's process: a login shell, or the shell running a command. Only the
// first start of a channel does anything; it reports whether the request is granted. A process
// that cannot start is told to the client on the error stream with status 255, as when sshd's
// child cannot run the shell.
func (s *session) start(command string, login bool) bool {
	if s.started {
		return false
	}
	s.started = true
	snap := s.h.opts.Settings.Snapshot()
	shell := defaultShell(snap.String(settings.SessionShell), s.h.opts.User, s.h.opts.Environ)
	in := EnvInput{
		User: s.h.opts.User.Name, Home: s.h.opts.User.Home, Shell: shell, Cave: s.h.opts.Environ, Client: s.env,
		Remote: s.conn.RemoteAddr(), Local: s.conn.LocalAddr(), Person: s.conn.PersonName(),
	}
	if s.pty != nil {
		in.Term, in.TTY = s.term, s.pty.Name()
	}
	if s.agent != nil {
		in.AuthSock = s.agent.Path()
	}
	env := sessionEnv(in)
	kind, detail := server.ActivityExec, command
	if login {
		kind, detail = server.ActivityShell, ""
	}
	logCommand := sanitize.Display(command, maxCommandLog)
	logger := s.h.opts.Logger.With("connection", s.conn.ID(), "person", s.conn.PersonName(), "kind", string(kind), "command", logCommand)

	proc, stdout, stderr, err := s.h.spawn(shell, command, login, env, s.channel, s.pty)
	if err != nil {
		logger.Error("session could not start", "err", err)
		go func() {
			_, _ = io.WriteString(s.channel.Stderr(), "cave: the session could not start: "+err.Error()+"\r\n")
			s.finish(startFailedStatus, "")
		}()
		return true
	}
	s.proc = proc
	logger.Info("session started", "pid", proc.pid)
	remove := s.conn.AddActivity(kind, detail)

	// The output goes to the client until the pipes end: when the process and everything it
	// started have closed them.
	var copies sync.WaitGroup
	copies.Add(1)
	go func() {
		defer copies.Done()
		_, _ = io.Copy(s.channel, stdout) // an error is the channel or the pipe ending
	}()
	if stderr != nil { // a terminal has one stream
		copies.Add(1)
		go func() {
			defer copies.Done()
			_, _ = io.Copy(s.channel.Stderr(), stderr) // as above
		}()
	}
	go func() {
		status, signal := wait(proc)
		close(proc.exited)
		if s.pty != nil {
			// Cave lets go of the terminal (macOS kept a handle until now, so that the output
			// still in it is read before the master sees the end).
			s.pty.Release(DrainTime)
		}
		// The process ended. Its descendants may still hold the pipes: what they write within
		// DrainTime is still sent, then the pipes are closed under them.
		drained := make(chan struct{})
		go func() {
			copies.Wait()
			close(drained)
		}()
		select {
		case <-drained:
		case <-time.After(DrainTime):
		}
		_ = stdout.Close() // ends a copy that is still waiting; closed twice is fine
		if stderr != nil {
			_ = stderr.Close()
		}
		copies.Wait()
		endGroup(proc)
		remove()
		logger.Info("session ended", "status", status, "signal", signal)
		s.finish(status, signal)
	}()
	return true
}

// startSubsystem starts a subsystem; Cave has one, sftp (docs/SPEC.md §9.7), which it serves
// itself on the channel. Like a shell or a command, it is the channel's one start. A terminal
// asked before it is not used.
func (s *session) startSubsystem(name string) bool {
	if s.started || name != "sftp" || s.h.sftp == nil || !s.h.opts.Settings.Snapshot().Bool(settings.SFTPEnabled) {
		return false
	}
	s.started, s.subsystem = true, true
	logger := s.h.opts.Logger.With("connection", s.conn.ID(), "person", s.conn.PersonName(), "kind", string(server.ActivitySFTP))
	logger.Info("session started")
	remove := s.conn.AddActivity(server.ActivitySFTP, "")
	go func() {
		err := s.h.sftp.Serve(s.ctx, s.channel, s.recordTransfer)
		remove()
		status := 0
		if err != nil {
			// The client broke the protocol, or the channel failed under it.
			logger.Warn("session ended", "status", startFailedStatus, "err", err)
			status = startFailedStatus
		} else {
			logger.Info("session ended", "status", status)
		}
		s.finish(status, "")
	}()
	return true
}

// recordTransfer puts a file the SFTP session read or wrote into the history, as this
// connection's.
func (s *session) recordTransfer(t sftp.Transfer) {
	s.h.opts.History.AddTransfer(history.Transfer{
		ConnectionID: s.conn.ID(), PersonID: s.conn.PersonID(), PersonName: s.conn.PersonName(), Path: t.Path,
		OpenedAt: t.OpenedAt, ClosedAt: t.ClosedAt, Downloaded: t.Downloaded, Uploaded: t.Uploaded, Created: t.Created,
		Result: history.Result(t.Result),
	})
}

// finish tells the client how the process ended and lets serve return.
func (s *session) finish(status int, signal string) {
	// A request that cannot be sent means the channel is gone, which serve notices by itself.
	if signal != "" {
		_, _ = s.channel.SendRequest("exit-signal", false, ssh.Marshal(struct {
			Signal     string
			CoreDumped bool
			Error      string
			Lang       string
		}{signal, false, "", ""}))
	} else {
		_, _ = s.channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(status)}))
	}
	close(s.done)
}

// hangUp tells a process that is still running that its session is gone (docs/SPEC.md §9.4):
// SIGHUP to its group, once. Cave never kills it; a job that ignores the signal goes on, as
// with sshd.
func (s *session) hangUp() {
	if s.proc == nil {
		return
	}
	select {
	case <-s.proc.exited:
	default:
		if s.pty != nil {
			hangUpTerminal(s.proc, s.pty)
			return
		}
		hangUpProcess(s.proc)
	}
}

// spawn starts the process and returns what to read its output from: the terminal when the
// session has one (and then no separate error stream), else the read ends of two pipes. Its
// input is what the client sends on the channel, until the client ends it.
func (h *handler) spawn(shell, command string, login bool, env []string, stdin io.Reader, terminal *pty.Pty) (*process, io.ReadCloser, io.ReadCloser, error) {
	cmd, err := newCommand(shell, command, login, terminal != nil)
	if err != nil {
		return nil, nil, nil, err
	}
	cmd.Env = env
	cmd.Dir = h.opts.User.Home
	if info, statErr := os.Stat(cmd.Dir); statErr != nil || !info.IsDir() {
		cmd.Dir = rootDir
	}

	// files are the pipes' ends, in the order: input (read, write), output, error.
	var files []*os.File
	closeAll := func() {
		for _, f := range files {
			_ = f.Close() // nothing was written through them
		}
	}
	if terminal != nil {
		slave := terminal.Slave()
		cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	} else {
		for i := 0; i < 3; i++ {
			r, w, err := h.pipe()
			if err != nil {
				closeAll()
				return nil, nil, nil, errors.NewErr(TagSessionPipeFailed, err)
			}
			files = append(files, r, w)
		}
		cmd.Stdin, cmd.Stdout, cmd.Stderr = files[0], files[3], files[5]
	}
	proc, err := startCommand(cmd, terminal)
	if err != nil {
		closeAll()
		return nil, nil, nil, errors.NewErr(TagSessionStartFailed, err).SetMetadata("shell", sanitize.Display(shell, maxCommandLog))
	}
	proc.exited = make(chan struct{})
	proc.group, err = startGroup(proc)
	if err != nil {
		abandon(proc) // the process runs outside its group: it is not left behind
		closeAll()
		return nil, nil, nil, err
	}

	if terminal != nil {
		// The process has the terminal; Cave keeps only the master side.
		terminal.CloseSlave()
		go func() {
			// It ends when the client ends its side or the terminal is closed. A terminal has no
			// end of input to pass on: the client's end of file changes nothing, as with sshd.
			_, _ = io.Copy(terminal.Input(), stdin)
		}()
		return proc, terminal.Output(), nil, nil
	}
	// The process has its ends of the pipes; Cave keeps the others.
	inW, outR, errR := files[1], files[2], files[4]
	_ = files[0].Close()
	_ = files[3].Close()
	_ = files[5].Close()
	go func() {
		_, _ = io.Copy(inW, stdin) // ends when the client ends its side, or the process stops reading
		_ = inW.Close()            // the process sees the end of its input
	}()
	return proc, outR, errR, nil
}
