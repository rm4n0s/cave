// Command tcave is Cave's terminal UI: an SSH server that an ordinary user starts,
// watches and stops (docs/SPEC.md §12).
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
	"github.com/rm4n0s/cave/internal/tui"
	"github.com/rm4n0s/errors"
)

const (
	// program is the name of this program, as the flags, the lock file and the messages say it.
	program = "tcave"
	// tagTcaveWriteFailed is the error of a text that could not be printed.
	tagTcaveWriteFailed = "TcaveWriteFailed"
	// closeTimeout is how long the application is given to close after the TUI ended.
	closeTimeout = 10 * time.Second
)

func main() {
	os.Exit(realMain())
}

// realMain is main without os.Exit, so that its deferred calls run. It returns the exit status:
// 0 for success, 2 for a command line that cannot be used, 1 for anything else.
func realMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals()...)
	defer stop()
	if err := run(ctx, os.Args[1:], os.LookupEnv, os.Stdin, os.Stdout, os.Stderr); err != nil {
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
// it. stdin and stdout are the terminal; stderr gets what could not be told by an error.
func run(ctx context.Context, args []string, env storage.Env, stdin io.Reader, stdout, stderr io.Writer) error {
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
		return err
	}
	runErr := tui.Run(ctx, a, tui.Options{
		In: stdin, Out: stdout, Page: flags.Page,
		ConfigDir: opts.Dirs.Config, StateDir: opts.Dirs.State, Version: app.Version(),
	})
	// The application is closed after the TUI, also when the TUI failed. ctx may be cancelled
	// by now (that is how a signal ends the program), so closing has a context of its own.
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
		return errors.NewErr(tagTcaveWriteFailed, err)
	}
	return nil
}
