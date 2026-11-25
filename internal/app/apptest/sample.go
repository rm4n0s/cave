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
	"github.com/rm4n0s/cave/internal/core/history"
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

// SampleSettings returns the snapshot of the settings with the values set over the defaults. A
// snapshot cannot be made by hand, so it is read from a store in an isolated folder.
func SampleSettings(t testing.TB, values map[string]any) settings.Snapshot {
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
		SettingsSnapshot: SampleSettings(t, nil),
		Clock:            clock.NewFake(Now),
	}
}

// Sample returns a fake with content that is the same on every run: a running server, three
// people (one disabled, one without a key), one connection and nine attempts, one of each
// result and one with a login name that a client made hostile, and a history of seven connections
// (the one open now, one of each other reason) and eight transfers (every direction and
// result, one with a long path). attempts.clear is "weekly".
func Sample(t testing.TB) *Fake {
	t.Helper()
	f := &Fake{
		ServerSnapshot:   baseServer(server.Running),
		SettingsSnapshot: SampleSettings(t, map[string]any{string(settings.AttemptsClear): settings.ClearWeekly}),
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
	f.HistorySnapshot = app.HistorySnapshot{Connections: sampleHistory(), Transfers: sampleTransfers(), LastClearedAt: ago(20 * day)}
	return f
}

// The ids of the sample's connections in the history.
const (
	HistoryOpenID      = "c_0000000000000001"
	HistoryClientID    = "c_00000000000000e2"
	HistoryKeepAliveID = "c_00000000000000e6"
)

// sampleHistory returns the connections of the sample's history, the newest first: the one open
// now (the connection of the registry) and one of every other reason.
func sampleHistory() []history.Connection {
	conn := func(id, person, personID, address string, since, lasted time.Duration, reason history.Reason) history.Connection {
		c := history.Connection{
			ID: id, PersonID: personID, PersonName: person, KeyFingerprint: fingerprint(11), Address: address,
			ClientVersion: "SSH-2.0-OpenSSH_9.8", ConnectedAt: ago(since), Reason: reason,
		}
		if reason != "" && reason != history.ReasonUnknown {
			c.EndedAt = ago(since - lasted)
			c.BytesIn, c.BytesOut = 48_213, 1_402_118
		}
		return c
	}
	open := conn(HistoryOpenID, "alice", AliceID, "198.51.100.7", 12*time.Minute, 0, "")
	open.Activities = []history.Activity{{Kind: "shell"}, {Kind: "local-forward", Detail: "127.0.0.1:5432"}}
	client := conn(HistoryClientID, "alice", AliceID, "198.51.100.7", 3*time.Hour, 2*time.Hour+5*time.Minute, history.ReasonClient)
	client.Activities = []history.Activity{{Kind: "sftp"}, {Kind: "exec", Detail: "git-upload-pack 'cave.git'"}}
	return []history.Connection{
		open,
		client,
		conn("c_00000000000000e3", "carol", CarolID, "2001:db8::42", 5*time.Hour, 45*time.Second, history.ReasonDisconnected),
		conn("c_00000000000000e4", "bob", BobID, "192.0.2.50", 2*day+time.Hour, 12*time.Minute, history.ReasonPersonChanged),
		conn("c_00000000000000e5", "alice", AliceID, "198.51.100.7", 3*day, 26*time.Hour, history.ReasonServerStopped),
		conn(HistoryKeepAliveID, "carol", CarolID, "203.0.113.80", 4*day, 4*time.Minute, history.ReasonKeepAlive),
		conn("c_00000000000000e7", "alice", AliceID, "198.51.100.7", 6*day, 0, history.ReasonUnknown),
	}
}

// sampleTransfers returns the transfers of the sample's history, the newest first.
func sampleTransfers() []history.Transfer {
	id := func(n int) string { return fmt.Sprintf("t_%016x", n) }
	tr := func(n int, conn, path string, closed, lasted time.Duration, down, up int64, created bool, result history.Result) history.Transfer {
		return history.Transfer{
			ID: id(n), ConnectionID: conn, PersonID: AliceID, PersonName: "alice", Path: path, OpenedAt: ago(closed + lasted),
			ClosedAt: ago(closed), Downloaded: down, Uploaded: up, Created: created, Result: result,
		}
	}
	long := "/home/ada/projects/" + strings.Repeat("deeply/nested/", 8) + "report-final-v2.pdf"
	return []history.Transfer{
		tr(1, HistoryOpenID, "/home/ada/notes.txt", 5*time.Minute, time.Second, 0, 2_048, true, history.ResultDone),
		tr(2, HistoryClientID, "/home/ada/photos/beach.jpg", 2*time.Hour, 12*time.Second, 3_400_000, 0, false, history.ResultDone),
		tr(3, HistoryClientID, "/home/ada/db.sqlite", 2*time.Hour+10*time.Minute, 3*time.Second, 40_960, 8_192, false, history.ResultDone),
		tr(4, HistoryClientID, long, 2*time.Hour+20*time.Minute, 2*time.Second, 1_250_000, 0, false, history.ResultDone),
		tr(5, HistoryClientID, "/home/ada/empty.log", 2*time.Hour+30*time.Minute, 0, 0, 0, true, history.ResultDone),
		tr(6, HistoryClientID, "/home/ada/backup.tar", 2*time.Hour+40*time.Minute, 90*time.Second, 0, 52_428_800, true, history.ResultInterrupted),
		tr(7, HistoryClientID, "/home/ada/disk-full.bin", 2*time.Hour+50*time.Minute, time.Second, 0, 4_096, true, history.ResultFailed),
		tr(8, HistoryClientID, "/home/ada/big.iso", 3*time.Hour-time.Minute, 30*time.Second, 1_100_000_000, 0, false, history.ResultDone),
	}
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
