package apptest

import (
	"context"
	"net"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"unicode"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/errors"
)

var ctx = context.Background()

func TestSampleIsTheSameEveryTime(t *testing.T) {
	a, b := Sample(t), Sample(t)
	if !reflect.DeepEqual(a.PeopleList, b.PeopleList) {
		t.Error("the people differ")
	}
	if !reflect.DeepEqual(a.AttemptsSnapshot, b.AttemptsSnapshot) {
		t.Error("the attempts differ")
	}
	if !reflect.DeepEqual(a.ConnectionList, b.ConnectionList) {
		t.Error("the connections differ")
	}
	if !reflect.DeepEqual(a.ServerSnapshot, b.ServerSnapshot) {
		t.Error("the server snapshots differ")
	}
	if a.Clock.Now() != Now || !a.Clock.Now().Equal(b.Clock.Now()) {
		t.Errorf("the clock shows %v", a.Clock.Now())
	}
	// What the settings snapshot shows is the same too.
	if got := a.Settings().String("attempts.clear"); got != "weekly" || got != b.Settings().String("attempts.clear") {
		t.Errorf("attempts.clear = %q", got)
	}
}

func TestSampleHasEveryOutcome(t *testing.T) {
	f := Sample(t)
	entries := f.Attempts().Entries
	if len(entries) != 9 {
		t.Fatalf("%d entries, want 9", len(entries))
	}
	seen := map[attempts.Outcome]int{}
	for i, e := range entries {
		seen[e.Outcome]++
		if e.LastAt.After(Now) || e.FirstAt.After(e.LastAt) {
			t.Errorf("entry %d: first %v, last %v", i, e.FirstAt, e.LastAt)
		}
		if i > 0 && e.LastAt.After(entries[i-1].LastAt) {
			t.Errorf("entry %d is newer than the one before it", i)
		}
		if e.Count < 1 {
			t.Errorf("entry %d: count %d", i, e.Count)
		}
	}
	for _, o := range []attempts.Outcome{attempts.Accepted, attempts.UnknownName, attempts.Disabled, attempts.WrongKey, attempts.NoKey, attempts.NotProven, attempts.HandshakeFailed, attempts.Refused} {
		if seen[o] == 0 {
			t.Errorf("no entry has the outcome %s", o)
		}
	}
	if seen[attempts.UnknownName] != 2 {
		t.Errorf("%d entries with an unknown name, want 2 (one with a hostile login name)", seen[attempts.UnknownName])
	}
	if f.Attempts().LastClearedAt.After(Now) || !f.Attempts().NextClearAt.After(Now) {
		t.Errorf("cleared %v, next %v", f.Attempts().LastClearedAt, f.Attempts().NextClearAt)
	}
	for _, p := range f.People() {
		if p.CreatedAt.After(Now) {
			t.Errorf("%s was created in the future", p.Name)
		}
		for _, k := range p.Keys {
			if k.AddedAt.After(Now) || !strings.HasPrefix(k.Fingerprint, "SHA256:") || len(k.Fingerprint) != 50 {
				t.Errorf("key %+v", k)
			}
		}
	}
	for _, c := range f.Connections() {
		if c.Since.After(Now) {
			t.Errorf("connection %s is from the future", c.ID)
		}
	}
	if fp := f.Server().HostKeyFingerprint; !strings.HasPrefix(fp, "SHA256:") || len(fp) != 50 {
		t.Errorf("host key fingerprint %q", fp)
	}
}

// controlFree fails the test when a string holds a control character; the hostile login name of
// the sample is there with its escape already replaced.
func controlFree(t *testing.T, what, s string) {
	t.Helper()
	for _, r := range s {
		if unicode.IsControl(r) {
			t.Errorf("%s holds the control character %U: %q", what, r, s)
			return
		}
	}
}

func TestSampleHasNoControlCharacters(t *testing.T) {
	f := Sample(t)
	for _, e := range f.Attempts().Entries {
		for _, s := range []string{e.PersonName, e.LoginName, e.Address, e.ClientVersion, e.KeyOwner} {
			controlFree(t, "an attempt", s)
		}
	}
	hostile := false
	for _, e := range f.Attempts().Entries {
		hostile = hostile || e.LoginName == "root\uFFFD[31mX"
	}
	if !hostile {
		t.Error("the sample has no entry with the hostile login name")
	}
	for _, p := range f.People() {
		controlFree(t, "a person", p.Name)
		for _, k := range p.Keys {
			controlFree(t, "a key", k.Comment)
		}
	}
	for _, c := range f.Connections() {
		for _, s := range []string{c.PersonName, c.Address, c.ClientVersion} {
			controlFree(t, "a connection", s)
		}
		for _, a := range c.Activities {
			controlFree(t, "an activity", a.Detail)
		}
	}
}

func TestEmpty(t *testing.T) {
	f := Empty(t)
	if f.Server().State != server.Stopped || f.Server().Port != 2222 || f.Server().Err != nil {
		t.Errorf("server %+v", f.Server())
	}
	if len(f.People()) != 0 || len(f.Connections()) != 0 || len(f.Attempts().Entries) != 0 {
		t.Error("an empty application has content")
	}
	if got := f.Settings().Int("server.port"); got != 2222 {
		t.Errorf("server.port = %d", got)
	}
	if got := f.Settings().String("attempts.clear"); got != "never" {
		t.Errorf("attempts.clear = %q", got)
	}
}

