package gui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/gui/testutil"
)

// theAttemptsState returns the state of the Attempts page, to ask it what it shows.
func theAttemptsState(tester *gluttertest.Tester) *attemptsState {
	return gluttertest.StateOf[*attemptsState](tester, gluttertest.ByType[*attemptsPage]())
}

// shownRows is how many entries the page's filter and search let through.
func shownRows(tester *gluttertest.Tester) int {
	s := theAttemptsState(tester)
	return len(s.visible(s.Widget().v))
}

// firstRow is the first entry of the table, in its order.
func firstRow(tester *gluttertest.Tester) attempts.Entry {
	s := theAttemptsState(tester)
	return s.visible(s.Widget().v)[0]
}

// first finds only the first of what the finder finds, in the order of the tree: the filter's
// button comes before the table, so `first(ByText("Accepted"))` is the button and not a cell.
type first struct{ gluttertest.Finder }

func (f first) Find(t *gluttertest.Tester) []widgets.Element {
	found := f.Finder.Find(t)
	if len(found) > 1 {
		found = found[:1]
	}
	return found
}

// last finds only the last of what the finder finds: a dropdown keeps a copy of its entries to
// measure them, before the entries of the open menu, so the entry to tap is the last.
type last struct{ gluttertest.Finder }

func (f last) Find(t *gluttertest.Tester) []widgets.Element {
	found := f.Finder.Find(t)
	if len(found) > 1 {
		found = found[len(found)-1:]
	}
	return found
}

// pickRow selects the entry with the address by tapping a cell of its row.
func pickRow(tester *gluttertest.Tester, address string) {
	tester.Tap(gluttertest.ByText(address))
	tester.PumpAndSettle()
}

func light(f *apptest.Fake, t *testing.T) *apptest.Fake {
	f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.theme": "light", "attempts.clear": "weekly"})
	return f
}

func TestGoldenAttempts(t *testing.T) {
	t.Run("sample light wide", func(t *testing.T) {
		pump(t, light(apptest.Sample(t), t), testutil.Wide, "attempts").MatchesGolden("attempts-light-wide")
	})
	t.Run("sample dark narrow", func(t *testing.T) {
		f := apptest.Sample(t)
		f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.theme": "dark", "attempts.clear": "weekly"})
		pump(t, f, testutil.Narrow, "attempts").MatchesGolden("attempts-dark-narrow")
	})
	t.Run("nobody tried", func(t *testing.T) {
		pump(t, light(apptest.Empty(t), t), testutil.Wide, "attempts").MatchesGolden("attempts-empty-light-wide")
	})
	t.Run("a row selected light wide", func(t *testing.T) {
		tester := pump(t, light(apptest.Sample(t), t), testutil.Wide, "attempts")
		pickRow(tester, "198.51.100.23")
		tester.MatchesGolden("attempts-selected-light-wide")
	})
	t.Run("a row selected dark narrow", func(t *testing.T) {
		f := apptest.Sample(t)
		f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.theme": "dark"})
		tester := pump(t, f, testutil.Narrow, "attempts")
		pickRow(tester, "198.51.100.23")
		tester.MatchesGolden("attempts-selected-dark-narrow")
	})
}

func TestHostileLoginNameIsDrawnAsText(t *testing.T) {
	tester := pump(t, apptest.Sample(t), testutil.Wide, "attempts")
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("root�[31mX")); err != nil {
		t.Error(err)
	}
}

func TestTheInfoLine(t *testing.T) {
	f := apptest.Sample(t)
	tester := pump(t, f, testutil.Wide, "attempts")
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Cleared 3 d ago · next clear in 4 d")); err != nil {
		t.Error(err)
	}
	f.AttemptsSnapshot.NextClearAt = time.Time{}
	f.Emit(app.Changed{Topics: app.TopicAttempts})
	eventually(t, tester, "the line without a next clear", func() bool {
		return tester.Exists(gluttertest.ByText("Cleared 3 d ago"))
	})
	f.AttemptsSnapshot.LastClearedAt = time.Time{}
	f.Emit(app.Changed{Topics: app.TopicAttempts})
	eventually(t, tester, "never cleared", func() bool { return tester.Exists(gluttertest.ByText("Never cleared")) })
}

