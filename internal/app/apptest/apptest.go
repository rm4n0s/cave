// Package apptest is the test support of the user interfaces (docs/SPEC.md §14.3). It gives them
// two applications:
//
//   - Fake, with content that is the same on every run (Sample, Empty), for golden files: a real
//     application has random ids, random fingerprints and a port the system chose, so what it
//     draws changes from run to run;
//   - Real, a real application in a temporary folder with its server on a loopback port the
//     system chose (Open), for the flows that must show a real effect.
//
// It is used only by tests.
package apptest

import (
	"context"
	"time"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/settings"
)

// Now is the time of the sample content, and what the clock of a sample shows.
var Now = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

// Application is what a user interface calls of the application. *app.App is one, and so is
// Fake, so that a user interface is tested over either.
type Application interface {
	// The snapshots.
	Server() app.ServerSnapshot
	People() []people.Person
	Attempts() app.AttemptsSnapshot
	Connections() []server.Connection
	History() app.HistorySnapshot
	Settings() settings.Snapshot

	// Subscribe registers a function for the application's events and returns the function that
	// takes it out again.
	Subscribe(fn func(app.Event)) (cancel func())

	// The commands.
	StartServer(ctx context.Context) error
	StopServer(ctx context.Context) error
	Set(ctx context.Context, key string, value any) error
	AddPerson(ctx context.Context, name, keysText string) (people.Person, error)
	CheckPersonName(name string) error
	RenamePerson(ctx context.Context, id, name string) error
	SetPersonEnabled(ctx context.Context, id string, enabled bool) error
	RemovePerson(ctx context.Context, id string) error
	AddKeys(ctx context.Context, personID, text string) ([]people.Key, error)
	ImportKeys(ctx context.Context, personID, path string) ([]people.Key, error)
	SetKeyComment(ctx context.Context, personID, keyID, comment string) error
	RemoveKey(ctx context.Context, personID, keyID string) error
	ClearAttempts(ctx context.Context) error
	RemoveAttempt(ctx context.Context, id string) error
	ClearHistory(ctx context.Context) error
	Disconnect(ctx context.Context, id string) error
	DisconnectAll()
	NewHostKey(ctx context.Context) error
}

// The real application is an Application: the two cannot drift apart.
var _ Application = (*app.App)(nil)
