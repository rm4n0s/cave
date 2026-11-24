package gui

import (
	"context"
	"github.com/rm4n0s/cave/internal/app/apptest"
	"golang.org/x/crypto/ssh"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/cave/internal/gui/testutil"
)

// syncRunner is the runner of the tests: the work runs at once, on the caller's goroutine, and
// then done, so a test needs no waiting.
func syncRunner(work func(ctx context.Context) error, done func(err error)) {
	err := work(context.Background())
	if done != nil {
		done(err)
	}
}

// pump builds the root over the application with the synchronous runner, shows the page ("" is
// the first) and pumps until the tree settles.
func pump(t *testing.T, a App, size ui.Size, page string) *gluttertest.Tester {
	t.Helper()
	return pumpWith(t, a, size, page, syncRunner)
}

// pumpWith is pump with a runner of the test's own, for a test that holds a command back.
func pumpWith(t *testing.T, a App, size ui.Size, page string, run runner) *gluttertest.Tester {
	t.Helper()
	opts := Options{Page: page, Version: "0.0.0-test"}
	if f, ok := a.(*apptest.Fake); ok {
		opts.Clock = f.Clock
	}
	return pumpOptions(t, a, size, opts, run)
}

// pumpOptions is pumpWith with the options of the test's own, the clock among them: a test of the
// real application gives the application's fake clock.
func pumpOptions(t *testing.T, a App, size ui.Size, opts Options, run runner) *gluttertest.Tester {
	t.Helper()
	tester := testutil.NewTester(t, size)
	tester.PumpWidget(newRoot(context.Background(), a, opts, run))
	tester.PumpAndSettle()
	return tester
}

// currentPage is the index of the page that is shown, read from the navigation that is on screen.
func currentPage(t *testing.T, tester *gluttertest.Tester) int {
	t.Helper()
	if tester.Exists(gluttertest.ByType[*m.NavigationRail]()) {
		return gluttertest.WidgetAs[*m.NavigationRail](tester, gluttertest.ByType[*m.NavigationRail]()).SelectedIndex().Value()
	}
	return gluttertest.WidgetAs[*m.NavigationBar](tester, gluttertest.ByType[*m.NavigationBar]()).SelectedIndex()
}

// eventually pumps the tester until cond is true, for up to five seconds of real time. Work that
// a page hands to another goroutine (the answer of a dialog) and what the application tells on
// its own goroutines come back to the UI thread through the tester's posted functions.
func eventually(t *testing.T, tester *gluttertest.Tester, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		tester.FlushPosted()
		tester.PumpAndSettle()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after 5 seconds: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// tapKey taps the widget with the key and lets the tree settle.
func tapKey(tester *gluttertest.Tester, key foundation.Key) {
	tester.Tap(gluttertest.ByKey(key))
	tester.PumpAndSettle()
}

// pumpGoroutines is pump with the real runner: a command runs on a goroutine and is given back
// through the tester's posted functions. A test that waits for something away from the UI thread
// (the file dialog) needs it; it then uses eventually to wait.
func pumpGoroutines(t *testing.T, a App, size ui.Size, page string) *gluttertest.Tester {
	t.Helper()
	tester := testutil.NewTester(t, size)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	tester.PumpWidget(newRoot(ctx, a, Options{Page: page, Version: "0.0.0-test"}, newRunner(ctx, tester.Binding.Post)))
	tester.PumpAndSettle()
	return tester
}

// closedClient waits until the connection of an ssh client has ended.
func closedClient(t *testing.T, client *ssh.Client) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		_ = client.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the connection is still open")
	}
}
