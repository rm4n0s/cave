package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/attempts"
)

const hostile = "root\uFFFD[31mX"

func TestGoldenAttempts(t *testing.T) {
	tests := []struct {
		name          string
		fake          func() *apptest.Fake
		width, height int
		keys          []string
		text          string // typed after the keys
		after         []string
	}{
		{"sample-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, nil, "", nil},
		{"sample-80x24", func() *apptest.Fake { return apptest.Sample(t) }, 80, 24, nil, "", nil},
		{"sample-60x16", func() *apptest.Fake { return apptest.Sample(t) }, 60, 16, nil, "", nil},
		{"empty-100x30", func() *apptest.Fake { return apptest.Empty(t) }, 100, 30, nil, "", nil},
		{"refused-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, []string{"f", "f"}, "", nil},
		{"accepted-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, []string{"f"}, "", nil},
		{"search-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, []string{"/"}, "198.51", []string{"enter"}},
		{"searching-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, []string{"/"}, "198.51", nil},
		{"nothing-matches-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, []string{"/"}, "zzz", []string{"enter"}},
		{"details-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, []string{"down", "down", "down", "enter"}, "", nil},
		{"details-60x16", func() *apptest.Fake { return apptest.Sample(t) }, 60, 16, []string{"down", "down", "down", "enter"}, "", nil},
		{"clear-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, []string{"C"}, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newFakeHarness(t, tt.fake(), tt.width, tt.height)
			h.key("3")
			h.keys(tt.keys...)
			h.typeText(tt.text)
			h.keys(tt.after...)
			h.golden("attempts-" + tt.name)
		})
	}
}

func TestAttemptsFirstLine(t *testing.T) {
	f := apptest.Sample(t)
	h := newFakeHarness(t, f, 100, 30)
	h.key("3")
	first := func() string { return strings.Split(h.screen(), "\n")[3] }
	if got := strings.TrimSpace(first()); got != "Filter: All   Search: —   Cleared 3 d ago · next clear in 4 d" {
		t.Errorf("the first line is %q", got)
	}
	f.AttemptsSnapshot.NextClearAt = time.Time{}
	h.send(changedMsg{})
	if got := strings.TrimSpace(first()); got != "Filter: All   Search: —   Cleared 3 d ago" {
		t.Errorf("without a next clear: %q", got)
	}
	f.AttemptsSnapshot.LastClearedAt = time.Time{}
	h.send(changedMsg{})
	if got := strings.TrimSpace(first()); got != "Filter: All   Search: —   never cleared" {
		t.Errorf("never cleared: %q", got)
	}
	// The times move with the clock of the model.
	f.AttemptsSnapshot.LastClearedAt = apptest.Now.Add(-time.Hour)
	f.Clock.Advance(2 * time.Hour)
	h.send(tickMsg{})
	if got := strings.TrimSpace(first()); got != "Filter: All   Search: —   Cleared 3 h ago" {
		t.Errorf("after the clock moved: %q", got)
	}
}

func TestAttemptsTableColumns(t *testing.T) {
	for _, tt := range []struct {
		width     int
		loginName bool
	}{{60, false}, {79, false}, {80, true}, {100, true}} {
		h := newFakeHarness(t, apptest.Sample(t), tt.width, 30)
		h.key("3")
		title := strings.Split(h.screen(), "\n")[4]
		if got := strings.Contains(title, "Login name"); got != tt.loginName {
			t.Errorf("width %d: the title line is %q", tt.width, title)
		}
		for _, want := range []string{"Who", "Address", "Result", "Count", "Last"} {
			if !strings.Contains(title, want) {
				t.Errorf("width %d: no %q in %q", tt.width, want, title)
			}
		}
	}
}

func TestHostileLoginNameIsDrawnAsText(t *testing.T) {
	h := newFakeHarness(t, apptest.Sample(t), 100, 30)
	h.key("3")
	lines := strings.Split(h.screen(), "\n")
	row := -1
	for i, l := range lines {
		if strings.Contains(l, hostile) {
			row = i
		}
	}
	if row < 0 {
		t.Fatalf("the login name is not on the screen as it was stored:\n%s", h.screen())
	}
	if !strings.Contains(lines[row], "Unknown name") || !strings.Contains(lines[row+1], "Wrong key") {
		t.Errorf("the rows around it are not where they belong:\n%s\n%s", lines[row], lines[row+1])
	}
	// No escape sequence came from the text: the one inside it was replaced when it was stored.
	if strings.Contains(h.raw(), "\x1b[31mX") {
		t.Error("the text started a color")
	}
	// Selecting the row and opening it does not interpret it either.
	h.keys("down", "down", "enter")
	if !strings.Contains(h.screen(), hostile) || strings.Contains(h.raw(), "\x1b[31mX") {
		t.Errorf("the details:\n%s", h.screen())
	}
}

func TestAttemptFilters(t *testing.T) {
	f := apptest.Sample(t)
	h := newFakeHarness(t, f, 100, 30)
	h.key("3")
	counts := []int{len(h.m().visibleAttempts())}
	names := []string{"All"}
	for i := 0; i < 3; i++ {
		h.key("f")
		counts = append(counts, len(h.m().visibleAttempts()))
		names = append(names, filterName(h.m().attemptFilter))
	}
	if want := []int{9, 1, 8, 9}; !equalInts(counts, want) {
		t.Errorf("the rows for each filter: %v, want %v", counts, want)
	}
	if want := []string{"All", "Accepted", "Refused", "All"}; strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("the filters go through %v", names)
	}
	for _, e := range func() []attempts.Entry { h.key("f"); h.key("f"); return h.m().visibleAttempts() }() {
		if e.Outcome == attempts.Accepted {
			t.Errorf("the refused filter lets %s through", e.Outcome)
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAttemptSearch(t *testing.T) {
	h := newFakeHarness(t, apptest.Sample(t), 100, 30)
	h.keys("3", "/")
	visible := func() int { return len(h.m().visibleAttempts()) }
	// The table follows each key.
	h.typeText("1")
	afterOne := visible()
	h.typeText("98.51")
	if afterOne <= visible() || visible() != 3 {
		t.Errorf("after '1' %d rows, after '198.51' %d, want more and then 3", afterOne, visible())
	}
	// Enter keeps the search and closes the field.
	h.key("enter")
	if h.m().prompt != nil || h.m().attemptSearch != "198.51" || visible() != 3 {
		t.Errorf("enter: prompt %v, search %q, %d rows", h.m().prompt, h.m().attemptSearch, visible())
	}
	if !strings.Contains(h.screen(), "Search: 198.51") {
		t.Errorf("the first line does not show the search:\n%s", h.screen())
	}
	// A new search starts from the old text; Esc clears it.
	h.key("/")
	if h.m().prompt.f.value() != "198.51" {
		t.Errorf("the field starts with %q", h.m().prompt.f.value())
	}
	h.key("esc")
	if h.m().attemptSearch != "" || visible() != 9 {
		t.Errorf("esc: search %q, %d rows", h.m().attemptSearch, visible())
	}
	// It ignores case, and matches who, the login name and the address.
	for needle, want := range map[string]int{"ALICE": 3, "admin": 1, "2001:db8": 1, "bob": 1, "nothing at all": 0} {
		h.key("/")
		h.typeText(needle)
		if visible() != want {
			t.Errorf("a search for %q gives %d rows, want %d", needle, visible(), want)
		}
		h.key("esc")
	}
	// The search and the filter work together.
	h.key("f") // accepted
	h.key("/")
	h.typeText("198.51")
	if visible() != 1 {
		t.Errorf("accepted and 198.51: %d rows, want 1", visible())
	}
	// While the field is open every key is the field's.
	h.typeText("q?s3")
	if h.quit || h.m().help || h.m().page != pageAttempts {
		t.Error("a key of the field did something else")
	}
}

func TestAttemptDetails(t *testing.T) {
	f := apptest.Sample(t)
	h := newFakeHarness(t, f, 100, 40)
	h.keys("3", "down", "down", "down", "enter")
	screen := h.screen()
	for _, want := range []string{
		"Result", "Wrong key", "alice", "198.51.100.23", "3", "2026-11-22 12:00:00 UTC", "2026-11-24 09:00:00 UTC",
		"SSH-2.0-OpenSSH_8.4", "This key belongs to bob.",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("the details lack %q:\n%s", want, screen)
		}
	}
	if n := strings.Count(screen, "SHA256:"); n != 2 {
		t.Errorf("%d key fingerprints, want 2:\n%s", n, screen)
	}
	// An entry that offered no key and has no owner.
	h.key("esc")
	if h.m().attemptDetail {
		t.Error("esc did not close the details")
	}
	h.keys("down", "enter") // the entry with no key offered
	if !strings.Contains(h.screen(), "none offered") || strings.Contains(h.screen(), "This key belongs") {
		t.Errorf("an entry without keys:\n%s", h.screen())
	}
	// Enter closes them too, and the keys of the page do nothing in the details.
	h.keys("d", "C", "f")
	if h.m().ask != nil || h.m().attemptFilter != 0 || len(f.Calls()) != 0 {
		t.Error("a key of the table acted in the details")
	}
	h.key("enter")
	if h.m().attemptDetail {
		t.Error("enter did not close the details")
	}
	// The details are closed when their entry goes.
	h.key("enter")
	f.AttemptsSnapshot.Entries = f.AttemptsSnapshot.Entries[:1]
	h.send(changedMsg{})
	if h.m().attemptDetail {
		t.Error("the details of an entry that is gone are still open")
	}
}

func TestRemoveAndClearAttempts(t *testing.T) {
	f := apptest.Sample(t)
	h := newFakeHarness(t, f, 100, 30)
	h.keys("3", "down", "d")
	if got := f.Calls(); len(got) != 1 || got[0] != "RemoveAttempt(a_0000000000000002)" {
		t.Errorf("calls = %q", got)
	}
	h.key("C")
	if !strings.Contains(h.screen(), "Clear the list of 9 attempts?") {
		t.Fatalf("the question:\n%s", h.screen())
	}
	h.key("enter") // the default is no
	h.keys("C", "n")
	if len(f.Calls()) != 1 {
		t.Fatalf("calls = %q", f.Calls())
	}
	h.keys("C", "y")
	if got := f.Calls(); len(got) != 2 || got[1] != "ClearAttempts()" {
		t.Errorf("calls = %q", got)
	}
	// With one attempt the question is in the singular; with none there is no question.
	f.AttemptsSnapshot.Entries = f.AttemptsSnapshot.Entries[:1]
	h.send(changedMsg{})
	h.key("C")
	if !strings.Contains(h.screen(), "Clear the list of 1 attempt?") {
		t.Errorf("the question:\n%s", h.screen())
	}
	h.key("n")
	f.AttemptsSnapshot.Entries = nil
	h.send(changedMsg{})
	h.keys("C", "d", "enter", "/")
	h.key("esc")
	if h.m().ask != nil || len(f.Calls()) != 2 {
		t.Errorf("ask %v, calls %q", h.m().ask, f.Calls())
	}
	if !strings.Contains(h.screen(), "Nobody tried to connect yet.") {
		t.Errorf("the empty page:\n%s", h.screen())
	}
}

func TestEmptyTexts(t *testing.T) {
	h := newFakeHarness(t, apptest.Sample(t), 100, 30)
	h.keys("3", "/")
	h.typeText("zzz")
	h.key("enter")
	if !strings.Contains(h.screen(), "Nothing matches.") || strings.Contains(h.screen(), "Nobody tried") {
		t.Errorf("a search that matches nothing:\n%s", h.screen())
	}
}

func TestAttemptsKeepsTheSelection(t *testing.T) {
	f := apptest.Sample(t)
	h := newFakeHarness(t, f, 100, 30)
	h.keys("3", "down", "down") // the third entry
	selected := h.m().attemptSel
	// A new attempt arrives while the user reads: it comes first, and the row stays under the
	// cursor.
	fresh := f.AttemptsSnapshot.Entries[0]
	fresh.ID, fresh.LoginName, fresh.Count = "a_00000000000000ff", "newcomer", 1
	f.AttemptsSnapshot.Entries = append([]attempts.Entry{fresh}, f.AttemptsSnapshot.Entries...)
	h.send(changedMsg{})
	if e, _ := h.m().selectedAttempt(); e.ID != selected || h.m().attemptList.selected != 3 {
		t.Errorf("selected %s at row %d, want %s at row 3", e.ID, h.m().attemptList.selected, selected)
	}
	// The selected entry is removed: the row stays where it was.
	var rest []attempts.Entry
	for _, e := range f.AttemptsSnapshot.Entries {
		if e.ID != selected {
			rest = append(rest, e)
		}
	}
	f.AttemptsSnapshot.Entries = rest
	h.send(changedMsg{})
	if e, ok := h.m().selectedAttempt(); !ok || e.ID == selected || h.m().attemptList.selected != 3 {
		t.Errorf("after the removal: %v at row %d", e.ID, h.m().attemptList.selected)
	}
	// Everything goes.
	f.AttemptsSnapshot.Entries = nil
	h.send(changedMsg{})
	if _, ok := h.m().selectedAttempt(); ok || h.m().attemptSel != "" {
		t.Error("something is selected in an empty list")
	}
}

func TestAttemptsListScrolls(t *testing.T) {
	f := apptest.Sample(t)
	template := f.AttemptsSnapshot.Entries[0]
	f.AttemptsSnapshot.Entries = nil
	for i := 0; i < 40; i++ {
		e := template
		e.ID = "a_" + strings.Repeat("0", 12) + "00" + string("0123456789abcdef"[i/16]) + string("0123456789abcdef"[i%16])
		e.LoginName = "login" + itoa(i)
		f.AttemptsSnapshot.Entries = append(f.AttemptsSnapshot.Entries, e)
	}
	h := newFakeHarness(t, f, 100, 20)
	h.keys("3", "end")
	if !strings.Contains(h.screen(), "login39") {
		t.Errorf("the last row is not in view:\n%s", h.screen())
	}
	h.key("home")
	if !strings.Contains(h.screen(), "login0") {
		t.Errorf("the first row is not in view:\n%s", h.screen())
	}
}

// ---- with the real application

func TestAttemptsWithTheRealApplication(t *testing.T) {
	r := apptest.Open(t, nil)
	key := r.Person("alice")
	r.Connect("alice", key)
	r.Refuse("nobody", key)
	r.Refuse("nobody", key)
	r.Eventually("the list does not have the three entries", func() bool {
		entries := r.App.Attempts().Entries
		return len(entries) == 2 && entries[0].Count+entries[1].Count == 3
	})
	h := newHarness(t, r.App, r.Clock, 120, 30)
	h.key("3")
	screen := h.screen()
	if !strings.Contains(screen, "Accepted") || !strings.Contains(screen, "Unknown name") {
		t.Fatalf("the table:\n%s", screen)
	}
	var refused string
	for _, l := range strings.Split(screen, "\n") {
		if strings.Contains(l, "Unknown name") {
			refused = l
		}
	}
	if fields := strings.Fields(refused); len(fields) < 5 || fields[len(fields)-4] != "2" && fields[len(fields)-3] != "2" {
		t.Errorf("the refused row has no count of 2: %q", refused)
	}
	if !strings.Contains(refused, "nobody") {
		t.Errorf("the refused row does not have the login name: %q", refused)
	}
	// Clear the list.
	h.keys("C", "y")
	h.send(changedMsg{})
	if n := len(r.App.Attempts().Entries); n != 0 {
		t.Errorf("%d entries are left", n)
	}
	if !strings.Contains(h.screen(), "Nobody tried to connect yet.") {
		t.Errorf("the page:\n%s", h.screen())
	}
}

func TestRemovingOneAttemptWithTheRealApplication(t *testing.T) {
	r := apptest.Open(t, nil)
	key := r.Person("alice")
	r.Refuse("nobody", key)
	r.Refuse("ghost", key)
	r.Eventually("two entries", func() bool { return len(r.App.Attempts().Entries) == 2 })
	h := newHarness(t, r.App, r.Clock, 120, 30)
	h.keys("3", "d")
	h.send(changedMsg{})
	if n := len(r.App.Attempts().Entries); n != 1 || len(h.m().visibleAttempts()) != 1 {
		t.Errorf("%d entries in the application, %d on the page", n, len(h.m().visibleAttempts()))
	}
}
