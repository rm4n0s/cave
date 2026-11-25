//go:build linux || darwin

package session

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/history"
	"github.com/rm4n0s/cave/internal/core/server/servertest"
	"github.com/rm4n0s/cave/internal/core/sshtest"
)

// waitTransfers waits until the history has n transfers, and returns them, the newest first.
func waitTransfers(t *testing.T, f *servertest.Fixture, n int) []history.Transfer {
	t.Helper()
	deadline := time.Now().Add(servertest.WaitFor)
	for {
		got := f.History.Transfers()
		if len(got) == n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("the history has %d transfers after %v, want %d: %+v", len(got), servertest.WaitFor, n, got)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSFTPTransfersAreInTheHistory(t *testing.T) {
	f := newFixture(t, nil)
	_, client := f.sftpSession()
	connection := f.Server.Connections()[0]

	handle := client.Open("notes.txt", sshtest.SFTPOpenWrite|sshtest.SFTPOpenCreate|sshtest.SFTPOpenTrunc, sshtest.SFTPAttrs{})
	if code := client.Write(handle, 0, []byte("twelve bytes")); code != sshtest.SFTPOK {
		t.Fatalf("write: status %d", code)
	}
	client.Close(handle)
	handle = client.Open("notes.txt", sshtest.SFTPOpenRead, sshtest.SFTPAttrs{})
	client.Read(handle, 0, 1000)
	client.Close(handle)

	got := waitTransfers(t, f.Fixture, 2)
	want := []struct {
		direction history.Direction
		down, up  int64
	}{{history.Download, 12, 0}, {history.Upload, 0, 12}}
	for i, tr := range got {
		if tr.ConnectionID != connection.ID || tr.PersonName != "alice" || tr.PersonID != connection.PersonID ||
			tr.Path != filepath.Join(f.Home, "notes.txt") || tr.Result != history.ResultDone {
			t.Errorf("transfer %d is %+v", i, tr)
		}
		if tr.Direction() != want[i].direction || tr.Downloaded != want[i].down || tr.Uploaded != want[i].up {
			t.Errorf("transfer %d moved %d down and %d up (%v)", i, tr.Downloaded, tr.Uploaded, tr.Direction())
		}
	}
}

func TestOpenSSHTransfersAreInTheHistory(t *testing.T) {
	f := newOpenSSHFixture(t)
	local := f.ssh.Dir()
	data := everyByte(300 << 10)
	writeFile(t, filepath.Join(local, "up.bin"), data)

	batch := "put " + filepath.Join(local, "up.bin") + " stored.bin\nget stored.bin " + filepath.Join(local, "down.bin") + "\n"
	if got := f.ssh.SFTP("alice", f.key, batch); got.ExitCode != 0 {
		t.Fatalf("sftp: %+v", got)
	}
	if got := f.ssh.SCP(f.key, filepath.Join(local, "up.bin"), f.ssh.Remote("alice", "copy.bin")); got.ExitCode != 0 {
		t.Fatalf("scp: %+v", got)
	}
	got := waitTransfers(t, f.Fixture, 3)
	want := []struct {
		path      string
		direction history.Direction
	}{{"copy.bin", history.Upload}, {"stored.bin", history.Download}, {"stored.bin", history.Upload}}
	for i, tr := range got {
		moved := tr.Uploaded + tr.Downloaded
		if tr.Path != filepath.Join(f.Home, want[i].path) || tr.Direction() != want[i].direction || moved != int64(len(data)) ||
			tr.Result != history.ResultDone || tr.PersonName != "alice" {
			t.Errorf("transfer %d is %+v, want %s, %v, %d bytes", i, tr, want[i].path, want[i].direction, len(data))
		}
	}

	// The old protocol runs scp through exec: no transfer, but the command in the connection's
	// record.
	if got := f.ssh.SCP(f.key, "-O", filepath.Join(local, "up.bin"), f.ssh.Remote("alice", "old.bin")); got.ExitCode != 0 {
		t.Fatalf("scp -O: %+v", got)
	}
	deadline := time.Now().Add(servertest.WaitFor)
	for {
		conns := f.History.Connections()
		if len(conns) == 3 && !conns[0].Open() {
			if a := conns[0].Activities; len(a) != 1 || a[0].Kind != "exec" || !strings.HasPrefix(a[0].Detail, "scp ") {
				t.Errorf("the connection of scp -O opened %+v", a)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the history has %+v", conns)
		}
		time.Sleep(time.Millisecond)
	}
	if n := len(f.History.Transfers()); n != 3 {
		t.Errorf("scp -O made a transfer: %d transfers", n)
	}
}
