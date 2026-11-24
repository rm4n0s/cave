package gui

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/desktop"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/gui/icon"
	"github.com/rm4n0s/cave/internal/gui/testutil"
)

// trayApp is a pumped app with a tray, over a fake desktop.
type trayApp struct {
	tester  *gluttertest.Tester
	desktop *gluttertest.FakeDesktop
	status  *trayStatus
}

// pumpTray pumps the whole app, tray included, over the fake desktop (a tray with a desktop that has
// none is pumpNoDesktop). The desktop is made before the app, as the app connects to it when it is built.
func pumpTray(t *testing.T, f *apptest.Fake) trayApp {
	t.Helper()
	tester := testutil.NewTester(t, testutil.Wide)
	fake := tester.FakeDesktop()
	status := &trayStatus{}
	tester.PumpWidget(newRoot(context.Background(), f, Options{Page: "overview", Clock: f.Clock, tray: status}, syncRunner))
	tester.PumpAndSettle()
	fake.WaitFor("the tray registers", fake.TrayRegistered)
	return trayApp{tester: tester, desktop: fake, status: status}
}

func TestTrayMenu(t *testing.T) {
	t.Run("running", func(t *testing.T) {
		f := apptest.Sample(t)
		a := pumpTray(t, f)
		got := a.desktop.TrayMenu()
		want := []string{"Show Cave", "Stop server", "Quit"}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("menu = %q, want %q", got, want)
		}
		a.desktop.ClickTrayMenuItem("Stop server")
		eventually(t, a.tester, "the call", func() bool { return len(f.Calls()) == 1 })
		if got := f.Calls()[0]; got != "StopServer()" {
			t.Errorf("calls = %q", f.Calls())
		}
	})
	t.Run("stopped", func(t *testing.T) {
		f := apptest.Empty(t)
		a := pumpTray(t, f)
		if got := a.desktop.TrayMenu(); len(got) != 3 || got[1] != "Start server" {
			t.Fatalf("menu = %q", got)
		}
		a.desktop.ClickTrayMenuItem("Start server")
		eventually(t, a.tester, "the call", func() bool { return len(f.Calls()) == 1 })
		if got := f.Calls()[0]; got != "StartServer()" {
			t.Errorf("calls = %q", f.Calls())
		}
	})
	t.Run("a failure shows in the window", func(t *testing.T) {
		f := apptest.Empty(t)
		f.Fail = map[string]error{"StartServer": listenFailed()}
		a := pumpTray(t, f)
		a.desktop.ClickTrayMenuItem("Start server")
		eventually(t, a.tester, "the sentence", func() bool {
			return a.tester.Exists(gluttertest.ByText(app.Message(listenFailed())))
		})
	})
	t.Run("show brings the window back", func(t *testing.T) {
		for _, show := range []struct {
			name string
			do   func(a trayApp)
		}{
			{"the menu", func(a trayApp) { a.desktop.ClickTrayMenuItem("Show Cave") }},
			{"a click on the icon", func(a trayApp) { a.desktop.ActivateTray() }},
		} {
			f := apptest.Sample(t)
			f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.closeToTray": true})
			a := pumpTray(t, f)
			a.tester.RequestWindowClose(ui.ImplicitViewID)
			a.tester.PumpAndSettle()
			if !a.tester.WindowHidden(ui.ImplicitViewID) {
				t.Fatalf("%s: the window was not hidden", show.name)
			}
			show.do(a)
			eventually(t, a.tester, show.name+" shows the window", func() bool { return !a.tester.WindowHidden(ui.ImplicitViewID) })
		}
	})
}

func TestTrayFollowsTheServer(t *testing.T) {
	f := apptest.Sample(t)
	a := pumpTray(t, f)
	pixmap := func() any { return a.desktop.TrayProperty("IconPixmap") }
	running := pixmap()
	if got := trayTooltip(f.ServerSnapshot); got != "Cave: running on port 2222" {
		t.Fatalf("the tooltip is %q", got)
	}
	f.ServerSnapshot.State = server.Stopped
	f.Emit(app.Changed{Topics: app.TopicServer})
	eventually(t, a.tester, "the menu offers to start", func() bool {
		menu := a.desktop.TrayMenu()
		return len(menu) == 3 && menu[1] == "Start server"
	})
	a.desktop.WaitFor("the icon changes", func() bool { return pixmap() != nil })
	if stopped := pixmap(); stopped == nil || reflect.DeepEqual(stopped, running) {
		t.Error("the icon did not change when the server stopped")
	}
	if got := trayTooltip(f.ServerSnapshot); got != "Cave: stopped" {
		t.Errorf("the tooltip is %q", got)
	}
	f.ServerSnapshot.State = server.Running
	f.Emit(app.Changed{Topics: app.TopicServer})
	eventually(t, a.tester, "the menu offers to stop again", func() bool { return a.desktop.TrayMenu()[1] == "Stop server" })
	if !reflect.DeepEqual(pixmap(), running) {
		t.Error("the icon is not the running one again")
	}
}

