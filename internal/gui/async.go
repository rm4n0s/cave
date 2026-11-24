package gui

import "context"

// runner runs a command of the application away from the UI thread and gives its error back on it.
type runner func(work func(ctx context.Context) error, done func(err error))

// newRunner makes the real runner: work runs on a goroutine of its own, with ctx (which ends with
// the window), and done is called through post, on the UI thread. done may be nil.
func newRunner(ctx context.Context, post func(func())) runner {
	return func(work func(ctx context.Context) error, done func(err error)) {
		go func() {
			err := work(ctx)
			if done != nil {
				post(func() { done(err) })
			}
		}()
	}
}
