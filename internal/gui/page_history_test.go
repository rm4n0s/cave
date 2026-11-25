package gui

import (
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/gui/testutil"
)

// theHistoryState returns the state of the History page, to ask it what it shows.
func theHistoryState(tester *gluttertest.Tester) *historyState {
	return gluttertest.StateOf[*historyState](tester, gluttertest.ByType[*historyPage]())
}

// dark gives the fake the dark theme.
func dark(f *apptest.Fake, t *testing.T) *apptest.Fake {
	f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.theme": "dark"})
	return f
}

// showTransfers switches the page to the Transfers tab.
func showTransfers(tester *gluttertest.Tester) {
	tester.Tap(gluttertest.Descendant(gluttertest.ByKey(historyTabsKey), gluttertest.ByText("Transfers")))
	tester.PumpAndSettle()
}

func TestGoldenHistory(t *testing.T) {
	t.Run("connections light wide", func(t *testing.T) {
		pump(t, light(apptest.Sample(t), t), testutil.Wide, "history").MatchesGolden("history-light-wide")
	})
	t.Run("connections dark narrow", func(t *testing.T) {
		pump(t, dark(apptest.Sample(t), t), testutil.Narrow, "history").MatchesGolden("history-dark-narrow")
	})
	t.Run("transfers light wide", func(t *testing.T) {
		tester := pump(t, light(apptest.Sample(t), t), testutil.Wide, "history")
		showTransfers(tester)
		tester.MatchesGolden("history-transfers-light-wide")
	})
	t.Run("transfers dark narrow", func(t *testing.T) {
		tester := pump(t, dark(apptest.Sample(t), t), testutil.Narrow, "history")
		showTransfers(tester)
		tester.MatchesGolden("history-transfers-dark-narrow")
	})
	t.Run("a connection selected light wide", func(t *testing.T) {
		tester := pump(t, light(apptest.Sample(t), t), testutil.Wide, "history")
		tester.Tap(gluttertest.ByText("Client left"))
		tester.PumpAndSettle()
		tester.MatchesGolden("history-connection-light-wide")
	})
	t.Run("a transfer selected dark narrow", func(t *testing.T) {
		tester := pump(t, dark(apptest.Sample(t), t), testutil.Narrow, "history")
		showTransfers(tester)
		tester.Tap(gluttertest.ByTextContaining("report-final-v2.pdf"))
		tester.PumpAndSettle()
		reveal(tester, historyDetailsKey) // narrow: the details are under the table
		tester.MatchesGolden("history-transfer-dark-narrow")
	})
	t.Run("empty light wide", func(t *testing.T) {
		pump(t, light(apptest.Empty(t), t), testutil.Wide, "history").MatchesGolden("history-empty-light-wide")
	})
	t.Run("the clear question light wide", func(t *testing.T) {
		tester := pump(t, light(apptest.Sample(t), t), testutil.Wide, "history")
		tapKey(tester, clearHistoryKey)
		tester.MatchesGolden("history-clear-light-wide")
	})
}

func TestClearHistory(t *testing.T) {
	t.Run("yes clears", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pump(t, f, testutil.Wide, "history")
		tapKey(tester, clearHistoryKey)
		tapKey(tester, confirmYesKey)
		if got := f.Calls(); len(got) != 1 || got[0] != "ClearHistory()" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("cancel does nothing", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pump(t, f, testutil.Wide, "history")
		tapKey(tester, clearHistoryKey)
		tapKey(tester, confirmCancelKey)
		if got := f.Calls(); len(got) != 0 {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("an empty history asks nothing", func(t *testing.T) {
		f := apptest.Empty(t)
		tester := pump(t, f, testutil.Wide, "history")
		tapKey(tester, clearHistoryKey)
		if tester.Exists(gluttertest.ByKey(confirmYesKey)) || len(f.Calls()) != 0 {
			t.Errorf("a question for nothing, calls %q", f.Calls())
		}
	})
	t.Run("Change… shows the setting", func(t *testing.T) {
		tester := pump(t, apptest.Sample(t), testutil.Wide, "history")
		tapKey(tester, changeHistoryClearKey)
		eventually(t, tester, "the page is Settings", func() bool { return currentPage(t, tester) == pageSettings })
	})
}

func TestHistoryTabsAndSearch(t *testing.T) {
	tester := pump(t, apptest.Sample(t), testutil.Wide, "history")
	s := theHistoryState(tester)
	if got := len(s.connections(s.Widget().v)); got != 7 {
		t.Fatalf("%d connections to start with, want 7", got)
	}
	for _, want := range []string{"Client left", "Connected now", "2 h 5 min", "Unknown: Cave did not close"} {
		if tester.Count(gluttertest.ByText(want)) == 0 {
			t.Errorf("the table does not show %q", want)
		}
	}
	tester.EnterText(gluttertest.ByKey(historySearchKey), "carol")
	tester.PumpAndSettle()
	s = theHistoryState(tester)
	if got := len(s.connections(s.Widget().v)); got != 2 {
		t.Errorf("the search for carol shows %d connections, want 2", got)
	}
	tester.EnterText(gluttertest.ByKey(historySearchKey), "beach")
	tester.PumpAndSettle()
	showTransfers(tester)
	s = theHistoryState(tester)
	if got := s.transfers(s.Widget().v); len(got) != 1 || got[0].Path != "/home/ada/photos/beach.jpg" {
		t.Errorf("the search for beach shows %+v", got)
	}
	if !tester.Exists(gluttertest.ByText("Download")) {
		t.Error("the transfer's direction is not shown")
	}
	tester.EnterText(gluttertest.ByKey(historySearchKey), "zzz")
	tester.PumpAndSettle()
	if !tester.Exists(gluttertest.ByText("Nothing matches.")) {
		t.Error("an empty search result says nothing")
	}
}

func TestHistoryFollowsTheApplication(t *testing.T) {
	f := apptest.Empty(t)
	tester := pump(t, f, testutil.Wide, "history")
	if !tester.Exists(gluttertest.ByText("No connections yet.")) {
		t.Fatal("the empty page says nothing")
	}
	f.HistorySnapshot = apptest.Sample(t).HistorySnapshot
	f.Emit(app.Changed{Topics: app.TopicHistory})
	eventually(t, tester, "the connections", func() bool { return tester.Exists(gluttertest.ByText("Client left")) })
}

func TestHistoryWithTheRealApplication(t *testing.T) {
	r := apptest.Open(t, nil)
	alice := r.Connect("alice", r.Person("alice"))
	r.Upload(alice, "notes.txt", []byte("twelve bytes"))
	if err := alice.Close(); err != nil {
		t.Fatal(err)
	}
	r.Eventually("the history has the connection's end", func() bool {
		h := r.App.History()
		return len(h.Connections) == 1 && !h.Connections[0].Open() && len(h.Transfers) == 1
	})
	tester := pumpOptions(t, r.App, testutil.Wide, Options{Page: "history", Clock: r.Clock}, syncRunner)
	if !tester.Exists(gluttertest.ByText("Client left")) {
		t.Error("the connection that ended is not shown")
	}
	showTransfers(tester)
	if !tester.Exists(gluttertest.ByText("Upload")) || tester.Count(gluttertest.ByTextContaining("notes.txt")) == 0 {
		t.Error("the upload is not shown")
	}
}