func TestFakeRecordsCalls(t *testing.T) {
	f := Empty(t)
	failure := errors.New("ConnectionNotFound", "no such connection")
	f.Fail = map[string]error{"Disconnect": failure}

	if _, err := f.AddPerson(ctx, "dave"); err != nil {
		t.Fatal(err)
	}
	_ = f.RenamePerson(ctx, "p_1", "erin")
	_ = f.SetPersonEnabled(ctx, "p_1", false)
	_ = f.Set(ctx, "server.port", 2200)
	_ = f.Set(ctx, "session.acceptEnv", []string{"LANG", "LC_*"})
	_, _ = f.AddKeys(ctx, "p_1", "ssh-ed25519 AAAA one\nssh-ed25519 BBBB two")
	_ = f.SetKeyComment(ctx, "p_1", "k_1", "desk")
	_ = f.StopServer(ctx)
	if err := f.Disconnect(ctx, "c_1"); err != failure {
		t.Errorf("Disconnect returned %v, want the error of Fail", err)
	}
	f.DisconnectAll()
	want := []string{
		"AddPerson(dave)",
		"RenamePerson(p_1, erin)",
		"SetPersonEnabled(p_1, false)",
		"Set(server.port, 2200)",
		"Set(session.acceptEnv, [LANG LC_*])",
		`AddKeys(p_1, "ssh-ed25519 AAAA one\nssh-ed25519 BBBB two")`,
		`SetKeyComment(p_1, k_1, "desk")`,
		"StopServer()",
		"Disconnect(c_1)",
		"DisconnectAll()",
	}
	if got := f.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("Calls() = %q\n want %q", got, want)
	}
	// Commands change nothing.
	if len(f.People()) != 0 || f.Server().State != server.Stopped {
		t.Error("a command changed the snapshots")
	}
	// Calls returns a copy.
	f.Calls()[0] = "changed"
	if f.Calls()[0] != want[0] {
		t.Error("the caller changed the list of calls")
	}
}

func TestFakeEmitsToSubscribers(t *testing.T) {
	f := Empty(t)
	var first, second atomic.Int32
	f.Subscribe(func(app.Event) { first.Add(1) })
	cancel := f.Subscribe(func(e app.Event) {
		if changed, ok := e.(app.Changed); !ok || !changed.Topics.Has(app.TopicPeople) {
			t.Errorf("the second subscriber got %+v", e)
		}
		second.Add(1)
	})
	if f.Subscribers() != 2 {
		t.Fatalf("%d subscribers, want 2", f.Subscribers())
	}
	f.Emit(app.Changed{Topics: app.TopicPeople})
	cancel()
	cancel() // twice is fine
	f.Emit(app.Changed{Topics: app.TopicPeople})
	if first.Load() != 2 || second.Load() != 1 || f.Subscribers() != 1 {
		t.Errorf("told %d and %d times with %d subscribers left, want 2, 1 and 1", first.Load(), second.Load(), f.Subscribers())
	}
}

func TestOpenGivesARunningApplication(t *testing.T) {
	var port int
	t.Run("the application", func(t *testing.T) {
		r := Open(t, nil)
		port = r.Port
		snap := r.App.Server()
		if snap.State != server.Running || snap.Port != r.Port || len(snap.Listening) != 1 || !strings.HasPrefix(snap.Listening[0], "127.0.0.1:") {
			t.Fatalf("the server is %+v, want it running on 127.0.0.1:%d", snap, r.Port)
		}
		key := r.Person("alice")
		client := r.Connect("alice", key)
		if conns := r.App.Connections(); len(conns) != 1 || conns[0].PersonName != "alice" {
			t.Errorf("connections: %+v", conns)
		}
		r.Refuse("nobody", key)
		r.Eventually("the list does not have two entries", func() bool { return len(r.App.Attempts().Entries) == 2 })
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		out, err := session.Output("echo hello")
		if err != nil || string(out) != "hello\n" {
			t.Errorf("a command gave %q, %v", out, err)
		}
		// A new host key: the helper follows it.
		if err := r.App.NewHostKey(ctx); err != nil {
			t.Fatal(err)
		}
		r.Connect("alice", key)
	})
	// The test of the application is over, so its cleanup has closed it.
	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err == nil {
		_ = conn.Close()
		t.Errorf("something still listens on port %d", port)
	}
}

func TestOpenSavesTheSettingsOfTheTest(t *testing.T) {
	r := Open(t, map[string]any{"server.startOnLaunch": false})
	if r.App.Server().State != server.Stopped {
		t.Errorf("the server is %v, want it stopped by the setting", r.App.Server().State)
	}
	s := r.App.Settings()
	if s.String("server.address") != "127.0.0.1" || s.Int("server.port") != r.Port || s.String("session.shell") != "/bin/sh" || s.Int("server.keepAliveSeconds") != 0 {
		t.Errorf("the settings of the test were not saved")
	}
}
