package session

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/server/servertest"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/cave/internal/core/sshtest"
)

// opensshFixture is a server that serves sessions, and the real OpenSSH programs set up for
// alice. The programs see nothing of the user's: sshtest gives them their own home, known_hosts
// and key.
type opensshFixture struct {
	*servertest.Fixture
	ssh *sshtest.OpenSSH
	key sshtest.Key
}

// newOpenSSHFixture starts the server and prepares OpenSSH; it skips the test without OpenSSH.
func newOpenSSHFixture(t *testing.T) *opensshFixture {
	t.Helper()
	sshtest.Require(t)
	f := servertest.New(t)
	// The sessions run /bin/sh in the isolated home, never the user's own shell or home.
	f.Set(string(settings.SessionShell), "/bin/sh")
	Register(f.Server, options(f))
	f.Start()
	_, key := f.Person("alice")
	return &opensshFixture{Fixture: f, ssh: f.OpenSSH(), key: key}
}

// requireProgram skips the test when a program that is not part of OpenSSH is not installed.
func requireProgram(t *testing.T, program string) {
	t.Helper()
	if _, err := exec.LookPath(program); err != nil {
		t.Skipf("%s is not installed", program)
	}
}

// writeFile writes a file of a test.
func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// sameFile fails the test when a file does not have the content.
func sameFile(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Error(err)
		return
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s has %d bytes that differ from the %d expected", path, len(got), len(want))
	}
}

// everyByte returns n bytes that go through every value.
func everyByte(n int) []byte {
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(i*7 + i/251)
	}
	return data
}

func TestOpenSSHExec(t *testing.T) {
	f := newOpenSSHFixture(t)
	tests := []struct {
		name    string
		command string
		stdin   string
		want    sshtest.Result
	}{
		{"output and success", "echo hello", "", sshtest.Result{Stdout: "hello\n"}},
		{"both streams", "echo out; echo err >&2", "", sshtest.Result{Stdout: "out\n", Stderr: "err\n"}},
		{"an exit status", "echo before; exit 3", "", sshtest.Result{Stdout: "before\n", ExitCode: 3}},
		{"a command that does not exist", "/nonexistent/command 2>/dev/null", "", sshtest.Result{ExitCode: 127}},
		{"standard input", "cat", "one\ntwo\n", sshtest.Result{Stdout: "one\ntwo\n"}},
		{"the end of standard input", "wc -l", "a\nb\nc\n", sshtest.Result{Stdout: "3\n"}},
		{"input that is not read", "echo unread", strings.Repeat("x", 100000), sshtest.Result{Stdout: "unread\n"}},
		{"quoting", `printf '%s|' "a b" 'c  d'`, "", sshtest.Result{Stdout: "a b|c  d|"}},
		{"who and where", `echo "$USER $HOME $CAVE_PERSON"; pwd`, "", sshtest.Result{Stdout: "tester " + f.Home + " alice\n" + f.Home + "\n"}},
		{"no terminal", "tty", "", sshtest.Result{Stdout: "not a tty\n", ExitCode: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := f.ssh.SSH(tt.stdin, "alice", f.key, nil, tt.command)
			if got != tt.want {
				t.Errorf("ssh %q = %+v, want %+v", tt.command, got, tt.want)
			}
		})
	}
}

func TestOpenSSHCarriesBinaryDataBothWays(t *testing.T) {
	f := newOpenSSHFixture(t)
	data := everyByte(2 << 20)
	got := f.ssh.SSH(string(data), "alice", f.key, nil, "cat")
	if got.ExitCode != 0 || got.Stdout != string(data) {
		t.Errorf("cat returned %d bytes with the exit status %d, want the %d sent: %s", len(got.Stdout), got.ExitCode, len(data), got.Stderr)
	}
	t.Logf("%d bytes of every value went through cat and came back the same", len(data))
}

