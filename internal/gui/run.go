package gui

import (
	"context"

	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/errors"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/ui"
)

// appID is the application ID of the desktop app, as Linux desktops know it.
const appID = "io.github.rm4n0s.cave"

// Options are what the desktop app needs besides the application.
type Options struct {
	// Page is the page shown first, one of app.Pages; "" is the Overview.
	Page string
	// ConfigDir and StateDir are the folders of Cave, shown on the Settings page.
	ConfigDir, StateDir string
	// Version is the version of Cave, shown on the Settings page.
	Version string
	// Clock is the clock of the relative times ("3 min ago") and of the tick that draws them
	// again; nil is the system's.
	Clock clock.Clock
}

// Run shows the window until the user closes it or ctx is cancelled (which is not an error). The
// application is not opened or closed here.
func Run(ctx context.Context, a App, opts Options) error {
	err := m.RunAppContext(ctx, newRoot(ctx, a, opts, nil), m.RunAppOpts{
		AppID: appID,
		Window: m.WindowConfig{
			Title:   "Cave",
			Size:    ui.Size{Width: 960, Height: 640},
			MinSize: ui.Size{Width: 720, Height: 480},
		},
	})
	if err != nil {
		return errors.NewErr("GuiRunFailed", err)
	}
	return nil
}
