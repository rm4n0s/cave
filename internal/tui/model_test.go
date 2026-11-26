package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/errors"
)

func TestTheRouteOfACommandsErrorStartsAtUpdate(t *testing.T) {
	// Bubble Tea runs the command that Update returns on a goroutine of its own. The route of an
	// error the application gives back to it starts at Update all the same, and goes through the
	// command and the application down to the Tag.
	tests := []struct {
		name string
		// keys and text come before last, the key whose command fails.
		keys  []string
		text  string
		last  string
		route string
	}{
		{
			name:  "a command at once",
			last:  "space",
			route: "Model.Update->Model.Update.func1->Model.togglePerson.func1->App.SetPersonEnabled->Store.SetEnabled->findPerson.PersonNotFound",
		},
		{
			name:  "a command after a question",
			keys:  []string{"d"},
			last:  "y",
			route: "Model.Update->Model.Update.func1->Model.askRemovePerson.func1->App.RemovePerson->Store.Remove->findPerson.PersonNotFound",
		},
		{
			name:  "the command of a prompt",
			keys:  []string{"e", "ctrl+u"},
			text:  "alicia",
			last:  "enter",
			route: "Model.Update->Model.Update.func1->Model.keyPeople.func2.1->App.RenamePerson->Store.Rename->findPerson.PersonNotFound",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := apptest.Open(t, nil)
			r.Person("alice")
			h := newHarness(t, r.App, r.Clock, 100, 30)
			h.key("2")
			// alice goes away behind the page's back, so the command for her fails.
			if err := r.App.RemovePerson(ctx, r.App.People()[0].ID); err != nil {
				t.Fatal(err)
			}
			h.keys(tt.keys...)
			h.typeText(tt.text)
			_, cmd := h.model.Update(keyPress(tt.last))
			if cmd == nil {
				t.Fatalf("%s gave no command:\n%s", tt.last, h.screen())
			}
			msgs := make(chan tea.Msg, 1)
			go func() { msgs <- cmd() }()
			err := commandErr(<-msgs)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T: %v", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
