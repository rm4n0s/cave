// Package gui is Cave's desktop UI (docs/SPEC.md §11), built on Glutter. It calls only
// internal/app, through the interface App, so that it is tested over a fake application with
// fixed content or a real one in a temporary folder (internal/app/apptest).
//
// One stateful widget at the top, the root, holds the snapshots of the application and the
// current page, and hands them down through an inherited widget. A command of the application
// never runs on the UI thread: it goes through a runner, which starts it away from the UI
// thread and gives its error back on it. Only Glutter's UI thread touches widgets.
package gui

import (
	"context"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/settings"
)

// App is what the desktop UI calls of the application. *app.App and apptest.Fake are both one.
// Later steps add the commands they need.
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
}
