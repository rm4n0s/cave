package apptest

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/settings"
)

// Fake is an Application that does nothing real. Its snapshots are the fields below; a command
// writes a line in the list of calls and returns what Fail says, and changes nothing. A test that
// wants other content changes the fields and calls Emit, as the real application would send a
// Changed. It is safe for concurrent use, because a user interface calls commands off its own
// thread; a test that changes the fields does it while no one else uses the Fake.
type Fake struct {
	ServerSnapshot   app.ServerSnapshot
	PeopleList       []people.Person
	AttemptsSnapshot app.AttemptsSnapshot
	ConnectionList   []server.Connection
	SettingsSnapshot settings.Snapshot
	// Clock is the fake clock that the user interface under test is given.
	Clock *clock.Fake
	// Fail makes the command with that method name (AddPerson, Set, ...) return the error.
	Fail map[string]error

	mu        sync.Mutex
	calls     []string
	listeners []fakeListener
	nextID    int
}

// fakeListener is one subscriber.
type fakeListener struct {
	id int
	fn func(app.Event)
}

var _ Application = (*Fake)(nil)

// Calls returns the commands called so far, oldest first, one line each: the method name and its
// arguments written with %v ("AddPerson(dave)", "SetPersonEnabled(p_00000000000000a1, false)",
// "Set(server.port, 2200)", "StopServer()"), except the arguments that are free text, which are
// quoted ("AddKeys(p_00000000000000a1, \"ssh-ed25519 AAAA…\")", "SetKeyComment(p, k, \"desk\")").
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// Emit tells every subscriber of the event, on the caller's goroutine.
func (f *Fake) Emit(event app.Event) {
	f.mu.Lock()
	listeners := f.listeners
	f.mu.Unlock()
	for _, l := range listeners {
		l.fn(event)
	}
}

// Subscribers says how many subscribers there are now.
func (f *Fake) Subscribers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.listeners)
}

// Subscribe registers a function for the events that Emit sends.
func (f *Fake) Subscribe(fn func(app.Event)) (cancel func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.nextID
	f.nextID++
	f.listeners = append(f.listeners, fakeListener{id: id, fn: fn})
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		for i, l := range f.listeners {
			if l.id == id {
				// A new slice, so that an Emit that is under way keeps its own list.
				f.listeners = append(append([]fakeListener(nil), f.listeners[:i]...), f.listeners[i+1:]...)
				return
			}
		}
	}
}

// record writes the call down and returns what Fail says for the method.
func (f *Fake) record(method string, args ...any) error {
	parts := make([]string, len(args))
	for i, a := range args {
		if q, ok := a.(quoted); ok {
			parts[i] = fmt.Sprintf("%q", string(q))
		} else {
			parts[i] = fmt.Sprint(a)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method+"("+strings.Join(parts, ", ")+")")
	return f.Fail[method]
}

// quoted marks an argument of free text, which Calls writes with quotes.
type quoted string

// The snapshots.

// Server returns ServerSnapshot.
func (f *Fake) Server() app.ServerSnapshot { return f.ServerSnapshot }

// People returns PeopleList.
func (f *Fake) People() []people.Person { return f.PeopleList }

// Attempts returns AttemptsSnapshot.
func (f *Fake) Attempts() app.AttemptsSnapshot { return f.AttemptsSnapshot }

// Connections returns ConnectionList.
func (f *Fake) Connections() []server.Connection { return f.ConnectionList }

// Settings returns SettingsSnapshot.
func (f *Fake) Settings() settings.Snapshot { return f.SettingsSnapshot }

// The commands. Each records its call and returns what Fail says; none changes the snapshots.

// StartServer records the call.
func (f *Fake) StartServer(context.Context) error { return f.record("StartServer") }

// StopServer records the call.
func (f *Fake) StopServer(context.Context) error { return f.record("StopServer") }

// Set records the call.
func (f *Fake) Set(_ context.Context, key string, value any) error {
	return f.record("Set", key, value)
}

// AddPerson records the call. The person it returns is only the name, with a fixed id.
func (f *Fake) AddPerson(_ context.Context, name string) (people.Person, error) {
	if err := f.record("AddPerson", name); err != nil {
		return people.Person{}, err
	}
	return people.Person{ID: "p_ffffffffffffffff", Name: name, Enabled: true}, nil
}

// RenamePerson records the call.
func (f *Fake) RenamePerson(_ context.Context, id, name string) error {
	return f.record("RenamePerson", id, name)
}

// SetPersonEnabled records the call.
func (f *Fake) SetPersonEnabled(_ context.Context, id string, enabled bool) error {
	return f.record("SetPersonEnabled", id, enabled)
}

// RemovePerson records the call.
func (f *Fake) RemovePerson(_ context.Context, id string) error {
	return f.record("RemovePerson", id)
}

// AddKeys records the call.
func (f *Fake) AddKeys(_ context.Context, personID, text string) ([]people.Key, error) {
	return nil, f.record("AddKeys", personID, quoted(text))
}

// ImportKeys records the call.
func (f *Fake) ImportKeys(_ context.Context, personID, path string) ([]people.Key, error) {
	return nil, f.record("ImportKeys", personID, path)
}

// SetKeyComment records the call.
func (f *Fake) SetKeyComment(_ context.Context, personID, keyID, comment string) error {
	return f.record("SetKeyComment", personID, keyID, quoted(comment))
}

// RemoveKey records the call.
func (f *Fake) RemoveKey(_ context.Context, personID, keyID string) error {
	return f.record("RemoveKey", personID, keyID)
}

// ClearAttempts records the call.
func (f *Fake) ClearAttempts(context.Context) error { return f.record("ClearAttempts") }

// RemoveAttempt records the call.
func (f *Fake) RemoveAttempt(_ context.Context, id string) error {
	return f.record("RemoveAttempt", id)
}

// Disconnect records the call.
func (f *Fake) Disconnect(_ context.Context, id string) error { return f.record("Disconnect", id) }

// DisconnectAll records the call.
func (f *Fake) DisconnectAll() { _ = f.record("DisconnectAll") }

// NewHostKey records the call.
func (f *Fake) NewHostKey(context.Context) error { return f.record("NewHostKey") }
