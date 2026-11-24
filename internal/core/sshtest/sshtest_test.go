package sshtest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// testServer is a minimal SSH server for the harness's own tests (not Cave's server): it accepts
// one user with one key, and answers every exec request with a line that repeats the command
// and what it read, with exit status 0, or 3 for the command "fail".
type testServer struct {
	addr    string
	hostKey ssh.PublicKey
}

// startServer starts the minimal server on a loopback port the OS chooses.
func startServer(t *testing.T, user string, allowed ssh.PublicKey) *testServer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	host, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if conn.User() == user && bytes.Equal(key.Marshal(), allowed.Marshal()) {
				return &ssh.Permissions{}, nil
			}
			return nil, os.ErrPermission
		},
	}
	config.AddHostKey(host)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
		wg.Wait()
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				serveConn(conn, config)
			}()
		}
	}()
	return &testServer{addr: listener.Addr().String(), hostKey: host.PublicKey()}
}

// serveConn serves one connection of the minimal server.
func serveConn(conn net.Conn, config *ssh.ServerConfig) {
	defer func() { _ = conn.Close() }()
	server, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer func() { _ = server.Close() }()
	go ssh.DiscardRequests(requests)
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}
		channel, reqs, err := newChannel.Accept()
		if err != nil {
			return
		}
		go func() {
			defer func() { _ = channel.Close() }()
			for req := range reqs {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)
					continue
				}
				var payload struct{ Command string }
				_ = ssh.Unmarshal(req.Payload, &payload)
				_ = req.Reply(true, nil)
				input := new(bytes.Buffer)
				_, _ = input.ReadFrom(channel)
				status := uint32(0)
				if payload.Command == "fail" {
					status = 3
				}
				_, _ = channel.Write([]byte("ran: " + payload.Command + "; read: " + input.String() + "\n"))
				_, _ = channel.Stderr().Write([]byte("on stderr\n"))
				_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
				return
			}
		}()
	}
}

func TestNewKey(t *testing.T) {
	tests := []struct {
		kind     KeyKind
		wantType string
	}{{Ed25519, "ssh-ed25519"}, {ECDSA, "ecdsa-sha2-nistp256"}, {RSA, "ssh-rsa"}}
	for _, tt := range tests {
		t.Run(tt.wantType, func(t *testing.T) {
			key := NewKey(t, tt.kind)
			if key.Public.Type() != tt.wantType || !strings.HasPrefix(key.AuthorizedKey, tt.wantType+" ") {
				t.Errorf("type %q, line %q", key.Public.Type(), key.AuthorizedKey)
			}
			if !bytes.Equal(key.Signer.PublicKey().Marshal(), key.Public.Marshal()) {
				t.Error("the signer and the public key differ")
			}
			// The private file is what OpenSSH needs: the same key, with mode 0600.
			data, err := os.ReadFile(key.PrivatePath)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := ssh.ParsePrivateKey(data)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(parsed.PublicKey().Marshal(), key.Public.Marshal()) {
				t.Error("the private file holds another key")
			}
			if runtime.GOOS != "windows" {
				info, err := os.Stat(key.PrivatePath)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0o600 {
					t.Errorf("the private file's mode is %o", info.Mode().Perm())
				}
			}
		})
	}
	if a, b := NewKey(t, Ed25519), NewKey(t, Ed25519); a.AuthorizedKey == b.AuthorizedKey || a.PrivatePath == b.PrivatePath {
		t.Error("two keys are the same")
	}
}

func TestTheClientInsideTheTest(t *testing.T) {
	key, stranger := NewKey(t, Ed25519), NewKey(t, Ed25519)
	server := startServer(t, "ada", key.Public)

	client := Dial(t, server.addr, "ada", key, server.hostKey)
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	session.Stdin = strings.NewReader("some input")
	out, err := session.Output("uptime")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "ran: uptime; read: some input\n" {
		t.Errorf("output = %q", out)
	}

	// A wrong key, a wrong name and a wrong host key are errors, not test failures.
	if _, err := DialErr(t, server.addr, ClientConfig("ada", server.hostKey, stranger)); err == nil {
		t.Error("a key the server does not know logged in")
	}
	if _, err := DialErr(t, server.addr, ClientConfig("eve", server.hostKey, key)); err == nil {
		t.Error("a name the server does not know logged in")
	}
	_, err = DialErr(t, server.addr, ClientConfig("ada", stranger.Public, key))
	if err == nil || !strings.Contains(err.Error(), "host key mismatch") {
		t.Errorf("a wrong host key: %v", err)
	}
	// Several keys: the client tries each.
	if _, err := DialErr(t, server.addr, ClientConfig("ada", server.hostKey, stranger, key)); err != nil {
		t.Errorf("the second key of two did not log in: %v", err)
	}
}

