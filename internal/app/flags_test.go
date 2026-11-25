package app

import (
	"flag"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/errors"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want Flags
	}{
		{"nothing", nil, Flags{LogLevel: slog.LevelInfo}},
		{"the version", []string{"--version"}, Flags{Version: true, LogLevel: slog.LevelInfo}},
		{"one dash", []string{"-version"}, Flags{Version: true, LogLevel: slog.LevelInfo}},
		{
			"everything",
			[]string{"--config-dir", "/a/config", "--state-dir=/a/state", "--port", "2022", "--no-start", "--allow-root", "--log-level", "debug"},
			Flags{ConfigDir: "/a/config", StateDir: "/a/state", Port: 2022, NoStart: true, AllowRoot: true, LogLevel: slog.LevelDebug},
		},
		{"the lowest port", []string{"--port", "1"}, Flags{Port: 1, LogLevel: slog.LevelInfo}},
		{"the highest port", []string{"--port=65535"}, Flags{Port: 65535, LogLevel: slog.LevelInfo}},
		{"the quietest log", []string{"--log-level", "error"}, Flags{LogLevel: slog.LevelError}},
		{"a page", []string{"--page", "attempts"}, Flags{Page: "attempts", LogLevel: slog.LevelInfo}},
		{"every page", []string{"--page=settings"}, Flags{Page: "settings", LogLevel: slog.LevelInfo}},
		{"a flag that is turned off", []string{"--no-start=false"}, Flags{LogLevel: slog.LevelInfo}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFlags("tcave", tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseFlags(%v) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

func TestParseFlagsErrors(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantRoute string
	}{
		{"a flag there is not", []string{"--nope"}, "ParseFlags.FlagsInvalid"},
		{"a port that is not a number", []string{"--port", "ssh"}, "ParseFlags.FlagsInvalid"},
		{"a flag without its value", []string{"--config-dir"}, "ParseFlags.FlagsInvalid"},
		{"an argument", []string{"start"}, "ParseFlags.FlagsUnexpectedArgument"},
		{"an argument after the flags", []string{"--no-start", "now"}, "ParseFlags.FlagsUnexpectedArgument"},
		{"port 0", []string{"--port", "0"}, "ParseFlags.FlagsPortInvalid"},
		{"a negative port", []string{"--port=-1"}, "ParseFlags.FlagsPortInvalid"},
		{"a port that does not exist", []string{"--port", "65536"}, "ParseFlags.FlagsPortInvalid"},
		{"a page there is not", []string{"--page", "home"}, "ParseFlags.FlagsPageInvalid"},
		{"a page in another case", []string{"--page", "People"}, "ParseFlags.FlagsPageInvalid"},
		{"a log level there is not", []string{"--log-level", "loud"}, "ParseLevel.LogLevelInvalid"},
		{"help", []string{"--help"}, "ParseFlags.FlagsHelp"},
		{"help, short", []string{"-h"}, "ParseFlags.FlagsHelp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFlags("cave", tt.args)
			errtest.Route(t, err, tt.wantRoute)
			if !reflect.DeepEqual(got, Flags{}) {
				t.Errorf("flags came back with the error: %+v", got)
			}
		})
	}
	// The cause of help is the flag package's own, and an argument is named in the metadata.
	_, err := ParseFlags("cave", []string{"-h"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("the cause of help is %v", errors.Unwrap(err))
	}
	_, err = ParseFlags("cave", []string{"start"})
	if meta := errtest.Route(t, err, "FlagsUnexpectedArgument"); meta["argument"] != "start" {
		t.Errorf("metadata = %v", meta)
	}
	_, err = ParseFlags("cave", []string{"--port", "70000"})
	if meta := errtest.Route(t, err, "FlagsPortInvalid"); meta["port"] != 70000 {
		t.Errorf("metadata = %v", meta)
	}
}

func TestAPageThatIsNotKnownIsNamedSafely(t *testing.T) {
	_, err := ParseFlags("cave", []string{"--page", "a\x1b[31mb"})
	meta := errtest.Route(t, err, "FlagsPageInvalid")
	if meta["page"] != "a\uFFFD[31mb" {
		t.Errorf("metadata = %q", meta["page"])
	}
}

func TestPages(t *testing.T) {
	want := []string{"overview", "people", "attempts", "connections", "history", "settings"}
	if !reflect.DeepEqual(Pages, want) {
		t.Errorf("Pages = %v, want %v", Pages, want)
	}
}

func TestUsage(t *testing.T) {
	for _, program := range []string{"cave", "tcave"} {
		text := Usage(program)
		if !strings.HasPrefix(text, "Usage: "+program+" [flags]\n") {
			t.Errorf("the usage of %s begins with %q", program, text[:min(len(text), 40)])
		}
		for _, name := range []string{"-config-dir", "-state-dir", "-port", "-no-start", "-allow-root", "-log-level", "-version", "-page"} {
			if !strings.Contains(text, "  "+name) {
				t.Errorf("the usage of %s has no %s:\n%s", program, name, text)
			}
		}
	}
}

func TestFlagsOptions(t *testing.T) {
	env := func(values map[string]string) storage.Env {
		return func(name string) (string, bool) {
			value, ok := values[name]
			return value, ok
		}
	}
	// The flags win over the CAVE_ variables.
	flags := Flags{ConfigDir: absolute("/flag/config"), StateDir: absolute("/flag/state"), Port: 2022, NoStart: true, AllowRoot: true, LogLevel: slog.LevelWarn}
	got, err := flags.Options("tcave", env(map[string]string{storage.EnvConfigDir: absolute("/env/config"), storage.EnvStateDir: absolute("/env/state"), "HOME": "/home/ada"}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Program != "tcave" || got.Dirs.Config != absolute("/flag/config") || got.Dirs.State != absolute("/flag/state") || got.Port != 2022 || !got.NoStart || !got.AllowRoot || got.LogLevel != slog.LevelWarn {
		t.Errorf("Options = %+v", got)
	}
	// Without flags, the variables decide.
	got, err = Flags{}.Options("cave", env(map[string]string{storage.EnvConfigDir: absolute("/env/config"), storage.EnvStateDir: absolute("/env/state"), "HOME": "/home/ada"}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Program != "cave" || got.Dirs.Config != absolute("/env/config") || got.Dirs.State != absolute("/env/state") || got.Port != 0 || got.NoStart || got.AllowRoot {
		t.Errorf("Options = %+v", got)
	}
	// A folder that is not an absolute path is refused, by the rule of the folders.
	_, err = Flags{ConfigDir: "relative/config"}.Options("cave", env(nil))
	errtest.Route(t, err, "Flags.Options->Resolve.StorageDirNotAbsolute")
}

func TestSessionHomeVariable(t *testing.T) {
	env := func(home string) storage.Env {
		return func(name string) (string, bool) {
			switch name {
			case storage.EnvConfigDir:
				return absolute("/env/config"), true
			case storage.EnvStateDir:
				return absolute("/env/state"), true
			case EnvSessionHome:
				return home, home != ""
			}
			return "", false
		}
	}
	tests := []struct {
		name, home string
		want       string
	}{
		{"not set", "", ""},
		{"an absolute path", absolute("/tmp/proof/home"), absolute("/tmp/proof/home")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Flags{}.Options("tcave", env(tt.home))
			if err != nil || got.SessionHome != tt.want {
				t.Errorf("SessionHome = %q, %v; want %q", got.SessionHome, err, tt.want)
			}
		})
	}
	t.Run("a relative path is refused", func(t *testing.T) {
		_, err := Flags{}.Options("tcave", env("proof/home"))
		if meta := errtest.Route(t, err, "Flags.Options.SessionHomeInvalid"); meta["dir"] != "proof/home" {
			t.Errorf("metadata %v", meta)
		}
	})
}

func TestIsUsageError(t *testing.T) {
	for _, args := range [][]string{{"--nope"}, {"x"}, {"--port", "0"}, {"--page", "home"}, {"--log-level", "loud"}} {
		_, err := ParseFlags("cave", args)
		if !IsUsageError(err) {
			t.Errorf("%v: %v is not a usage error", args, err)
		}
	}
	_, help := ParseFlags("cave", []string{"--help"})
	for name, err := range map[string]error{
		"help":                 help,
		"nil":                  nil,
		"an error of the app":  errors.New("RunningAsRoot", "x"),
		"an error of the kind": io.EOF,
	} {
		if IsUsageError(err) {
			t.Errorf("%s is a usage error", name)
		}
	}
}
