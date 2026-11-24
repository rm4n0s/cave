package apptest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/cave/internal/core/storage/storagetest"
)

// base64Alphabet is where the characters of a sample fingerprint come from.
const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// fingerprint returns a fingerprint that looks like OpenSSH's, "SHA256:" and 43 characters, and
// is always the same for a seed.
func fingerprint(seed int) string {
	var b strings.Builder
	b.WriteString("SHA256:")
	for i := 0; i < 43; i++ {
		b.WriteByte(base64Alphabet[(seed*7+i*5+i/3)%len(base64Alphabet)])
	}
	return b.String()
}

// The ids and fingerprints of the sample, which the tests of the user interfaces may use.
const (
	AliceID = "p_00000000000000a1"
	BobID   = "p_00000000000000b2"
	CarolID = "p_00000000000000c3"
)

// ago and ahead are times before and after Now.
func ago(d time.Duration) time.Time   { return Now.Add(-d) }
func ahead(d time.Duration) time.Time { return Now.Add(d) }

const day = 24 * time.Hour

// sampleSettings opens a settings store in an isolated folder, sets the values and returns its
// snapshot: a snapshot cannot be made any other way.
func sampleSettings(t testing.TB, values map[string]any) settings.Snapshot {
	t.Helper()
	store, err := settings.Open(context.Background(), storagetest.Isolate(t))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range values {
		if err := store.Set(context.Background(), key, value); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	return store.Snapshot()
}

// baseServer is the snapshot of a server on its default port.
func baseServer(state server.State) app.ServerSnapshot {
	snap := app.ServerSnapshot{
		State:              state,
		Port:               2222,
		HostKeyFingerprint: fingerprint(1),
		Addresses:          []string{"192.0.2.10", "2001:db8::10", "127.0.0.1"},
	}
	if state == server.Running {
		snap.Listening = []string{"[::]:2222"}
	}
	return snap
}

// Empty returns a fake of an application that has just been installed: the server is stopped,
// and there are no people, no connections and no attempts; the settings are the defaults.
func Empty(t testing.TB) *Fake {
	t.Helper()
	return &Fake{
		ServerSnapshot:   baseServer(server.Stopped),
		SettingsSnapshot: sampleSettings(t, nil),
		Clock:            clock.NewFake(Now),
	}
}

// Sample returns a fake with content that is the same on every run: a running server, three
// people (one disabled, one without a key), one connection and nine attempts, one of each
// result and one with a login name that a client made hostile. attempts.clear is "weekly".
func Sample(t testing.TB) *Fake {
	t.Helper()
	f := &Fake{
		ServerSnapshot:   baseServer(server.Running),
		SettingsSnapshot: sampleSettings(t, map[string]any{string(settings.AttemptsClear): settings.ClearWeekly}),
		Clock:            clock.NewFake(Now),
	}
	f.PeopleList = []people.Person{
		{ID: AliceID, Name: "alice", Enabled: true, CreatedAt: ago(30 * day), Keys: []people.Key{
			{ID: "k_00000000000001a1", Type: "ssh-ed25519", Fingerprint: fingerprint(11), Comment: "laptop", AddedAt: ago(29 * day)},
			{ID: "k_00000000000001a2", Type: "ssh-rsa", Fingerprint: fingerprint(12), Comment: "work desktop", AddedAt: ago(5 * day)},
		}},
		{ID: BobID, Name: "bob", Enabled: false, CreatedAt: ago(12 * day), Keys: []people.Key{
			{ID: "k_00000000000002b1", Type: "ssh-ed25519", Fingerprint: fingerprint(21), AddedAt: ago(11 * day)},
		}},
		{ID: CarolID, Name: "carol", Enabled: true, CreatedAt: ago(2 * day)},
	}
	f.ConnectionList = []server.Connection{{
		ID: "c_0000000000000001", PersonID: AliceID, PersonName: "alice",
		KeyID: "k_00000000000001a1", KeyFingerprint: fingerprint(11),
		Address: "198.51.100.7", ClientVersion: "SSH-2.0-OpenSSH_9.8", Since: ago(12 * time.Minute),
		Activities: []server.Activity{
			{Kind: server.ActivityShell},
			{Kind: server.ActivityLocalForward, Detail: "127.0.0.1:5432"},
		},
	}}
	f.AttemptsSnapshot = app.AttemptsSnapshot{Entries: sampleAttempts(), LastClearedAt: ago(3 * day), NextClearAt: ahead(4 * day)}
	return f
}

// sampleAttempts returns the nine entries, the one with the newest last attempt first.
func sampleAttempts() []attempts.Entry {
	id := func(n int) string { return fmt.Sprintf("a_%016x", n) }
	return []attempts.Entry{
		{ID: id(1), Outcome: attempts.Accepted, PersonID: AliceID, PersonName: "alice", LoginName: "alice",
			Address: "198.51.100.7", Count: 3, FirstAt: ago(2 * day), LastAt: ago(12 * time.Minute),
			ClientVersion: "SSH-2.0-OpenSSH_9.8", Keys: []string{fingerprint(11)}},
		{ID: id(2), Outcome: attempts.UnknownName, LoginName: "admin",
			Address: "203.0.113.9", Count: 1204, FirstAt: ago(6 * day), LastAt: ago(20 * time.Minute),
			ClientVersion: "SSH-2.0-libssh_0.9.6", Keys: []string{fingerprint(31)}},
		{ID: id(3), Outcome: attempts.UnknownName, LoginName: "root\uFFFD[31mX",
			Address: "203.0.113.10", Count: 3, FirstAt: ago(3 * time.Hour), LastAt: ago(time.Hour),
			ClientVersion: "SSH-2.0-Go", Keys: []string{fingerprint(32)}},
		{ID: id(4), Outcome: attempts.WrongKey, PersonID: AliceID, PersonName: "alice", LoginName: "alice",
			Address: "198.51.100.23", Count: 3, FirstAt: ago(2 * day), LastAt: ago(3 * time.Hour),
			ClientVersion: "SSH-2.0-OpenSSH_8.4", Keys: []string{fingerprint(21), fingerprint(33)}, KeyOwner: "bob"},
		{ID: id(5), Outcome: attempts.NoKey, LoginName: strings.Repeat("a", 63) + "…",
			Address: "2001:db8::77", Count: 1, FirstAt: ago(5 * time.Hour), LastAt: ago(5 * time.Hour),
			ClientVersion: "SSH-2.0-PuTTY_Release_0.80"},
		{ID: id(6), Outcome: attempts.NotProven, PersonID: AliceID, PersonName: "alice", LoginName: "alice",
			Address: "198.51.100.30", Count: 1, FirstAt: ago(day), LastAt: ago(day),
			ClientVersion: "SSH-2.0-OpenSSH_9.6", Keys: []string{fingerprint(12)}},
		{ID: id(7), Outcome: attempts.Disabled, PersonID: BobID, PersonName: "bob", LoginName: "bob",
			Address: "192.0.2.50", Count: 3, FirstAt: ago(3 * day), LastAt: ago(2 * day),
			ClientVersion: "SSH-2.0-OpenSSH_9.8", Keys: []string{fingerprint(21)}},
		{ID: id(8), Outcome: attempts.HandshakeFailed,
			Address: "192.0.2.60", Count: 1, FirstAt: ago(4 * day), LastAt: ago(4 * day)},
		{ID: id(9), Outcome: attempts.Refused,
			Address: "192.0.2.61", Count: 1, FirstAt: ago(6 * day), LastAt: ago(6 * day)},
	}
}