func TestFilterAndSearch(t *testing.T) {
	tester := pump(t, apptest.Sample(t), testutil.Wide, "attempts")
	if got := shownRows(tester); got != 9 {
		t.Fatalf("%d rows to start with, want 9", got)
	}
	tester.Tap(first{gluttertest.ByText("Accepted")})
	tester.PumpAndSettle()
	if got := shownRows(tester); got != 1 {
		t.Errorf("Accepted shows %d rows, want 1", got)
	}
	tester.Tap(first{gluttertest.ByText("Refused")})
	tester.PumpAndSettle()
	if got := shownRows(tester); got != 8 {
		t.Errorf("Refused shows %d rows, want 8", got)
	}
	tester.Tap(first{gluttertest.ByText("All")})
	tester.PumpAndSettle()
	tester.EnterText(gluttertest.ByKey(attemptSearchKey), "198.51")
	tester.PumpAndSettle()
	rows := theAttemptsState(tester).visible(theAttemptsState(tester).Widget().v)
	if len(rows) == 0 {
		t.Fatal("the search for an address found nothing")
	}
	for _, e := range rows {
		if !strings.Contains(e.Address, "198.51") {
			t.Errorf("the search for 198.51 shows %s", e.Address)
		}
	}
	// The search ignores case and looks at who and the login name too.
	tester.EnterText(gluttertest.ByKey(attemptSearchKey), "ADMIN")
	tester.PumpAndSettle()
	if got := shownRows(tester); got != 1 || firstRow(tester).LoginName != "admin" {
		t.Errorf("the search for ADMIN shows %d rows", got)
	}
	// Both together.
	tester.EnterText(gluttertest.ByKey(attemptSearchKey), "alice")
	tester.Tap(first{gluttertest.ByText("Accepted")})
	tester.PumpAndSettle()
	if got := shownRows(tester); got != 1 || app.Who(firstRow(tester)) != "alice" {
		t.Errorf("alice and Accepted show %d rows", got)
	}
	// Nothing matches.
	tester.EnterText(gluttertest.ByKey(attemptSearchKey), "no such thing")
	tester.PumpAndSettle()
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Nothing matches.")); err != nil {
		t.Error(err)
	}
}

func TestSorting(t *testing.T) {
	tester := pump(t, apptest.Sample(t), testutil.Wide, "attempts")
	// The default is the last attempt, newest first.
	if got := firstRow(tester); app.Who(got) != "alice" || got.Count != 3 {
		t.Errorf("the first row to start with is %s, %d", app.Who(got), got.Count)
	}
	tester.Tap(gluttertest.ByText("Count"))
	tester.PumpAndSettle()
	tester.Tap(gluttertest.ByText("Count"))
	tester.PumpAndSettle()
	if got := firstRow(tester); got.Count != 1204 {
		t.Errorf("after two taps on Count the first row has %d, want 1204", got.Count)
	}
	tester.Tap(gluttertest.ByText("Who"))
	tester.PumpAndSettle()
	first := app.Who(firstRow(tester))
	tester.Tap(gluttertest.ByText("Who"))
	tester.PumpAndSettle()
	second := app.Who(firstRow(tester))
	if first == second {
		t.Errorf("a second tap on Who did not turn the order around (%s both times)", first)
	}
}

