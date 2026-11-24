package gui

import (
	"context"
	"strconv"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/glutter/foundation"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
)

// serverToggleKey and the other keys name what the tests tap.
var (
	serverToggleKey = foundation.NewKey("server-toggle")
	addPersonKey    = foundation.NewKey("overview-add-person")
)

// runningColor is the color of the dot of a running server; there is no green in the theme.
var runningColor = ui.NewColor(0xFF2E7D32)

// overviewPage is the first page (docs/SPEC.md §11.1): the server's state with the button that
// starts and stops it, the host key, how to connect, and the counts.
type overviewPage struct {
	m.StatefulWidget
	v *view
}

// newOverviewPage makes the page over the view.
func newOverviewPage(v *view) *overviewPage {
	return &overviewPage{StatefulWidget: m.NewStatefulWidgetBase(), v: v}
}

// CreateState creates the state.
func (p *overviewPage) CreateState() m.State { return &overviewState{} }

type overviewState struct {
	m.StateOf[*overviewPage]
	// busy is true while StartServer or StopServer runs: the button is off.
	busy     bool
	disposed bool
}

// Dispose remembers that the page is gone, so that a command that ends later does not touch it.
func (s *overviewState) Dispose() { s.disposed = true }

// toggle starts the server when it is stopped and stops it when it runs or starts.
func (s *overviewState) toggle() {
	v := s.Widget().v
	if s.busy {
		return
	}
	stop := v.server.State == server.Running || v.server.State == server.Starting
	s.busy = true
	v.run(func(ctx context.Context) error {
		if stop {
			return v.app.StopServer(ctx)
		}
		return v.app.StartServer(ctx)
	}, func(err error) {
		if s.disposed {
			return
		}
		s.SetState(func() { s.busy = false })
		if err != nil {
			showSnack(s.Context(), app.Message(err))
		}
	})
	// The command may have ended at once (a synchronous runner): the button is off only if it
	// is still running.
	if s.busy {
		s.SetState(func() {})
	}
}

// Build lays out the cards.
func (s *overviewState) Build(bc m.BuildContext) m.Widget {
	v := s.Widget().v
	var children []m.Widget
	if len(v.people) == 0 {
		// The first thing to do is the first thing on the page, above the fold of a small window.
		children = append(children, nobodyCard(bc, v), gap(16))
	}
	children = append(children, s.serverCard(bc, v), gap(16), s.hostKeyCard(bc, v), gap(16), s.connectCard(bc, v), gap(16), s.counts(bc, v))
	return column(children)
}

// stateWords is the server's state in words, and the color of its dot.
func stateWords(bc m.BuildContext, snap app.ServerSnapshot) (string, ui.Color) {
	scheme := m.ThemeOf(bc).ColorScheme
	switch snap.State {
	case server.Running:
		return "Running on port " + strconv.Itoa(snap.Port), runningColor
	case server.Starting:
		return "Starting…", scheme.Outline()
	case server.Stopping:
		return "Stopping…", scheme.Outline()
	}
	if snap.Err != nil {
		return "Not running", scheme.Error()
	}
	return "Stopped", scheme.Outline()
}

// dot is a small colored circle.
func dot(color ui.Color) m.Widget {
	return m.NewContainer(nil, m.ContainerOpts{
		Width: foundation.Some(12.0), Height: foundation.Some(12.0),
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(color), Shape: painting.BoxShapeCircle}),
	})
}

