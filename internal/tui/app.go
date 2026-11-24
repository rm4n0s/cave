// Package tui is Cave's terminal UI (docs/SPEC.md §12), built on Bubble Tea and Lip Gloss. It
// calls only internal/app, through the interface App, so that it is tested over a fake
// application with fixed content or a real one in a temporary folder (internal/app/apptest).
//
// The model holds plain data: the snapshots of the application, the size, the current page and
// the small state of each page. View only draws what the model holds. A command of the
// application never runs inside Update: a key that changes something returns a tea.Cmd that
// calls the application and gives back a doneMsg, so the terminal never waits for a disk.
package tui

import (
	"context"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/settings"
)

// App is what the TUI calls of the application. *app.App and apptest.Fake are both one.
type App interface {
	Server() app.ServerSnapshot
	People() []people.Person
	Attempts() app.AttemptsSnapshot
	Connections() []server.Connection
	Settings() settings.Snapshot
	Subscribe(fn func(app.Event)) (cancel func())
	StartServer(ctx context.Context) error
	StopServer(ctx context.Context) error
	AddPerson(ctx context.Context, name string) (people.Person, error)
	RenamePerson(ctx context.Context, id, name string) error
	SetPersonEnabled(ctx context.Context, id string, enabled bool) error
	RemovePerson(ctx context.Context, id string) error
	AddKeys(ctx context.Context, personID, text string) ([]people.Key, error)
	ImportKeys(ctx context.Context, personID, path string) ([]people.Key, error)
	SetKeyComment(ctx context.Context, personID, keyID, comment string) error
	RemoveKey(ctx context.Context, personID, keyID string) error
	ClearAttempts(ctx context.Context) error
	RemoveAttempt(ctx context.Context, id string) error
	Disconnect(ctx context.Context, id string) error
	DisconnectAll()
}