func TestRemoveAndClear(t *testing.T) {
	t.Run("remove", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pump(t, f, testutil.Wide, "attempts")
		pickRow(tester, "203.0.113.9")
		tapKey(tester, removeAttemptKey)
		if got := f.Calls(); len(got) != 1 || got[0] != "RemoveAttempt(a_0000000000000002)" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("clear: cancel and then clear", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pump(t, f, testutil.Wide, "attempts")
		tapKey(tester, clearListKey)
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Clear the list of 9 attempts?")); err != nil {
			t.Fatal(err)
		}
		tapKey(tester, confirmCancelKey)
		eventually(t, tester, "the dialog closes", func() bool { return !tester.Exists(gluttertest.ByKey(confirmYesKey)) })
		if got := f.Calls(); len(got) != 0 {
			t.Fatalf("Cancel made the calls %q", got)
		}
		tapKey(tester, clearListKey)
		tapKey(tester, confirmYesKey)
		eventually(t, tester, "the call", func() bool { return len(f.Calls()) == 1 })
		if got := f.Calls()[0]; got != "ClearAttempts()" {
			t.Errorf("calls = %q", f.Calls())
		}
	})
	t.Run("clearing an empty list asks nothing", func(t *testing.T) {
		tester := pump(t, apptest.Empty(t), testutil.Wide, "attempts")
		tapKey(tester, clearListKey)
		if err := gluttertest.FindsNothing(tester, gluttertest.ByKey(confirmYesKey)); err != nil {
			t.Error(err)
		}
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Nobody tried to connect yet.")); err != nil {
			t.Error(err)
		}
	})
	t.Run("change goes to Settings", func(t *testing.T) {
		tester := pump(t, apptest.Sample(t), testutil.Wide, "attempts")
		tapKey(tester, changeClearKey)
		if got := currentPage(t, tester); got != pageSettings {
			t.Errorf("the page is %d, want Settings", got)
		}
	})
}

func TestTheDetails(t *testing.T) {
	tester := pump(t, apptest.Sample(t), testutil.Wide, "attempts")
	if err := gluttertest.FindsNothing(tester, gluttertest.ByKey(removeAttemptKey)); err != nil {
		t.Fatalf("details before a row is selected: %v", err)
	}
	pickRow(tester, "198.51.100.23")
	for _, want := range []string{"SSH-2.0-OpenSSH_8.4", "This key belongs to bob.", "Wrong key"} {
		if tester.Count(gluttertest.ByTextContaining(want)) == 0 {
			t.Errorf("the details do not say %q", want)
		}
	}
	// The selection stays with the entry, not the row: a new entry in front of it does not move it.
	f := apptest.Sample(t)
	tester = pump(t, f, testutil.Wide, "attempts")
	pickRow(tester, "198.51.100.23")
	f.AttemptsSnapshot.Entries = append([]attempts.Entry{{
		ID: "a_00000000000000ff", Outcome: attempts.UnknownName, LoginName: "new", Address: "192.0.2.99",
		Count: 1, FirstAt: apptest.Now, LastAt: apptest.Now,
	}}, f.AttemptsSnapshot.Entries...)
	f.Emit(app.Changed{Topics: app.TopicAttempts})
	eventually(t, tester, "the new entry", func() bool { return tester.Exists(gluttertest.ByText("192.0.2.99")) })
	if got := theAttemptsState(tester).selected; got != "a_0000000000000004" {
		t.Errorf("the selection moved to %s", got)
	}
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByTextContaining("This key belongs to bob.")); err != nil {
		t.Error(err)
	}
	// When the entry goes, the details go.
	var rest []attempts.Entry
	for _, e := range f.AttemptsSnapshot.Entries {
		if e.ID != "a_0000000000000004" {
			rest = append(rest, e)
		}
	}
	f.AttemptsSnapshot.Entries = rest
	f.Emit(app.Changed{Topics: app.TopicAttempts})
	eventually(t, tester, "the details go", func() bool { return !tester.Exists(gluttertest.ByKey(removeAttemptKey)) })
}

