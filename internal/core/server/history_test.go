package server

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/history"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"golang.org/x/crypto/ssh"
)

// waitEnded waits until the history has n connections that ended, and returns the history's
// connections.
func (f *fixture) waitEnded(n int) []history.Connection {
	f.t.Helper()
	deadline := time.Now().Add(waitFor)
	for {
		conns := f.history.Connections()
		ended := 0
		for _, c := range conns {
			if !c.Open() {
				ended++
			}
		}
		if ended == n {
			return conns
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("the history has %d connections that ended after %v, want %d: %+v", ended, waitFor, n, conns)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestHistoryReasons(t *testing.T) {
	tests := []struct {
		name string
		end  func(t *testing.T, f *fixture, client *ssh.Client, personID string)
		want history.Reason
	}{
		{"the client leaves", func(t *testing.T, _ *fixture, client *ssh.Client, _ string) {
			if err := client.Close(); err != nil {
				t.Fatal(err)
			}
		}, history.ReasonClient},
		{"Disconnect", func(t *testing.T, f *fixture, _ *ssh.Client, _ string) {
			if err := f.server.Disconnect(f.server.Connections()[0].ID); err != nil {
				t.Fatal(err)
			}
		}, history.ReasonDisconnected},
		{"DisconnectAll", func(_ *testing.T, f *fixture, _ *ssh.Client, _ string) {
			f.server.DisconnectAll()
		}, history.ReasonDisconnected},
		{"the person is disabled", func(t *testing.T, f *fixture, _ *ssh.Client, id string) {
			if err := f.people.SetEnabled(ctx, id, false); err != nil {
				t.Fatal(err)
			}
		}, history.ReasonPersonChanged},
		{"the person is renamed", func(t *testing.T, f *fixture, _ *ssh.Client, id string) {
			if err := f.people.Rename(ctx, id, "alicia"); err != nil {
				t.Fatal(err)
			}
		}, history.ReasonPersonChanged},
		{"the person is removed", func(t *testing.T, f *fixture, _ *ssh.Client, id string) {
			if err := f.people.Remove(ctx, id); err != nil {
				t.Fatal(err)
			}
		}, history.ReasonPersonChanged},
		{"the key is removed", func(t *testing.T, f *fixture, _ *ssh.Client, id string) {
			p, _ := f.people.Get(id)
			if err := f.people.RemoveKey(ctx, id, p.Keys[0].ID); err != nil {
				t.Fatal(err)
			}
		}, history.ReasonPersonChanged},
		{"the server stops", func(t *testing.T, f *fixture, _ *ssh.Client, _ string) {
			if err := f.server.Stop(ctx); err != nil {
				t.Fatal(err)
			}
		}, history.ReasonServerStopped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			alice, key := f.person("alice")
			client := f.connect("alice", key)
			open := f.history.Connections()
			if len(open) != 1 || !open[0].Open() || open[0].PersonName != "alice" || open[0].Address != "127.0.0.1" ||
				open[0].KeyFingerprint != ssh.FingerprintSHA256(key.Public) || open[0].ClientVersion != "SSH-2.0-Go" || !open[0].ConnectedAt.Equal(start) {
				t.Fatalf("while connected, the history has %+v", open)
			}
			f.clock.Advance(75 * time.Second)
			tt.end(t, f, client, alice.ID)
			got := f.waitEnded(1)[0]
			if got.Reason != tt.want {
				t.Errorf("reason %q, want %q", got.Reason, tt.want)
			}
			if d, ok := got.Duration(); !ok || d != 75*time.Second {
				t.Errorf("duration %v, %v; want 1m15s", d, ok)
			}
			if got.ID != open[0].ID {
				t.Errorf("the end has the id %q, the start %q", got.ID, open[0].ID)
			}
		})
	}
	t.Run("the client stops answering", func(t *testing.T) {
		f := newFixture(t)
		q := f.quiet(60, false)
		for range KeepAliveMax + 1 {
			f.clock.Advance(60 * time.Second)
		}
		<-q.gone
		if got := f.waitEnded(1)[0]; got.Reason != history.ReasonKeepAlive {
			t.Errorf("reason %q, want keepalive", got.Reason)
		}
	})
}

// workChannels registers a handler for the channel type "work": it shows the channel's extra
// data as an exec activity, writes the number of bytes its first byte asks for (in MiB, 0 for
// none) and closes the channel.
func (f *fixture) workChannels() {
	f.server.HandleChannel("work", func(_ context.Context, conn *Conn, newChannel ssh.NewChannel) {
		channel, requests, err := newChannel.Accept()
		if err != nil {
			return
		}
		go ssh.DiscardRequests(requests)
		defer conn.AddActivity(ActivityExec, string(newChannel.ExtraData()))()
		if data := newChannel.ExtraData(); len(data) > 0 && data[0] == '!' {
			_, _ = channel.Write(make([]byte, 1<<20)) // the client may have gone; the count shows what went
		}
		_ = channel.CloseWrite()
		_ = channel.Close()
	})
}

// work opens a "work" channel with the detail and reads it to its end.
func work(t *testing.T, client *ssh.Client, detail string) int {
	t.Helper()
	channel, requests, err := client.OpenChannel("work", []byte(detail))
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(requests)
	n := 0
	buf := make([]byte, 32<<10)
	for {
		m, err := channel.Read(buf)
		n += m
		if err != nil {
			return n
		}
	}
}

func TestHistoryActivities(t *testing.T) {
	f := newStopped(t)
	f.workChannels()
	f.start()
	_, key := f.person("alice")
	client := f.connect("alice", key)
	for _, d := range []string{"uptime", "ls", "uptime", "date"} {
		work(t, client, d)
	}
	for i := range 40 {
		work(t, client, fmt.Sprint("cmd ", i))
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	got := f.waitEnded(1)[0]
	want := []history.Activity{{Kind: "exec", Detail: "uptime"}, {Kind: "exec", Detail: "ls"}, {Kind: "exec", Detail: "date"}}
	if !reflect.DeepEqual(got.Activities[:3], want) {
		t.Errorf("the first activities are %+v, want each once, in order", got.Activities[:3])
	}
	if len(got.Activities) != history.MaxActivities || got.MoreActivities != 43-history.MaxActivities {
		t.Errorf("%d activities and %d more, want %d and %d", len(got.Activities), got.MoreActivities, history.MaxActivities, 43-history.MaxActivities)
	}
}

func TestHistoryBytes(t *testing.T) {
	f := newStopped(t)
	f.workChannels()
	f.start()
	_, key := f.person("alice")
	client := f.connect("alice", key)
	if n := work(t, client, "!big"); n != 1<<20 {
		t.Fatalf("read %d bytes", n)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	got := f.waitEnded(1)[0]
	if got.BytesOut < 1<<20 || got.BytesIn <= 0 || got.BytesIn > 1<<20 {
		t.Errorf("bytes in %d, out %d; want out past 1 MiB and some in", got.BytesIn, got.BytesOut)
	}
}

func TestRefusedAttemptsAreNotHistory(t *testing.T) {
	f := newFixture(t)
	f.person("alice")
	other := sshtest.NewKey(t, sshtest.Ed25519)
	if _, err := f.dial(f.config("alice", other)); err == nil {
		t.Fatal("a wrong key was let in")
	}
	f.waitTotal(1)
	if got := f.history.Connections(); len(got) != 0 {
		t.Errorf("the history has %+v", got)
	}
}