func (s *overviewState) serverCard(bc m.BuildContext, v *view) m.Widget {
	theme := m.ThemeOf(bc)
	words, color := stateWords(bc, v.server)
	label, onPressed := "Start server", s.toggle
	if v.server.State == server.Running || v.server.State == server.Starting {
		label = "Stop server"
	}
	if s.busy {
		onPressed = nil
	}
	children := []m.Widget{
		m.NewRow([]m.Widget{
			dot(color), hgap(12),
			m.NewExpanded(m.NewText(words, m.TextOpts{Style: theme.TextTheme.TitleLarge})),
		}),
		gap(16),
		m.NewFilledButton(onPressed, m.NewPadding(m.NewEdgeInsetsSymmetric(8, 16), m.NewText(label)), m.FilledButtonOpts{Key: serverToggleKey}),
	}
	if v.server.Err != nil {
		children = append(children, gap(12), m.NewText(app.Message(v.server.Err), m.TextOpts{
			Style: theme.TextTheme.BodyMedium.CopyWith(m.TextStyleCopy{Color: foundation.Some(theme.ColorScheme.Error())}),
		}))
	}
	return card(children)
}

func (s *overviewState) hostKeyCard(bc m.BuildContext, v *view) m.Widget {
	theme := m.ThemeOf(bc)
	return card([]m.Widget{
		m.NewText("Host key", m.TextOpts{Style: theme.TextTheme.TitleMedium}),
		gap(8),
		m.NewRow([]m.Widget{
			m.NewExpanded(m.NewText(v.server.HostKeyFingerprint, m.TextOpts{Style: monoStyle(theme.TextTheme.BodyMedium)})),
			copyButton(bc, s.Lifetime(), "copy-fingerprint", v.server.HostKeyFingerprint),
		}),
		gap(4),
		m.NewText("A person sees this the first time they connect.", m.TextOpts{Style: theme.TextTheme.BodySmall}),
	})
}

// connectName is the name to put in the lines that tell how to connect: the first person who is
// enabled, else the word name.
func connectName(v *view) string {
	for _, p := range v.people {
		if p.Enabled {
			return p.Name
		}
	}
	return "name"
}

func (s *overviewState) connectCard(bc m.BuildContext, v *view) m.Widget {
	theme := m.ThemeOf(bc)
	children := []m.Widget{m.NewText("How to connect", m.TextOpts{Style: theme.TextTheme.TitleMedium}), gap(8)}
	if len(v.server.Addresses) == 0 {
		children = append(children, m.NewText("This computer's addresses are not known.", m.TextOpts{Style: theme.TextTheme.BodyMedium}))
	}
	name := connectName(v)
	for i, address := range v.server.Addresses {
		line := v.server.Command(name, address)
		children = append(children, m.NewRow([]m.Widget{
			m.NewExpanded(m.NewText(line, m.TextOpts{Style: monoStyle(theme.TextTheme.BodyMedium)})),
			copyButton(bc, s.Lifetime(), "copy-ssh-"+strconv.Itoa(i), line),
		}))
	}
	return card(children)
}

// counts are the three numbers in a row.
func (s *overviewState) counts(bc m.BuildContext, v *view) m.Widget {
	return m.NewRow([]m.Widget{
		m.NewExpanded(count(bc, "People", len(v.people), "count-people")), hgap(16),
		m.NewExpanded(count(bc, "Connected now", len(v.connections), "count-connections")), hgap(16),
		m.NewExpanded(count(bc, "Attempts in the list", len(v.attempts.Entries), "count-attempts")),
	})
}

func count(bc m.BuildContext, label string, n int, key string) m.Widget {
	theme := m.ThemeOf(bc)
	return card([]m.Widget{
		m.NewText(strconv.Itoa(n), m.TextOpts{Key: foundation.NewKey(key), Style: theme.TextTheme.HeadlineMedium}),
		m.NewText(label, m.TextOpts{Style: theme.TextTheme.BodySmall}),
	})
}

// nobodyCard tells that nobody can connect yet, and takes the user to the People page.
func nobodyCard(bc m.BuildContext, v *view) m.Widget {
	return card([]m.Widget{
		m.NewText("Nobody can connect yet.", m.TextOpts{Style: m.ThemeOf(bc).TextTheme.TitleMedium}),
		gap(12),
		m.NewFilledButton(func() { v.selectPage(pagePeople) }, m.NewText("Add a person"), m.FilledButtonOpts{Key: addPersonKey}),
	})
}
