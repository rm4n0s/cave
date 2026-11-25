package tui

import (
	"strings"
	"testing"

	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/history"
)

func TestGoldenHistory(t *testing.T) {
	tests := []struct {
		name          string
		fake          func() *apptest.Fake
		width, height int
		keys          []string
	}{
		{"sample-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, nil},
		{"sample-60x16", func() *apptest.Fake { return apptest.Sample(t) }, 60, 16, nil},
		{"transfers-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, []string{"t"}},
		{"transfers-60x16", func() *apptest.Fake { return apptest.Sample(t) }, 60, 16, []string{"t"}},
		{"empty-100x30", func() *apptest.Fake { return apptest.Empty(t) }, 100, 30, nil},
		{"empty-transfers-100x30", func() *apptest.Fake { return apptest.Empty(t) }, 100, 30, []string{"t"}},
		{"connection-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, []string{"down", "enter"}},
		{"connection-60x16", func() *apptest.Fake { return apptest.Sample(t) }, 60, 16, []string{"down", "enter"}},
		{"transfer-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, []string{"t", "down", "down", "down", "enter"}},
		{"clear-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, []string{"C"}},
		{"search-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, []string{"/", "c", "a", "r", "enter"}},
		{"nothing-matches-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, []string{"t", "/", "z", "z", "enter"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newFakeHarness(t, tt.fake(), tt.width, tt.height)
			h.key("5")
			h.keys(tt.keys...)
			h.golden("history-" + tt.name)
		})
	}
}

func TestHistoryTables(t *testing.T) {
	h := newFakeHarness(t, apptest.Sample(t), 100, 30)
	h.key("5")
	screen := h.screen()
	for _, want := range []string{"connected now", "2 h 5 min", "Client left", "Disconnected by you", "Person or key changed",
		"Server stopped", "Stopped answering", "Unknown: Cave did not close", "Cleared 20 d ago"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the Connections tab has no %q:\n%s", want, screen)
		}
	}
	h.key("t")
	screen = h.screen()
	for _, want := range []string{"Upload", "Download", "Both", "3.4 MB", "41.0 KB / 8.2 KB", "Interrupted", "Failed",
		"…d/deeply/nested/report-final-v2.pdf"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the Transfers tab has no %q:\n%s", want, screen)
		}
	}
}

func TestHistoryKeys(t *testing.T) {
	t.Run("C and y clear the history", func(t *testing.T) {
		f := apptest.Sample(t)
		h := newFakeHarness(t, f, 100, 30)
		h.keys("5", "C", "y")
		if got := f.Calls(); len(got) != 1 || got[0] != "ClearHistory()" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("C and n change nothing", func(t *testing.T) {
		f := apptest.Sample(t)
		h := newFakeHarness(t, f, 100, 30)
		h.keys("5", "C", "n")
		if got := f.Calls(); len(got) != 0 {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("an empty history asks nothing", func(t *testing.T) {
		f := apptest.Empty(t)
		h := newFakeHarness(t, f, 100, 30)
		h.keys("5", "C", "enter", "t", "enter")
		if h.m().ask != nil || h.m().histDetail || len(f.Calls()) != 0 {
			t.Errorf("ask %v, details %v, calls %q", h.m().ask, h.m().histDetail, f.Calls())
		}
	})
	t.Run("t goes round the tabs, and esc closes the details", func(t *testing.T) {
		h := newFakeHarness(t, apptest.Sample(t), 100, 30)
		h.keys("5", "t")
		if h.m().histTab != tabTransfers {
			t.Fatal("t did not switch to the transfers")
		}
		h.keys("enter")
		if !h.m().histDetail || !strings.Contains(h.screen(), "esc back") {
			t.Fatalf("the details are not shown:\n%s", h.screen())
		}
		h.keys("esc", "t")
		if h.m().histDetail || h.m().histTab != tabConnections {
			t.Error("esc and t did not go back to the connections")
		}
	})
	t.Run("the keys are on the help", func(t *testing.T) {
		h := newFakeHarness(t, apptest.Sample(t), 100, 30)
		h.keys("5", "?")
		for _, want := range []string{"other tab", "search", "clear"} {
			if !strings.Contains(h.screen(), want) {
				t.Errorf("the help has no %q:\n%s", want, h.screen())
			}
		}
	})
}

func TestHistoryKeepsTheSelection(t *testing.T) {
	f := apptest.Sample(t)
	h := newFakeHarness(t, f, 100, 30)
	h.keys("5", "down", "down") // carol, disconnected by you
	want := f.HistorySnapshot.Connections[2].ID
	if h.m().histConnSel != want {
		t.Fatalf("selected %q", h.m().histConnSel)
	}
	fresh := f.HistorySnapshot.Connections[0]
	fresh.ID = "c_00000000000000ff"
	f.HistorySnapshot.Connections = append([]history.Connection{fresh}, f.HistorySnapshot.Connections...)
	h.send(changedMsg{})
	if c, _ := h.m().selectedHistoryConnection(); c.ID != want || h.m().histConnList.selected != 3 {
		t.Errorf("selected %s at row %d, want %s at row 3", c.ID, h.m().histConnList.selected, want)
	}
	// The history is cleared while the details are open: they close.
	h.key("enter")
	f.HistorySnapshot.Connections = f.HistorySnapshot.Connections[:1]
	h.send(changedMsg{})
	if h.m().histDetail {
		t.Error("the details of a record that is gone are still open")
	}
}

func TestHistoryWithTheRealApplication(t *testing.T) {
	r := apptest.Open(t, nil)
	alice := r.Connect("alice", r.Person("alice"))
	r.Upload(alice, "notes.txt", []byte("twelve bytes"))
	if err := alice.Close(); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, r.App, r.Clock, 100, 30)
	h.key("5")
	r.Eventually("the page does not show the connection that ended", func() bool {
		h.send(changedMsg{})
		return strings.Contains(h.screen(), "Client left")
	})
	h.key("t")
	r.Eventually("the page does not show the upload", func() bool {
		h.send(changedMsg{})
		return strings.Contains(h.screen(), "notes.txt") && strings.Contains(h.screen(), "Upload")
	})
	h.keys("t", "C", "y")
	r.Eventually("the history is not cleared", func() bool {
		h.send(changedMsg{})
		return strings.Contains(h.screen(), "No connections yet.")
	})
}
