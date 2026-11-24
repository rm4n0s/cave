package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/errors"
)

// TagRunFailed is the Tag of the error of a terminal program that failed.
const TagRunFailed = "TuiRunFailed"

// tickEvery is how often the relative times ("3 min ago") are drawn again.
const tickEvery = 30 * time.Second

// Run shows the TUI until the user quits or ctx is cancelled (which is not an error). The
// application is not opened or closed here. While it runs, nothing is written to the terminal
// but the TUI: the logs go to the file.
func Run(ctx context.Context, a App, opts Options) error {
	clk := opts.Clock
	if clk == nil {
		clk = clock.System()
	}
	programOpts := []tea.ProgramOption{tea.WithContext(ctx), tea.WithoutSignalHandler()}
	if opts.In != nil {
		programOpts = append(programOpts, tea.WithInput(opts.In))
	}
	if opts.Out != nil {
		programOpts = append(programOpts, tea.WithOutput(opts.Out))
	}
	if opts.Width > 0 && opts.Height > 0 {
		programOpts = append(programOpts, tea.WithWindowSize(opts.Width, opts.Height))
	}
	if opts.Env != nil {
		programOpts = append(programOpts, tea.WithEnvironment(opts.Env))
	}
	program := tea.NewProgram(NewModel(ctx, a, clk, opts), programOpts...)

	// The application tells its changes on its own goroutines, which must not wait for the
	// program: the message is handed over on a goroutine of its own.
	cancel := a.Subscribe(func(event app.Event) {
		if _, ok := event.(app.Changed); ok {
			go program.Send(changedMsg{})
		}
	})
	defer cancel()

	ticker := clk.NewTicker(tickEvery)
	stopTicks := make(chan struct{})
	ticksDone := make(chan struct{})
	go func() {
		defer close(ticksDone)
		for {
			select {
			case <-ticker.C():
				program.Send(tickMsg{})
			case <-stopTicks:
				return
			}
		}
	}()
	defer func() {
		ticker.Stop()
		close(stopTicks)
		<-ticksDone
	}()

	if _, err := program.Run(); err != nil {
		if ctx.Err() != nil {
			return nil // the program was stopped on purpose
		}
		return errors.NewErr(TagRunFailed, err)
	}
	return nil
}
