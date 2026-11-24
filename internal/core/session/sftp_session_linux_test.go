package session

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/server/servertest"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/cave/internal/core/sftp"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"golang.org/x/crypto/ssh"
)

// sftpChannel is a session channel that runs the sftp subsystem.
type sftpChannel struct {
	ssh.Channel
	t *testing.T
	// status gets the exit status the server reports when the session ends.
	status chan int
}

// exitStatus waits for the exit status of the session.
func (c *sftpChannel) exitStatus() int {
	c.t.Helper()
	select {
	case status := <-c.status:
		return status
	case <-time.After(servertest.WaitFor):
		c.t.Fatal("the session reported no exit status")
		return -1
	}
}

// request sends a request of the channel and says whether it was granted.
func (c *sftpChannel) request(name string, payload []byte) bool {
	c.t.Helper()
	ok, err := c.SendRequest(name, true, payload)
	if err != nil {
		c.t.Fatalf("%s: %v", name, err)
	}
	return ok
}

// sessionChannel opens a session channel, without starting anything on it.
func (f *fixture) sessionChannel() *sftpChannel {
	f.t.Helper()
	channel, requests, err := f.client.OpenChannel("session", nil)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = channel.Close() })
	c := &sftpChannel{Channel: channel, t: f.t, status: make(chan int, 1)}
	go func() {
		for request := range requests {
			if request.Type == "exit-status" && len(request.Payload) == 4 {
				c.status <- int(binary.BigEndian.Uint32(request.Payload))
			}
		}
	}()
	return c
}

// sftpSession opens a session with the sftp subsystem and returns a client that has sent INIT.
func (f *fixture) sftpSession() (*sftpChannel, *sshtest.SFTPClient) {
	f.t.Helper()
	channel := f.sessionChannel()
	if !channel.request("subsystem", ssh.Marshal(struct{ Name string }{"sftp"})) {
		f.t.Fatal("the sftp subsystem was refused")
	}
	client := sshtest.NewSFTPClient(f.t, channel)
	if version, _ := client.Init(); version != sftp.Version {
		f.t.Fatalf("the server speaks version %d", version)
	}
	return channel, client
}

