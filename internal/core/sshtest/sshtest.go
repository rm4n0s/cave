// Package sshtest is the harness of the tests that connect to an SSH server: keys made for the
// test, a client inside the test process, and the real OpenSSH programs, run so that they never
// read or write the user's keys, configuration, known_hosts or agent (docs/SPEC.md §14.1,
// CLAUDE.md "Testing notes"). It is used only by tests.
package sshtest

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// Timeout is how long one command of OpenSSH, or one dial of the client, may take.
const Timeout = 30 * time.Second

// KeyKind says what kind of key NewKey makes.
type KeyKind int

// The kinds of keys.
const (
	// Ed25519 is the kind most tests use: it is made in microseconds.
	Ed25519 KeyKind = iota
	// ECDSA is a P-256 key.
	ECDSA
	// RSA is a 2048-bit RSA key, which takes a moment to make.
	RSA
)

// Key is a key pair made for one test.
type Key struct {
	// Signer signs for the client inside the test.
	Signer ssh.Signer
	// Public is the public key.
	Public ssh.PublicKey
	// AuthorizedKey is the public key as "<type> <base64>", the text a person's key is added
	// from.
	AuthorizedKey string
	// PrivatePath is the private key in a file of mode 0600 under the test's temporary folder,
	// for OpenSSH's -i.
	PrivatePath string
}

// NewKey makes a key pair and writes its private key under the test's temporary folder.
func NewKey(t testing.TB, kind KeyKind) Key {
	t.Helper()
	var private any
	var err error
	switch kind {
	case Ed25519:
		_, private, err = ed25519.GenerateKey(rand.Reader)
	case ECDSA:
		private, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case RSA:
		private, err = rsa.GenerateKey(rand.Reader, 2048)
	default:
		t.Fatalf("sshtest: unknown kind of key %d", kind)
	}
	if err != nil {
		t.Fatalf("sshtest: %v", err)
	}
	// ed25519.GenerateKey returns the private key by value; the other two by pointer.
	if key, ok := private.(ed25519.PrivateKey); ok {
		private = &key
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatalf("sshtest: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(private, "sshtest")
	if err != nil {
		t.Fatalf("sshtest: %v", err)
	}
	path := filepath.Join(t.TempDir(), "id_test")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("sshtest: %v", err)
	}
	return Key{
		Signer:        signer,
		Public:        signer.PublicKey(),
		AuthorizedKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))),
		PrivatePath:   path,
	}
}

// ClientConfig returns the configuration of a client inside the test that logs in as user with
// the keys, and accepts only the host key given.
func ClientConfig(user string, hostKey ssh.PublicKey, keys ...Key) *ssh.ClientConfig {
	signers := make([]ssh.Signer, 0, len(keys))
	for _, k := range keys {
		signers = append(signers, k.Signer)
	}
	return &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signers...)},
		HostKeyCallback: ssh.FixedHostKey(hostKey),
		Timeout:         Timeout,
	}
}

// DialErr connects a client inside the test to addr and returns the error of a connection or an
// authentication that failed. A client it returns is closed when the test ends.
func DialErr(t testing.TB, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
	t.Helper()
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() {
		// The test may have closed it, or the server may have: only a leak matters.
		_ = client.Close()
	})
	return client, nil
}

// Dial connects a client inside the test to addr as user with the key, and fails the test when
// it cannot.
func Dial(t testing.TB, addr, user string, key Key, hostKey ssh.PublicKey) *ssh.Client {
	t.Helper()
	client, err := DialErr(t, addr, ClientConfig(user, hostKey, key))
	if err != nil {
		t.Fatalf("sshtest: dial %s as %s: %v", addr, user, err)
	}
	return client
}

// programs are the OpenSSH programs the harness runs.
var programs = []string{"ssh", "sftp", "scp", "ssh-keygen", "ssh-agent", "ssh-add"}

// Require skips the test when the OpenSSH programs are not installed, or on Windows, where the
// harness does not run them.
func Require(t testing.TB) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the tests with the OpenSSH programs run on Linux")
	}
	for _, program := range programs {
		if _, err := exec.LookPath(program); err != nil {
			t.Skipf("OpenSSH's %s is not installed", program)
		}
	}
}

// Result is what a command of OpenSSH printed and how it ended.
type Result struct {
	// Stdout and Stderr are the command's output.
	Stdout, Stderr string
	// ExitCode is its exit status; -1 when it was killed (the timeout).
	ExitCode int
}

// OpenSSH runs the real OpenSSH programs against one server, away from the user's files.
type OpenSSH struct {
	t testing.TB
	// host and port are the server's address.
	host, port string
	// dir holds the known_hosts file and the home the programs see.
	dir string
	// agent is the socket of the agent the commands may use, or "".
	agent string
	// control is the socket of the master connection the commands share, or "".
	control string
}

