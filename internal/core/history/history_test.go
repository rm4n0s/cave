package history

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
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/ids"
	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/cave/internal/core/storage/storagetest"
)

var ctx = context.Background()

// start is the time of the tests' clock.
var start = time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)

// fixture is a history in its own folders, with its clock and what it logs.
type fixture struct {
	*Store
	dirs  storage.Dirs
	clock *clock.Fake
	log   *bytes.Buffer
	path  string
}

// newFixture opens an empty history.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{dirs: storagetest.Isolate(t), clock: clock.NewFake(start), log: &bytes.Buffer{}}
	f.path = filepath.Join(f.dirs.State, FileName)
	f.reopen(t)
	return f
}

// reopen closes the history and opens it again from its files.
func (f *fixture) reopen(t *testing.T) {
	t.Helper()
	if f.Store != nil {
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(ctx, Options{Dirs: f.dirs, Clock: f.clock, Logger: slog.New(slog.NewJSONHandler(f.log, nil))})
	if err != nil {
		t.Fatal(err)
	}
	f.Store = s
	t.Cleanup(func() {
		// A test may have closed it, or broken its file on purpose; only a leak matters here.
		_ = s.Close()
	})
}

// lines returns the lines of a file of the history.
func lines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// kinds returns the kind of every line of a file.
func kinds(t *testing.T, path string) []string {
	t.Helper()
	var out []string
	for _, s := range lines(t, path) {
		var l line
		if err := json.Unmarshal([]byte(s), &l); err != nil {
			t.Fatalf("a line that does not parse: %q", s)
		}
		out = append(out, l.Kind)
	}
	return out
}

// conn returns a connection record that starts at the clock's time.
func (f *fixture) conn(name string) Connection {
	return Connection{
		ID: ids.New(connectionPrefix), PersonID: "p_00000000000000a1", PersonName: name, KeyFingerprint: "SHA256:abc",
		Address: "198.51.100.7", ClientVersion: "SSH-2.0-OpenSSH_9.9", ConnectedAt: f.clock.Now(),
	}
}

// transfer returns a transfer record of a connection that closes at the clock's time.
func (f *fixture) transfer(c Connection, path string, down, up int64, result Result) Transfer {
	return Transfer{
		ConnectionID: c.ID, PersonID: c.PersonID, PersonName: c.PersonName, Path: path, OpenedAt: f.clock.Now().Add(-time.Second),
		ClosedAt: f.clock.Now(), Downloaded: down, Uploaded: up, Result: result,
	}
}

func TestRecordsRoundTrip(t *testing.T) {
	f := newFixture(t)
	ended := f.conn("alice")
	f.Connected(ended)
	f.clock.Advance(90 * time.Second)
	f.AddTransfer(f.transfer(ended, "/home/u/up.txt", 0, 300, ResultDone))
	f.clock.Advance(time.Second)
	f.AddTransfer(f.transfer(ended, "/home/u/down.txt", 400, 0, ResultFailed))
	f.clock.Advance(time.Second)
	f.AddTransfer(f.transfer(ended, "/home/u/both.txt", 5, 6, ResultInterrupted))
	f.Ended(ended.ID, f.clock.Now(), ReasonDisconnected, 1000, 2000,
		[]Activity{{"shell", ""}, {"exec", "ls"}, {"exec", "ls"}, {"sftp", ""}}, 0)
	f.clock.Advance(time.Minute)
	open := f.conn("bob")
	f.Connected(open)

	before := f.Connections()
	transfers := f.Transfers()
	if len(before) != 2 || before[0].PersonName != "bob" || !before[0].Open() || before[1].Reason != ReasonDisconnected {
		t.Fatalf("connections = %+v", before)
	}
	if d, ok := before[1].Duration(); !ok || d != 92*time.Second {
		t.Errorf("duration = %v, %v; want 1m32s", d, ok)
	}
	if got := before[1].Activities; !reflect.DeepEqual(got, []Activity{{"shell", ""}, {"exec", "ls"}, {"sftp", ""}}) {
		t.Errorf("activities = %+v, want each once in order", got)
	}
	if len(transfers) != 3 || transfers[0].Path != "/home/u/both.txt" {
		t.Fatalf("transfers = %+v", transfers)
	}
	wantDirections := []Direction{Both, Download, Upload}
	for i, tr := range transfers {
		if tr.Direction() != wantDirections[i] || !ids.Valid(transferPrefix, tr.ID) {
			t.Errorf("transfer %d: direction %v, id %q", i, tr.Direction(), tr.ID)
		}
	}

	f.reopen(t)
	after := f.Connections()
	if len(after) != 2 {
		t.Fatalf("after reopen: %+v", after)
	}
	if after[0].Reason != ReasonUnknown || !after[0].EndedAt.IsZero() {
		t.Errorf("the open connection came back as %+v, want unknown without an end", after[0])
	}
	if _, ok := after[0].Duration(); ok {
		t.Error("an unknown end has a duration")
	}
	if !reflect.DeepEqual(after[1], before[1]) {
		t.Errorf("the ended connection came back as\n%+v\nwant\n%+v", after[1], before[1])
	}
	if got := f.Transfers(); !reflect.DeepEqual(got, transfers) {
		t.Errorf("transfers came back as\n%+v\nwant\n%+v", got, transfers)
	}
}

func TestDirection(t *testing.T) {
	tests := []struct {
		name     string
		down, up int64
		created  bool
		want     Direction
	}{
		{"written", 0, 10, false, Upload},
		{"created empty", 0, 0, true, Upload},
		{"read", 10, 0, false, Download},
		{"read and written", 10, 10, false, Both},
		{"created and read", 10, 0, true, Download},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Transfer{Downloaded: tt.down, Uploaded: tt.up, Created: tt.created}).Direction(); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEndedAndConnectedEdges(t *testing.T) {
	f := newFixture(t)
	c := f.conn("alice")
	f.Ended(c.ID, f.clock.Now(), ReasonClient, 0, 0, nil, 0) // not recorded: nothing written
	f.Connected(c)
	f.Connected(c) // twice: kept once
	f.Ended(c.ID, f.clock.Now(), Reason("bogus"), 0, 0, nil, 0)
	f.Ended(c.ID, f.clock.Now(), ReasonKeepAlive, 0, 0, nil, 0) // ended already: nothing changes
	got := f.Connections()
	if len(got) != 1 || got[0].Reason != ReasonClient {
		t.Errorf("connections = %+v; want one, ended by the client (an unknown reason is the client)", got)
	}
	if k := kinds(t, f.path); !reflect.DeepEqual(k, []string{kindState, kindConnected, kindEnded}) {
		t.Errorf("the file has %v", k)
	}
	tr := f.AddTransfer(Transfer{ConnectionID: c.ID, Path: "/x", Result: Result("bogus"), Downloaded: -1, Uploaded: -5})
	if tr.Result != ResultDone || tr.Downloaded != 0 || tr.Uploaded != 0 {
		t.Errorf("transfer = %+v", tr)
	}
}

func TestHostileTextIsCleaned(t *testing.T) {
	f := newFixture(t)
	long := strings.Repeat("x", 300)
	c := f.conn("alice")
	c.ClientVersion = "SSH-2.0-\x1b[31mred\u202E" + long
	f.Connected(c)
	f.Ended(c.ID, f.clock.Now(), ReasonClient, 0, 0, []Activity{{"exec", "rm \x1b[2J" + long}}, 0)
	f.AddTransfer(f.transfer(c, "/home/\u202Eu/\x1b]0;x\x07"+long, 1, 0, ResultDone))

	check := func(t *testing.T) {
		got := f.Connections()[0]
		tr := f.Transfers()[0]
		for _, s := range []string{got.ClientVersion, got.Activities[0].Detail, tr.Path} {
			if strings.ContainsAny(s, "\x1b\u202E\x07") {
				t.Errorf("not cleaned: %q", s)
			}
		}
		if n := len([]rune(got.ClientVersion)); n != maxTextLength {
			t.Errorf("client version has %d characters, want %d", n, maxTextLength)
		}
		if n := len([]rune(got.Activities[0].Detail)); n != maxDetailLength {
			t.Errorf("detail has %d characters, want %d", n, maxDetailLength)
		}
		if n := len([]rune(tr.Path)); n != MaxPathLength {
			t.Errorf("path has %d characters, want %d", n, MaxPathLength)
		}
	}
	check(t)
	data, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("\x1b")) || bytes.Contains(data, []byte("\u202E")) {
		t.Error("the file holds a raw control or format character")
	}
	f.reopen(t)
	check(t)
}

func TestActivityLimit(t *testing.T) {
	f := newFixture(t)
	c := f.conn("alice")
	f.Connected(c)
	var acts []Activity
	for i := range 40 {
		acts = append(acts, Activity{"exec", fmt.Sprintf("cmd %d", i)}, Activity{"exec", fmt.Sprintf("cmd %d", i)})
	}
	f.Ended(c.ID, f.clock.Now(), ReasonClient, 0, 0, acts, 2)
	got := f.Connections()[0]
	if len(got.Activities) != MaxActivities || got.MoreActivities != 10 {
		t.Errorf("%d activities, %d more; want %d and 10 (8 past the limit and the 2 given)", len(got.Activities), got.MoreActivities, MaxActivities)
	}
}

func TestSkippedLines(t *testing.T) {
	dirs := storagetest.Isolate(t)
	path := filepath.Join(dirs.State, FileName)
	good := `{"v":1,"kind":"connected","id":"c_00000000000000a1","personName":"alice","connectedAt":"2026-11-25T10:00:00Z"}`
	content := strings.Join([]string{
		`{"v":1,"kind":"state","lastClearedAt":"2026-11-20T00:00:00Z"}`,
		good,
		`not json`,
		`{"v":2,"kind":"connected","id":"c_00000000000000a2","connectedAt":"2026-11-25T10:00:00Z"}`,
		`{"v":1,"kind":"mystery","id":"c_00000000000000a3"}`,
		`{"v":1,"kind":"note","x":"` + strings.Repeat("y", 70<<10) + `"}`,
		`{"v":1,"kind":"ended","id":"c_00000000000000ff","endedAt":"2026-11-25T10:01:00Z","reason":"client"}`,
		`{"v":1,"kind":"ended","id":"c_00000000000000a1","endedAt":"2026-11-25T10:01:00Z","reason":"client"}`,
		`{"v":1,"kind":"transfer","id":"t_00000000000000b1","path":"/a","openedAt":"2026-11-25T10:00:10Z","closedAt":"2026-11-25T10:00:20Z","uploaded":3,"result":"done"}`,
		`{"v":1,"kind":"connected","id":"c_00`,
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	log := &bytes.Buffer{}
	s, err := Open(ctx, Options{Dirs: dirs, Clock: clock.NewFake(start), Logger: slog.New(slog.NewJSONHandler(log, nil))})
	if err != nil {
		t.Fatal(err)
	}
	conns, transfers := s.Connections(), s.Transfers()
	if len(conns) != 1 || conns[0].Reason != ReasonClient || len(transfers) != 1 {
		t.Errorf("connections %+v, transfers %+v", conns, transfers)
	}
	if want := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC); !s.LastClearedAt().Equal(want) {
		t.Errorf("last cleared %v, want %v", s.LastClearedAt(), want)
	}
	var record struct {
		Msg   string `json:"msg"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(log.Bytes(), &record); err != nil || record.Msg != "history lines skipped" || record.Count != 6 {
		t.Errorf("the log has %q, want the count 6", log.String())
	}
	// The cut last line gets its newline before the next one, so that both read again.
	c := Connection{ID: "c_00000000000000c1", PersonName: "carol", ConnectedAt: start}
	s.Connected(c)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	all := lines(t, path)
	if all[len(all)-1] != `{"v":1,"kind":"connected","id":"c_00000000000000c1","personName":"carol","connectedAt":"2026-11-25T10:00:00Z"}` {
		t.Errorf("the last line is %q", all[len(all)-1])
	}
}

func TestRotation(t *testing.T) {
	f := newFixture(t)
	early := f.conn("alice")
	f.Connected(early)
	for i := range 10 {
		if i == 5 {
			// The file is full now: the next line starts a new one, and only once more.
			info, err := os.Stat(f.path)
			if err != nil {
				t.Fatal(err)
			}
			f.maxBytes = info.Size() + 10
		}
		f.clock.Advance(time.Second)
		f.AddTransfer(f.transfer(early, fmt.Sprintf("/home/u/file-%d", i), 1, 0, ResultDone))
	}
	f.Ended(early.ID, f.clock.Now(), ReasonClient, 1, 2, nil, 0)
	if k := kinds(t, f.path+".1"); !reflect.DeepEqual(k[:2], []string{kindState, kindConnected}) {
		t.Errorf("the file put aside has %v", k)
	}
	if k := kinds(t, f.path); k[0] != kindState || k[len(k)-1] != kindEnded {
		t.Errorf("the new file has %v", k)
	}
	f.reopen(t)
	got := f.Connections()
	if len(got) != 1 || got[0].Reason != ReasonClient {
		t.Errorf("a connection that began before the rotation and ended after: %+v", got)
	}
	if n := len(f.Transfers()); n != 10 {
		t.Errorf("%d transfers, want 10", n)
	}
}

func TestLimitsInMemory(t *testing.T) {
	f := newFixture(t)
	for range MaxConnections + 1 {
		f.clock.Advance(time.Second)
		f.Connected(f.conn("alice"))
	}
	for i := range MaxTransfers + 1 {
		f.clock.Advance(time.Second)
		f.AddTransfer(Transfer{Path: fmt.Sprint(i), OpenedAt: f.clock.Now(), ClosedAt: f.clock.Now(), Result: ResultDone})
	}
	check := func(t *testing.T) {
		conns, transfers := f.Connections(), f.Transfers()
		if len(conns) != MaxConnections || len(transfers) != MaxTransfers {
			t.Fatalf("%d connections and %d transfers", len(conns), len(transfers))
		}
		if !conns[len(conns)-1].ConnectedAt.Equal(start.Add(2 * time.Second)) {
			t.Errorf("the oldest kept connection began at %v; the very first should be gone", conns[len(conns)-1].ConnectedAt)
		}
		if transfers[len(transfers)-1].Path != "1" || transfers[0].Path != fmt.Sprint(MaxTransfers) {
			t.Errorf("transfers from %q to %q", transfers[len(transfers)-1].Path, transfers[0].Path)
		}
	}
	check(t)
	f.reopen(t)
	check(t)
}

func TestClear(t *testing.T) {
	f := newFixture(t)
	f.maxBytes = 400
	gone := f.conn("alice")
	f.Connected(gone)
	f.Ended(gone.ID, f.clock.Now(), ReasonClient, 0, 0, nil, 0)
	open := f.conn("bob")
	f.Connected(open)
	for i := range 5 {
		f.AddTransfer(f.transfer(open, fmt.Sprint("/f", i), 1, 0, ResultDone))
	}
	if _, err := os.Stat(f.path + ".1"); err != nil {
		t.Fatalf("the test needs a file put aside: %v", err)
	}
	notified := 0
	cancel := f.Subscribe(func() { notified++ })
	defer cancel()
	f.clock.Advance(time.Hour)
	if err := f.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if notified != 1 {
		t.Errorf("notified %d times", notified)
	}
	if _, err := os.Stat(f.path + ".1"); !os.IsNotExist(err) {
		t.Errorf("the file put aside is still there: %v", err)
	}
	if got := f.Connections(); len(got) != 1 || got[0].ID != open.ID || len(f.Transfers()) != 0 {
		t.Errorf("after the clear: %+v, %d transfers", got, len(f.Transfers()))
	}
	if !f.LastClearedAt().Equal(start.Add(time.Hour)) {
		t.Errorf("last cleared %v", f.LastClearedAt())
	}
	if k := kinds(t, f.path); !reflect.DeepEqual(k, []string{kindState, kindConnected}) {
		t.Errorf("the new file has %v", k)
	}
	f.Ended(open.ID, f.clock.Now(), ReasonServerStopped, 0, 0, nil, 0)
	f.reopen(t)
	if got := f.Connections(); len(got) != 1 || got[0].Reason != ReasonServerStopped {
		t.Errorf("after reopen: %+v; the end should have found its start", got)
	}
	if !f.LastClearedAt().Equal(start.Add(time.Hour)) {
		t.Errorf("after reopen, last cleared %v", f.LastClearedAt())
	}
	// A clear with nothing open still leaves the file, for the time of the clear.
	if err := f.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if k := kinds(t, f.path); !reflect.DeepEqual(k, []string{kindState}) {
		t.Errorf("the new file has %v", k)
	}
}

func TestClearPolicy(t *testing.T) {
	tests := []struct {
		name    string
		policy  attempts.Policy
		atStart bool
		advance time.Duration
		cleared bool
		next    bool
	}{
		{"never", attempts.Never, true, 0, false, false},
		{"on start, at start", attempts.OnStart, true, 0, true, false},
		{"on start, while running", attempts.OnStart, false, 0, false, false},
		{"daily, before the time", attempts.Daily, false, 23 * time.Hour, false, true},
		{"daily, at the time", attempts.Daily, false, 24 * time.Hour, true, true},
		{"hourly, past the time while closed", attempts.Hourly, true, 5 * time.Hour, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.PolicyChanged(tt.policy)
			c := f.conn("alice")
			f.Connected(c)
			f.Ended(c.ID, f.clock.Now(), ReasonClient, 0, 0, nil, 0)
			f.clock.Advance(tt.advance)
			f.reopen(t)
			if got := f.ApplyPolicy(tt.policy, tt.atStart); got != tt.cleared {
				t.Errorf("cleared = %v, want %v", got, tt.cleared)
			}
			if empty := len(f.Connections()) == 0; empty != tt.cleared {
				t.Errorf("%d connections after the policy", len(f.Connections()))
			}
			if hasNext := !f.NextClearAt().IsZero(); hasNext != tt.next {
				t.Errorf("next clear %v", f.NextClearAt())
			}
			next := f.NextClearAt()
			f.reopen(t)
			if !f.NextClearAt().Equal(next) {
				t.Errorf("the next clear came back as %v, want %v", f.NextClearAt(), next)
			}
		})
	}
	t.Run("a period with no time yet starts it; another policy drops it", func(t *testing.T) {
		f := newFixture(t)
		if f.ApplyPolicy(attempts.Weekly, true) {
			t.Error("cleared")
		}
		if want := start.Add(7 * 24 * time.Hour); !f.NextClearAt().Equal(want) {
			t.Errorf("next %v, want %v", f.NextClearAt(), want)
		}
		f.ApplyPolicy(attempts.Never, false)
		if !f.NextClearAt().IsZero() {
			t.Errorf("next %v", f.NextClearAt())
		}
	})
}

func TestNothingIsWrittenAfterClose(t *testing.T) {
	f := newFixture(t)
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Errorf("a second Close: %v", err)
	}
	c := f.conn("alice")
	f.Connected(c)
	f.Ended(c.ID, f.clock.Now(), ReasonClient, 0, 0, nil, 0)
	f.AddTransfer(f.transfer(c, "/x", 1, 0, ResultDone))
	if err := f.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Errorf("a file was written after Close: %v", err)
	}
	if len(f.Connections()) != 0 || len(f.Transfers()) != 0 {
		t.Error("records were kept after Close")
	}
}

func TestSubscribe(t *testing.T) {
	f := newFixture(t)
	var a, b int
	cancelA := f.Subscribe(func() { a++ })
	cancelB := f.Subscribe(func() { b++ })
	c := f.conn("alice")
	f.Connected(c)
	cancelA()
	f.Ended(c.ID, f.clock.Now(), ReasonClient, 0, 0, nil, 0)
	f.AddTransfer(f.transfer(c, "/x", 1, 0, ResultDone))
	cancelB()
	f.PolicyChanged(attempts.Daily)
	if a != 1 || b != 3 {
		t.Errorf("a = %d, b = %d; want 1 and 3", a, b)
	}
}
