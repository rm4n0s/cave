package attempts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/cave/internal/core/storage/storagetest"
	"github.com/rm4n0s/errors"
)

var ctx = context.Background()

// start is the time of the tests' clock.
var start = time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)

// fixture is a list in its own folders, with its clock and what it logs.
type fixture struct {
	*List
	dirs  storage.Dirs
	clock *clock.Fake
	log   *bytes.Buffer
	path  string
}

// newFixture opens an empty list.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{dirs: storagetest.Isolate(t), clock: clock.NewFake(start), log: &bytes.Buffer{}}
	f.path = filepath.Join(f.dirs.State, FileName)
	f.reopen(t)
	return f
}

// reopen opens the list again from its file.
func (f *fixture) reopen(t *testing.T) {
	t.Helper()
	l, err := Open(ctx, Options{Dirs: f.dirs, Clock: f.clock, Logger: slog.New(slog.NewJSONHandler(f.log, nil))})
	if err != nil {
		t.Fatal(err)
	}
	f.List = l
}

// fileExists says whether the attempts file is there.
func (f *fixture) fileExists(t *testing.T) bool {
	t.Helper()
	_, err := os.Stat(f.path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return err == nil
}

// writeFile writes the attempts file.
func (f *fixture) writeFile(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(f.path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// wrongKey is an attempt of alice with a wrong key from an address.
func wrongKey(address string) Attempt {
	return Attempt{Outcome: WrongKey, PersonID: "p_1111111111111111", PersonName: "alice", LoginName: "alice", Address: address,
		ClientVersion: "SSH-2.0-OpenSSH_10.6", Keys: []string{"SHA256:aaa"}}
}

func TestRecordCounts(t *testing.T) {
	f := newFixture(t)
	first := f.Record(wrongKey("203.0.113.7"))
	if first.Count != 1 || !first.FirstAt.Equal(start) || !first.LastAt.Equal(start) || first.Outcome != WrongKey ||
		first.PersonName != "alice" || first.LoginName != "alice" || first.Address != "203.0.113.7" || !strings.HasPrefix(first.ID, "a_") {
		t.Errorf("the first entry is %+v", first)
	}

	f.clock.Advance(time.Minute)
	again := wrongKey("203.0.113.7")
	again.ClientVersion = "SSH-2.0-PuTTY_Release_0.83"
	again.Keys = []string{"SHA256:bbb", "SHA256:ccc"}
	again.KeyOwner = "bob"
	f.Record(again)
	f.clock.Advance(time.Minute)
	third := f.Record(again)
	if third.ID != first.ID || third.Count != 3 || !third.FirstAt.Equal(start) || !third.LastAt.Equal(start.Add(2*time.Minute)) {
		t.Errorf("after three attempts: %+v", third)
	}
	// The details are those of the last attempt.
	if third.ClientVersion != "SSH-2.0-PuTTY_Release_0.83" || !reflect.DeepEqual(third.Keys, []string{"SHA256:bbb", "SHA256:ccc"}) || third.KeyOwner != "bob" {
		t.Errorf("the details are not the last attempt's: %+v", third)
	}

	// Each of address, login name, outcome and person makes another entry.
	others := []Attempt{
		wrongKey("203.0.113.8"),
		{Outcome: WrongKey, PersonID: "p_1111111111111111", PersonName: "alice", LoginName: "Alice", Address: "203.0.113.7"},
		{Outcome: Accepted, PersonID: "p_1111111111111111", PersonName: "alice", LoginName: "alice", Address: "203.0.113.7"},
		{Outcome: WrongKey, PersonID: "p_2222222222222222", PersonName: "alice", LoginName: "alice", Address: "203.0.113.7"},
		{Outcome: UnknownName, LoginName: "root", Address: "203.0.113.7"},
		{Outcome: HandshakeFailed, Address: "203.0.113.7"},
		{Outcome: Refused, Address: "203.0.113.7"},
		{Outcome: NoKey, LoginName: "alice", PersonID: "p_1111111111111111", PersonName: "alice", Address: "203.0.113.7"},
		{Outcome: NotProven, LoginName: "alice", PersonID: "p_1111111111111111", PersonName: "alice", Address: "203.0.113.7"},
		{Outcome: Disabled, LoginName: "alice", PersonID: "p_1111111111111111", PersonName: "alice", Address: "203.0.113.7"},
	}
	for _, a := range others {
		if e := f.Record(a); e.Count != 1 {
			t.Errorf("%+v was counted into another entry: %+v", a, e)
		}
	}
	if f.Len() != 1+len(others) {
		t.Errorf("Len() = %d, want %d", f.Len(), 1+len(others))
	}
	seen := map[string]bool{}
	for _, e := range f.Entries() {
		if seen[e.ID] {
			t.Errorf("the id %s is used twice", e.ID)
		}
		seen[e.ID] = true
	}
}

func TestRecordMakesTextSafe(t *testing.T) {
	f := newFixture(t)
	keys := make([]string, 20)
	for i := range keys {
		keys[i] = fmt.Sprintf("SHA256:key%02d\x1b[0m", i)
	}
	e := f.Record(Attempt{
		Outcome:       UnknownName,
		LoginName:     "\x1b[2J\x1b[31mroot\u202E" + strings.Repeat("x", 200),
		Address:       "203.0.113.7\nfake line",
		ClientVersion: "SSH-2.0-evil\x07\x1b]0;title\x07" + strings.Repeat("v", 200),
		PersonName:    "ali\x00ce",
		KeyOwner:      "b\u200Bob",
		Keys:          keys,
	})
	for name, text := range map[string]string{"login name": e.LoginName, "address": e.Address, "client version": e.ClientVersion,
		"person name": e.PersonName, "key owner": e.KeyOwner, "a key": strings.Join(e.Keys, "")} {
		for _, r := range text {
			if r < 0x20 || r == 0x7f || r == '\u202E' || r == '\u200B' {
				t.Errorf("the %s holds U+%04X: %q", name, r, text)
			}
		}
	}
	if !strings.HasPrefix(e.LoginName, "\uFFFD[2J\uFFFD[31mroot\uFFFD") {
		t.Errorf("login name = %q", e.LoginName)
	}
	if n := len([]rune(e.LoginName)); n != MaxTextLength {
		t.Errorf("the login name has %d characters, want %d", n, MaxTextLength)
	}
	if n := len([]rune(e.ClientVersion)); n != MaxTextLength {
		t.Errorf("the client version has %d characters, want %d", n, MaxTextLength)
	}
	if len(e.Keys) != MaxKeys {
		t.Errorf("%d keys, want %d", len(e.Keys), MaxKeys)
	}
	// The same hostile attempt again is the same entry: the group is made of the safe texts.
	again := f.Record(Attempt{Outcome: UnknownName, LoginName: "\x1b[2J\x1b[31mroot\u202E" + strings.Repeat("x", 200), Address: "203.0.113.7\nfake line"})
	if again.ID != e.ID || again.Count != 2 {
		t.Errorf("the second hostile attempt: %+v", again)
	}
	// Record does not keep the caller's slice.
	keys[0] = "changed"
	if got := f.Entries()[0].Keys; len(got) != 0 {
		t.Errorf("the last attempt had no keys, the entry has %v", got)
	}
}

func TestTheLimitDropsTheOldest(t *testing.T) {
	f := newFixture(t)
	address := func(i int) string { return fmt.Sprintf("10.%d.%d.%d", i>>16, (i>>8)&255, i&255) }
	for i := 0; i < MaxEntries; i++ {
		f.clock.Advance(time.Millisecond) // short steps: few delayed writes
		f.Record(Attempt{Outcome: HandshakeFailed, Address: address(i)})
	}
	if f.Len() != MaxEntries {
		t.Fatalf("Len() = %d, want %d", f.Len(), MaxEntries)
	}
	// The oldest entry gets a new attempt: it is no longer the oldest.
	f.clock.Advance(time.Millisecond) // short steps: few delayed writes
	if e := f.Record(Attempt{Outcome: HandshakeFailed, Address: address(0)}); e.Count != 2 {
		t.Fatalf("the oldest entry was not found: %+v", e)
	}
	for i := MaxEntries; i < MaxEntries+10; i++ {
		f.clock.Advance(time.Millisecond) // short steps: few delayed writes
		f.Record(Attempt{Outcome: HandshakeFailed, Address: address(i)})
	}
	if f.Len() != MaxEntries {
		t.Errorf("Len() = %d after the overflow, want %d", f.Len(), MaxEntries)
	}
	present := map[string]bool{}
	for _, e := range f.Entries() {
		present[e.Address] = true
	}
	if !present[address(0)] {
		t.Error("the entry with a new attempt was dropped")
	}
	for i := 1; i <= 10; i++ {
		if present[address(i)] {
			t.Errorf("the old entry %s was kept", address(i))
		}
	}
	for i := MaxEntries; i < MaxEntries+10; i++ {
		if !present[address(i)] {
			t.Errorf("the new entry %s is missing", address(i))
		}
	}
	if !present[address(11)] {
		t.Error("an entry that was not among the oldest was dropped")
	}
}

func TestEntriesOrder(t *testing.T) {
	f := newFixture(t)
	for _, address := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"} {
		f.Record(Attempt{Outcome: HandshakeFailed, Address: address})
		f.clock.Advance(time.Minute)
	}
	f.Record(Attempt{Outcome: HandshakeFailed, Address: "10.0.0.1"}) // the oldest becomes the newest
	var got []string
	for _, e := range f.Entries() {
		got = append(got, e.Address)
	}
	if want := []string{"10.0.0.1", "10.0.0.3", "10.0.0.2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	// The result is a copy.
	entries := f.Entries()
	entries[0].Count = 99
	if f.Entries()[0].Count != 2 {
		t.Error("the list was changed through a copy")
	}
}

func TestRemoveAndClear(t *testing.T) {
	f := newFixture(t)
	a := f.Record(wrongKey("10.0.0.1"))
	b := f.Record(wrongKey("10.0.0.2"))
	if !f.LastClearedAt().IsZero() {
		t.Error("a new list says it was cleared")
	}

	if err := f.Remove(a.ID); err != nil {
		t.Fatal(err)
	}
	if entries := f.Entries(); len(entries) != 1 || entries[0].ID != b.ID {
		t.Errorf("after the removal: %+v", entries)
	}
	meta := errtest.Route(t, f.Remove(a.ID), "List.Remove.AttemptNotFound")
	if meta["id"] != a.ID {
		t.Errorf("metadata = %v", meta)
	}

	f.clock.Advance(time.Hour)
	f.Clear()
	if f.Len() != 0 {
		t.Errorf("Len() = %d after Clear", f.Len())
	}
	if !f.LastClearedAt().Equal(start.Add(time.Hour)) {
		t.Errorf("LastClearedAt() = %v", f.LastClearedAt())
	}
	if !f.NextClearAt().IsZero() {
		t.Error("a clear by hand set a next time")
	}
	// The same attempt after a clear starts a new count.
	if e := f.Record(wrongKey("10.0.0.2")); e.Count != 1 || e.ID == b.ID {
		t.Errorf("after the clear: %+v", e)
	}
}

func TestPolicies(t *testing.T) {
	periods := []struct {
		policy Policy
		period time.Duration
	}{{Hourly, time.Hour}, {Daily, 24 * time.Hour}, {Weekly, 7 * 24 * time.Hour}, {Monthly, 30 * 24 * time.Hour}}
	for _, p := range periods {
		t.Run(string(p.policy), func(t *testing.T) {
			if got, ok := p.policy.Period(); !ok || got != p.period {
				t.Fatalf("Period() = %v, %v", got, ok)
			}
			f := newFixture(t)
			f.Record(wrongKey("10.0.0.1"))
			f.PolicyChanged(p.policy)
			if want := start.Add(p.period); !f.NextClearAt().Equal(want) {
				t.Fatalf("NextClearAt() = %v, want %v", f.NextClearAt(), want)
			}
			// Before the time: nothing, also at start.
			f.clock.Advance(p.period - time.Second)
			if f.ApplyPolicy(p.policy, false) || f.ApplyPolicy(p.policy, true) || f.Len() != 1 {
				t.Fatal("cleared before the time")
			}
			// At the time: cleared, and the next period starts.
			f.clock.Advance(time.Second)
			if !f.ApplyPolicy(p.policy, false) || f.Len() != 0 {
				t.Fatal("not cleared at the time")
			}
			if !f.LastClearedAt().Equal(start.Add(p.period)) || !f.NextClearAt().Equal(start.Add(2*p.period)) {
				t.Errorf("last = %v, next = %v", f.LastClearedAt(), f.NextClearAt())
			}
			f.Record(wrongKey("10.0.0.1"))
			if f.ApplyPolicy(p.policy, false) {
				t.Error("cleared twice in one period")
			}

			// The time passes while Cave is closed: the list is cleared when it starts.
			if err := f.Flush(ctx); err != nil {
				t.Fatal(err)
			}
			f.clock.Advance(3 * p.period)
			f.reopen(t)
			if f.Len() != 1 {
				t.Fatal("the entry was not in the file")
			}
			if !f.ApplyPolicy(p.policy, true) || f.Len() != 0 {
				t.Error("an overdue period did not clear at start")
			}
			if want := f.clock.Now().Add(p.period); !f.NextClearAt().Equal(want) {
				t.Errorf("NextClearAt() = %v, want %v", f.NextClearAt(), want)
			}
		})
	}

	t.Run("never", func(t *testing.T) {
		f := newFixture(t)
		f.Record(wrongKey("10.0.0.1"))
		f.clock.Advance(1000 * time.Hour)
		if f.ApplyPolicy(Never, true) || f.ApplyPolicy(Never, false) || f.Len() != 1 {
			t.Error("never cleared")
		}
		if _, ok := Never.Period(); ok {
			t.Error("never has a period")
		}
	})
	t.Run("onStart", func(t *testing.T) {
		f := newFixture(t)
		f.Record(wrongKey("10.0.0.1"))
		if f.ApplyPolicy(OnStart, false) || f.Len() != 1 {
			t.Error("onStart cleared while running")
		}
		if !f.ApplyPolicy(OnStart, true) || f.Len() != 0 {
			t.Error("onStart did not clear at start")
		}
		if !f.LastClearedAt().Equal(start) || !f.NextClearAt().IsZero() {
			t.Errorf("last = %v, next = %v", f.LastClearedAt(), f.NextClearAt())
		}
	})
	t.Run("a change of policy restarts the period", func(t *testing.T) {
		f := newFixture(t)
		f.Record(wrongKey("10.0.0.1"))
		f.PolicyChanged(Daily)
		f.clock.Advance(23 * time.Hour)
		f.PolicyChanged(Hourly) // one hour from now, not from the day's start
		if want := start.Add(24 * time.Hour); !f.NextClearAt().Equal(want) {
			t.Errorf("NextClearAt() = %v, want %v", f.NextClearAt(), want)
		}
		f.PolicyChanged(Weekly)
		f.clock.Advance(2 * time.Hour)
		if f.ApplyPolicy(Weekly, false) || f.Len() != 1 {
			t.Error("the old period's time cleared after the change")
		}
		f.PolicyChanged(Never)
		if !f.NextClearAt().IsZero() {
			t.Error("never has a next time")
		}
		f.PolicyChanged(OnStart)
		if !f.NextClearAt().IsZero() {
			t.Error("onStart has a next time")
		}
	})
	t.Run("a period in force without a time starts now", func(t *testing.T) {
		f := newFixture(t)
		f.Record(wrongKey("10.0.0.1"))
		if f.ApplyPolicy(Daily, true) || f.Len() != 1 {
			t.Error("cleared without a time having passed")
		}
		if want := start.Add(24 * time.Hour); !f.NextClearAt().Equal(want) {
			t.Errorf("NextClearAt() = %v, want %v", f.NextClearAt(), want)
		}
	})
	t.Run("a policy that is not periodic drops a left-over time", func(t *testing.T) {
		f := newFixture(t)
		f.PolicyChanged(Hourly)
		f.ApplyPolicy(Never, false)
		if !f.NextClearAt().IsZero() {
			t.Error("the time of the old policy was kept")
		}
	})
}

func TestTheFileIsWrittenAtMostEveryFiveSeconds(t *testing.T) {
	f := newFixture(t)
	if f.fileExists(t) {
		t.Fatal("opening wrote the file")
	}
	f.Record(wrongKey("10.0.0.1"))
	if f.fileExists(t) {
		t.Fatal("the record wrote the file at once")
	}
	f.clock.Advance(SaveDelay - time.Millisecond)
	f.Record(wrongKey("10.0.0.2"))
	if f.fileExists(t) {
		t.Fatal("the file was written before the delay")
	}
	f.clock.Advance(time.Millisecond)
	if !f.fileExists(t) {
		t.Fatal("the file was not written after the delay")
	}
	// Both records are in the one write.
	firstWrite, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(firstWrite, []byte("10.0.0.1")) || !bytes.Contains(firstWrite, []byte("10.0.0.2")) {
		t.Errorf("the write lacks a record:\n%s", firstWrite)
	}

	// A burst: one write, a delay after its first record.
	for i := 0; i < 100; i++ {
		f.Record(wrongKey("10.0.0.3"))
		f.clock.Advance(10 * time.Millisecond)
	}
	if now, _ := os.ReadFile(f.path); !bytes.Equal(now, firstWrite) {
		t.Error("the file was written during the burst")
	}
	f.clock.Advance(SaveDelay)
	if now, _ := os.ReadFile(f.path); !bytes.Contains(now, []byte(`"count":100`)) {
		t.Errorf("the burst is not in the file:\n%s", now)
	}

	// Nothing changed: nothing is written, and no write is pending.
	if err := os.Remove(f.path); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(time.Hour)
	if f.fileExists(t) || f.clock.Pending() != 0 {
		t.Errorf("a write without a change (pending timers: %d)", f.clock.Pending())
	}
	if err := f.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if f.fileExists(t) {
		t.Error("Flush wrote without a change")
	}

	// Flush writes at once and cancels the delayed write.
	f.Record(wrongKey("10.0.0.4"))
	if f.clock.Pending() != 1 {
		t.Fatalf("pending timers: %d, want 1", f.clock.Pending())
	}
	if err := f.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if !f.fileExists(t) || f.clock.Pending() != 0 {
		t.Errorf("after Flush: file %v, pending timers %d", f.fileExists(t), f.clock.Pending())
	}
	if f.log.Len() != 0 {
		t.Errorf("something was logged:\n%s", f.log)
	}
}

func TestTheFileRoundTrips(t *testing.T) {
	f := newFixture(t)
	f.Record(wrongKey("203.0.113.7"))
	f.clock.Advance(time.Minute)
	f.Record(wrongKey("203.0.113.7"))
	f.Record(Attempt{Outcome: Accepted, PersonID: "p_1111111111111111", PersonName: "alice", LoginName: "alice", Address: "2001:db8::1",
		ClientVersion: "SSH-2.0-OpenSSH_10.6", Keys: []string{"SHA256:aaa"}})
	f.Record(Attempt{Outcome: HandshakeFailed, Address: "198.51.100.9"})
	f.clock.Advance(time.Minute)
	f.Clear()
	f.Record(Attempt{Outcome: UnknownName, LoginName: "root", Address: "198.51.100.9", Keys: []string{"SHA256:zzz"}, KeyOwner: "alice"})
	f.Record(wrongKey("203.0.113.7"))
	f.PolicyChanged(Weekly)
	before := f.Entries()
	if err := f.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(f.path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 && filepath.Separator == '/' {
		t.Errorf("the file's mode is %o, want 600", info.Mode().Perm())
	}

	var onDisk struct {
		Version       int
		LastClearedAt time.Time
		NextClearAt   time.Time
		Entries       []map[string]any
	}
	data, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("the file is not JSON: %v", err)
	}
	if onDisk.Version != 1 || len(onDisk.Entries) != 2 || !onDisk.LastClearedAt.Equal(start.Add(2*time.Minute)) ||
		!onDisk.NextClearAt.Equal(start.Add(2*time.Minute+7*24*time.Hour)) {
		t.Errorf("the file is not as docs/SPEC.md §8.5 says:\n%s", data)
	}

	f.reopen(t)
	if after := f.Entries(); !reflect.DeepEqual(after, before) {
		t.Errorf("after reopening:\n%+v\nwant:\n%+v", after, before)
	}
	if !f.LastClearedAt().Equal(start.Add(2*time.Minute)) || !f.NextClearAt().Equal(start.Add(2*time.Minute+7*24*time.Hour)) {
		t.Errorf("last = %v, next = %v", f.LastClearedAt(), f.NextClearAt())
	}
	// The same attempt after reopening counts into the entry from the file.
	if e := f.Record(wrongKey("203.0.113.7")); e.Count != 2 {
		t.Errorf("after reopening, the count is %d, want 2", e.Count)
	}
}

func TestOpenRefusesBrokenFiles(t *testing.T) {
	entry := func(id, outcome string, count int) string {
		return fmt.Sprintf(`{"id":%q,"outcome":%q,"address":"10.0.0.1","count":%d,"firstAt":"2026-11-24T10:00:00Z","lastAt":"2026-11-24T11:00:00Z"}`, id, outcome, count)
	}
	const id1, id2 = "a_1111111111111111", "a_2222222222222222"
	tests := []struct {
		name      string
		content   string
		wantRoute string
	}{
		{"not JSON", `{"version":1,"entries":[`, "Open.AttemptsParseFailed"},
		{"empty", ``, "Open.AttemptsParseFailed"},
		{"entries of the wrong type", `{"version":1,"entries":{}}`, "Open.AttemptsParseFailed"},
		{"a newer version", `{"version":2,"entries":[]}`, "Open.AttemptsVersionUnsupported"},
		{"no version", `{"entries":[]}`, "Open.AttemptsVersionUnsupported"},
		{"an outcome that does not exist", `{"version":1,"entries":[` + entry(id1, "welcomed", 1) + `]}`, "Open.AttemptsFileInvalid"},
		{"a count of zero", `{"version":1,"entries":[` + entry(id1, "accepted", 0) + `]}`, "Open.AttemptsFileInvalid"},
		{"a negative count", `{"version":1,"entries":[` + entry(id1, "accepted", -4) + `]}`, "Open.AttemptsFileInvalid"},
		{"a malformed id", `{"version":1,"entries":[` + entry("first", "accepted", 1) + `]}`, "Open.AttemptsFileInvalid"},
		{"a repeated id", `{"version":1,"entries":[` + entry(id1, "accepted", 1) + "," + entry(id1, "refused", 1) + `]}`, "Open.AttemptsFileInvalid"},
		{"a last time before the first", `{"version":1,"entries":[{"id":"` + id2 + `","outcome":"refused","address":"10.0.0.1","count":1,"firstAt":"2026-11-24T11:00:00Z","lastAt":"2026-11-24T10:00:00Z"}]}`, "Open.AttemptsFileInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.writeFile(t, tt.content)
			_, err := Open(ctx, Options{Dirs: f.dirs, Clock: f.clock, Logger: slog.New(slog.NewJSONHandler(f.log, nil))})
			meta := errtest.Route(t, err, tt.wantRoute)
			if meta["path"] != f.path {
				t.Errorf("metadata = %v", meta)
			}
			if data, _ := os.ReadFile(f.path); string(data) != tt.content {
				t.Error("the broken file was changed")
			}
		})
	}

	t.Run("a file that is too large", func(t *testing.T) {
		f := newFixture(t)
		if err := os.WriteFile(f.path, make([]byte, maxFileBytes+1), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Open(ctx, Options{Dirs: f.dirs, Clock: f.clock})
		errtest.Route(t, err, "Open->ReadFile.FileTooLarge")
	})
}

func TestOpenTidiesWhatAFileMayHold(t *testing.T) {
	f := newFixture(t)
	// Hostile text put in the file by hand, a count past the limit, and two entries of one group.
	f.writeFile(t, `{"version":1,"entries":[
		{"id":"a_1111111111111111","outcome":"unknownName","loginName":"ro\u001b[31mot","address":"10.0.0.1","count":3,"firstAt":"2026-11-24T10:00:00Z","lastAt":"2026-11-24T11:00:00Z","clientVersion":"old"},
		{"id":"a_2222222222222222","outcome":"unknownName","loginName":"ro\u001b[31mot","address":"10.0.0.1","count":4,"firstAt":"2026-11-24T09:00:00Z","lastAt":"2026-11-24T12:00:00Z","clientVersion":"new"},
		{"id":"a_3333333333333333","outcome":"refused","address":"10.0.0.2","count":99007199254740993,"firstAt":"2026-11-24T10:00:00Z","lastAt":"2026-11-24T10:00:00Z"}
	]}`)
	f.reopen(t)
	entries := f.Entries()
	if len(entries) != 2 {
		t.Fatalf("%d entries, want 2: %+v", len(entries), entries)
	}
	merged := entries[0]
	if merged.LoginName != "ro\uFFFD[31mot" || merged.Count != 7 || merged.ClientVersion != "new" ||
		!merged.FirstAt.Equal(time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)) || !merged.LastAt.Equal(time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("the merged entry is %+v", merged)
	}
	if entries[1].Count != MaxCount {
		t.Errorf("count = %d, want %d", entries[1].Count, int64(MaxCount))
	}
	// A count at the limit stays there.
	if e := f.Record(Attempt{Outcome: Refused, Address: "10.0.0.2"}); e.Count != MaxCount {
		t.Errorf("count after one more = %d, want %d", e.Count, int64(MaxCount))
	}
}

func TestOpenKeepsOnlyTheNewestWhenTheFileHasTooMany(t *testing.T) {
	f := newFixture(t)
	var b strings.Builder
	b.WriteString(`{"version":1,"entries":[`)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < MaxEntries+25; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		at := base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		fmt.Fprintf(&b, `{"id":"a_%016x","outcome":"refused","address":"10.%d.%d.%d","count":1,"firstAt":%q,"lastAt":%q}`, i, i>>16, (i>>8)&255, i&255, at, at)
	}
	b.WriteString(`]}`)
	f.writeFile(t, b.String())
	f.reopen(t)
	entries := f.Entries()
	if len(entries) != MaxEntries {
		t.Fatalf("%d entries, want %d", len(entries), MaxEntries)
	}
	if oldest := entries[len(entries)-1]; !oldest.LastAt.Equal(base.Add(25 * time.Minute)) {
		t.Errorf("the oldest kept entry is from %v, want the 26th", oldest.LastAt)
	}
}

func TestADelayedWriteThatFailsIsLoggedAndTriedAgain(t *testing.T) {
	f := newFixture(t)
	f.Record(wrongKey("10.0.0.1"))
	if err := os.RemoveAll(f.dirs.State); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(SaveDelay)
	var logged struct {
		Level string           `json:"level"`
		Err   errors.ErrorJson `json:"err"`
	}
	if err := json.Unmarshal(f.log.Bytes(), &logged); err != nil {
		t.Fatalf("the log is not one JSON line: %v\n%s", err, f.log)
	}
	// A logged route starts below the function that logged it, here the delayed save.
	if logged.Level != "ERROR" || logged.Err.Route() != "List.Flush->WriteFileAtomic->writeFileAtomic.StorageWriteCreateFailed" {
		t.Errorf("logged %s with the route %s", logged.Level, logged.Err.Route())
	}
	// Flush reports the same failure to its caller.
	errtest.Route(t, f.Flush(ctx), "List.Flush->WriteFileAtomic->writeFileAtomic.StorageWriteCreateFailed")

	// The folder is back: the next change is written with the one that failed.
	if err := os.MkdirAll(f.dirs.State, 0o700); err != nil {
		t.Fatal(err)
	}
	f.Record(wrongKey("10.0.0.2"))
	f.clock.Advance(SaveDelay)
	data, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatalf("the file was not written after the folder came back: %v", err)
	}
	if !bytes.Contains(data, []byte("10.0.0.1")) || !bytes.Contains(data, []byte("10.0.0.2")) {
		t.Errorf("the file lacks a record:\n%s", data)
	}
}

