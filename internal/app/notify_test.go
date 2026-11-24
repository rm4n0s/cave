package app

import (
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/sshtest"
)

// collector gathers the Accepted and Rejected events a subscriber is told, which arrive on a
// goroutine of the application.
type collector struct {
	got chan Event
}

func newCollector(a *App) *collector {
	c := &collector{got: make(chan Event, 256)}
	a.Subscribe(func(e Event) {
		switch e.(type) {
		case Accepted, Rejected:
			c.got <- e
		}
	})
	return c
}

// next waits for an event, for up to waitFor.
func (c *collector) next(t *testing.T) Event {
	t.Helper()
	select {
	case e := <-c.got:
		return e
	case <-time.After(waitFor):
		t.Fatal("no event came")
		return nil
	}
}

// none checks that no event arrives within a short time.
func (c *collector) none(t *testing.T) {
	t.Helper()
	select {
	case e := <-c.got:
		t.Fatalf("an event came that should not have: %+v", e)
	case <-time.After(200 * time.Millisecond):
	}
}

// refuse makes an attempt with a login name nobody has, and returns once the server has
// answered.
func (f *fixture) refuse(a *App, key sshtest.Key) {
	f.t.Helper()
	config := sshtest.ClientConfig("nobody", a.hostKey.Signer().PublicKey(), key)
	if _, err := sshtest.DialErr(f.t, f.addr(), config); err == nil {
		f.t.Fatal("a name nobody has was let in")
	}
}

func TestAcceptedIsSent(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(nil)
	events := newCollector(a)
	f.connect(a, "alice", f.person(a, "alice"), f.port)
	if got := events.next(t); got != (Accepted{Person: "alice", Address: "127.0.0.1"}) {
		t.Errorf("got %+v, want alice from 127.0.0.1", got)
	}
	events.none(t)
}

func TestRejectedAtMostOnceAMinute(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(nil)
	events := newCollector(a)
	key := f.person(a, "alice")

	for i := 0; i < 3; i++ {
		f.refuse(a, key)
	}
	eventually(t, "the three attempts are not counted into one entry", func() bool {
		entries := a.Attempts().Entries
		return len(entries) == 1 && entries[0].Count == 3
	})
	first, ok := events.next(t).(Rejected)
	if !ok || first.Entry.Outcome != attempts.UnknownName || first.Entry.LoginName != "nobody" {
		t.Fatalf("the first event is %+v", first)
	}
	events.none(t) // the other two are not told

	// A minute later the next one is.
	f.clock.Advance(time.Minute)
	f.refuse(a, key)
	second, ok := events.next(t).(Rejected)
	if !ok || second.Entry.Count != 4 {
		t.Errorf("the second event is %+v, want the entry counted 4 times", second)
	}
	events.none(t)
}

func TestASlowSubscriberDoesNotBlockTheServer(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(func(o *Options) { o.NoStart = true })
	release := make(chan struct{})
	defer close(release) // before the cleanup that closes the application waits for the subscriber
	told := make(chan struct{}, 1)
	a.Subscribe(func(e Event) {
		if _, ok := e.(Accepted); ok {
			select {
			case told <- struct{}{}:
			default:
			}
			<-release
		}
	})

	// Many more attempts than the queue holds, none of them waits for the subscriber.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			a.attempts.Record(attempts.Attempt{Outcome: attempts.Accepted, PersonID: "p_1", PersonName: "alice", Address: "192.0.2.1"})
		}
	}()
	select {
	case <-done:
	case <-time.After(waitFor):
		t.Fatal("recording attempts waits for a subscriber that does not return")
	}
	select {
	case <-told:
	case <-time.After(waitFor):
		t.Error("the subscriber was never told")
	}
}

func TestNothingIsSentAfterClose(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(func(o *Options) { o.NoStart = true })
	events := newCollector(a)
	f.close(a)
	a.attempts.Record(attempts.Attempt{Outcome: attempts.Accepted, PersonID: "p_1", PersonName: "alice", Address: "192.0.2.1"})
	a.attempts.Record(attempts.Attempt{Outcome: attempts.UnknownName, LoginName: "nobody", Address: "192.0.2.1"})
	events.none(t)
}
