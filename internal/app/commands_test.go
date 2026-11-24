package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/cave/internal/core/hostkey"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"golang.org/x/crypto/ssh"
)

// eventually waits, for up to waitFor of real time, until cond is true.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("after %v: %s", waitFor, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// addr is the address the server of the fixture listens on.
func (f *fixture) addr() string {
	return "127.0.0.1:" + strconv.Itoa(f.port)
}

func TestPeopleCommands(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(func(o *Options) { o.NoStart = true })
	f.clock.Advance(time.Second) // what opening changed is told and done with
	var got events
	a.Subscribe(got.add)

	key := sshtest.NewKey(t, sshtest.Ed25519)
	alice, err := a.AddPerson(ctx, "alice", key.AuthorizedKey+" laptop")
	if err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(CoalesceTime)
	if topics := got.take(); len(topics) != 1 || !topics[0].Has(TopicPeople) {
		t.Errorf("after AddPerson the subscriber got %v, want one event with the people", topics)
	}
	if list := a.People(); len(list) != 1 || list[0].Name != "alice" || !list[0].Enabled || len(list[0].Keys) != 1 || list[0].Keys[0].Comment != "laptop" {
		t.Fatalf("People() = %+v, want alice with her key", list)
	}
	keys := alice.Keys
	if err := a.SetKeyComment(ctx, alice.ID, keys[0].ID, "desk"); err != nil {
		t.Fatal(err)
	}
	if err := a.RenamePerson(ctx, alice.ID, "bob"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetPersonEnabled(ctx, alice.ID, false); err != nil {
		t.Fatal(err)
	}
	p := a.People()[0]
	if p.Name != "bob" || p.Enabled || len(p.Keys) != 1 || p.Keys[0].Comment != "desk" {
		t.Fatalf("after the commands: %+v", p)
	}
	if err := a.RemoveKey(ctx, alice.ID, keys[0].ID); err != nil {
		t.Fatal(err)
	}
	if len(a.People()[0].Keys) != 0 {
		t.Errorf("the key is still there: %+v", a.People()[0])
	}
	if err := a.RemovePerson(ctx, alice.ID); err != nil {
		t.Fatal(err)
	}
	if list := a.People(); len(list) != 0 {
		t.Errorf("People() = %+v, want none", list)
	}
}

func TestImportKeys(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(func(o *Options) { o.NoStart = true })
	p, err := a.people.Add(ctx, "alice") // without a key: ImportKeys is what is tested
	if err != nil {
		t.Fatal(err)
	}
	one, two := sshtest.NewKey(t, sshtest.Ed25519), sshtest.NewKey(t, sshtest.Ed25519)
	path := filepath.Join(t.TempDir(), "authorized_keys")
	text := "# two keys\n" + one.AuthorizedKey + " one\n" + two.AuthorizedKey + " two\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	keys, err := a.ImportKeys(ctx, p.ID, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || len(a.People()[0].Keys) != 2 {
		t.Errorf("ImportKeys added %d keys, the person has %d, want 2 and 2", len(keys), len(a.People()[0].Keys))
	}
}