// waitActivities waits until alice's connection shows n activities.
func (f *fixture) waitActivities(n int) []server.Activity {
	f.t.Helper()
	deadline := time.Now().Add(servertest.WaitFor)
	for {
		activities := f.WaitConnections(1)[0].Activities
		if len(activities) == n {
			return activities
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("the connection shows %+v, want %d activities", activities, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTheSFTPSubsystem(t *testing.T) {
	f := newFixture(t, nil)
	channel, client := f.sftpSession()

	// A file written and read back; relative paths start in the session's home.
	content := []byte("written over sftp\n")
	handle := client.Open("notes.txt", sshtest.SFTPOpenWrite|sshtest.SFTPOpenCreate|sshtest.SFTPOpenExcl, sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrPermissions, Mode: 0o600})
	if code := client.Write(handle, 0, content); code != sshtest.SFTPOK {
		t.Fatalf("write: status %d", code)
	}
	client.Close(handle)
	onDisk, err := os.ReadFile(filepath.Join(f.Home, "notes.txt"))
	if err != nil || string(onDisk) != string(content) {
		t.Errorf("the file in the home has %q, %v", onDisk, err)
	}
	handle = client.Open("notes.txt", sshtest.SFTPOpenRead, sshtest.SFTPAttrs{})
	if data, code := client.Read(handle, 0, 1000); string(data) != string(content) || code != sshtest.SFTPOK {
		t.Errorf("read %q, status %d", data, code)
	}
	client.Close(handle)
	if names := client.Call(sshtest.SFTPRealpath, ".").Names(); len(names) != 1 || names[0].Name != f.Home {
		t.Errorf("the start is %+v, want %s", names, f.Home)
	}

	// The registry shows the session as sftp.
	if activities := f.waitActivities(1); activities[0].Kind != server.ActivitySFTP || activities[0].Detail != "" {
		t.Errorf("activities = %+v", activities)
	}
	// The client ends its side: the session ends with success and leaves the registry.
	if err := channel.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if status := channel.exitStatus(); status != 0 {
		t.Errorf("the session ended with the exit status %d", status)
	}
	f.waitActivities(0)
	log := f.Log.String()
	for _, want := range []string{`"msg":"session started"`, `"msg":"session ended"`, `"kind":"sftp"`, `"person":"alice"`} {
		if !strings.Contains(log, want) {
			t.Errorf("the log has no %s:\n%s", want, log)
		}
	}
}

func TestSFTPTurnedOffRefusesTheSubsystem(t *testing.T) {
	f := newFixture(t, nil)
	// A session that began while it was on goes on.
	_, running := f.sftpSession()
	f.Set(string(settings.SFTPEnabled), false)

	sess, err := f.client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	if err := sess.RequestSubsystem("sftp"); err == nil {
		t.Fatal("the subsystem was granted although sftp.enabled is off")
	}
	// The refusal was not the channel's one start: a command still runs.
	if got := finish(t, sess, "", func() error { return sess.Run("echo still here") }); got.stdout != "still here\n" {
		t.Errorf("%+v", got)
	}
	if code := running.Call(sshtest.SFTPStat, ".").Code(); code != sshtest.SFTPOK {
		t.Errorf("the session that was running: status %d", code)
	}

	// On again: granted again.
	f.Set(string(settings.SFTPEnabled), true)
	f.sftpSession()
}

func TestSubsystemsThatAreRefused(t *testing.T) {
	f := newFixture(t, nil)
	t.Run("a subsystem Cave does not have", func(t *testing.T) {
		for _, name := range []string{"", "netconf", "SFTP", "sftp ", "sftp\x00"} {
			sess, err := f.client.NewSession()
			if err != nil {
				t.Fatal(err)
			}
			if err := sess.RequestSubsystem(name); err == nil {
				t.Errorf("the subsystem %q was granted", name)
			}
			_ = sess.Close()
		}
	})
	t.Run("a request that does not parse", func(t *testing.T) {
		sess, err := f.client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sess.Close() }()
		if ok, err := sess.SendRequest("subsystem", true, []byte{0, 0, 0, 9, 's'}); err != nil || ok {
			t.Errorf("subsystem = %v, %v, want it refused", ok, err)
		}
	})
	t.Run("after a command", func(t *testing.T) {
		sess, err := f.client.NewSession()
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
		if err := sess.RequestSubsystem("sftp"); err == nil {
			t.Error("the subsystem was granted on a channel that runs a command")
		}
		_ = stdin.Close()
		if err := sess.Wait(); err != nil {
			t.Errorf("the command ended with %v", err)
		}
	})
	t.Run("a second start after the subsystem", func(t *testing.T) {
		channel, client := f.sftpSession()
		// Nothing of a subsystem is a process: an env or a pty-req comes too late, and a signal
		// has nobody to reach.
		for name, payload := range map[string][]byte{
			"exec":      ssh.Marshal(struct{ Command string }{"echo no"}),
			"subsystem": ssh.Marshal(struct{ Name string }{"sftp"}),
			"shell":     nil,
			"env":       ssh.Marshal(struct{ Name, Value string }{"LANG", "C"}),
			"pty-req":   ssh.Marshal(ptyRequest{Term: "xterm", Cols: 80, Rows: 24}),
			"signal":    ssh.Marshal(struct{ Signal string }{"TERM"}),
		} {
			if channel.request(name, payload) {
				t.Errorf("%s was granted on a channel that runs a subsystem", name)
			}
		}
		// The subsystem goes on.
		if code := client.Call(sshtest.SFTPStat, ".").Code(); code != sshtest.SFTPOK {
			t.Errorf("stat: status %d", code)
		}
	})
}

func TestASignalForASessionWithoutAProcessIsRefused(t *testing.T) {
	// A channel whose start failed, or that runs a subsystem, has no process. A signal request
	// that arrives then must be refused, not reach for a process that is not there.
	for _, s := range []*session{{started: true}, {started: true, subsystem: true}, {}} {
		s.request(&ssh.Request{Type: "signal", Payload: ssh.Marshal(struct{ Signal string }{"TERM"})})
		s.hangUp()
	}
}

func TestAnSFTPPacketOverTheLimitEndsTheSession(t *testing.T) {
	f := newFixture(t, nil)
	during := openFDs(t)
	channel, client := f.sftpSession()
	if err := os.WriteFile(filepath.Join(f.Home, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		client.Open("file", sshtest.SFTPOpenRead, sshtest.SFTPAttrs{})
	}
	if got := openFDs(t); got != during+5 {
		t.Fatalf("%d files are open, want %d", got, during+5)
	}
	client.SendRaw(binary.BigEndian.AppendUint32(nil, sftp.MaxPacket+1))
	if status := channel.exitStatus(); status != 255 {
		t.Errorf("the session ended with the exit status %d, want 255", status)
	}
	logged := loggedError(t, f.Log.String(), "session ended")
	if logged.Tag != sftp.TagPacketTooLarge || !logged.HasRoute("Server.Serve->conn.readPacket.SftpPacketTooLarge") {
		t.Errorf("logged %+v with the route %s", logged, logged.Route())
	}
	// The files the session had open are closed.
	waitFDs(t, during)
}

func TestClosingTheChannelEndsAnSFTPSession(t *testing.T) {
	for name, end := range map[string]func(f *fixture, channel *sftpChannel){
		"the client closes the channel": func(_ *fixture, channel *sftpChannel) { _ = channel.Close() },
		"the connection ends":           func(f *fixture, _ *sftpChannel) { _ = f.client.Close() },
		"the user disconnects it":       func(f *fixture, _ *sftpChannel) { f.Server.DisconnectAll() },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			if err := os.WriteFile(filepath.Join(f.Home, "file"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			channel, client := f.sftpSession()
			held := 0
			for i := 0; i < 3; i++ {
				client.Open("file", sshtest.SFTPOpenRead, sshtest.SFTPAttrs{})
				held++
			}
			withFiles := filesOpenOn(t, filepath.Join(f.Home, "file"))
			if withFiles != held {
				t.Fatalf("%d handles are open on the file, want %d", withFiles, held)
			}
			end(f, channel)
			deadline := time.Now().Add(servertest.WaitFor)
			for filesOpenOn(t, filepath.Join(f.Home, "file")) != 0 {
				if time.Now().After(deadline) {
					t.Fatal("the session's files are still open")
				}
				time.Sleep(5 * time.Millisecond)
			}
		})
	}
}

// openFDs counts the files this process has open.
func openFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// waitFDs waits until this process has no more than want files open.
func waitFDs(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(servertest.WaitFor)
	for openFDs(t) > want {
		if time.Now().After(deadline) {
			t.Fatalf("%d files are open, want %d", openFDs(t), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// filesOpenOn counts the descriptors of this process that are open on a file.
func filesOpenOn(t *testing.T, path string) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if target, err := os.Readlink("/proc/self/fd/" + entry.Name()); err == nil && target == path {
			count++
		}
	}
	return count
}
