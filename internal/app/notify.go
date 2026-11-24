package app

import (
	"sync"
	"time"

	"github.com/rm4n0s/cave/internal/core/attempts"
)

// rejectedEvery is the least time between two Rejected events (docs/SPEC.md §6).
const rejectedEvery = time.Minute

// eventQueue is how many Accepted and Rejected events wait for the subscribers. A subscriber
// that is slower than the connections arrive loses the newest ones: a notification is not worth
// holding up a connection.
const eventQueue = 64

// Accepted is sent when somebody connected: the person's name and the address they came from.
type Accepted struct {
	Person  string
	Address string
}

func (Accepted) event() {}

// Rejected is sent when an attempt was refused, at most once a minute: the entry of the
// attempts list the attempt was counted into.
type Rejected struct {
	Entry attempts.Entry
}

func (Rejected) event() {}

// notifier turns the attempts that are recorded into Accepted and Rejected events. They are
// recorded on the goroutine of a connection, which must never wait for a subscriber, so they go
// through a queue to a goroutine of their own.
type notifier struct {
	events chan Event
	done   chan struct{}
	wg     sync.WaitGroup
	// cancel ends the watch of the attempts list.
	cancel func()

	mu           sync.Mutex
	sentRejected bool
	lastRejected time.Time
}

// startNotifier begins to watch the attempts list. Close ends it with stopNotifier.
func (a *App) startNotifier() {
	n := &notifier{events: make(chan Event, eventQueue), done: make(chan struct{})}
	n.cancel = a.attempts.SubscribeRecords(func(e attempts.Entry) { a.queueRecorded(n, e) })
	a.notifier = n
	n.wg.Add(1)
	go a.dispatch(n)
}

// stopNotifier ends the watch and waits until the goroutine that told the subscribers is done,
// so that nothing is sent after it returns. The queue is never closed: a connection that is
// still being recorded may put an event in it, and a send on a closed channel panics.
func (a *App) stopNotifier() {
	n := a.notifier
	if n == nil {
		return
	}
	n.cancel()
	close(n.done)
	n.wg.Wait()
}

// queueRecorded makes the event of an attempt that was recorded and queues it. It runs on the
// goroutine that recorded the attempt and never waits.
func (a *App) queueRecorded(n *notifier, e attempts.Entry) {
	var event Event
	if e.Outcome == attempts.Accepted {
		event = Accepted{Person: e.PersonName, Address: e.Address}
	} else {
		if !n.allowRejected(a.clock.Now()) {
			return
		}
		event = Rejected{Entry: e}
	}
	select {
	case n.events <- event:
	default:
	}
}

// allowRejected says whether a Rejected may be sent at now, and remembers that one was: the
// first, and then one a minute (by the application's clock).
func (n *notifier) allowRejected(now time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.sentRejected && now.Sub(n.lastRejected) < rejectedEvery {
		return false
	}
	n.sentRejected, n.lastRejected = true, now
	return true
}

// dispatch tells the subscribers of each queued event, until the application is closed.
func (a *App) dispatch(n *notifier) {
	defer n.wg.Done()
	for {
		select {
		case event := <-n.events:
			a.mu.Lock()
			closed, listeners := a.closed, a.listeners
			a.mu.Unlock()
			if closed {
				return
			}
			for _, l := range listeners {
				l.fn(event)
			}
		case <-n.done:
			return
		}
	}
}
