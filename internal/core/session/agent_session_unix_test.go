//go:build linux || darwin

package session

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/agentfwd"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/server/servertest"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// agentFixture is a server whose sessions may get a forwarded agent, with alice connected and a
// real ssh-agent of the test on her side that holds a key of its own.
type agentFixture struct {
	*fixture
	// runtime is the folder the sessions' agent sockets are made in.
	runtime string
	// agentKey is the key that only the client's agent has.
	agentKey sshtest.Key
	// agentSocket is the socket of the client's agent.
	agentSocket string
}

// newAgentFixture starts the server with a runtime folder whose path is short enough for
// sockets. With forward, alice's client carries auth-agent channels to its agent, as "ssh -A"
// does.
func newAgentFixture(t *testing.T, forward bool) *agentFixture {
	t.Helper()
	sshtest.Require(t)
	a := &agentFixture{runtime: filepath.Join(sshtest.ShortDir(t), "cave"), agentKey: sshtest.NewKey(t, sshtest.Ed25519)}
	a.agentSocket = sshtest.StartAgent(t, a.agentKey)
	a.fixture = newFixture(t, func(f *servertest.Fixture) {
		opts := options(f)
		opts.RuntimeDir = a.runtime
		Register(f.Server, opts)
	})
	if forward {
		if err := agent.ForwardToRemote(a.client, a.agentSocket); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

// sockets returns the agent sockets that exist in the runtime folder.
func (a *agentFixture) sockets() []string {
	a.t.Helper()
	found, err := filepath.Glob(filepath.Join(a.runtime, "agent-*", agentfwd.SocketName))
	if err != nil {
		a.t.Fatal(err)
	}
	return found
}

// waitNoSockets waits until no agent socket and no folder of one is left.
func (a *agentFixture) waitNoSockets() {
	a.t.Helper()
	deadline := time.Now().Add(servertest.WaitFor)
	for {
		entries, err := os.ReadDir(a.runtime)
		if err == nil && len(entries) == 0 || os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			a.t.Fatalf("the runtime folder still has %v, %v", entries, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAgentForwarding(t *testing.T) {
	a := newAgentFixture(t, true)
	sess, err := a.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	if err := agent.RequestAgentForwarding(sess); err != nil {
		t.Fatalf("auth-agent-req: %v", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	// The session prints where its agent is and what the agent has, then waits.
	if err := sess.Start(`echo "$SSH_AUTH_SOCK"; ssh-add -l; echo "status=$?"; cat`); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	line := func() string {
		text, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("the session's output ended after %q: %v", text, err)
		}
		return strings.TrimSpace(text)
	}
	socket := line()
	listed := line()
	if status := line(); status != "status=0" {
		t.Errorf("ssh-add ended with %s", status)
	}

	// The variable names the session's own socket, not Cave's own agent.
	sockets := a.sockets()
	if len(sockets) != 1 || socket != sockets[0] {
		t.Fatalf("SSH_AUTH_SOCK is %q, the runtime folder has %v", socket, sockets)
	}
	for name, want := range map[string]os.FileMode{socket: os.ModeSocket | 0o600, filepath.Dir(socket): os.ModeDir | 0o700} {
		info, err := os.Lstat(name)
		if err != nil || info.Mode() != want {
			t.Errorf("%s has the mode %v, %v, want %v", name, info, err, want)
		}
	}
	// The agent behind it is the client's: it lists the key that only that agent has.
	if want := ssh.FingerprintSHA256(a.agentKey.Public); !strings.Contains(listed, want) {
		t.Errorf("ssh-add -l printed %q, want the key %s", listed, want)
	}
	// The registry shows the forwarding beside the command.
	kinds := map[server.ActivityKind]bool{}
	for _, activity := range a.WaitConnections(1)[0].Activities {
		kinds[activity.Kind] = true
	}
	if !kinds[server.ActivityAgent] || !kinds[server.ActivityExec] {
		t.Errorf("the registry shows %v", kinds)
	}

	// The session ends: the socket and its folder are removed, and the registry is empty.
	_ = stdin.Close()
	if err := sess.Wait(); err != nil {
		t.Errorf("the session ended with %v", err)
	}
	a.waitNoSockets()
	deadline := time.Now().Add(servertest.WaitFor)
	for len(a.WaitConnections(1)[0].Activities) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the registry still shows %+v", a.WaitConnections(1)[0].Activities)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAgentForwardingTurnedOff(t *testing.T) {
	a := newAgentFixture(t, true)
	a.Set(string(settings.ForwardingAgent), false)
	sess, err := a.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	if err := agent.RequestAgentForwarding(sess); err == nil {
		t.Fatal("agent forwarding was granted although forwarding.agent is off")
	}
	if got := a.sockets(); len(got) != 0 {
		t.Errorf("a socket was made: %v", got)
	}
	// The session goes on without an agent: no variable, and above all not Cave's own agent
	// (caveEnv has one).
	got := finish(t, sess, "", func() error { return sess.Run(`echo "sock=$SSH_AUTH_SOCK"`) })
	if got.stdout != "sock=\n" {
		t.Errorf("%+v", got)
	}
	if log := a.Log.String(); !strings.Contains(log, `"msg":"agent forwarding refused"`) || !strings.Contains(log, "forwarding.agent is off") {
		t.Errorf("the log does not say why:\n%s", log)
	}

	// On again: granted again.
	a.Set(string(settings.ForwardingAgent), true)
	again, err := a.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = again.Close() }()
	if err := agent.RequestAgentForwarding(again); err != nil {
		t.Errorf("on again: %v", err)
	}
}

func TestWithoutARequestThereIsNoAgent(t *testing.T) {
	a := newAgentFixture(t, true)
	got := a.run(`echo "sock=$SSH_AUTH_SOCK"`, "")
	if got.stdout != "sock=\n" || len(a.sockets()) != 0 {
		t.Errorf("%+v, sockets %v", got, a.sockets())
	}
}

func TestAgentRequestsThatAreRefused(t *testing.T) {
	a := newAgentFixture(t, true)
	request := func(sess *ssh.Session) bool {
		ok, err := sess.SendRequest("auth-agent-req@openssh.com", true, nil)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	t.Run("a second request", func(t *testing.T) {
		sess, err := a.client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sess.Close() }()
		if !request(sess) {
			t.Fatal("the first request was refused")
		}
		if request(sess) {
			t.Error("a second request was granted")
		}
		if got := a.sockets(); len(got) != 1 {
			t.Errorf("the runtime folder has %v, want one socket", got)
		}
	})
	a.waitNoSockets()
	t.Run("after the start", func(t *testing.T) {
		sess, err := a.client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sess.Close() }()
		stdin, err := sess.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := sess.Start("cat"); err != nil {
			t.Fatal(err)
		}
		if request(sess) {
			t.Error("a request after the start was granted")
		}
		_ = stdin.Close()
		if err := sess.Wait(); err != nil {
			t.Errorf("the command ended with %v", err)
		}
	})
	t.Run("a socket that cannot be made", func(t *testing.T) {
		// The runtime folder is replaced by a file while Cave runs.
		if err := os.RemoveAll(a.runtime); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(a.runtime, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(a.runtime) })
		sess, err := a.client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sess.Close() }()
		if request(sess) {
			t.Error("the request was granted without a socket")
		}
		logged := a.LoggedError("agent forwarding refused")
		if logged.Tag != agentfwd.TagRuntimeDirFailed || !logged.HasRoute("Listen->listen.AgentRuntimeDirFailed") {
			t.Errorf("logged %+v with the route %s", logged, logged.Route())
		}
		// The session goes on without an agent.
		if got := finish(t, sess, "", func() error { return sess.Run(`echo "sock=$SSH_AUTH_SOCK"`) }); got.stdout != "sock=\n" {
			t.Errorf("%+v", got)
		}
	})
}

func TestAClientThatAsksForForwardingAndOffersNoAgent(t *testing.T) {
	// The client sends the request and then refuses the channels: it has no agent.
	a := newAgentFixture(t, false)
	sess, err := a.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	if err := agent.RequestAgentForwarding(sess); err != nil {
		t.Fatal(err)
	}
	got := finish(t, sess, "", func() error { return sess.Run("ssh-add -l") })
	if got.status == 0 || got.stdout != "" {
		t.Errorf("ssh-add listed keys without an agent: %+v", got)
	}
	logged := a.LoggedError("the client did not open its agent")
	if logged.Tag != agentfwd.TagOpenFailed || !logged.HasRoute("Socket.carry.AgentOpenFailed") {
		t.Errorf("logged %+v with the route %s", logged, logged.Route())
	}
	a.waitNoSockets()
}

func TestTheEndOfTheConnectionRemovesTheAgentSocket(t *testing.T) {
	for name, end := range map[string]func(a *agentFixture, sess *ssh.Session){
		"the client closes the channel": func(_ *agentFixture, sess *ssh.Session) { _ = sess.Close() },
		"the connection ends":           func(a *agentFixture, _ *ssh.Session) { _ = a.client.Close() },
		"the user disconnects it":       func(a *agentFixture, _ *ssh.Session) { a.Server.DisconnectAll() },
	} {
		t.Run(name, func(t *testing.T) {
			a := newAgentFixture(t, true)
			sess, err := a.client.NewSession()
			if err != nil {
				t.Fatal(err)
			}
			if err := agent.RequestAgentForwarding(sess); err != nil {
				t.Fatal(err)
			}
			if err := sess.Start("sleep 600"); err != nil {
				t.Fatal(err)
			}
			if got := a.sockets(); len(got) != 1 {
				t.Fatalf("the runtime folder has %v, want one socket", got)
			}
			end(a, sess)
			a.waitNoSockets()
		})
	}
}

func TestOpenSSHAgentForwarding(t *testing.T) {
	f := newOpenSSHFixtureWith(t, filepath.Join(sshtest.ShortDir(t), "cave"))
	// An agent of the test with a key that is in no file ssh is given: it is offered to Cave
	// (which does not know it) and forwarded with "-A".
	agentKey := sshtest.NewKey(t, sshtest.Ed25519)
	f.ssh.UseAgent(sshtest.StartAgent(t, agentKey))
	fingerprint := ssh.FingerprintSHA256(agentKey.Public)

	got := f.ssh.SSH("", "alice", f.key, []string{"-A"}, `ssh-add -l; echo "status=$?"; test -S "$SSH_AUTH_SOCK" && echo "a socket"`)
	if !strings.Contains(got.Stdout, fingerprint) || !strings.Contains(got.Stdout, "status=0\na socket\n") {
		t.Errorf("ssh -A: %+v, want the key %s listed", got, fingerprint)
	}
	t.Logf("in the session, ssh-add -l listed the key that only the client's agent has")

	// Without "-A" there is no agent in the session.
	got = f.ssh.SSH("", "alice", f.key, nil, `echo "sock=$SSH_AUTH_SOCK"`)
	if got.Stdout != "sock=\n" {
		t.Errorf("without -A: %+v", got)
	}
	// With forwarding.agent off, "-A" gives none either, and the session still runs.
	f.Set(string(settings.ForwardingAgent), false)
	got = f.ssh.SSH("", "alice", f.key, []string{"-A"}, `echo "sock=$SSH_AUTH_SOCK"`)
	if got.Stdout != "sock=\n" {
		t.Errorf("with forwarding.agent off: %+v", got)
	}
	t.Logf("without -A, and with forwarding.agent off, the session has no SSH_AUTH_SOCK")
}