func TestATimeThatJSONCannotHold(t *testing.T) {
	f := newFixture(t)
	// A clock in the year 10000: encoding/json refuses a year with five digits.
	f.clock = clock.NewFake(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC))
	f.reopen(t)
	f.Record(wrongKey("10.0.0.1"))
	errtest.Route(t, f.Flush(ctx), "List.Flush.AttemptsEncodeFailed")
	if f.fileExists(t) {
		t.Error("the file was written")
	}
}

func TestFlushWithACancelledContext(t *testing.T) {
	f := newFixture(t)
	f.Record(wrongKey("10.0.0.1"))
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	errtest.Route(t, f.Flush(cancelled), "List.Flush->WriteFileAtomic->writeFileAtomic.StorageWriteCancelled")
	// The list is still behind its file: a Flush that can write does.
	if err := f.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if !f.fileExists(t) {
		t.Error("the change was lost by the failed Flush")
	}
	_, err := Open(cancelled, Options{Dirs: f.dirs, Clock: f.clock})
	errtest.Route(t, err, "Open->ReadFile.StorageReadCancelled")
}

func TestListeners(t *testing.T) {
	f := newFixture(t)
	var first, second int
	cancel := f.Subscribe(func() { first++ })
	f.Subscribe(func() { second++ })
	e := f.Record(wrongKey("10.0.0.1"))    // 1
	f.Record(wrongKey("10.0.0.1"))         // 2
	if err := f.Remove(e.ID); err != nil { // 3
		t.Fatal(err)
	}
	if err := f.Remove(e.ID); err == nil { // not a change
		t.Fatal("removing twice worked")
	}
	f.Clear()               // 4
	f.PolicyChanged(Hourly) // 5
	f.ApplyPolicy(Hourly, false)
	f.clock.Advance(time.Hour)
	f.ApplyPolicy(Hourly, false) // 6: cleared
	if first != 6 || second != 6 {
		t.Errorf("first = %d, second = %d; want 6 and 6", first, second)
	}
	cancel()
	cancel()
	f.Record(wrongKey("10.0.0.1"))
	if first != 6 || second != 7 {
		t.Errorf("after the cancel: first = %d, second = %d; want 6 and 7", first, second)
	}
}

func TestConcurrentUse(t *testing.T) {
	f := newFixture(t)
	var wg sync.WaitGroup
	const writers, perWriter = 8, 200
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				f.Record(wrongKey(fmt.Sprintf("10.0.%d.%d", w, i%10)))
				if i%50 == 0 {
					f.Entries()
					f.Len()
					f.LastClearedAt()
					if err := f.Flush(ctx); err != nil {
						t.Errorf("Flush: %v", err)
					}
				}
			}
		}()
	}
	wg.Wait()
	var total int64
	for _, e := range f.Entries() {
		total += e.Count
	}
	if f.Len() != writers*10 || total != writers*perWriter {
		t.Errorf("%d entries with %d attempts, want %d and %d", f.Len(), total, writers*10, writers*perWriter)
	}
	if err := f.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	if f.Len() != writers*10 {
		t.Errorf("the file has %d entries, want %d", f.Len(), writers*10)
	}
}
