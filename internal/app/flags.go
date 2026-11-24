package app

import (
	"flag"
	"io"
	"log/slog"
	"runtime"
	"slices"
	"strings"

	"github.com/rm4n0s/cave/internal/core/logging"
	"github.com/rm4n0s/cave/internal/core/sanitize"
	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/errors"
)

// The Tags of a command line that cannot be used.
const (
	TagFlagsInvalid            = "FlagsInvalid"
	TagFlagsUnexpectedArgument = "FlagsUnexpectedArgument"
	TagFlagsPortInvalid        = "FlagsPortInvalid"
	TagFlagsHelp               = "FlagsHelp"
	TagFlagsPageInvalid        = "FlagsPageInvalid"
)

// Pages are the names of the pages of both user interfaces, in the order they are shown; --page
// takes one of them. It is never changed.
var Pages = []string{"overview", "people", "attempts", "connections", "settings"}

// Flags are the flags both programs take (docs/SPEC.md §5.1, §6, §9.1, §15, §16).
type Flags struct {
	// ConfigDir and StateDir are --config-dir and --state-dir; "" when not given.
	ConfigDir, StateDir string
	// Port is --port; 0 when not given.
	Port int
	// NoStart is --no-start.
	NoStart bool
	// AllowRoot is --allow-root.
	AllowRoot bool
	// LogLevel is --log-level; info when not given.
	LogLevel slog.Level
	// Version is --version: print the version and do nothing else.
	Version bool
	// Page is --page: the page the user interface shows first, one of Pages; "" when not given.
	Page string
}

// flagSet is the flag set of a program, writing into f and level.
func flagSet(program string, f *Flags, level *string) *flag.FlagSet {
	fs := flag.NewFlagSet(program, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&f.ConfigDir, "config-dir", "", "the `folder` of the settings, the people and the host key")
	fs.StringVar(&f.StateDir, "state-dir", "", "the `folder` of the attempts list, the log and the lock")
	fs.IntVar(&f.Port, "port", 0, "listen on this `port` for this run, whatever the settings say")
	fs.BoolVar(&f.NoStart, "no-start", false, "do not start the server at launch")
	fs.BoolVar(&f.AllowRoot, "allow-root", false, "run although the user is root: every person gets a root shell")
	fs.StringVar(level, "log-level", "info", "what the log has: debug, info, warn or error")
	fs.BoolVar(&f.Version, "version", false, "print the version and exit")
	fs.StringVar(&f.Page, "page", "", "the `page` to show first: "+strings.Join(Pages, ", "))
	return fs
}

// Usage returns the text of a program's help: every flag with what it does, and the two folders
// the program uses when neither a flag nor a CAVE_ variable names them (docs/SPEC.md §5.1).
func Usage(program string) string {
	return usageFor(program, runtime.GOOS)
}

// usageFor is Usage for an operating system, so that a test can check the text of both.
func usageFor(program, goos string) string {
	var f Flags
	var level string
	fs := flagSet(program, &f, &level)
	var text strings.Builder
	text.WriteString("Usage: " + program + " [flags]\n\n")
	fs.SetOutput(&text)
	fs.PrintDefaults()
	config, state := "$XDG_CONFIG_HOME/cave, else ~/.config/cave", "$XDG_STATE_HOME/cave, else ~/.local/state/cave"
	if goos == "windows" {
		config, state = `%AppData%\Cave`, `%LocalAppData%\Cave`
	}
	text.WriteString("\nThe folders, when neither a flag nor CAVE_CONFIG_DIR and CAVE_STATE_DIR name them:\n")
	text.WriteString("  configuration  " + config + "\n")
	text.WriteString("  state          " + state + "\n")
	return text.String()
}

// ParseFlags reads a program's command line, without the program's name. A command line that
// cannot be used is an error whose Tag says why; FlagsHelp is the one of "-h" and "--help", for
// which the program prints Usage and ends well.
func ParseFlags(program string, args []string) (Flags, error) {
	var f Flags
	var level string
	fs := flagSet(program, &f, &level)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return Flags{}, errors.NewErr(TagFlagsHelp, err)
		}
		return Flags{}, errors.NewErr(TagFlagsInvalid, err)
	}
	if fs.NArg() > 0 {
		return Flags{}, errors.New(TagFlagsUnexpectedArgument, "the program takes flags only", "argument", fs.Arg(0))
	}
	portGiven := false
	fs.Visit(func(given *flag.Flag) {
		if given.Name == "port" {
			portGiven = true
		}
	})
	if portGiven && (f.Port < 1 || f.Port > 65535) {
		return Flags{}, errors.New(TagFlagsPortInvalid, "a port is a number from 1 to 65535", "port", f.Port)
	}
	if f.Page != "" && !slices.Contains(Pages, f.Page) {
		return Flags{}, errors.New(TagFlagsPageInvalid, "there is no such page", "page", sanitize.Display(f.Page, 32))
	}
	parsed, err := logging.ParseLevel(level)
	if err != nil {
		return Flags{}, err
	}
	f.LogLevel = parsed
	return f, nil
}

// Options returns the options to open the application with these flags: the folders are the
// flags', else those of the CAVE_ variables, else the platform's (storage.Resolve).
func (f Flags) Options(program string, env storage.Env) (Options, error) {
	dirs, err := storage.Resolve(storage.Options{ConfigDir: f.ConfigDir, StateDir: f.StateDir}, env)
	if err != nil {
		return Options{}, err
	}
	return Options{
		Program: program, Dirs: dirs, LogLevel: f.LogLevel, Port: f.Port, NoStart: f.NoStart, AllowRoot: f.AllowRoot,
	}, nil
}

// IsUsageError says whether err is a command line that cannot be used (an unknown flag, a value
// out of range, an argument where only flags are taken), for which the programs exit with the
// status 2 instead of 1. Help is not one.
func IsUsageError(err error) bool {
	appErr, ok := errors.FromError(err)
	if !ok {
		return false
	}
	switch appErr.Tag {
	case TagFlagsInvalid, TagFlagsUnexpectedArgument, TagFlagsPortInvalid, TagFlagsPageInvalid, logging.TagLevelInvalid:
		return true
	}
	return false
}
