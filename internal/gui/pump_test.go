package gui

import (
	"context"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
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
	tester := testutil.NewTester(t, size)
	tester.PumpWidget(newRoot(context.Background(), a, Options{Page: page, Version: "0.0.0-test"}, syncRunner))
	tester.PumpAndSettle()
	return tester
}