func TestTrayIconGolden(t *testing.T) {
	testutil.MatchesGoldenImage(t, "tray-icon-running", trayIcon(true))
	testutil.MatchesGoldenImage(t, "tray-icon-stopped", trayIcon(false))
	if b := trayIcon(true).Bounds(); b.Dx() != trayIconSize || b.Dy() != trayIconSize {
		t.Errorf("the icon is %v", b)
	}
	if icon := trayWindowIcon(true); len(icon.Images) == 0 {
		t.Error("the icon of the tray has no picture")
	}
}

func TestNotifications(t *testing.T) {
	hostile := attempts.Entry{
		ID: "a_1", Outcome: attempts.UnknownName, LoginName: "root�[31mX", Address: "203.0.113.10",
	}
	tests := []struct {
		name     string
		settings map[string]any
		event    app.Event
		summary  string
		body     string // "" when no notification is expected
	}{
		{"accepted, on", map[string]any{"notify.accepted": true}, app.Accepted{Person: "alice", Address: "198.51.100.7"}, "alice connected", "From 198.51.100.7"},
		{"accepted, off", map[string]any{"notify.accepted": false}, app.Accepted{Person: "alice", Address: "198.51.100.7"}, "", ""},
		{"rejected, on", map[string]any{"notify.rejected": true}, app.Rejected{Entry: attempts.Entry{
			Outcome: attempts.WrongKey, PersonName: "alice", LoginName: "alice", Address: "198.51.100.23"}},
			"A connection was refused", "alice from 198.51.100.23: Wrong key"},
		{"rejected, off", map[string]any{"notify.rejected": false}, app.Rejected{Entry: hostile}, "", ""},
		{"hostile text as it is stored", map[string]any{"notify.rejected": true}, app.Rejected{Entry: hostile},
			"A connection was refused", "Unknown from 203.0.113.10: Unknown name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := apptest.Sample(t)
			f.SettingsSnapshot = apptest.SampleSettings(t, tt.settings)
			a := pumpTray(t, f)
			f.Emit(tt.event)
			if tt.body == "" {
				// Nothing is shown: give the event the time it would need.
				for i := 0; i < 20; i++ {
					a.tester.FlushPosted()
					time.Sleep(5 * time.Millisecond)
				}
				if got := a.desktop.Notifications(); len(got) != 0 {
					t.Errorf("a notification was shown: %+v", got)
				}
				return
			}
			eventually(t, a.tester, "the notification", func() bool { return len(a.desktop.Notifications()) == 1 })
			n := a.desktop.Notifications()[0]
			if n.Summary != tt.summary || n.Body != tt.body {
				t.Errorf("the notification is %q / %q, want %q / %q", n.Summary, n.Body, tt.summary, tt.body)
			}
			for _, r := range n.Summary + n.Body {
				if r < ' ' || r == 0x7f {
					t.Errorf("the notification has the control character %q", r)
				}
			}
		})
	}
}

func TestNotificationActionShowsTheWindow(t *testing.T) {
	f := apptest.Sample(t)
	a := pumpTray(t, f)
	a.tester.RequestWindowClose(ui.ImplicitViewID) // asks, because alice is connected: Cancel it
	a.tester.PumpAndSettle()
	tapKey(a.tester, confirmCancelKey)
	f.Emit(app.Accepted{Person: "bob", Address: "192.0.2.1"})
	eventually(t, a.tester, "the notification", func() bool { return len(a.desktop.Notifications()) == 1 })
	a.desktop.InvokeNotificationAction(a.desktop.Notifications()[0].ID, "default")
	a.tester.FlushPosted()
	if a.tester.WindowHidden(ui.ImplicitViewID) {
		t.Error("the window is hidden after the action Show Cave")
	}
}