func TestOpenSSHShellOnATerminal(t *testing.T) {
	f := newOpenSSHFixture(t)
	// "-tt" asks for a terminal although ssh's own input is not one, as scripts do.
	input := "echo \"argv0=$0 sum=$((40+2))\"\ntty >/dev/null && echo on a terminal\nexit 7\n"
	got := f.ssh.SSH(input, "alice", f.key, []string{"-tt"})
	for _, want := range []string{"argv0=-sh sum=42\r\n", "on a terminal\r\n"} {
		if !strings.Contains(got.Stdout, want) {
			t.Errorf("the shell did not print %q:\n%q", want, got.Stdout)
		}
	}
	if got.ExitCode != 7 {
		t.Errorf("the exit status is %d, want 7: %s", got.ExitCode, got.Stderr)
	}
	// A terminal has one stream: nothing of the session is on ssh's error stream.
	if got.Stderr != "" {
		t.Errorf("stderr = %q", got.Stderr)
	}

	// A command on a terminal: the terminal type is ssh's, and the line ends are a terminal's.
	got = f.ssh.SSH("", "alice", f.key, []string{"-tt"}, `test -t 1 && echo "tty=${SSH_TTY%/*}"; exit 4`)
	if got.Stdout != "tty=/dev/pts\r\n" || got.ExitCode != 4 {
		t.Errorf("%+v", got)
	}
}

func TestOpenSSHMultiplexing(t *testing.T) {
	f := newOpenSSHFixture(t)
	stop := f.ssh.StartMaster("alice", f.key)
	// Two sessions at once on the one connection, each waiting for a file.
	gate := filepath.Join(f.Home, "gate")
	command := "while [ ! -e gate ]; do sleep 0.05; done; echo through"
	results := make(chan sshtest.Result, 2)
	for i := 0; i < 2; i++ {
		go func() { results <- f.ssh.SSH("", "alice", f.key, nil, command) }()
	}
	deadline := time.Now().Add(servertest.WaitFor)
	var conns []server.Connection
	for {
		conns = f.Server.Connections()
		if len(conns) == 1 && len(conns[0].Activities) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the registry has %+v, want one connection with two sessions", conns)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, activity := range conns[0].Activities {
		if activity.Kind != server.ActivityExec {
			t.Errorf("activity %+v", activity)
		}
	}
	t.Logf("the registry has %d connection of %s with %d sessions", len(conns), conns[0].PersonName, len(conns[0].Activities))
	writeFile(t, gate, nil)
	for i := 0; i < 2; i++ {
		if got := <-results; got.Stdout != "through\n" || got.ExitCode != 0 {
			t.Errorf("%+v", got)
		}
	}
	// A third session, after the others, still on the same connection.
	if got := f.ssh.SSH("", "alice", f.key, nil, "echo third; exit 9"); got.Stdout != "third\n" || got.ExitCode != 9 {
		t.Errorf("%+v", got)
	}
	if conns := f.Server.Connections(); len(conns) != 1 {
		t.Errorf("the registry has %d connections, want the one of the master", len(conns))
	}
	stop()
	f.WaitConnections(0)
	// One connection was made, so alice was let in once.
	entries := f.Attempts.Entries()
	if len(entries) != 1 || entries[0].Count != 1 || entries[0].PersonName != "alice" {
		t.Errorf("attempts = %+v", entries)
	}
	t.Logf("the attempts list has %d entry: %s, %d time", len(entries), entries[0].PersonName, entries[0].Count)
}

func TestOpenSSHScp(t *testing.T) {
	f := newOpenSSHFixture(t)
	local := f.ssh.Dir()
	data := everyByte(300000)
	writeFile(t, filepath.Join(local, "up.bin"), data)
	writeFile(t, filepath.Join(local, "tree", "a.txt"), []byte("a\n"))
	writeFile(t, filepath.Join(local, "tree", "sub", "b.txt"), []byte("b\n"))

	// "-O" is the old protocol, which runs scp on the other side through exec.
	steps := []struct {
		name string
		args []string
	}{
		{"a file up", []string{"-O", filepath.Join(local, "up.bin"), f.ssh.Remote("alice", "stored.bin")}},
		{"a file down", []string{"-O", f.ssh.Remote("alice", "stored.bin"), filepath.Join(local, "down.bin")}},
		{"a folder up", []string{"-O", "-r", filepath.Join(local, "tree"), f.ssh.Remote("alice", "tree-copy")}},
		{"a folder down", []string{"-O", "-r", f.ssh.Remote("alice", "tree-copy"), filepath.Join(local, "tree-back")}},
	}
	for _, step := range steps {
		if got := f.ssh.SCP(f.key, step.args...); got.ExitCode != 0 {
			t.Fatalf("%s: %+v", step.name, got)
		}
	}
	sameFile(t, filepath.Join(f.Home, "stored.bin"), data)
	sameFile(t, filepath.Join(local, "down.bin"), data)
	sameFile(t, filepath.Join(f.Home, "tree-copy", "sub", "b.txt"), []byte("b\n"))
	sameFile(t, filepath.Join(local, "tree-back", "a.txt"), []byte("a\n"))
	sameFile(t, filepath.Join(local, "tree-back", "sub", "b.txt"), []byte("b\n"))

	// A file that is not there is scp's error, with its exit status.
	got := f.ssh.SCP(f.key, "-O", f.ssh.Remote("alice", "missing.bin"), filepath.Join(local, "missing.bin"))
	if got.ExitCode == 0 || !strings.Contains(got.Stderr, "No such file") {
		t.Errorf("a missing file: %+v", got)
	}
}

