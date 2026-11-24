package forward

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/server/servertest"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/cave/internal/core/sshtest"
)

// dialSocket connects to a Unix socket that ssh is about to make, once it is there.
func dialSocket(t *testing.T, socket string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(servertest.WaitFor)
	for {
		conn, err := net.Dial("unix", socket)
		if err == nil {
			t.Cleanup(func() { _ = conn.Close() })
			return conn
		}
		if time.Now().After(deadline) {
			t.Fatalf("ssh did not make its socket: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// forwards waits until the registry shows n forwards made for clients, and returns their
// targets.
func (f *fixture) forwards(n int) []string {
	f.t.Helper()
	deadline := time.Now().Add(servertest.WaitFor)
	for {
		var targets []string
		for _, conn := range f.Server.Connections() {
			for _, activity := range conn.Activities {
				if activity.Kind == server.ActivityLocalForward {
					targets = append(targets, activity.Detail)
				}
			}
		}
		if len(targets) == n {
			return targets
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("the registry shows the forwards %v, want %d", targets, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The real ssh listens on a Unix socket under a folder of the test, not on a port: a test never
// listens on a port it chose itself.

func TestOpenSSHLocalForward(t *testing.T) {
	echo := startEcho(t, "tcp4")
	f := newFixture(t, nil)
	o := f.OpenSSH()
	socket := filepath.Join(sshtest.ShortDir(t), "local.sock")
	stop := o.Start("alice", f.key, "-o", "ExitOnForwardFailure=yes", "-L", socket+":"+echo.addr)

	conn := dialSocket(t, socket)
	exchange(t, conn, "through ssh -L")
	if targets := f.forwards(1); targets[0] != echo.addr {
		t.Errorf("the registry shows the forward to %v, want %s", targets, echo.addr)
	}
	// The end of sending goes through, and the answer after it comes back.
	if err := conn.(*net.UnixConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	rest, err := io.ReadAll(conn)
	if err != nil || string(rest) != "bye" {
		t.Errorf("after the end of sending %q came back, %v", rest, err)
	}
	f.forwards(0)
	t.Logf("a message went through the forward to the echo server on 127.0.0.1 and came back; the registry showed the forward")
	if stderr := stop(); stderr != "" {
		t.Errorf("ssh wrote %q", stderr)
	}
}

func TestOpenSSHDynamicForward(t *testing.T) {
	echo := startEcho(t, "tcp4")
	f := newFixture(t, nil)
	o := f.OpenSSH()
	socket := filepath.Join(sshtest.ShortDir(t), "socks.sock")
	stop := o.Start("alice", f.key, "-o", "ExitOnForwardFailure=yes", "-D", socket)

	conn := dialSocket(t, socket)
	// A SOCKS5 client, by hand: no authentication, then "connect to 127.0.0.1:<port>".
	steps := []struct {
		name       string
		send, want []byte
	}{
		{"the greeting", []byte{5, 1, 0}, []byte{5, 0}},
		{"the request", []byte{5, 1, 0, 1, 127, 0, 0, 1, byte(echo.port >> 8), byte(echo.port)}, []byte{5, 0, 0, 1}},
	}
	for _, step := range steps {
		if _, err := conn.Write(step.send); err != nil {
			t.Fatal(err)
		}
		got := make([]byte, len(step.want))
		if _, err := io.ReadFull(conn, got); err != nil || string(got) != string(step.want) {
			t.Fatalf("%s was answered with %v, %v, want %v", step.name, got, err, step.want)
		}
	}
	// The rest of the answer to the request: the address and the port ssh bound, which say
	// nothing here.
	if _, err := io.ReadFull(conn, make([]byte, 6)); err != nil {
		t.Fatal(err)
	}
	exchange(t, conn, "through ssh -D")
	if targets := f.forwards(1); targets[0] != echo.addr {
		t.Errorf("the registry shows the forward to %v, want %s", targets, echo.addr)
	}
	t.Logf("a SOCKS5 request for the echo server on 127.0.0.1 was answered, and a message came back through it")
	_ = conn.Close()
	f.forwards(0)
	if stderr := stop(); stderr != "" {
		t.Errorf("ssh wrote %q", stderr)
	}
}

func TestOpenSSHLocalForwardIsRefusedWhenTurnedOff(t *testing.T) {
	echo := startEcho(t, "tcp4")
	f := newFixture(t, nil)
	f.Set(string(settings.ForwardingLocal), false)
	o := f.OpenSSH()
	socket := filepath.Join(sshtest.ShortDir(t), "local.sock")
	stop := o.Start("alice", f.key, "-L", socket+":"+echo.addr)

	// ssh takes the connection on its side, asks Cave, and closes it when Cave refuses.
	conn := dialSocket(t, socket)
	if _, err := io.WriteString(conn, "anybody there?"); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(servertest.WaitFor)); err != nil {
		t.Fatal(err)
	}
	// Closed or reset, as ssh pleases: nothing comes through.
	if got, _ := io.ReadAll(conn); len(got) != 0 {
		t.Errorf("through a forward that is turned off came %q", got)
	}
	if echo.accepted.Load() != 0 || f.dials.Load() != 0 {
		t.Errorf("the echo server got %d connections, Cave dialed %d times", echo.accepted.Load(), f.dials.Load())
	}
	// ssh keeps the reason to itself at the log level the tests run it with; Cave's log has it.
	log := f.Log.String()
	if !strings.Contains(log, `"msg":"forward refused"`) || !strings.Contains(log, "forwarding.local is off") {
		t.Errorf("Cave's log does not say why:\n%s", log)
	}
	f.forwards(0)
	t.Logf("with forwarding.local off nothing came through, the echo server got no connection, and Cave logged the refusal")
	stop()
	if _, err := os.Stat(socket); err == nil {
		_ = os.Remove(socket) // ssh was killed before it could remove its socket
	}
}

func TestOpenSSHRemoteForward(t *testing.T) {
	echo := startEcho(t, "tcp4")
	f := newFixture(t, nil)
	o := f.OpenSSH()
	// Port 0: Cave lets the system choose. ssh is told the port; the test reads it from the
	// registry, which shows what Cave listens on.
	stop := o.Start("alice", f.key, "-o", "ExitOnForwardFailure=yes", "-R", "0:"+echo.addr)
	address := f.remoteForwards(1)[0]
	if !strings.HasPrefix(address, "127.0.0.1:") {
		t.Fatalf("Cave listens on %s, want a port of 127.0.0.1", address)
	}

	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	exchange(t, conn, "through ssh -R")
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(servertest.WaitFor)); err != nil {
		t.Fatal(err)
	}
	if rest, err := io.ReadAll(conn); err != nil || string(rest) != "bye" {
		t.Errorf("after the end of sending %q came back, %v", rest, err)
	}
	if echo.accepted.Load() != 1 {
		t.Errorf("the echo server got %d connections", echo.accepted.Load())
	}
	t.Logf("Cave listened on a port of 127.0.0.1 for ssh; a message sent there reached the echo server behind ssh and came back")

	// ssh ends: Cave stops listening.
	if stderr := stop(); stderr != "" {
		t.Errorf("ssh wrote %q", stderr)
	}
	f.remoteForwards(0)
	refused(t, address)
}

func TestOpenSSHRemoteForwardIsRefusedWhenTurnedOff(t *testing.T) {
	echo := startEcho(t, "tcp4")
	f := newFixture(t, nil)
	f.Set(string(settings.ForwardingRemote), false)
	o := f.OpenSSH()
	// With ExitOnForwardFailure, ssh gives up when Cave refuses to listen.
	got := o.SSH("", "alice", f.key, []string{"-N", "-o", "ExitOnForwardFailure=yes", "-R", "0:" + echo.addr})
	if got.ExitCode == 0 || !strings.Contains(got.Stderr, "remote port forwarding failed") {
		t.Errorf("ssh -R with forwarding.remote off: %+v", got)
	}
	f.remoteForwards(0)
	if echo.accepted.Load() != 0 {
		t.Errorf("the echo server got %d connections", echo.accepted.Load())
	}
	t.Logf("with forwarding.remote off, ssh reports: %s", strings.TrimSpace(got.Stderr))
}
