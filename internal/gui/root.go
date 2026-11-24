package gui

import (
	"context"
	"slices"

	"github.com/rm4n0s/cave/internal/app"
	m "github.com/rm4n0s/glutter/material"
)

// root is the widget at the top of Cave's window: it holds the snapshots of the application and
// the current page, subscribes to the application, and puts the Material app (theme, window
// contents) around the shell.
type root struct {
	m.StatefulWidget
	ctx  context.Context
	a    App
	opts Options
	// run is the runner of commands; nil means the real one.
	run runner
}

// newRoot makes the root. ctx ends the commands that are still running when the window closes.
func newRoot(ctx context.Context, a App, opts Options, run runner) *root {
	return &root{StatefulWidget: m.NewStatefulWidgetBase(), ctx: ctx, a: a, opts: opts, run: run}
}

// CreateState creates the state.
func (r *root) CreateState() m.State { return &rootState{} }

type rootState struct {
	m.StateOf[*root]
	v        *view
	cancel   func()
	endWork  context.CancelFunc
	disposed bool
}

// InitState reads the snapshots, makes the runner and subscribes to the application.
func (s *rootState) InitState() {
	w := s.Widget()
	binding := m.WidgetsBindingOf(s.Context())
	ctx, endWork := context.WithCancel(w.ctx)
	s.endWork = endWork
	run := w.run
	if run == nil {
		run = newRunner(ctx, binding.Post)
	}
	page := max(slices.Index(app.Pages, w.opts.Page), 0)
	s.v = (&view{app: w.a, run: run, page: page, selectPage: s.selectPage, opts: w.opts}).
		with(app.TopicServer | app.TopicPeople | app.TopicAttempts | app.TopicConnections | app.TopicSettings)
	// The application tells its changes on its own goroutines: the work is handed to the UI thread.
	s.cancel = w.a.Subscribe(func(event app.Event) {
		changed, ok := event.(app.Changed)
		if !ok {
			return
		}
		binding.Post(func() { s.refresh(changed.Topics) })
	})
}

// Dispose cancels the subscription and the commands that are still running.
func (s *rootState) Dispose() {
	s.disposed = true
	s.cancel()
	s.endWork()
}

// refresh reads the snapshots of the topics again. It runs on the UI thread.
func (s *rootState) refresh(topics app.Topics) {
	if s.disposed {
		return
	}
	s.SetState(func() { s.v = s.v.with(topics) })
}

// selectPage shows the page with the index in app.Pages.
func (s *rootState) selectPage(page int) {
	if page < 0 || page >= len(app.Pages) || page == s.v.page {
		return
	}
	s.SetState(func() {
		c := *s.v
		c.page = page
		s.v = &c
	})
}

// Build makes the Material app, with the theme of the settings, around the shell.
func (s *rootState) Build(m.BuildContext) m.Widget {
	return newViewData(s.v, m.NewMaterialApp(m.MaterialAppOpts{
		Title:                      "Cave",
		Theme:                      lightTheme(),
		DarkTheme:                  darkTheme(),
		ThemeMode:                  themeMode(s.v.settings),
		DebugShowCheckedModeBanner: m.Some(false),
		Home:                       newShell(),
	}))
}
