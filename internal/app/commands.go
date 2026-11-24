package app

import (
	"context"

	"github.com/rm4n0s/cave/internal/core/hostkey"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/cave/internal/core/server"
)

// The commands of the application (docs/SPEC.md §10). Each one hands its work to the store or
// the server that owns it and returns that error as it is, so that its Tag and route reach the
// caller; the UIs turn it into a sentence with Message. None of them sends a Changed event
// itself: the stores and the server tell the application when something changed (see open),
// and none of them disconnects anybody itself: the server watches the people store and closes
// the connections of a person who was renamed, disabled or removed, and of a key that was
// removed (docs/SPEC.md §7.1).

// AddPerson adds a person with the public keys in keysText (the authorized_keys format, one per
// line). A person needs a key to connect, so there is no person without one: a text with no key,
// or one key that is not acceptable, adds nobody.
func (a *App) AddPerson(ctx context.Context, name, keysText string) (people.Person, error) {
	return a.people.AddWithKeys(ctx, name, keysText)
}

// CheckPersonName says whether a person could be added with the name: nil, or why not
// (PersonNameInvalid, PersonNameTaken). It changes nothing, so a UI can tell the name is wrong
// before it asks for the keys.
func (a *App) CheckPersonName(name string) error {
	return a.people.CheckName(name)
}

// RenamePerson changes the login name of a person and disconnects them.
func (a *App) RenamePerson(ctx context.Context, id, name string) error {
	return a.people.Rename(ctx, id, name)
}

// SetPersonEnabled lets a person connect again (true) or stops them (false); stopping a
// person disconnects them.
func (a *App) SetPersonEnabled(ctx context.Context, id string, enabled bool) error {
	return a.people.SetEnabled(ctx, id, enabled)
}

// RemovePerson removes a person with their keys and disconnects them.
func (a *App) RemovePerson(ctx context.Context, id string) error {
	return a.people.Remove(ctx, id)
}

// AddKeys adds the public keys in text (the authorized_keys format, one per line) to a person.
// Either all of them are added or none.
func (a *App) AddKeys(ctx context.Context, personID, text string) ([]people.Key, error) {
	return a.people.AddKeys(ctx, personID, text)
}

// ImportKeys adds the public keys of a file (a .pub file or an authorized_keys file) to a
// person, under the rules of AddKeys.
func (a *App) ImportKeys(ctx context.Context, personID, path string) ([]people.Key, error) {
	return a.people.ImportKeys(ctx, personID, path)
}

// SetKeyComment changes the comment of a key of a person.
func (a *App) SetKeyComment(ctx context.Context, personID, keyID, comment string) error {
	return a.people.SetKeyComment(ctx, personID, keyID, comment)
}

// RemoveKey removes a key of a person and disconnects the connections that used it.
func (a *App) RemoveKey(ctx context.Context, personID, keyID string) error {
	return a.people.RemoveKey(ctx, personID, keyID)
}

// ClearAttempts empties the attempts list and saves it at once, so that a list cleared by hand
// is not back after a crash.
func (a *App) ClearAttempts(ctx context.Context) error {
	a.attempts.Clear()
	return a.attempts.Flush(ctx)
}

// RemoveAttempt removes one entry of the attempts list.
func (a *App) RemoveAttempt(_ context.Context, id string) error {
	return a.attempts.Remove(id)
}

// Disconnect closes one connection, by the ID that Connections gives.
func (a *App) Disconnect(_ context.Context, id string) error {
	return a.server.Disconnect(id)
}

// DisconnectAll closes every connection. It cannot fail.
func (a *App) DisconnectAll() {
	a.server.DisconnectAll()
}

// NewHostKey makes a new host key and puts it in place of the old one. Connections that are
// open stay open; the next connection is made with the new key, so every client that knew the
// old one will be told that the host key changed.
func (a *App) NewHostKey(ctx context.Context) error {
	a.serverMu.Lock()
	defer a.serverMu.Unlock()
	key, err := hostkey.Create(ctx, a.opts.Dirs)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.hostKey = key
	a.mu.Unlock()
	a.server.SetHostKey(key.Signer())
	a.changed(TopicServer)
	return nil
}

// CountConnections says how many of the connections belong to a person (keyID empty) or came
// in with a key (keyID given). The UIs use it to say how many connections a removal will close.
func CountConnections(conns []server.Connection, personID, keyID string) int {
	n := 0
	for _, c := range conns {
		if keyID == "" && c.PersonID == personID || keyID != "" && c.KeyID == keyID {
			n++
		}
	}
	return n
}
