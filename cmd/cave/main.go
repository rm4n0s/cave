// Command cave is Cave's desktop application: an SSH server that an ordinary user starts,
// watches and stops (docs/SPEC.md §11).
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
	// tagCaveUsage is the error of a command line that cave does not understand.
	tagCaveUsage = "CaveUsage"
	// tagCaveUnexpectedArgument is the error of an argument where cave takes only flags.
	tagCaveUnexpectedArgument = "CaveUnexpectedArgument"
	// tagCaveVersionWriteFailed is the error of a version that could not be printed.
	tagCaveVersionWriteFailed = "CaveVersionWriteFailed"
	// tagCaveNotBuiltYet is the error of starting the desktop app before step 6.1 builds it.
	tagCaveNotBuiltYet = "CaveNotBuiltYet"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "cave:", err)
		if appErr, ok := errors.FromError(err); ok && (appErr.Tag == tagCaveUsage || appErr.Tag == tagCaveUnexpectedArgument) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

// run is main with its arguments and streams given, so that tests can call it.
func run(args []string, stdout, _ io.Writer) error {
	fs := flag.NewFlagSet("cave", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	version := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return errors.NewErr(tagCaveUsage, err)
	}
	if fs.NArg() > 0 {
		return errors.New(tagCaveUnexpectedArgument, "unexpected argument", "argument", fs.Arg(0))
	}
	if *version {
		if _, err := fmt.Fprintln(stdout, "cave", app.Version()); err != nil {
			return errors.NewErr(tagCaveVersionWriteFailed, err)
		}
		return nil
	}
	// TODO(step 6.1): open the application and run the desktop UI.
	return errors.New(tagCaveNotBuiltYet, "the desktop app is not built yet; only --version works")
}
