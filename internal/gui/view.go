package gui

import (
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
	app         App
	run         runner
	server      app.ServerSnapshot
	people      []people.Person
	attempts    app.AttemptsSnapshot
	connections []server.Connection
	settings    settings.Snapshot
	// page is the index in app.Pages of the page that is shown.
	page int
	// selectPage shows another page (the index in app.Pages).
	selectPage func(page int)
	opts       Options
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