// NewOpenSSH prepares the OpenSSH programs for the server at addr ("127.0.0.1:port"), whose
// host key must be hostKey: it writes a known_hosts file with only that key. It skips the test
// when OpenSSH is not installed (Require).
func NewOpenSSH(t testing.TB, addr string, hostKey ssh.PublicKey) *OpenSSH {
	t.Helper()
	Require(t)
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("sshtest: %v", err)
	}
	o := &OpenSSH{t: t, host: host, port: port, dir: t.TempDir()}
	if err := os.Mkdir(filepath.Join(o.dir, "home"), 0o700); err != nil {
		t.Fatalf("sshtest: %v", err)
	}
	line := "[" + host + "]:" + port + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(hostKey))) + "\n"
	if err := os.WriteFile(o.knownHosts(), []byte(line), 0o600); err != nil {
		t.Fatalf("sshtest: %v", err)
	}
	return o
}

// knownHosts is the path of the known_hosts file.
func (o *OpenSSH) knownHosts() string {
	return filepath.Join(o.dir, "known_hosts")
}

// UseAgent makes the commands that follow use the agent on the socket (StartAgent) besides
// their key file: its keys are offered for authentication, and "-A" forwards it.
func (o *OpenSSH) UseAgent(socket string) {
	o.agent = socket
}

// Options returns the options every command gets, which keep it away from the user's files and
// from asking anything: no configuration file, only the test's known_hosts, only the key given,
// no agent but the test's, no password, no prompt.
func (o *OpenSSH) Options(key Key) []string {
	// Without an agent, only the key file is offered. With the test's agent, its keys are
	// offered too (ssh takes the first value an option gets, so this cannot be added later).
	agent, identitiesOnly := "none", "yes"
	if o.agent != "" {
		agent, identitiesOnly = o.agent, "no"
	}
	control := "none"
	if o.control != "" {
		control = o.control
	}
	return []string{
		"-F", "none",
		"-o", "BatchMode=yes",
		"-o", "IdentitiesOnly=" + identitiesOnly,
		"-o", "IdentityAgent=" + agent,
		"-o", "UserKnownHostsFile=" + o.knownHosts(),
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UpdateHostKeys=no",
		"-o", "PreferredAuthentications=publickey",
		"-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no",
		"-o", "ControlPath=" + control,
		"-o", "ConnectTimeout=10",
		"-o", "LogLevel=ERROR",
		"-i", key.PrivatePath,
	}
}

// Env returns the environment every command gets: a home under the test's folder, no agent of
// the user, and messages in English.
func (o *OpenSSH) Env() []string {
	env := []string{
		"HOME=" + filepath.Join(o.dir, "home"),
		"PATH=" + os.Getenv("PATH"),
		"LANG=C",
		"LC_ALL=C",
	}
	if o.agent != "" {
		env = append(env, "SSH_AUTH_SOCK="+o.agent)
	}
	return env
}

// Run runs one OpenSSH program with the arguments as they are, the harness's environment, and
// stdin as its standard input, for at most Timeout.
func (o *OpenSSH) Run(stdin string, program string, args ...string) Result {
	o.t.Helper()
	return o.RunWith(nil, stdin, program, args...)
}

// RunWith is Run for a program that needs more in its environment (rsync, git): env is added
// to the harness's environment.
func (o *OpenSSH) RunWith(env []string, stdin string, program string, args ...string) Result {
	o.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = append(o.Env(), env...)
	cmd.Dir = o.dir
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// When the timeout kills the program, do not wait for a child that holds its output.
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			o.t.Fatalf("sshtest: %s: %v", program, err)
		}
		res.ExitCode = exitErr.ExitCode()
	}
	return res
}

// Dir returns a folder of the harness, under the test's folder, where a test may keep the files
// it sends and receives.
func (o *OpenSSH) Dir() string {
	return o.dir
}

// SSHCommand returns the command line "ssh <options> -p <port>" with the key, for the programs
// that start ssh themselves ("rsync -e", GIT_SSH_COMMAND). They split it at spaces.
func (o *OpenSSH) SSHCommand(key Key) string {
	o.t.Helper()
	words := append([]string{"ssh"}, o.Options(key)...)
	words = append(words, "-p", o.port)
	for _, word := range words {
		if strings.ContainsAny(word, " \t'\"\\") {
			o.t.Fatalf("sshtest: %q cannot be put in a command line", word)
		}
	}
	return strings.Join(words, " ")
}

