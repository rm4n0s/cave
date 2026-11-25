package app

import (
	"os"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/history"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"golang.org/x/crypto/ssh"
)

// waitHistory waits until the history has the connections that ended and the transfers, and
// returns it.
func waitHistory(t *testing.T, a *App, ended, transfers int) HistorySnapshot {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for {
		h := a.History()
		n := 0
		for _, c := range h.Connections {
			if !c.Open() {
				n++
			}
		}
		if n == ended && len(h.Transfers) == transfers {
			return h
		}
		if time.Now().After(deadline) {
			t.Fatalf("the history is %+v, want %d connections that ended and %d transfers", h, ended, transfers)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// upload writes a file over the sftp subsystem of the client's connection.
func upload(t *testing.T, client *ssh.Client, name string, data []byte) {
	t.Helper()
	channel, requests, err := client.OpenChannel("session", nil)
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(requests)
	defer func() { _ = channel.Close() }() // the session is done with
	if ok, err := channel.SendRequest("subsystem", true, ssh.Marshal(struct{ Name string }{"sftp"})); err != nil || !ok {
		t.Fatalf("the sftp subsystem was refused: %v", err)
	}
	sftp := sshtest.NewSFTPClient(t, channel)
	sftp.Init()
	handle := sftp.Open(name, sshtest.SFTPOpenWrite|sshtest.SFTPOpenCreate|sshtest.SFTPOpenTrunc, sshtest.SFTPAttrs{})
	if code := sftp.Write(handle, 0, data); code != sshtest.SFTPOK {
		t.Fatalf("write: status %d", code)
	}
	sftp.Close(handle)
}

func TestHistorySnapshot(t *testing.T) {
	f := newFixture(t, nil)
	if err := os.MkdirAll(f.options().User.Home, 0o700); err != nil {
		t.Fatal(err)
	}
	a := f.open(nil)
	key := f.person(a, "alice")
	f.clock.Advance(time.Second) // what opening changed is told and done with
	var seen events
	a.Subscribe(seen.add)
	client := f.connect(a, "alice", key, f.port)
	connection := a.Connections()[0]
	upload(t, client, "up.txt", []byte("twelve bytes"))
	f.clock.Advance(2 * time.Minute)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	h := waitHistory(t, a, 1, 1)
	if c := h.Connections[0]; c.ID != connection.ID || c.PersonName != "alice" || c.Reason != history.ReasonClient {
		t.Errorf("the connection is %+v", c)
	}
	if d, ok := h.Connections[0].Duration(); !ok || d != 2*time.Minute {
		t.Errorf("the duration is %v, %v", d, ok)
	}
	if tr := h.Transfers[0]; tr.ConnectionID != connection.ID || tr.Uploaded != 12 || tr.Direction() != history.Upload {
		t.Errorf("the transfer is %+v", tr)
	}
	var told Topics
	deadline := time.Now().Add(waitFor)
	for !told.Has(TopicHistory) {
		if time.Now().After(deadline) {
			t.Fatal("no Changed told about the history")
		}
		f.clock.Advance(CoalesceTime)
		for _, topics := range seen.take() {
			told |= topics
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Cleared: nothing is left but what is open, and the time of the clear is shown.
	other := f.connect(a, "alice", key, f.port)
	if err := a.ClearHistory(ctx); err != nil {
		t.Fatal(err)
	}
	h = a.History()
	if len(h.Connections) != 1 || !h.Connections[0].Open() || len(h.Transfers) != 0 || !h.LastClearedAt.Equal(f.clock.Now().UTC()) {
		t.Errorf("after the clear the history is %+v", h)
	}
	if !alive(other) {
		t.Error("clearing the history ended a connection")
	}

	// The history of the run is there at the next start; the open connection ended with Cave.
	f.close(a)
	b := f.open(func(o *Options) { o.NoStart = true })
	if h := b.History(); len(h.Connections) != 1 || h.Connections[0].Reason != history.ReasonServerStopped {
		t.Errorf("after a restart the history is %+v", h)
	}
}

func TestHistoryClearSetting(t *testing.T) {
	t.Run("onStart", func(t *testing.T) {
		f := newFixture(t, map[string]any{string(settings.HistoryClear): settings.ClearOnStart})
		a := f.open(func(o *Options) { o.NoStart = true })
		a.history.Connected(history.Connection{ID: "c_00000000000000a1", PersonName: "alice", ConnectedAt: start})
		f.close(a)
		f.clock.Advance(time.Hour)
		b := f.open(func(o *Options) { o.NoStart = true })
		if h := b.History(); len(h.Connections) != 0 || !h.LastClearedAt.Equal(start.Add(time.Hour)) {
			t.Errorf("after a start with onStart the history is %+v", h)
		}
	})
	t.Run("daily", func(t *testing.T) {
		f := newFixture(t, nil)
		a := f.open(func(o *Options) { o.NoStart = true })
		a.history.Connected(history.Connection{ID: "c_00000000000000a1", PersonName: "alice", ConnectedAt: start})
		if err := a.Set(ctx, string(settings.HistoryClear), string(attempts.Daily)); err != nil {
			t.Fatal(err)
		}
		if h := a.History(); !h.NextClearAt.Equal(start.Add(24 * time.Hour)) {
			t.Fatalf("next clear %v", h.NextClearAt)
		}
		f.clock.Advance(23 * time.Hour)
		if len(a.History().Connections) != 1 {
			t.Fatal("cleared before the day passed")
		}
		f.clock.Advance(time.Hour)
		if h := a.History(); len(h.Connections) != 1 || !h.LastClearedAt.Equal(start.Add(24*time.Hour)) {
			// The open connection stays: only what ended goes.
			t.Errorf("after the day the history is %+v", h)
		}
		if err := a.Set(ctx, string(settings.HistoryClear), string(attempts.Never)); err != nil {
			t.Fatal(err)
		}
		if h := a.History(); !h.NextClearAt.IsZero() {
			t.Errorf("never has a next clear: %v", h.NextClearAt)
		}
	})
}