func TestOpenSSHRsync(t *testing.T) {
	requireProgram(t, "rsync")
	f := newOpenSSHFixture(t)
	local := f.ssh.Dir()
	data := everyByte(200000)
	writeFile(t, filepath.Join(local, "src", "big.bin"), data)
	writeFile(t, filepath.Join(local, "src", "sub", "note.txt"), []byte("note\n"))

	remote := f.ssh.Remote("alice", "synced/")
	rsync := func(args ...string) sshtest.Result {
		t.Helper()
		res := f.ssh.Run("", "rsync", append([]string{"-a", "-e", f.ssh.SSHCommand(f.key)}, args...)...)
		t.Logf("$ rsync -a -e ssh %s  (exit status %d)", strings.Join(args, " "), res.ExitCode)
		return res
	}
	if got := rsync(filepath.Join(local, "src")+"/", remote); got.ExitCode != 0 {
		t.Fatalf("rsync up: %+v", got)
	}
	sameFile(t, filepath.Join(f.Home, "synced", "big.bin"), data)
	sameFile(t, filepath.Join(f.Home, "synced", "sub", "note.txt"), []byte("note\n"))
	if got := rsync(remote, filepath.Join(local, "back")+"/"); got.ExitCode != 0 {
		t.Fatalf("rsync down: %+v", got)
	}
	sameFile(t, filepath.Join(local, "back", "big.bin"), data)
	sameFile(t, filepath.Join(local, "back", "sub", "note.txt"), []byte("note\n"))
}

func TestOpenSSHGit(t *testing.T) {
	requireProgram(t, "git")
	f := newOpenSSHFixture(t)
	local := f.ssh.Dir()
	// git reads no configuration of the user or the system, asks nothing, and uses the harness's
	// ssh.
	env := []string{
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Alice", "GIT_AUTHOR_EMAIL=alice@example.invalid",
		"GIT_COMMITTER_NAME=Alice", "GIT_COMMITTER_EMAIL=alice@example.invalid",
		"GIT_SSH_COMMAND=" + f.ssh.SSHCommand(f.key),
	}
	git := func(dir string, args ...string) sshtest.Result {
		t.Helper()
		res := f.ssh.RunWith(env, "", "git", append([]string{"-C", dir, "-c", "init.defaultBranch=main"}, args...)...)
		t.Logf("$ git %s  (exit status %d)", strings.Join(args, " "), res.ExitCode)
		if res.ExitCode != 0 {
			t.Fatalf("git %v: %+v", args, res)
		}
		return res
	}
	// A repository in the session's home, which is what the person reaches.
	repo := filepath.Join(f.Home, "project.git")
	seed := filepath.Join(local, "seed")
	writeFile(t, filepath.Join(seed, "README"), []byte("first\n"))
	git(local, "init", "--quiet", "--bare", repo)
	git(seed, "init", "--quiet")
	git(seed, "add", "README")
	git(seed, "commit", "--quiet", "-m", "first")
	git(seed, "push", "--quiet", repo, "main")

	url := "ssh://" + f.ssh.Remote("alice", "")
	url = strings.TrimSuffix(url, ":") + ":" + strings.TrimPrefix(f.Addr, "127.0.0.1:") + repo
	clone := filepath.Join(local, "clone")
	git(local, "clone", "--quiet", url, clone)
	sameFile(t, filepath.Join(clone, "README"), []byte("first\n"))

	writeFile(t, filepath.Join(clone, "README"), []byte("first\nsecond\n"))
	git(clone, "commit", "--quiet", "-a", "-m", "second")
	git(clone, "push", "--quiet", "origin", "main")
	if got := git(local, "--git-dir", repo, "log", "--format=%s", "main"); got.Stdout != "second\nfirst\n" {
		t.Errorf("the repository's history is %q", got.Stdout)
	}
}
