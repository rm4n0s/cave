package gui

import (
	"context"
	"time"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/settings"
	m "github.com/rm4n0s/glutter/material"
)

// view is what the root hands down: the snapshots of the application, the current page and the
// ways to change them. It is never changed after it is made; the root makes a new one when
// something changes, so the widgets below compare pointers.
type view struct {
	// ctx ends when the window closes; a request to the system (the clipboard) takes it.
	ctx         context.Context
	app         App
	run         runner
	server      app.ServerSnapshot
	people      []people.Person
	attempts    app.AttemptsSnapshot
	connections []server.Connection
	history     app.HistorySnapshot
	settings    settings.Snapshot
	// window acts on the window: show, hide, and the question before quitting.
	window *windowControl
	// hasTray is true when the app has a tray icon: it was not made without one, and the desktop
	// has a tray.
	hasTray bool
	// now is the time the relative times are counted from; the root moves it on every tick.
	now time.Time
	// page is the index in app.Pages of the page that is shown.
	page int
	// selectPage shows another page (the index in app.Pages).
	selectPage func(page int)
	// showSettings shows the Settings page scrolled to the group with the name (one of the Group
	// constants of internal/app).
	showSettings func(group string)
	// settingsGroup is the group the Settings page scrolls to when it is shown; "" for none.
	settingsGroup string
	opts          Options
}

// with returns a copy of the view with the snapshots of the topics read again.
func (v *view) with(topics app.Topics) *view {
	c := *v
	if topics.Has(app.TopicServer) {
		c.server = v.app.Server()
	}
	if topics.Has(app.TopicPeople) {
		c.people = v.app.People()
	}
	if topics.Has(app.TopicAttempts) {
		c.attempts = v.app.Attempts()
	}
	if topics.Has(app.TopicConnections) {
		c.connections = v.app.Connections()
	}
	if topics.Has(app.TopicHistory) {
		c.history = v.app.History()
	}
	if topics.Has(app.TopicSettings) {
		c.settings = v.app.Settings()
	}
	return &c
}

// viewData is the inherited widget that carries the view to the widgets below the root.
type viewData struct {
	m.InheritedWidget
	v *view
}

// newViewData makes the widget that gives the view to its child.
func newViewData(v *view, child m.Widget) *viewData {
	return &viewData{InheritedWidget: m.NewInheritedWidgetBase(child), v: v}
}

// UpdateShouldNotify says that the widgets that read the view are built again when it is a new one.
func (w *viewData) UpdateShouldNotify(old m.Widget) bool { return w.v != old.(*viewData).v }

// viewOf returns the view of the nearest root, and makes the widget of bc depend on it.
func viewOf(bc m.BuildContext) *view {
	data, ok := m.DependOn[*viewData](bc)
	if !ok {
		panic("gui: viewOf called below no root")
	}
	return data.v
}
