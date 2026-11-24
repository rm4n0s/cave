package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/errors"
)

// listenFailed is an error as the server gives it when the port is taken.
func listenFailed() error {
	return errors.NewErr("ListenFailed", errors.New("Plain", "listen tcp 127.0.0.1:2222: bind: address already in use")).
		SetMetadata("address", "127.0.0.1:2222")
}

func TestGoldenFrame(t *testing.T) {
	stopped := apptest.Sample(t)
	stopped.ServerSnapshot.State = server.Stopped
	stopped.ServerSnapshot.Listening = nil
	failed := apptest.Sample(t)
	failed.ServerSnapshot.State = server.Stopped
	failed.ServerSnapshot.Err = listenFailed()
	tests := []struct {
		name          string
		fake          *apptest.Fake
		width, height int
	}{
		{"sample-100x30", apptest.Sample(t), 100, 30},
		{"sample-60x16", apptest.Sample(t), 60, 16},
		{"empty-100x30", apptest.Empty(t), 100, 30},
		{"stopped-100x30", stopped, 100, 30},
		{"error-100x30", failed, 100, 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newFakeHarness(t, tt.fake, tt.width, tt.height).golden("frame-" + tt.name)
		})
	}
}

func TestGoldenHelp(t *testing.T) {
	h := newFakeHarness(t, apptest.Sample(t), 100, 30)
	h.key("?")
	h.golden("help-100x30")
	h.key("x") // any key closes it, and the key does nothing else
	if strings.Contains(h.screen(), "Keys everywhere") {
		t.Error("the help is still there after a key")
	}
	if h.m().page != pageOverview {
		t.Error("the key that closed the help was also used")
	}
}

func TestTooSmall(t *testing.T) {
	const want = "Terminal too small (needs 60×16)"
	for _, size := range [][2]int{{59, 16}, {60, 15}, {20, 5}, {1, 1}} {
		h := newFakeHarness(t, apptest.Sample(t), size[0], size[1])
		if got := h.screen(); got != want {
			t.Errorf("%dx%d: %q", size[0], size[1], got)
		}
	}
	h := newFakeHarness(t, apptest.Sample(t), 60, 16)
	if strings.Contains(h.screen(), "Terminal too small") {
		t.Error("60×16 is big enough")
	}
	// A terminal made smaller and then bigger again.
	h.resize(40, 10)
	h.resize(80, 24)
	if !strings.Contains(h.screen(), "Overview") {
		t.Error("the frame did not come back")
	}
}

func TestNothingIsDrawnBeforeTheSizeIsKnown(t *testing.T) {
	f := apptest.Sample(t)
	m := NewModel(ctx, f, f.Clock, Options{})
	if got := m.View().Content; got != "" {
		t.Errorf("the view before the size is %q", got)
	}
}

func TestPageKeys(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want page
	}{
		{"1", []string{"2", "1"}, pageOverview},
		{"2", []string{"2"}, pagePeople},
		{"3", []string{"3"}, pageAttempts},
		{"4", []string{"4"}, pageConnections},
		{"5", []string{"5"}, pageSettings},
		{"tab", []string{"tab"}, pagePeople},
		{"tab from the last page", []string{"5", "tab"}, pageOverview},
		{"shift+tab", []string{"3", "shift+tab"}, pagePeople},
		{"shift+tab from the first page", []string{"shift+tab"}, pageSettings},
		{"a key that is not a page", []string{"3", "6", "0", "x"}, pageAttempts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newFakeHarness(t, apptest.Sample(t), 100, 30)
			h.keys(tt.keys...)
			if got := h.m().page; got != tt.want {
				t.Errorf("page = %d, want %d", got, tt.want)
			}
			if !strings.Contains(h.screen(), "["+string(rune('1'+tt.want))+" "+tt.want.title()+"]") {
				t.Errorf("the page is not marked in the header:\n%s", h.screen())
			}
		})
	}
}

