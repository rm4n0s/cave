package gui

import (
	"github.com/rm4n0s/cave/internal/app"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/ui"
)

// quitGuard is what asks before Cave quits while people are connected (docs/SPEC.md §11.6): it
// listens for the request to exit that the window's close button makes. With nobody connected the
// exit goes on; with connections it is cancelled and a question is asked, and the answer Quit
// exits. The end of the user's session is never asked about: refusing would stop the logout.
type quitGuard struct {
	m.StatefulWidget
	child m.Widget
}

func newQuitGuard(child m.Widget) *quitGuard {
	return &quitGuard{StatefulWidget: m.NewStatefulWidgetBase(), child: child}
}

// CreateState creates the state.
func (g *quitGuard) CreateState() m.State { return &quitState{} }

type quitState struct {
	m.StateOf[*quitGuard]
	listener *m.AppLifecycleListener
	// v is the view of the last build: the callback of the listener is not a build and cannot
	// depend on the inherited view, so it reads the connections from here.
	v *view
	// asking is true while the question is on the screen, so that a second click on the close
	// button does not open a second one.
	asking bool
}

// InitState starts to listen for the request to exit.
func (s *quitState) InitState() {
	s.listener = m.NewAppLifecycleListener(m.WidgetsBindingOf(s.Context()), m.AppLifecycleListenerOpts{
		OnExitRequested: s.exitRequested,
	})
}

// Dispose stops listening.
func (s *quitState) Dispose() { s.listener.Dispose() }

// exitRequested answers the request to exit. It runs on the UI thread.
func (s *quitState) exitRequested() ui.AppExitResponse {
	binding := m.WidgetsBindingOf(s.Context())
	if binding.PlatformDispatcher().ExitRequestReason() == ui.AppExitReasonSessionEnd || s.v == nil {
		return ui.AppExitResponseExit
	}
	question := app.QuitQuestion(s.v.connections)
	if question == "" {
		return ui.AppExitResponseExit
	}
	if !s.asking {
		s.asking = true
		answer := confirm(s.Context(), "Quit Cave", question, "Quit", true)
		post := binding.Post
		// The answer is false for No, and for a dialog that was closed without a choice.
		go func() {
			yes := <-answer
			post(func() {
				s.asking = false
				if yes {
					binding.ExitApplication(ui.AppExitTypeRequired)
				}
			})
		}()
	}
	return ui.AppExitResponseCancel
}

// Build remembers the view and shows the child.
func (s *quitState) Build(bc m.BuildContext) m.Widget {
	s.v = viewOf(bc)
	return s.Widget().child
}
