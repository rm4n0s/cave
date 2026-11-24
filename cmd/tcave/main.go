// Command tcave is Cave's terminal UI: an SSH server that an ordinary user starts,
// watches and stops (docs/SPEC.md §12).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/errors"
)

const (
	// tagTcaveUsage is the error of a command line that tcave does not understand.
	tagTcaveUsage = "TcaveUsage"
	// tagTcaveVersionWriteFailed is the error of a version that could not be printed.
	tagTcaveVersionWriteFailed = "TcaveVersionWriteFailed"
	// tagTcaveNotBuiltYet is the error of starting the TUI before step 5.1 builds it.
	tagTcaveNotBuiltYet = "TcaveNotBuiltYet"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "tcave:", err)
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == tagTcaveUsage {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

// run is main with its arguments and streams given, so that tests can call it.
func run(args []string, stdout, _ io.Writer) error {
	fs := flag.NewFlagSet("tcave", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	version := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return errors.NewErr(tagTcaveUsage, err)
	}
	if fs.NArg() > 0 {
		return errors.New(tagTcaveUsage, "unexpected argument", "argument", fs.Arg(0))
	}
	if *version {
		if _, err := fmt.Fprintln(stdout, "tcave", app.Version()); err != nil {
			return errors.NewErr(tagTcaveVersionWriteFailed, err)
		}
		return nil
	}
	// TODO(step 5.1): open the application and run the TUI.
	return errors.New(tagTcaveNotBuiltYet, "the TUI is not built yet; only --version works")
}
