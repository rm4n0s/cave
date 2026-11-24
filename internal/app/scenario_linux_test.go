package app

import (
	"testing"

	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/sshtest"
)

// TestScenarioWithOpenSSH is what a user does, through the commands and with the real ssh: add a
// person with a key, connect, see the connection and the attempt, disconnect, be refused as an
// unknown name, and clear the list. Its output is the proof of step 4.2.
func TestScenarioWithOpenSSH(t *testing.T) {
	sshtest.Require(t)
	f := newFixture(t, nil)
	a := f.open(nil)

	// 1. A person with a key, and the line to give them.
	key := sshtest.NewKey(t, sshtest.Ed25519)
	if _, err := a.AddPerson(ctx, "alice", key.AuthorizedKey+" laptop"); err != nil {
		t.Fatal(err)
	}
	t.Logf("1. added alice with one key; the line to give her is: %s", a.Server().Command("alice", "127.0.0.1"))

	// 2. She connects and runs a command.
	o := sshtest.NewOpenSSH(t, f.addr(), a.hostKey.Signer().PublicKey())
	got := o.SSH("", "alice", key, nil, "echo", "hello")
	if got.ExitCode != 0 || got.Stdout != "hello\n" {
		t.Fatalf("ssh: %+v", got)
	}
	eventually(t, "the list does not have the accepted attempt", func() bool { return len(a.Attempts().Entries) == 1 })
	entry := a.Attempts().Entries[0]
	if entry.Outcome != attempts.Accepted || entry.PersonName != "alice" || entry.Count != 1 {
		t.Fatalf("the attempt is %+v", entry)
	}
	t.Logf("2. alice ran a command; the list has 1 entry: %s, %s, %d time", entry.PersonName, entry.Outcome, entry.Count)

	// 3. She stays connected, and the application shows the connection.
	// The master is not stopped by the test: the user disconnects it below, and the helper kills
	// what is left when the test ends.
	o.StartMaster("alice", key)
	eventually(t, "the application does not show the connection", func() bool { return len(a.Connections()) == 1 })
	conn := a.Connections()[0]
	if conn.PersonName != "alice" {
		t.Fatalf("the connection is %+v", conn)
	}
	t.Logf("3. connected now: %s from %s", conn.PersonName, conn.Address)

	// 4. The user disconnects her.
	if err := a.Disconnect(ctx, conn.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the connection is still listed", func() bool { return len(a.Connections()) == 0 })
	t.Log("4. disconnected alice; nobody is connected")

	// 5. Somebody whose name nobody has is refused, and the list says so.
	if got := o.SSH("", "mallory", key, nil, "true"); got.ExitCode != 255 {
		t.Fatalf("ssh as mallory: %+v", got)
	}
	eventually(t, "the list does not have the refused attempt", func() bool { return len(a.Attempts().Entries) == 2 })
	var refused attempts.Entry
	for _, e := range a.Attempts().Entries {
		if e.Outcome != attempts.Accepted {
			refused = e
		}
	}
	if refused.Outcome != attempts.UnknownName || refused.LoginName != "mallory" {
		t.Fatalf("the refused attempt is %+v", refused)
	}
	t.Logf("5. mallory was refused; the list has 2 entries: alice accepted, %s as %s", refused.Outcome, refused.LoginName)

	// 6. The user clears the list.
	if err := a.ClearAttempts(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(a.Attempts().Entries); n != 0 {
		t.Fatalf("the list has %d entries after it was cleared", n)
	}
	t.Log("6. cleared the list; it is empty")
}
