package gui

import (
	"context"
	"log/slog"

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
	// NoDesktop makes the app without the parts that talk to the desktop: no tray icon and no
	// notifications. The tests of everything but those two make it so: a tray over a desktop that
	// has none reports an error, and a notification that is started as the tree is taken down
	// reaches a widget that is gone.
	NoDesktop bool
	// tray is what is known of the desktop's tray; Run makes it, and the tests that need to make
	// the tray missing give their own. nil makes the root one.
	tray *trayStatus
}

// Run shows the window until the user closes it or ctx is cancelled (which is not an error). The
// application is not opened or closed here.
func Run(ctx context.Context, a App, opts Options) error {
	// Glutter says that the desktop has no tray by an error that it reports once; Cave notes it
	// and goes on without a tray. Any other error is logged.
	opts.tray = &trayStatus{}
	err := m.RunAppContext(ctx, newRoot(ctx, a, opts, nil), m.RunAppOpts{
		ErrorReporter: func(err error) {
			if appErr, ok := errors.FromError(err); ok && appErr.HasRoute("TrayUnavailable") {
				opts.tray.markUnavailable()
				return
			}
			slog.Warn("the toolkit reported an error", "err", err)
		},
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
