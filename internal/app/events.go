package app

import "time"

// CoalesceTime is the least time between two Changed events: what changes within it is told in
// one event (docs/SPEC.md §10).
const CoalesceTime = 100 * time.Millisecond

// Topics is a set of the things that can change.
type Topics uint8

// The topics: each has its snapshot.
const (
	TopicServer Topics = 1 << iota
	TopicPeople
	TopicAttempts
	TopicConnections
	TopicSettings
)

// Has says whether the set has every topic of other.
func (t Topics) Has(other Topics) bool {
	return t&other == other
}

// Event is something the application tells its subscribers: a Changed, an Accepted or a
// Rejected.
type Event interface {
	// event marks the types that are events.
	event()
}

// Changed says that the snapshots of some topics are not what they were: a user interface reads
// them again.
type Changed struct {
	Topics Topics
}

func (Changed) event() {}

// listener is one subscriber.
type listener struct {
	id int
	fn func(Event)
}

// Subscribe registers a function that gets the application's events, and returns the function
// that takes it out again. The function is called on goroutines of the application, never on
// the subscriber's own: a user interface hands the event to its own thread and reads the
// snapshots there.
func (a *App) Subscribe(fn func(Event)) (cancel func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := a.nextID
	a.nextID++
	a.listeners = append(a.listeners, listener{id: id, fn: fn})
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		for i, l := range a.listeners {
			if l.id == id {
				// A new slice, so that an event that is being sent keeps its own list.
				a.listeners = append(append([]listener(nil), a.listeners[:i]...), a.listeners[i+1:]...)
				return
			}
		}
	}
}

// changed notes that a topic changed. The subscribers are told CoalesceTime after the first
// change that they have not been told of, in one event with everything that changed meanwhile.
func (a *App) changed(topic Topics) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	a.pending |= topic
	if a.flush == nil {
		a.flush = a.clock.AfterFunc(CoalesceTime, a.send)
	}
}

// send tells the subscribers what changed.
func (a *App) send() {
	a.mu.Lock()
	topics, listeners := a.pending, a.listeners
	a.pending, a.flush = 0, nil
	closed := a.closed
	a.mu.Unlock()
	if closed || topics == 0 {
		return
	}
	for _, l := range listeners {
		l.fn(Changed{Topics: topics})
	}
}
