package gui

import (
	"context"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/gui/testutil"
)

// listenFailed is the error of a port that is taken, as the server gives it.
func listenFailed() error {
	return errors.NewErr("ListenFailed", syscall.EADDRINUSE).SetMetadata("address", "127.0.0.1:2222")
}

func TestGoldenOverview(t *testing.T) {
	tests := []struct {
		name   string
		size   ui.Size
		fake   func(t *testing.T) *apptest.Fake
		golden string
	}{
		{"sample light wide", testutil.Wide, func(t *testing.T) *apptest.Fake { return apptest.Sample(t) }, "overview-light-wide"},
		{"sample dark narrow", testutil.Narrow, func(t *testing.T) *apptest.Fake {
			f := apptest.Sample(t)
			f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.theme": "dark"})
			return f
		}, "overview-dark-narrow"},
		{"nobody light wide", testutil.Wide, func(t *testing.T) *apptest.Fake { return apptest.Empty(t) }, "overview-empty-light-wide"},
		{"error light wide", testutil.Wide, func(t *testing.T) *apptest.Fake {
			f := apptest.Empty(t)
			f.ServerSnapshot.Err = listenFailed()
			return f
		}, "overview-error-light-wide"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.fake(t)
			if tt.size == testutil.Wide {
				f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.theme": "light"})
			}
			pump(t, f, tt.size, "overview").MatchesGolden(tt.golden)
		})
	}
}

func TestTheStateInWords(t *testing.T) {
	tests := []struct {
		name  string
		state server.State
		err   error
		want  string
	}{
		{"running", server.Running, nil, "Running on port 2222"},
		{"stopped", server.Stopped, nil, "Stopped"},
		{"starting", server.Starting, nil, "Starting…"},
		{"stopping", server.Stopping, nil, "Stopping…"},
		{"not running", server.Stopped, listenFailed(), "Not running"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := apptest.Sample(t)
			f.ServerSnapshot.State, f.ServerSnapshot.Err = tt.state, tt.err
			tester := pump(t, f, testutil.Wide, "overview")
			if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(tt.want)); err != nil {
				t.Error(err)
			}
			wantSentence := tt.err != nil
			got := tester.Exists(gluttertest.ByTextContaining("Choose another port in Settings."))
			if got != wantSentence {
				t.Errorf("the sentence of the error is shown: %v, want %v", got, wantSentence)
			}
		})
	}
}

func TestStartAndStop(t *testing.T) {
	t.Run("stop", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pump(t, f, testutil.Wide, "overview")
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Stop server")); err != nil {
			t.Fatal(err)
		}
		tester.Tap(gluttertest.ByKey(serverToggleKey))
		tester.PumpAndSettle()
		if got := f.Calls(); len(got) != 1 || got[0] != "StopServer()" {
			t.Errorf("calls = %q, want [StopServer()]", got)
		}
	})
	t.Run("starting also shows stop", func(t *testing.T) {
		f := apptest.Sample(t)
		f.ServerSnapshot.State = server.Starting
		tester := pump(t, f, testutil.Wide, "overview")
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Stop server")); err != nil {
			t.Error(err)
		}
	})
	t.Run("start", func(t *testing.T) {
		f := apptest.Empty(t)
		tester := pump(t, f, testutil.Wide, "overview")
		tester.Tap(gluttertest.ByKey(serverToggleKey))
		tester.PumpAndSettle()
		if got := f.Calls(); len(got) != 1 || got[0] != "StartServer()" {
			t.Errorf("calls = %q, want [StartServer()]", got)
		}
	})
	t.Run("a failure shows its sentence", func(t *testing.T) {
		f := apptest.Empty(t)
		f.Fail = map[string]error{"StartServer": listenFailed()}
		tester := pump(t, f, testutil.Wide, "overview")
		tester.Tap(gluttertest.ByKey(serverToggleKey))
		tester.Pump(100 * time.Millisecond)
		tester.PumpAndSettle()
		want := app.Message(listenFailed())
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(want)); err != nil {
			t.Error(err)
		}
	})
}

