// Command cave is Cave's desktop application: an SSH server that an ordinary user starts,
// watches and stops (docs/SPEC.md §11).
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/cave/internal/gui"
	"github.com/rm4n0s/errors"
)

const (
	// program is the name of this program, as the flags, the lock file and the messages say it.
	program = "cave"
	// tagCaveWriteFailed is the error of a text that could not be printed.
	tagCaveWriteFailed = "CaveWriteFailed"
	// closeTimeout is how long the application is given to close after the window closed.
	closeTimeout = 10 * time.Second
)

// windows are the two windows of this program. They are a parameter of run, so that a test can
// run the program without opening a window.
type windows struct {
	// show opens the application's window and returns when it is closed.
	show func(ctx context.Context, a gui.App, opts gui.Options) error
	// showStartError opens the small window that tells why Cave could not start.
	showStartError func(ctx context.Context, message string) error
}

func main() {
	os.Exit(realMain())
}

// realMain is main without os.Exit, so that its deferred calls run. It returns the exit status:
// 0 for success, 2 for a command line that cannot be used, 1 for anything else.
func realMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals()...)
	defer stop()
	w := windows{show: gui.Run, showStartError: gui.RunStartError}
	if err := run(ctx, os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr, w); err != nil {
		if _, werr := fmt.Fprintln(os.Stderr, program+":", app.Message(err)); werr != nil {
			return 1 // there is nowhere to tell: standard error is closed
		}
		if app.IsUsageError(err) {
			return 2
		}
		return 1
	}
	return 0
}

// run is the program with its arguments, environment and streams given, so that tests can call
// it. stdout gets what the flags --version and --help print; stderr gets what could not be told
// by an error.
func run(ctx context.Context, args []string, env storage.Env, stdout, stderr io.Writer, w windows) error {
	flags, err := app.ParseFlags(program, args)
	if err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == app.TagFlagsHelp {
			return printTo(stdout, app.Usage(program))
		}
		return err
	}
	if flags.Version {
		return printTo(stdout, program+" "+app.Version()+"\n")
	}
	opts, err := flags.Options(program, env)
	if err != nil {
		return err
	}
	a, err := app.Open(ctx, opts)
	if err != nil {
		// A desktop app may have no terminal: the reason is also shown in a window of its own.
		// What the window could not do is told on standard error, and the first error is returned.
		if _, werr := fmt.Fprintln(stderr, program+":", app.Message(err)); werr != nil {
			return err // nothing more can be told
		}
		if serr := w.showStartError(ctx, app.Message(err)); serr != nil {
			if _, werr := fmt.Fprintln(stderr, program+":", app.Message(serr)); werr != nil {
				return err
			}
		}
		return err
	}
	runErr := w.show(ctx, a, gui.Options{
		Page:      flags.Page,
		ConfigDir: opts.Dirs.Config, StateDir: opts.Dirs.State, Version: app.Version(),
	})
	// The application is closed after the window, also when the window failed. ctx may be
	// cancelled by now (that is how a signal ends the program), so closing has a context of its own.
	closeCtx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	closeErr := a.Close(closeCtx)
	if runErr != nil {
		if closeErr != nil {
			// Two failures: the first is returned, and the second is told here.
			if _, werr := fmt.Fprintln(stderr, program+":", app.Message(closeErr)); werr != nil {
				return runErr // nothing more can be told
			}
		}
		return runErr
	}
	return closeErr
}

// printTo writes text to a stream.
func printTo(w io.Writer, text string) error {
	if _, err := io.WriteString(w, text); err != nil {
		return errors.NewErr(tagCaveWriteFailed, err)
	}
	return nil
}