func TestStartStopKey(t *testing.T) {
	t.Run("a running server is stopped", func(t *testing.T) {
		f := apptest.Sample(t)
		newFakeHarness(t, f, 100, 30).key("s")
		if got := f.Calls(); len(got) != 1 || got[0] != "StopServer()" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("a stopped server is started", func(t *testing.T) {
		f := apptest.Empty(t)
		newFakeHarness(t, f, 100, 30).key("s")
		if got := f.Calls(); len(got) != 1 || got[0] != "StartServer()" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("a server that is starting is stopped", func(t *testing.T) {
		f := apptest.Empty(t)
		f.ServerSnapshot.State = server.Starting
		newFakeHarness(t, f, 100, 30).key("s")
		if got := f.Calls(); len(got) != 1 || got[0] != "StopServer()" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("a failure is shown as a sentence and goes with the next key", func(t *testing.T) {
		f := apptest.Empty(t)
		f.Fail = map[string]error{"StartServer": listenFailed()}
		h := newFakeHarness(t, f, 100, 30)
		h.key("s")
		want := app.Message(listenFailed())
		if !strings.Contains(h.screen(), want[:60]) {
			t.Errorf("the screen does not show %q:\n%s", want, h.screen())
		}
		if !h.m().messageErr {
			t.Error("the message is not marked as an error")
		}
		h.key("2")
		if strings.Contains(h.screen(), "Cave cannot listen") {
			t.Error("the message stayed after a key")
		}
	})
	t.Run("the help is not a place to start the server", func(t *testing.T) {
		f := apptest.Empty(t)
		h := newFakeHarness(t, f, 100, 30)
		h.keys("?", "s")
		if len(f.Calls()) != 0 {
			t.Errorf("calls = %q", f.Calls())
		}
	})
}

func TestEveryLineFits(t *testing.T) {
	fakes := map[string]func() *apptest.Fake{
		"sample": func() *apptest.Fake { return apptest.Sample(t) },
		"empty":  func() *apptest.Fake { return apptest.Empty(t) },
	}
	for name, make := range fakes {
		for _, width := range []int{60, 80, 100, 137} {
			for _, height := range []int{16, 30, 50} {
				for p := pageOverview; p < pageCount; p++ {
					for _, help := range []bool{false, true} {
						h := newFakeHarness(t, make(), width, height)
						h.key(string(rune('1' + p)))
						switch {
						case help:
							h.key("?")
						case p == pageConnections:
							h.key("q") // the sample has a connection: the question is drawn
						}
						lines := strings.Split(h.screen(), "\n")
						if len(lines) != height {
							t.Fatalf("%s %dx%d page %d help %v: %d lines", name, width, height, p, help, len(lines))
						}
						for i, line := range lines {
							if w := lipgloss.Width(line); w > width {
								t.Fatalf("%s %dx%d page %d help %v: line %d is %d cells wide: %q", name, width, height, p, help, i, w, line)
							}
						}
					}
				}
			}
		}
	}
}

func TestChangedReadsTheSnapshotsAgain(t *testing.T) {
	f := apptest.Sample(t)
	h := newFakeHarness(t, f, 100, 30)
	if !strings.Contains(h.screen(), "1 connected · 9 attempts") {
		t.Fatalf("the header at first:\n%s", h.screen())
	}
	f.ConnectionList = nil
	f.AttemptsSnapshot.Entries = f.AttemptsSnapshot.Entries[:2]
	f.ServerSnapshot.State = server.Stopped
	// Nothing is read until the application says so.
	if !strings.Contains(h.screen(), "1 connected") {
		t.Error("the model read the snapshots without being told")
	}
	h.send(changedMsg{})
	screen := h.screen()
	if !strings.Contains(screen, "0 connected · 2 attempts") || !strings.Contains(screen, "stopped") {
		t.Errorf("the header after the change:\n%s", screen)
	}
	// A tick reads them again too: the times are drawn again.
	f.ConnectionList = apptest.Sample(t).ConnectionList
	h.send(tickMsg{})
	if !strings.Contains(h.screen(), "1 connected") {
		t.Error("a tick did not read the snapshots")
	}
}

func TestQuitKeys(t *testing.T) {
	// With nobody connected, the quit keys quit at once, from the help too.
	for _, keys := range [][]string{{"q"}, {"ctrl+c"}, {"?", "ctrl+c"}} {
		h := newFakeHarness(t, apptest.Empty(t), 100, 30)
		h.keys(keys...)
		if !h.quit {
			t.Errorf("%v did not quit", keys)
		}
	}
	// The help closes with any key, q included.
	h := newFakeHarness(t, apptest.Empty(t), 100, 30)
	h.keys("?", "q")
	if h.quit || h.m().help {
		t.Errorf("q in the help: quit %v, help %v", h.quit, h.m().help)
	}
}

func TestTheFirstPage(t *testing.T) {
	for name, want := range map[string]page{
		"": pageOverview, "overview": pageOverview, "people": pagePeople, "attempts": pageAttempts,
		"connections": pageConnections, "settings": pageSettings, "nonsense": pageOverview,
	} {
		f := apptest.Empty(t)
		m := NewModel(ctx, f, f.Clock, Options{Page: name})
		if m.page != want {
			t.Errorf("page %q starts on %d, want %d", name, m.page, want)
		}
	}
	for _, name := range app.Pages {
		if pageNamed(name).title() == "" || strings.ToLower(pageNamed(name).title()) != name {
			t.Errorf("app.Pages has %q, which the TUI does not know", name)
		}
	}
}

func TestTheCountsInTheHeader(t *testing.T) {
	f := apptest.Sample(t)
	f.AttemptsSnapshot.Entries = f.AttemptsSnapshot.Entries[:1]
	f.ConnectionList = nil
	first := strings.Split(newFakeHarness(t, f, 100, 30).screen(), "\n")[0]
	if !strings.HasSuffix(strings.TrimRight(first, " "), "0 connected · 1 attempt") {
		t.Errorf("the header is %q", first)
	}
	f.AttemptsSnapshot.Entries = nil
	first = strings.Split(newFakeHarness(t, f, 100, 30).screen(), "\n")[0]
	if !strings.HasSuffix(strings.TrimRight(first, " "), "0 connected · 0 attempts") {
		t.Errorf("the header is %q", first)
	}
}

func TestTheStateInTheHeader(t *testing.T) {
	tests := []struct {
		name  string
		state server.State
		err   error
		want  string
	}{
		{"running", server.Running, nil, "running on port 2222"},
		{"stopped", server.Stopped, nil, "stopped"},
		{"starting", server.Starting, nil, "starting…"},
		{"stopping", server.Stopping, nil, "stopping…"},
		{"failed", server.Stopped, listenFailed(), "not running"},
	}
	for _, tt := range tests {
		f := apptest.Empty(t)
		f.ServerSnapshot.State, f.ServerSnapshot.Err = tt.state, tt.err
		first := strings.Split(newFakeHarness(t, f, 100, 30).screen(), "\n")[0]
		if !strings.HasPrefix(first, "Cave   "+tt.want) {
			t.Errorf("%s: the header is %q", tt.name, first)
		}
	}
}