func TestTheButtonIsOffWhileTheCommandRuns(t *testing.T) {
	var finish func()
	held := func(work func(ctx context.Context) error, done func(err error)) {
		err := work(context.Background())
		finish = func() { done(err) }
	}
	f := apptest.Empty(t)
	tester := pumpWith(t, f, testutil.Wide, "overview", held)
	tester.Tap(gluttertest.ByKey(serverToggleKey))
	tester.PumpAndSettle()
	// A second tap while the command has not ended does nothing.
	tester.Tap(gluttertest.ByKey(serverToggleKey))
	tester.PumpAndSettle()
	if got := f.Calls(); len(got) != 1 {
		t.Fatalf("calls = %q, want one", got)
	}
	finish()
	tester.PumpAndSettle()
	tester.Tap(gluttertest.ByKey(serverToggleKey))
	tester.PumpAndSettle()
	if got := f.Calls(); len(got) != 2 {
		t.Errorf("calls = %q after the command ended, want two", got)
	}
}

func TestCopyButtons(t *testing.T) {
	f := apptest.Sample(t)
	tester := pump(t, f, testutil.Wide, "overview")
	copied := func() string {
		tester.Pump(100 * time.Millisecond)
		text, _ := tester.ClipboardText()
		return text
	}
	tester.Tap(gluttertest.ByKey(foundation.NewKey("copy-fingerprint")))
	if got := copied(); got != f.ServerSnapshot.HostKeyFingerprint {
		t.Errorf("the clipboard has %q, want the fingerprint %q", got, f.ServerSnapshot.HostKeyFingerprint)
	}
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Copied")); err != nil {
		t.Errorf("the snack bar: %v", err)
	}
	tester.PumpAndSettle(gluttertest.PumpAndSettleOpts{Timeout: 30 * time.Second})
	tester.Tap(gluttertest.ByKey(foundation.NewKey("copy-ssh-0")))
	// The first enabled person is alice.
	want := f.ServerSnapshot.Command("alice", f.ServerSnapshot.Addresses[0])
	if got := copied(); got != want {
		t.Errorf("the clipboard has %q, want %q", got, want)
	}
	if !strings.HasPrefix(want, "ssh ") {
		t.Errorf("the line is %q", want)
	}
}

func TestTheNameInTheSshLines(t *testing.T) {
	f := apptest.Empty(t)
	tester := pump(t, f, testutil.Wide, "overview")
	want := f.ServerSnapshot.Command("name", f.ServerSnapshot.Addresses[0])
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(want)); err != nil {
		t.Errorf("with nobody there, the line says name: %v", err)
	}
}

func TestAddAPersonGoesToPeople(t *testing.T) {
	tester := pump(t, apptest.Empty(t), testutil.Wide, "overview")
	tester.Tap(gluttertest.ByKey(addPersonKey))
	tester.PumpAndSettle()
	if got := currentPage(t, tester); got != pagePeople {
		t.Errorf("the page is %d, want the People page", got)
	}
	// With people there, the card is not shown.
	tester = pump(t, apptest.Sample(t), testutil.Wide, "overview")
	if err := gluttertest.FindsNothing(tester, gluttertest.ByKey(addPersonKey)); err != nil {
		t.Error(err)
	}
}

func TestTheCountsFollowTheApplication(t *testing.T) {
	f := apptest.Sample(t)
	tester := pump(t, f, testutil.Wide, "overview")
	for key, want := range map[string]string{"count-people": "3", "count-connections": "1", "count-attempts": "9"} {
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByKey(foundation.NewKey(key))); err != nil {
			t.Fatal(err)
		}
		w := gluttertest.WidgetAs[*m.Text](tester, gluttertest.ByKey(foundation.NewKey(key)))
		if w.PlainText() != want {
			t.Errorf("%s says %q, want %q", key, w.PlainText(), want)
		}
	}
}

func TestOverviewWithTheRealApplication(t *testing.T) {
	r := apptest.Open(t, nil)
	tester := pump(t, r.App, testutil.Wide, "overview")
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Stop server")); err != nil {
		t.Fatalf("the server is not running at the start: %v", err)
	}
	tester.Tap(gluttertest.ByKey(serverToggleKey))
	deadline := time.Now().Add(10 * time.Second)
	for r.App.Server().State != server.Stopped {
		if time.Now().After(deadline) {
			t.Fatalf("the server did not stop: %v", r.App.Server().State)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The application tells its change on its own goroutine; the page shows it after a pump.
	deadline = time.Now().Add(10 * time.Second)
	for !tester.Exists(gluttertest.ByText("Stopped")) {
		if time.Now().After(deadline) {
			t.Fatal("the page does not say Stopped")
		}
		// The application holds its events for CoalesceTime of its own, fake, clock.
		r.Clock.Advance(app.CoalesceTime)
		tester.FlushPosted()
		tester.PumpAndSettle()
		time.Sleep(10 * time.Millisecond)
	}
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Start server")); err != nil {
		t.Error(err)
	}
}