// StartMaster starts a master connection as user ("ssh -M -N") that the commands that follow
// share: each of them is then a session of that one connection. The returned function ends the
// master ("ssh -O exit") and waits for it; the master is killed when the test ends, whatever
// happened.
func (o *OpenSSH) StartMaster(user string, key Key) (stop func()) {
	o.t.Helper()
	// A socket's path must be short: t.TempDir() can be too long.
	dir, err := os.MkdirTemp("", "cave-mux-")
	if err != nil {
		o.t.Fatalf("sshtest: %v", err)
	}
	o.t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			o.t.Errorf("sshtest: %v", err)
		}
	})
	o.control = filepath.Join(dir, "master.sock")
	args := append(o.Options(key), "-M", "-N", "-p", o.port, user+"@"+o.host)
	master := exec.Command("ssh", args...)
	master.Env = o.Env()
	master.Dir = o.dir
	var stderr bytes.Buffer
	master.Stderr = &stderr
	if err := master.Start(); err != nil {
		o.t.Fatalf("sshtest: ssh -M: %v", err)
	}
	ended := make(chan struct{})
	go func() {
		_ = master.Wait() // how it ended does not matter: it is ended on purpose
		close(ended)
	}()
	o.t.Cleanup(func() {
		_ = master.Process.Kill() // already gone after stop
		<-ended
	})
	control := func(command string) Result {
		return o.Run("", "ssh", append(o.Options(key), "-O", command, "-p", o.port, user+"@"+o.host)...)
	}
	deadline := time.Now().Add(Timeout)
	for control("check").ExitCode != 0 {
		select {
		case <-ended:
			o.t.Fatalf("sshtest: the master connection ended: %s", stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			o.t.Fatalf("sshtest: the master connection is not ready: %s", stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	return func() {
		o.t.Helper()
		if res := control("exit"); res.ExitCode != 0 {
			o.t.Errorf("sshtest: ssh -O exit: %+v", res)
		}
		select {
		case <-ended:
		case <-time.After(Timeout):
			o.t.Errorf("sshtest: the master connection did not end")
		}
		o.control = ""
	}
}

// SSH runs "ssh <options> user@host <command>" with the key. options are more options of ssh
// itself (such as "-tt" or "-A"), and command is what the server is asked to run; without a
// command, ssh asks for a shell. stdin is what the remote side reads.
func (o *OpenSSH) SSH(stdin, user string, key Key, options []string, command ...string) Result {
	o.t.Helper()
	full := append(o.Options(key), "-p", o.port)
	full = append(full, options...)
	full = append(full, user+"@"+o.host)
	full = append(full, command...)
	res := o.Run(stdin, "ssh", full...)
	o.log(res, "ssh", append(append(append([]string{}, options...), user+"@"+o.host), command...))
	return res
}

// SFTP runs sftp in batch mode with the commands of batch, one per line.
func (o *OpenSSH) SFTP(user string, key Key, batch string) Result {
	o.t.Helper()
	full := append(o.Options(key), "-P", o.port, "-b", "-", user+"@"+o.host)
	res := o.Run(batch, "sftp", full...)
	o.log(res, "sftp", []string{"-b", "-", user + "@" + o.host})
	return res
}

// SCP runs scp with the arguments; Remote makes the remote ones.
func (o *OpenSSH) SCP(key Key, args ...string) Result {
	o.t.Helper()
	full := append(o.Options(key), "-P", o.port)
	res := o.Run("", "scp", append(full, args...)...)
	o.log(res, "scp", args)
	return res
}

// log writes a command and its exit status to the test's log, without the options every command
// gets, so that a verbose run reads as a transcript.
func (o *OpenSSH) log(res Result, program string, args []string) {
	o.t.Helper()
	o.t.Logf("$ %s %s  (exit status %d)", program, strings.Join(args, " "), res.ExitCode)
}

// Remote returns "user@host:path" for scp.
func (o *OpenSSH) Remote(user, path string) string {
	return user + "@" + o.host + ":" + path
}

// StartAgent starts a real ssh-agent on a socket under a temporary folder, adds the keys to it,
// and returns the socket. The agent is stopped when the test ends. The user's own agent is
// never touched.
func StartAgent(t testing.TB, keys ...Key) string {
	t.Helper()
	Require(t)
	// A socket's path must be short: t.TempDir() can be too long.
	dir, err := os.MkdirTemp("", "cave-agent-")
	if err != nil {
		t.Fatalf("sshtest: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("sshtest: %v", err)
		}
	})
	socket := filepath.Join(dir, "agent.sock")
	agent := exec.Command("ssh-agent", "-D", "-a", socket)
	agent.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "LANG=C"}
	if err := agent.Start(); err != nil {
		t.Fatalf("sshtest: ssh-agent: %v", err)
	}
	t.Cleanup(func() {
		// Killing it is how a foreground agent is stopped; Wait then reports the kill.
		_ = agent.Process.Kill()
		_ = agent.Wait()
	})
	deadline := time.Now().Add(Timeout)
	for {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sshtest: ssh-agent did not make its socket %s", socket)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, key := range keys {
		ctx, cancel := context.WithTimeout(context.Background(), Timeout)
		add := exec.CommandContext(ctx, "ssh-add", "-q", key.PrivatePath)
		add.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "LANG=C", "SSH_AUTH_SOCK=" + socket}
		out, err := add.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("sshtest: ssh-add: %v\n%s", err, out)
		}
	}
	return socket
}
