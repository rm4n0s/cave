package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/server"
	"golang.org/x/crypto/ssh"
)

// threeConnections is the sample's connection and two more, with every kind of activity.
func threeConnections(f *apptest.Fake) {
	alice := f.ConnectionList[0]
	alice.Activities = []server.Activity{
		{Kind: server.ActivityShell}, {Kind: server.ActivityExec, Detail: "ls"}, {Kind: server.ActivitySFTP}, {Kind: server.ActivityAgent},
	}
	bob := alice
	bob.ID, bob.PersonID, bob.PersonName, bob.Address = "c_0000000000000002", apptest.BobID, "bob", "203.0.113.5"
	bob.Since = apptest.Now.Add(-3 * time.Hour)
	bob.Activities = []server.Activity{
		{Kind: server.ActivityLocalForward, Detail: "127.0.0.1:5432"}, {Kind: server.ActivityRemoteForward, Detail: "[::1]:80"},
	}
	carol := alice
	carol.ID, carol.PersonID, carol.PersonName, carol.Address = "c_0000000000000003", apptest.CarolID, "carol", "2001:db8::9"
	carol.Since = apptest.Now.Add(-2 * 24 * time.Hour)
	carol.Activities = nil
	f.ConnectionList = []server.Connection{alice, bob, carol}
}

func TestGoldenConnections(t *testing.T) {
	tests := []struct {
		name          string
		fake          func() *apptest.Fake
		width, height int
		keys          []string
	}{
		{"sample-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, nil},
		{"sample-60x16", func() *apptest.Fake { return apptest.Sample(t) }, 60, 16, nil},
		{"empty-100x30", func() *apptest.Fake { return apptest.Empty(t) }, 100, 30, nil},
		{"three-100x30", func() *apptest.Fake { f := apptest.Sample(t); threeConnections(f); return f }, 100, 30, nil},
		{"three-60x16", func() *apptest.Fake { f := apptest.Sample(t); threeConnections(f); return f }, 60, 16, []string{"down"}},
		{"disconnect-100x30", func() *apptest.Fake { f := apptest.Sample(t); threeConnections(f); return f }, 100, 30, []string{"down", "d"}},
		{"disconnect-all-100x30", func() *apptest.Fake { f := apptest.Sample(t); threeConnections(f); return f }, 100, 30, []string{"D"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newFakeHarness(t, tt.fake(), tt.width, tt.height)
			h.key("4")
			h.keys(tt.keys...)
			h.golden("connections-" + tt.name)
		})
	}
}

func TestConnectionsTable(t *testing.T) {
	f := apptest.Sample(t)
	threeConnections(f)
	h := newFakeHarness(t, f, 100, 30)
	h.key("4")
	screen := h.screen()
	for _, want := range []string{
		"shell, exec, sftp, agent", "forward to 127.0.0.1:5432, listening on [::1]:80", "idle",
		"12 min ago", "3 h ago", "2 d ago", "198.51.100.7", "2001:db8::9",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("the page lacks %q:\n%s", want, screen)
		}
	}
	// The times move with the clock of the model.
	f.Clock.Advance(5 * time.Minute)
	h.send(tickMsg{})
	if !strings.Contains(h.screen(), "17 min ago") {
		t.Errorf("after five minutes:\n%s", h.screen())
	}
}

func TestDisconnectOne(t *testing.T) {
	f := apptest.Sample(t)
	threeConnections(f)
	h := newFakeHarness(t, f, 100, 30)
	h.keys("4", "down", "d")
	if !strings.Contains(h.screen(), "Disconnect bob (203.0.113.5)?") {
		t.Fatalf("the question:\n%s", h.screen())
	}
	h.key("enter") // the default is no
	h.keys("d", "n")
	if len(f.Calls()) != 0 {
		t.Fatalf("calls = %q", f.Calls())
	}
	h.keys("d", "y")
	if got := f.Calls(); len(got) != 1 || got[0] != "Disconnect(c_0000000000000002)" {
		t.Errorf("calls = %q", got)
	}
}