// manyAttempts is a fake with n entries, all different.
func manyAttempts(t *testing.T, n int) *apptest.Fake {
	t.Helper()
	f := apptest.Empty(t)
	for i := 0; i < n; i++ {
		f.AttemptsSnapshot.Entries = append(f.AttemptsSnapshot.Entries, attempts.Entry{
			ID: fmt.Sprintf("a_%016x", i+1), Outcome: attempts.UnknownName, LoginName: fmt.Sprintf("user%d", i),
			Address: "203.0.113.9", Count: 1, FirstAt: apptest.Now, LastAt: apptest.Now.Add(-time.Duration(i) * time.Second),
		})
	}
	return f
}

func TestOnlyTheFirstRowsAreBuilt(t *testing.T) {
	// The time to show 5 000 entries is about the time to show the 500 that are built, however
	// fast the machine is (a race detector makes it several times slower, and both with it): if
	// every row were built it would be ten times as long.
	start := time.Now()
	pump(t, manyAttempts(t, maxAttemptRows), testutil.Wide, "attempts")
	base := time.Since(start)
	start = time.Now()
	tester := pump(t, manyAttempts(t, 5000), testutil.Wide, "attempts")
	if took := time.Since(start); took > 3*base+500*time.Millisecond {
		t.Errorf("5000 entries took %v to show, 500 took %v: the rows that are not shown are built", took, base)
	}
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Showing 500 of 5000. Search to see others.")); err != nil {
		t.Error(err)
	}
	// The search narrows it below the cap.
	tester.EnterText(gluttertest.ByKey(attemptSearchKey), "user4999")
	tester.PumpAndSettle()
	if err := gluttertest.FindsNothing(tester, gluttertest.ByKey(foundation.NewKey("attempts-cap"))); err != nil {
		t.Error(err)
	}
	if got := shownRows(tester); got != 1 {
		t.Errorf("the search for user4999 shows %d rows", got)
	}
}

func TestTheTimesMoveOn(t *testing.T) {
	f := apptest.Sample(t)
	tester := pump(t, f, testutil.Wide, "attempts")
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Cleared 3 d ago · next clear in 4 d")); err != nil {
		t.Fatal(err)
	}
	f.Clock.Advance(24 * time.Hour)
	for i := 0; i < 2*24*2; i++ { // a day of ticks
		f.Clock.Advance(tickEvery)
		tester.FlushPosted()
	}
	tester.PumpAndSettle()
	if tester.Exists(gluttertest.ByText("Cleared 3 d ago · next clear in 4 d")) {
		t.Error("the line did not move on with the clock")
	}
}

func TestAttemptsWithTheRealApplication(t *testing.T) {
	r := apptest.Open(t, nil)
	key := r.Person("alice")
	r.Connect("alice", key)
	r.Refuse("nobody", key)
	r.Refuse("nobody", key)
	// The second refusal goes into the entry of the first, so two entries are not enough: the
	// three attempts must all be counted.
	r.Eventually("the application does not show the three attempts in two entries", func() bool {
		entries := r.App.Attempts().Entries
		return len(entries) == 2 && entries[0].Count+entries[1].Count == 3
	})
	tester := pumpOptions(t, r.App, testutil.Wide, Options{Page: "attempts", Clock: r.Clock}, syncRunner)
	counts := map[string]int64{}
	for _, e := range theAttemptsState(tester).visible(theAttemptsState(tester).Widget().v) {
		counts[app.Who(e)+"/"+app.ResultText(e.Outcome)] = e.Count
	}
	if counts["alice/Accepted"] != 1 || counts["Unknown/Unknown name"] != 2 || len(counts) != 2 {
		t.Errorf("rows = %v, want alice accepted once and an unknown name twice", counts)
	}
	for _, want := range []string{"Accepted", "Unknown name", "alice", "nobody"} {
		if tester.Count(gluttertest.ByText(want)) == 0 {
			t.Errorf("the table does not show %q", want)
		}
	}
}