func TestCommandErrors(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(func(o *Options) { o.NoStart = true })
	alice, err := a.people.Add(ctx, "alice") // without a key: AddKeys is what is tested
	if err != nil {
		t.Fatal(err)
	}
	bobKey := f.person(a, "bob")
	newKey := sshtest.NewKey(t, sshtest.Ed25519)
	if _, err := a.AddKeys(ctx, alice.ID, ""); err == nil {
		t.Fatal("empty text was accepted") // the route is checked in the table
	}
	const nobody = "p_0000000000000000"
	tests := []struct {
		name  string
		run   func() error
		route []string
	}{
		{"a name that is not valid", func() error { _, err := a.AddPerson(ctx, "-bad", newKey.AuthorizedKey); return err }, []string{"App.AddPerson", "PersonNameInvalid"}},
		{"a name that is taken, in another case", func() error { _, err := a.AddPerson(ctx, "Alice", newKey.AuthorizedKey); return err }, []string{"App.AddPerson", "PersonNameTaken"}},
		{"a person without a key", func() error { _, err := a.AddPerson(ctx, "carol", ""); return err }, []string{"App.AddPerson", "KeyTextEmpty"}},
		{"a person with a text that is not a key", func() error { _, err := a.AddPerson(ctx, "carol", "not a key"); return err }, []string{"App.AddPerson", "KeyParseFailed"}},
		{"a person with a key that another person has", func() error { _, err := a.AddPerson(ctx, "carol", bobKey.AuthorizedKey); return err }, []string{"App.AddPerson", "KeyAlreadyAssigned"}},
		{"a name to check that is not valid", func() error { return a.CheckPersonName("-bad") }, []string{"App.CheckPersonName", "PersonNameInvalid"}},
		{"a name to check that is taken", func() error { return a.CheckPersonName("ALICE") }, []string{"App.CheckPersonName", "PersonNameTaken"}},
		{"rename of nobody", func() error { return a.RenamePerson(ctx, nobody, "x") }, []string{"App.RenamePerson", "PersonNotFound"}},
		{"enable nobody", func() error { return a.SetPersonEnabled(ctx, nobody, true) }, []string{"App.SetPersonEnabled", "PersonNotFound"}},
		{"remove nobody", func() error { return a.RemovePerson(ctx, nobody) }, []string{"App.RemovePerson", "PersonNotFound"}},
		{"no key in the text", func() error { _, err := a.AddKeys(ctx, alice.ID, ""); return err }, []string{"App.AddKeys", "KeyTextEmpty"}},
		{"a key that another person has", func() error { _, err := a.AddKeys(ctx, alice.ID, bobKey.AuthorizedKey); return err }, []string{"App.AddKeys", "KeyAlreadyAssigned"}},
		{"keys for nobody", func() error { _, err := a.AddKeys(ctx, nobody, bobKey.AuthorizedKey); return err }, []string{"App.AddKeys", "PersonNotFound"}},
		{"a file that is not there", func() error { _, err := a.ImportKeys(ctx, alice.ID, filepath.Join(t.TempDir(), "none")); return err }, []string{"App.ImportKeys"}},
		{"a comment of a key that is not there", func() error { return a.SetKeyComment(ctx, alice.ID, "k_0000000000000000", "x") }, []string{"App.SetKeyComment", "KeyNotFound"}},
		{"remove a key that is not there", func() error { return a.RemoveKey(ctx, alice.ID, "k_0000000000000000") }, []string{"App.RemoveKey", "KeyNotFound"}},
		{"remove an attempt that is not there", func() error { return a.RemoveAttempt(ctx, "a_0000000000000000") }, []string{"App.RemoveAttempt", "AttemptNotFound"}},
		{"disconnect a connection that is not there", func() error { return a.Disconnect(ctx, "nobody") }, []string{"App.Disconnect", server.TagConnectionNotFound}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errtest.Route(t, tt.run(), tt.route...)
		})
	}
	// Nothing of the failures stayed: alice has no key, bob has his one.
	for _, p := range a.People() {
		if want := map[string]int{"alice": 0, "bob": 1}[p.Name]; len(p.Keys) != want {
			t.Errorf("%s has %d keys, want %d", p.Name, len(p.Keys), want)
		}
	}
}

func TestChangingAPersonDisconnectsThem(t *testing.T) {
	tests := []struct {
		name   string
		change func(a *App, p person) error
	}{
		{"rename", func(a *App, p person) error { return a.RenamePerson(ctx, p.id, "carol") }},
		{"disable", func(a *App, p person) error { return a.SetPersonEnabled(ctx, p.id, false) }},
		{"remove the person", func(a *App, p person) error { return a.RemovePerson(ctx, p.id) }},
		{"remove the key", func(a *App, p person) error { return a.RemoveKey(ctx, p.id, p.keyID) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, nil)
			a := f.open(nil)
			key := f.person(a, "alice")
			client := f.connect(a, "alice", key, f.port)
			p := a.People()[0]
			if err := tt.change(a, person{id: p.ID, keyID: p.Keys[0].ID}); err != nil {
				t.Fatal(err)
			}
			waitGone(t, client)
			eventually(t, "the application still shows a connection", func() bool { return len(a.Connections()) == 0 })
		})
	}
}

// person is the ids a test needs to change a person.
type person struct{ id, keyID string }

func TestDisconnect(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(nil)
	alice := f.connect(a, "alice", f.person(a, "alice"), f.port)
	bob := f.connect(a, "bob", f.person(a, "bob"), f.port)
	var aliceID string
	for _, c := range a.Connections() {
		if c.PersonName == "alice" {
			aliceID = c.ID
		}
	}
	if aliceID == "" {
		t.Fatal("no connection of alice")
	}
	if err := a.Disconnect(ctx, aliceID); err != nil {
		t.Fatal(err)
	}
	waitGone(t, alice)
	if !alive(bob) {
		t.Error("Disconnect closed the connection of bob as well")
	}
	a.DisconnectAll()
	waitGone(t, bob)
	eventually(t, "connections are still listed", func() bool { return len(a.Connections()) == 0 })
}