func TestDisconnectAll(t *testing.T) {
	f := apptest.Sample(t)
	threeConnections(f)
	h := newFakeHarness(t, f, 100, 30)
	h.keys("4", "D")
	if !strings.Contains(h.screen(), "Disconnect all 3 connections?") {
		t.Fatalf("the question:\n%s", h.screen())
	}
	h.key("esc")
	if len(f.Calls()) != 0 {
		t.Fatalf("calls = %q", f.Calls())
	}
	h.keys("D", "y")
	if got := f.Calls(); len(got) != 1 || got[0] != "DisconnectAll()" {
		t.Errorf("calls = %q", got)
	}
	// With one connection the question is not about "all 1".
	g := apptest.Sample(t)
	h = newFakeHarness(t, g, 100, 30)
	h.keys("4", "D")
	if !strings.Contains(h.screen(), "Disconnect the connection?") {
		t.Errorf("the question for one connection:\n%s", h.screen())
	}
}

func TestNothingToDisconnect(t *testing.T) {
	f := apptest.Empty(t)
	h := newFakeHarness(t, f, 100, 30)
	h.keys("4", "d", "D", "down", "up", "enter")
	if h.m().ask != nil || len(f.Calls()) != 0 {
		t.Errorf("ask %v, calls %q", h.m().ask, f.Calls())
	}
	if !strings.Contains(h.screen(), "Nobody is connected.") {
		t.Errorf("the page:\n%s", h.screen())
	}
}

func TestConnectionsKeepsTheSelection(t *testing.T) {
	f := apptest.Sample(t)
	threeConnections(f)
	h := newFakeHarness(t, f, 100, 30)
	h.keys("4", "down") // bob
	if h.m().connSel != "c_0000000000000002" {
		t.Fatalf("selected %q", h.m().connSel)
	}
	// A new connection comes first: the row stays under the cursor.
	fresh := f.ConnectionList[0]
	fresh.ID, fresh.PersonName = "c_00000000000000ff", "dave"
	f.ConnectionList = append([]server.Connection{fresh}, f.ConnectionList...)
	h.send(changedMsg{})
	if c, _ := h.m().selectedConnection(); c.ID != "c_0000000000000002" || h.m().connList.selected != 2 {
		t.Errorf("selected %s at row %d, want bob at row 2", c.ID, h.m().connList.selected)
	}
	// Bob leaves: the row stays where it was.
	f.ConnectionList = append(f.ConnectionList[:2:2], f.ConnectionList[3:]...)
	h.send(changedMsg{})
	if c, ok := h.m().selectedConnection(); !ok || c.PersonName != "carol" {
		t.Errorf("after bob left the selection is on %v", c.PersonName)
	}
	f.ConnectionList = nil
	h.send(changedMsg{})
	if _, ok := h.m().selectedConnection(); ok || h.m().connSel != "" {
		t.Error("a connection is selected in an empty list")
	}
}

// aliveClient says whether a client's connection still answers.
func aliveClient(client *ssh.Client) bool {
	_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
	return err == nil
}

func TestConnectionsWithTheRealApplication(t *testing.T) {
	r := apptest.Open(t, nil)
	alice := r.Connect("alice", r.Person("alice"))
	bob := r.Connect("bob", r.Person("bob"))
	h := newHarness(t, r.App, r.Clock, 100, 30)
	h.key("4")
	r.Eventually("the page does not show two rows", func() bool {
		h.send(changedMsg{})
		return len(h.m().connections) == 2
	})
	screen := h.screen()
	if !strings.Contains(screen, "alice") || !strings.Contains(screen, "bob") || !strings.Contains(screen, "idle") {
		t.Fatalf("the page:\n%s", screen)
	}
	// Select bob, whichever row he is on, and disconnect him.
	for c, _ := h.m().selectedConnection(); c.PersonName != "bob"; c, _ = h.m().selectedConnection() {
		h.key("down")
	}
	h.keys("d", "y")
	closed(t, bob)
	if !aliveClient(alice) {
		t.Error("disconnecting bob closed alice's connection")
	}
	r.Eventually("the page still shows bob", func() bool {
		h.send(changedMsg{})
		return len(h.m().connections) == 1
	})
	// Disconnect everybody.
	h.keys("D", "y")
	closed(t, alice)
}