func TestClose(t *testing.T) {
	closeToTray := map[string]any{"ui.closeToTray": true}
	tests := []struct {
		name       string
		settings   map[string]any
		noTray     bool
		connected  bool
		wantAllow  bool
		wantHidden bool
		wantAsk    bool
	}{
		{"close to the tray, with a tray", closeToTray, false, true, false, true, false},
		{"close to the tray, with a tray and nobody connected", closeToTray, false, false, false, true, false},
		{"close to the tray, without a tray: as before, nobody", closeToTray, true, false, true, false, false},
		{"close to the tray, without a tray: as before, a connection", closeToTray, true, true, false, false, true},
		{"not close to the tray, with a tray, nobody", map[string]any{"ui.closeToTray": false}, false, false, true, false, false},
		{"not close to the tray, with a tray, a connection", map[string]any{"ui.closeToTray": false}, false, true, false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := apptest.Sample(t)
			if !tt.connected {
				f.ConnectionList = nil
			}
			f.SettingsSnapshot = apptest.SampleSettings(t, tt.settings)
			a := pumpTray(t, f)
			if tt.noTray {
				a.status.markUnavailable()
				eventually(t, a.tester, "the tray is taken out", func() bool {
					return !a.tester.Exists(gluttertest.ByType[*desktop.SystemTray]())
				})
			}
			allowed := a.tester.RequestWindowClose(ui.ImplicitViewID)
			a.tester.PumpAndSettle()
			if allowed != tt.wantAllow {
				t.Errorf("the exit was allowed: %v, want %v", allowed, tt.wantAllow)
			}
			if got := a.tester.WindowHidden(ui.ImplicitViewID); got != tt.wantHidden {
				t.Errorf("the window is hidden: %v, want %v", got, tt.wantHidden)
			}
			if got := a.tester.Exists(gluttertest.ByKey(confirmYesKey)); got != tt.wantAsk {
				t.Errorf("the question is shown: %v, want %v", got, tt.wantAsk)
			}
			if got := a.tester.ExitRequests(); got != 0 {
				t.Errorf("the app asked the platform to exit (%d)", got)
			}
		})
	}
}

func TestTrayQuit(t *testing.T) {
	t.Run("nobody connected: quits at once", func(t *testing.T) {
		f := apptest.Sample(t)
		f.ConnectionList = nil
		a := pumpTray(t, f)
		a.desktop.ClickTrayMenuItem("Quit")
		eventually(t, a.tester, "the exit", func() bool { return a.tester.ExitRequests() == 1 })
	})
	t.Run("a connection: the window is shown and the question asked", func(t *testing.T) {
		f := apptest.Sample(t)
		f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.closeToTray": true})
		a := pumpTray(t, f)
		a.tester.RequestWindowClose(ui.ImplicitViewID) // hides the window
		a.tester.PumpAndSettle()
		if !a.tester.WindowHidden(ui.ImplicitViewID) {
			t.Fatal("the window was not hidden")
		}
		a.desktop.ClickTrayMenuItem("Quit")
		eventually(t, a.tester, "the question", func() bool { return a.tester.Exists(gluttertest.ByKey(confirmYesKey)) })
		if a.tester.WindowHidden(ui.ImplicitViewID) {
			t.Error("the window is still hidden under the question")
		}
		if got := a.tester.ExitRequests(); got != 0 {
			t.Fatalf("the exit was asked before the answer (%d)", got)
		}
		tapKey(a.tester, confirmYesKey)
		eventually(t, a.tester, "the exit", func() bool { return a.tester.ExitRequests() == 1 })
	})
}

func TestWithoutATray(t *testing.T) {
	// A desktop with no tray: the app builds, reports the missing tray once, and works.
	tester := testutil.NewTester(t, testutil.Wide)
	status := &trayStatus{}
	f := apptest.Sample(t)
	tester.PumpWidget(newRoot(context.Background(), f, Options{Page: "overview", Clock: f.Clock, tray: status}, syncRunner))
	tester.PumpAndSettle()
	reported := false
	for _, err := range tester.TakeErrors() {
		if appErr, ok := errors.FromError(err); ok && appErr.HasRoute("TrayUnavailable") {
			reported = true
		}
	}
	if !reported {
		t.Fatal("the missing tray was not reported with the Tag TrayUnavailable")
	}
	status.markUnavailable()
	tester.FlushPosted()
	tester.PumpAndSettle()
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Stop server")); err != nil {
		t.Errorf("the app does not work without a tray: %v", err)
	}
	if got := len(tester.TakeErrors()); got != 0 {
		t.Errorf("the app reported %d errors without a tray", got)
	}
}

func TestTheWindowIconIsValidAndHasEverySize(t *testing.T) {
	win := windowIcon()
	if err := win.Validate(); err != nil {
		t.Fatal(err)
	}
	if win.Name != appID {
		t.Errorf("the icon's name is %q, want the application ID %q", win.Name, appID)
	}
	var sizes []int
	for _, img := range win.Images {
		sizes = append(sizes, img.Size)
	}
	if !slices.Equal(sizes, icon.Sizes) {
		t.Errorf("the icon has the sizes %v, want %v", sizes, icon.Sizes)
	}
}
