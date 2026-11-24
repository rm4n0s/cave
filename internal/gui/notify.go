package gui

import (
	"log/slog"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/glutter/desktop"
)

// notification is what is shown to the user: a summary and a body.
type notification struct {
	summary, body string
}

// notificationFor says what to tell the user about an event of the application, or false when
// nothing: the event is not one that is told of, or its setting is off. The settings are those of the
// view, which is read at the time of the event.
func notificationFor(event app.Event, snap settings.Snapshot) (notification, bool) {
	switch e := event.(type) {
	case app.Accepted:
		if !snap.Bool(settings.NotifyAccepted) {
			return notification{}, false
		}
		return notification{summary: e.Person + " connected", body: "From " + e.Address}, true
	case app.Rejected:
		if !snap.Bool(settings.NotifyRejected) {
			return notification{}, false
		}
		return notification{
			summary: "A connection was refused",
			body:    app.Who(e.Entry) + " from " + e.Entry.Address + ": " + app.ResultText(e.Entry.Outcome),
		}, true
	}
	return notification{}, false
}

// notify shows the notification of an event, if there is one. It runs on the UI thread. The desktop
// call waits for the desktop, so it is made on a goroutine; a notification that fails is logged
// and nothing else happens. It is shown also while the window is focused: the user may be looking
// at another page.
func (s *rootState) notify(event app.Event) {
	if s.opts.NoDesktop {
		return
	}
	n, ok := notificationFor(event, s.v.settings)
	if !ok {
		return
	}
	bc, ctx := s.Context(), s.ctx
	show := s.v.window.show
	post := s.post
	go func() {
		_, err := desktop.ShowNotification(ctx, bc, n.summary, desktop.NotificationOpts{
			Body:    n.body,
			AppName: "Cave",
			Actions: []desktop.NotificationAction{{ID: "default", Label: "Show Cave"}},
			OnAction: func(id string) {
				if id == "default" {
					post(show)
				}
			},
		})
		if err != nil {
			slog.Warn("a notification could not be shown", "err", err)
		}
	}()
}
