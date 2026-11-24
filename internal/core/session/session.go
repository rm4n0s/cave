package session

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/rm4n0s/cave/internal/core/sanitize"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// The Tags of this package's errors.
const (
	TagSessionStartFailed = "SessionStartFailed"
	TagSessionPipeFailed  = "SessionPipeFailed"
	TagSessionUnsupported = "SessionUnsupported"
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
)

// Options are what the session handler needs. Every field is required.
type Options struct {
	// Settings has session.shell and session.acceptEnv.
	Settings *settings.Store
	// Logger gets the starts and ends of sessions.
	Logger *slog.Logger
	// User is the OS user the sessions run as.
	User User
	// Environ is Cave's own environment, as os.Environ gives it.
	Environ []string
}

// handler serves session channels.
type handler struct {
	opts Options
	// pipe makes a pipe; tests replace it to make it fail.
	pipe func() (r, w *os.File, err error)
}

// Register makes the server serve session channels.
func Register(srv *server.Server, opts Options) {
	h := &handler{opts: opts, pipe: os.Pipe}
	srv.HandleChannel("session", h.serve)
}

// session is one session channel.
type session struct {
	h       *handler
	conn    *server.Conn
	channel ssh.Channel
	// env holds the variables the client set.
	env map[string]string
	// proc is the running process, or nil before one was started.
	proc *process
	// done is closed when the process ended and its output and status were sent.
	done chan struct{}
}

// process is the process of a session.
type process struct {
	cmd *exec.Cmd
	// exited is closed when the process ended.
	exited chan struct{}
}

// serve runs one session channel until the process it started has ended and reported, or the
// client or the connection ended it.
func (h *handler) serve(_ context.Context, conn *server.Conn, newChannel ssh.NewChannel) {
	channel, requests, err := newChannel.Accept()
	if err != nil {
		return // the connection is gone
	}
	s := &session{h: h, conn: conn, channel: channel, env: map[string]string{}, done: make(chan struct{})}
	defer func() { _ = channel.Close() }() // a close that fails means the channel is closed already
	for {
		select {
		case request, ok := <-requests:
			if !ok {
				// The channel ended before the process did: the client closed it, or the
				// connection is gone.
				s.hangUp()
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
			ok = signalProcess(s.proc.cmd, payload.Signal)
		}
	case "break":
		ok = true
		// TODO(step 2.5): pty-req and window-change.
		// TODO(step 3.1): the sftp subsystem.
		// TODO(step 3.5): auth-agent-req@openssh.com.
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
	if s.proc != nil || !validEnvName(v.Name) || len(v.Name)+len(v.Value) > MaxEnvBytes {
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

// start starts the session's process: a login shell, or the shell running a command. Only the
// first start of a channel does anything; it reports whether the request is granted. A process
// that cannot start is told to the client on the error stream with status 255, as when sshd's
// child cannot run the shell.
func (s *session) start(command string, login bool) bool {
	if s.proc != nil {
		return false
	}
	snap := s.h.opts.Settings.Snapshot()
	shell := PickShell(snap.String(settings.SessionShell), s.h.opts.User, s.h.opts.Environ)
	env := BuildEnv(EnvInput{
		User: s.h.opts.User.Name, Home: s.h.opts.User.Home, Shell: shell, Cave: s.h.opts.Environ, Client: s.env,
		Remote: s.conn.RemoteAddr(), Local: s.conn.LocalAddr(), Person: s.conn.PersonName(),
	})
	kind, detail := server.ActivityExec, command
	if login {
		kind, detail = server.ActivityShell, ""
	}
	logCommand := sanitize.Display(command, maxCommandLog)
	logger := s.h.opts.Logger.With("connection", s.conn.ID(), "person", s.conn.PersonName(), "kind", string(kind), "command", logCommand)

	proc, stdout, stderr, err := s.h.spawn(shell, command, login, env, s.channel)
	if err != nil {
		logger.Error("session could not start", "err", err)
		s.proc = &process{} // the channel had its one start
		go func() {
			_, _ = io.WriteString(s.channel.Stderr(), "cave: the session could not start: "+err.Error()+"\r\n")
			s.finish(startFailedStatus, "")
		}()
		return true
	}
	s.proc = proc
	logger.Info("session started", "pid", proc.cmd.Process.Pid)
	remove := s.conn.AddActivity(kind, detail)

	// The output goes to the client until the pipes end: when the process and everything it
	// started have closed them.
	var copies sync.WaitGroup
	copies.Add(2)
	go func() {
		defer copies.Done()
		_, _ = io.Copy(s.channel, stdout) // an error is the channel or the pipe ending
	}()
	go func() {
		defer copies.Done()
		_, _ = io.Copy(s.channel.Stderr(), stderr) // as above
	}()
	go func() {
		status, signal := wait(proc.cmd)
		close(proc.exited)
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
		_ = stderr.Close()
		copies.Wait()
		remove()
		logger.Info("session ended", "status", status, "signal", signal)
		s.finish(status, signal)
	}()
	return true
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
	if s.proc == nil || s.proc.cmd == nil {
		return
	}
	select {
	case <-s.proc.exited:
	default:
		hangUpProcess(s.proc.cmd)
	}
}

// spawn starts the process with pipes for its three streams, and returns the read ends of its
// output. Its input is what the client sends on the channel, until the client ends it.
func (h *handler) spawn(shell, command string, login bool, env []string, stdin io.Reader) (*process, *os.File, *os.File, error) {
	var files []*os.File
	closeAll := func() {
		for _, f := range files {
			_ = f.Close() // nothing was written through them
		}
	}
	ends := make([]*os.File, 0, 6)
	for i := 0; i < 3; i++ {
		r, w, err := h.pipe()
		if err != nil {
			closeAll()
			return nil, nil, nil, errors.NewErr(TagSessionPipeFailed, err)
		}
		files = append(files, r, w)
		ends = append(ends, r, w)
	}
	inR, inW, outR, outW, errR, errW := ends[0], ends[1], ends[2], ends[3], ends[4], ends[5]

	cmd, err := newCommand(shell, command, login)
	if err != nil {
		closeAll()
		return nil, nil, nil, err
	}
	cmd.Env = env
	cmd.Dir = h.opts.User.Home
	if info, statErr := os.Stat(cmd.Dir); statErr != nil || !info.IsDir() {
		cmd.Dir = rootDir
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, errW
	if err := cmd.Start(); err != nil {
		closeAll()
		return nil, nil, nil, errors.NewErr(TagSessionStartFailed, err).SetMetadata("shell", sanitize.Display(shell, maxCommandLog))
	}
	// The process has its ends of the pipes; Cave keeps the others.
	_ = inR.Close()
	_ = outW.Close()
	_ = errW.Close()
	go func() {
		_, _ = io.Copy(inW, stdin) // ends when the client ends its side, or the process stops reading
		_ = inW.Close()            // the process sees the end of its input
	}()
	return &process{cmd: cmd, exited: make(chan struct{})}, outR, errR, nil
}
