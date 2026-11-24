package gui

import (
	"sync"

	m "github.com/rm4n0s/glutter/material"
)

// windowControl is how the parts of the desktop app act on the window: the tray shows it, the close
// button hides it, and the tray's Quit asks the question that the close button asks. The parts that
// know how (the quit guard) register themselves here.
type windowControl struct {
	binding *m.WidgetsBinding
	// askQuit asks before Cave quits and quits; it is set by the quit guard.
	askQuit func()
}

// controller is the controller of the window of the app, if it has one.
func (w *windowControl) controller() *m.RegularWindowController {
	c, ok := w.binding.WindowingOwner().ImplicitWindowController()
	if !ok {
		return nil
	}
	return c
}

// show brings the window back, hidden or minimized, and asks for the focus.
func (w *windowControl) show() {
	if c := w.controller(); c != nil {
		c.SetHidden(false)
		c.Activate()
	}
}

// hide takes the window off the screen and the task bar. The app goes on: a hidden window still
// counts as open, and its widgets keep their state.
func (w *windowControl) hide() {
	if c := w.controller(); c != nil {
		c.SetHidden(true)
	}
}

// quit asks before Cave quits, when anybody is connected, and then quits.
func (w *windowControl) quit() {
	if w.askQuit != nil {
		w.askQuit()
	}
}

// trayStatus remembers whether the desktop has a system tray: Glutter tells that it has not by an
// error that it reports once, and from then on Cave works without one. It is shared by the
// goroutine of the error reporter and the UI thread.
type trayStatus struct {
	mu       sync.Mutex
	gone     bool
	onChange func()
}

// markUnavailable notes that there is no tray, and tells who asked to be told.
func (t *trayStatus) markUnavailable() {
	t.mu.Lock()
	t.gone = true
	fn := t.onChange
	t.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// unavailable says whether the desktop has no tray.
func (t *trayStatus) unavailable() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.gone
}

// subscribe sets the function that is called, on any goroutine, when the tray turns out to be
// missing. nil takes it away.
func (t *trayStatus) subscribe(fn func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onChange = fn
}
