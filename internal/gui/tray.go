package gui

import (
	"context"
	"strconv"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/glutter/desktop"
	m "github.com/rm4n0s/glutter/material"
)

// trayHost puts the tray icon around its child: the tooltip says what the server does, the icon is
// green when it runs, and the menu shows the window, starts or stops the server, and quits. The
// icon, the tooltip and the menu follow the server because the host is built again with every
// change of the view. When the desktop has no tray, or the app was made without one, it is just
// its child.
type trayHost struct {
	m.StatelessWidget
	child m.Widget
}

func newTrayHost(child m.Widget) *trayHost {
	return &trayHost{StatelessWidget: m.NewStatelessWidgetBase(), child: child}
}

// trayTooltip is what the tooltip of the icon says.
func trayTooltip(snap app.ServerSnapshot) string {
	if snap.State == server.Running {
		return "Cave: running on port " + strconv.Itoa(snap.Port)
	}
	return "Cave: stopped"
}

// Build wraps the child in the tray.
func (h *trayHost) Build(bc m.BuildContext) m.Widget {
	v := viewOf(bc)
	if !v.hasTray {
		return h.child
	}
	running := v.server.State == server.Running
	toggle, label := func() {
		runTray(bc, v, func(ctx context.Context) error { return v.app.StartServer(ctx) })
	}, "Start server"
	if v.server.State == server.Running || v.server.State == server.Starting {
		toggle, label = func() {
			runTray(bc, v, func(ctx context.Context) error { return v.app.StopServer(ctx) })
		}, "Stop server"
	}
	return desktop.NewSystemTray(trayWindowIcon(running), h.child, desktop.SystemTrayOpts{
		ID:         "cave",
		Title:      "Cave",
		Tooltip:    trayTooltip(v.server),
		OnActivate: v.window.show,
		Menu: []desktop.TrayMenuEntry{
			desktop.NewTrayMenuItem("Show Cave", v.window.show),
			desktop.NewTrayMenuItem(label, toggle),
			desktop.NewTrayMenuItem("Quit", v.window.quit),
		},
	})
}

// runTray runs a command that the menu of the tray asked for. A failure is shown in the window,
// which is brought forward for it, because a menu has nowhere to say it.
func runTray(bc m.BuildContext, v *view, work func(ctx context.Context) error) {
	v.run(work, func(err error) {
		if err != nil {
			v.window.show()
			showSnack(bc, app.Message(err))
		}
	})
}