func TestOpenSSHAgainstTheServer(t *testing.T) {
	key, stranger := NewKey(t, Ed25519), NewKey(t, Ed25519)
	server := startServer(t, "ada", key.Public)
	o := NewOpenSSH(t, server.addr, server.hostKey)

	res := o.SSH("typed input", "ada", key, nil, "uptime", "-s")
	if res.ExitCode != 0 || res.Stdout != "ran: uptime -s; read: typed input\n" || res.Stderr != "on stderr\n" {
		t.Errorf("ssh: %+v", res)
	}
	if res := o.SSH("", "ada", key, nil, "fail"); res.ExitCode != 3 {
		t.Errorf("the exit status is %d, want 3", res.ExitCode)
	}
	res = o.SSH("", "ada", stranger, nil, "uptime")
	if res.ExitCode != 255 || !strings.Contains(res.Stderr, "Permission denied (publickey)") {
		t.Errorf("a wrong key: %+v", res)
	}

	// A host key that is not the one in known_hosts is refused: nothing is learned or asked.
	other := startServer(t, "ada", key.Public)
	wrong := NewOpenSSH(t, other.addr, server.hostKey)
	res = wrong.SSH("", "ada", key, nil, "uptime")
	if res.ExitCode != 255 || !strings.Contains(res.Stderr, "Host key verification failed") {
		t.Errorf("a wrong host key: %+v", res)
	}
	hosts, err := os.ReadFile(wrong.knownHosts())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(hosts), "\n") != 1 {
		t.Errorf("ssh changed the known_hosts file:\n%s", hosts)
	}
}

func TestEveryCommandIsIsolated(t *testing.T) {
	key := NewKey(t, Ed25519)
	server := startServer(t, "ada", key.Public)
	t.Setenv("SSH_AUTH_SOCK", "/home/someone/agent.sock")
	t.Setenv("HOME", "/home/someone")
	o := NewOpenSSH(t, server.addr, server.hostKey)

	options := strings.Join(o.Options(key), " ")
	for _, want := range []string{
		"-F none", "BatchMode=yes", "IdentitiesOnly=yes", "IdentityAgent=none", "UserKnownHostsFile=" + o.knownHosts(),
		"GlobalKnownHostsFile=/dev/null", "StrictHostKeyChecking=yes", "PreferredAuthentications=publickey",
		"PasswordAuthentication=no", "ControlPath=none", "-i " + key.PrivatePath,
	} {
		if !strings.Contains(options, want) {
			t.Errorf("the options lack %q: %s", want, options)
		}
	}
	env := o.Env()
	for _, entry := range env {
		if strings.HasPrefix(entry, "SSH_AUTH_SOCK=") || strings.Contains(entry, "/home/someone") {
			t.Errorf("the environment has %q", entry)
		}
	}
	if !strings.HasPrefix(env[0], "HOME="+o.dir) {
		t.Errorf("the home is %q", env[0])
	}

	// With an agent of the test, the options and the environment name its socket.
	o.UseAgent("/tmp/test-agent.sock")
	if got := strings.Join(o.Options(key), " "); !strings.Contains(got, "IdentityAgent=/tmp/test-agent.sock") ||
		strings.Contains(got, "IdentityAgent=none") || !strings.Contains(got, "IdentitiesOnly=no") {
		t.Errorf("the options with an agent: %s", got)
	}
	if got := strings.Join(o.Env(), " "); !strings.Contains(got, "SSH_AUTH_SOCK=/tmp/test-agent.sock") {
		t.Errorf("the environment with an agent: %s", got)
	}
	if got := o.Remote("ada", "/tmp/file"); got != "ada@127.0.0.1:/tmp/file" {
		t.Errorf("Remote = %q", got)
	}
}

func TestStartAgent(t *testing.T) {
	Require(t)
	key, second := NewKey(t, Ed25519), NewKey(t, ECDSA)
	var socket string
	t.Run("the agent holds the keys", func(t *testing.T) {
		socket = StartAgent(t, key, second)
		conn, err := net.Dial("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		held, err := agent.NewClient(conn).List()
		if err != nil {
			t.Fatal(err)
		}
		if len(held) != 2 || !bytes.Equal(held[0].Marshal(), key.Public.Marshal()) || !bytes.Equal(held[1].Marshal(), second.Public.Marshal()) {
			t.Errorf("the agent holds %d keys", len(held))
		}

		// ssh logs in with a key that only the agent has: the key file given is another key.
		server := startServer(t, "ada", key.Public)
		o := NewOpenSSH(t, server.addr, server.hostKey)
		o.UseAgent(socket)
		other := NewKey(t, Ed25519)
		if res := o.SSH("", "ada", other, nil, "uptime"); res.ExitCode != 0 {
			t.Errorf("ssh with the agent: %+v", res)
		}
	})
	// The subtest ended: its agent is stopped and its socket is gone.
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Errorf("the agent's socket is still there: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(socket)); !os.IsNotExist(err) {
		t.Errorf("the agent's folder is still there: %v", err)
	}
	if out, err := exec.Command("pgrep", "-f", "ssh-agent -D -a "+socket).Output(); err == nil && len(bytes.TrimSpace(out)) != 0 {
		t.Errorf("the agent is still running: %s", out)
	}
}
