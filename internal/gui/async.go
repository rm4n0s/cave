package gui

import (
	"context"

	"github.com/rm4n0s/errors"
)

// runner runs a command of the application away from the UI thread and gives its error back on it.
type runner func(work func(ctx context.Context) error, done func(err error))

// newRunner makes the real runner: work runs on a goroutine of its own, with ctx (which ends with
// the window), and done is called through post, on the UI thread. done may be nil. The work is
// handed over through errors.Async on the UI thread, so the route of an error it returns starts
// at the code that ran the command, as if work had been called there.
func newRunner(ctx context.Context, post func(func())) runner {
	return func(work func(ctx context.Context) error, done func(err error)) {
		run := errors.Async(func() error { return work(ctx) })
		go func() {
			err := run()
			if done != nil {
				post(func() { done(err) })
			}
		}()
	}
}