func TestClearAndRemoveAttempts(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(nil)
	key := f.person(a, "alice")
	f.connect(a, "alice", key, f.port)
	if _, err := sshtest.DialErr(t, f.addr(), sshtest.ClientConfig("nobody", a.hostKey.Signer().PublicKey(), key)); err == nil {
		t.Fatal("a name nobody has was let in")
	}
	eventually(t, "the list does not have two entries", func() bool { return len(a.Attempts().Entries) == 2 })
	var refused attempts.Entry
	for _, e := range a.Attempts().Entries {
		if e.Outcome != attempts.Accepted {
			refused = e
		}
	}
	if refused.LoginName != "nobody" {
		t.Fatalf("the refused entry is %+v", refused)
	}
	if err := a.RemoveAttempt(ctx, refused.ID); err != nil {
		t.Fatal(err)
	}
	if entries := a.Attempts().Entries; len(entries) != 1 || entries[0].Outcome != attempts.Accepted {
		t.Fatalf("after RemoveAttempt: %+v", entries)
	}
	f.clock.Advance(time.Hour)
	if err := a.ClearAttempts(ctx); err != nil {
		t.Fatal(err)
	}
	snap := a.Attempts()
	if len(snap.Entries) != 0 || !snap.LastClearedAt.Equal(f.clock.Now()) {
		t.Errorf("after ClearAttempts: %d entries, cleared at %v, want none at %v", len(snap.Entries), snap.LastClearedAt, f.clock.Now())
	}
	data, err := os.ReadFile(filepath.Join(f.dirs.State, attempts.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"loginName"`) || strings.Contains(string(data), `"personName"`) {
		t.Errorf("the file still has entries (it must be saved at once):\n%s", data)
	}
}

func TestNewHostKey(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(nil)
	key := f.person(a, "alice")
	oldKey := a.hostKey.Signer().PublicKey()
	oldFingerprint := a.Server().HostKeyFingerprint
	client := f.connect(a, "alice", key, f.port)
	f.clock.Advance(time.Second)
	var got events
	a.Subscribe(got.add)

	if err := a.NewHostKey(ctx); err != nil {
		t.Fatal(err)
	}
	newFingerprint := a.Server().HostKeyFingerprint
	if newFingerprint == oldFingerprint || newFingerprint != ssh.FingerprintSHA256(a.hostKey.Signer().PublicKey()) {
		t.Errorf("fingerprint %q after %q", newFingerprint, oldFingerprint)
	}
	f.clock.Advance(CoalesceTime)
	if topics := got.take(); len(topics) != 1 || !topics[0].Has(TopicServer) {
		t.Errorf("after NewHostKey the subscriber got %v, want an event with the server", topics)
	}
	if !alive(client) {
		t.Error("the connection that was open ended")
	}
	if _, err := sshtest.DialErr(t, f.addr(), sshtest.ClientConfig("alice", oldKey, key)); err == nil {
		t.Error("a client that expects the old host key was let in")
	}
	fresh := sshtest.Dial(t, f.addr(), "alice", key, a.hostKey.Signer().PublicKey())
	if !alive(fresh) {
		t.Error("a client that expects the new host key does not work")
	}
	// The new key is the one on the disk, and it is private to the user.
	loaded, err := hostkey.LoadOrCreate(ctx, f.dirs)
	if err != nil || loaded.Fingerprint() != newFingerprint {
		t.Errorf("the key on the disk is %v (%v), want %s", loaded, err, newFingerprint)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(f.dirs.Config, hostkey.PrivateFileName))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("the host key file: %v, %v; want mode 0600", info, err)
		}
	}
}

func TestCountConnections(t *testing.T) {
	conns := []server.Connection{
		{ID: "c1", PersonID: "p1", KeyID: "k1"},
		{ID: "c2", PersonID: "p1", KeyID: "k2"},
		{ID: "c3", PersonID: "p2", KeyID: "k3"},
	}
	tests := []struct {
		name            string
		conns           []server.Connection
		personID, keyID string
		want            int
	}{
		{"nobody is connected", nil, "p1", "", 0},
		{"a person with two connections", conns, "p1", "", 2},
		{"a person with one", conns, "p2", "", 1},
		{"a person that is not there", conns, "p9", "", 0},
		{"a key", conns, "p1", "k2", 1},
		{"a key that is not there", conns, "p1", "k9", 0},
	}
	for _, tt := range tests {
		if got := CountConnections(tt.conns, tt.personID, tt.keyID); got != tt.want {
			t.Errorf("%s: CountConnections = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestAPersonIsNotAddedWithoutAKey(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(func(o *Options) { o.NoStart = true })
	f.clock.Advance(time.Second)
	var got events
	a.Subscribe(got.add)
	taken := f.person(a, "bob")
	f.clock.Advance(CoalesceTime)
	got.take()
	for name, text := range map[string]string{"no text": "", "a comment only": "# a key\n", "not a key": "hello", "a key of bob": taken.AuthorizedKey} {
		if _, err := a.AddPerson(ctx, "carol", text); err == nil {
			t.Errorf("%s: carol was added", name)
		}
	}
	f.clock.Advance(CoalesceTime)
	if list := a.People(); len(list) != 1 || list[0].Name != "bob" {
		t.Errorf("People() = %+v, want only bob", list)
	}
	if topics := got.take(); len(topics) != 0 {
		t.Errorf("a refused addition told the subscriber: %v", topics)
	}
}
