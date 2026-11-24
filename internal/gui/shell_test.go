package gui

import (
	"context"
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/gui/testutil"
)

func TestGoldenShell(t *testing.T) {
	t.Run("light wide", func(t *testing.T) {
		pump(t, apptest.Sample(t), testutil.Wide, "").MatchesGolden("shell-light-wide")
	})
	t.Run("dark narrow", func(t *testing.T) {
		f := apptest.Sample(t)
		f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.theme": "dark"})
		pump(t, f, testutil.Narrow, "").MatchesGolden("shell-dark-narrow")
	})
}

func TestNavigation(t *testing.T) {
	tests := []struct {
		name         string
		size         func() (w, h float64)
		wantRail     bool
		wantBar      bool
		sizeIsNarrow bool
	}{
		{name: "wide", wantRail: true},
		{name: "narrow", wantBar: true, sizeIsNarrow: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := testutil.Wide
			if tt.sizeIsNarrow {
				size = testutil.Narrow
			}
			tester := pump(t, apptest.Sample(t), size, "settings")
			if err := presence(tester, gluttertest.ByType[*m.NavigationRail](), tt.wantRail); err != nil {
				t.Errorf("rail: %v", err)
			}
			if err := presence(tester, gluttertest.ByType[*m.NavigationBar](), tt.wantBar); err != nil {
				t.Errorf("bar: %v", err)
			}
			// The label of the page that is shown is on its destination and on its headline; the
			// label of another page is on its destination only. Settings is shown first, so every
			// page is tapped from another one.
			for i, d := range destinations {
				if i != len(destinations)-1 {
					if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(d.label)); err != nil {
						t.Errorf("destination %d (%s) before the tap: %v", i, d.label, err)
					}
				}
				tester.Tap(gluttertest.ByText(d.label))
				tester.PumpAndSettle()
				if err := gluttertest.FindsNWidgets(tester, gluttertest.ByText(d.label), 2); err != nil {
					t.Errorf("page %d (%s): %v", i, d.label, err)
				}
			}
		})
	}
}

// presence checks that the finder finds one widget, or nothing.
func presence(tester *gluttertest.Tester, f gluttertest.Finder, want bool) error {
	if want {
		return gluttertest.FindsOneWidget(tester, f)
	}
	return gluttertest.FindsNothing(tester, f)
}

func TestTheFirstPage(t *testing.T) {
	tester := pump(t, apptest.Sample(t), testutil.Wide, "attempts")
	if err := gluttertest.FindsNWidgets(tester, gluttertest.ByText("Attempts"), 2); err != nil {
		t.Errorf("the Attempts page is not first: %v", err)
	}
	if err := gluttertest.FindsNWidgets(tester, gluttertest.ByText("Overview"), 1); err != nil {
		t.Errorf("the Overview headline is on screen: %v", err)
	}
}

func TestTheThemeFollowsTheSetting(t *testing.T) {
	f := apptest.Sample(t)
	f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.theme": "light"})
	tester := pump(t, f, testutil.Wide, "")
	brightness := func() ui.Brightness {
		return m.ThemeOf(tester.Element(gluttertest.ByType[*headline]())).Brightness()
	}
	if got := brightness(); got != ui.BrightnessLight {
		t.Fatalf("brightness = %v, want light", got)
	}
	f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.theme": "dark"})
	f.Emit(app.Changed{Topics: app.TopicSettings})
	tester.FlushPosted()
	tester.PumpAndSettle()
	if got := brightness(); got != ui.BrightnessDark {
		t.Errorf("brightness = %v, want dark", got)
	}
}

func TestTheBadgeFollowsTheConnections(t *testing.T) {
	f := apptest.Empty(t)
	tester := pump(t, f, testutil.Wide, "")
	badge := gluttertest.ByKey(foundation.NewKey("connections-badge"))
	if err := gluttertest.FindsNothing(tester, badge); err != nil {
		t.Fatalf("a badge with nobody connected: %v", err)
	}
	f.ConnectionList = []server.Connection{{ID: "c1"}, {ID: "c2"}, {ID: "c3"}}
	f.Emit(app.Changed{Topics: app.TopicConnections})
	tester.FlushPosted()
	tester.PumpAndSettle()
	if err := gluttertest.FindsOneWidget(tester, badge); err != nil {
		t.Fatalf("no badge with three connections: %v", err)
	}
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("3")); err != nil {
		t.Errorf("the badge does not say 3: %v", err)
	}
}

func TestDisposeCancelsTheSubscription(t *testing.T) {
	f := apptest.Sample(t)
	tester := pump(t, f, testutil.Wide, "")
	if f.Subscribers() != 1 {
		t.Fatalf("Subscribers() = %d while the root is shown, want 1", f.Subscribers())
	}
	tester.PumpWidget(m.NewSizedBox(nil))
	tester.PumpAndSettle()
	if f.Subscribers() != 0 {
		t.Errorf("Subscribers() = %d after the root was removed, want 0", f.Subscribers())
	}
	// An event that comes after that does nothing.
	f.Emit(app.Changed{Topics: app.TopicServer})
	tester.FlushPosted()
}

func TestTheRealRunnerGivesTheErrorBackOnTheUIThread(t *testing.T) {
	posted := make(chan func(), 1)
	run := newRunner(context.Background(), func(fn func()) { posted <- fn })
	var got error
	called := false
	want := context.DeadlineExceeded
	run(func(context.Context) error { return want }, func(err error) { called, got = true, err })
	fn := <-posted
	if called {
		t.Fatal("done ran before it was posted")
	}
	fn()
	if !called || got != want {
		t.Errorf("done called %v with %v, want %v", called, got, want)
	}
}
