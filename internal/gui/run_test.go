package gui

import (
	"context"
	"testing"

	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/cave/internal/gui/testutil"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
)

// noDisplay takes every way to a screen out of the environment, so that a test of a failing run
// never opens a window on the user's desktop.
func noDisplay(t *testing.T) {
	t.Helper()
	t.Setenv("GLUTTER_BACKEND", "no-such-backend")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
}

func TestRunFailsWithoutADisplay(t *testing.T) {
	noDisplay(t)
	err := Run(context.Background(), apptest.Sample(t), Options{})
	errtest.Route(t, err, "Run.GuiRunFailed")
}

func TestRunStartErrorFailsWithoutADisplay(t *testing.T) {
	noDisplay(t)
	err := RunStartError(context.Background(), "Cave is already running.")
	errtest.Route(t, err, "RunStartError.GuiStartErrorFailed")
}

func TestStartErrorWindowShowsTheMessageAndQuits(t *testing.T) {
	// The message is drawn as it is, also when it looks like markup or a format string.
	const message = "Cave is already running on these folders. <b>%s</b> {name}"
	tester := testutil.NewTester(t, ui.Size{Width: 480, Height: 240})
	tester.PumpWidget(newStartErrorApp(message))
	tester.PumpAndSettle()
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(message)); err != nil {
		t.Fatal(err)
	}
	tester.MatchesGolden("start-error")
	tester.Tap(gluttertest.ByText("Quit"))
	tester.PumpAndSettle()
	if got := tester.ExitRequests(); got != 1 {
		t.Errorf("ExitRequests() = %d after Quit, want 1", got)
	}
}
