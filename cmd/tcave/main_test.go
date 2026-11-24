package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/errors"
)

// failingWriter is a stream that cannot be written to.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// testEnv is an environment with no variable of the user's: where the folders go comes from the
// flags, and the runtime folder is the test's own (macOS keeps it under TMPDIR).
func testEnv(t *testing.T) func(string) (string, bool) {
	t.Helper()
	runtime := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{"HOME": t.TempDir(), "XDG_RUNTIME_DIR": runtime, "TMPDIR": runtime}
	return func(name string) (string, bool) {
		v, ok := vars[name]
		return v, ok
	}
}

func TestRunPrintsWhatDoesNotNeedTheTerminal(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantOut string
	}{
		{"version", []string{"--version"}, "tcave " + app.Version() + "\n"},
		{"version with one dash", []string{"-version"}, "tcave " + app.Version() + "\n"},
		{"help", []string{"--help"}, app.Usage("tcave")},
		{"help, short", []string{"-h"}, app.Usage("tcave")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if err := run(context.Background(), tt.args, testEnv(t), strings.NewReader(""), &out, &errOut); err != nil {
				t.Fatal(err)
			}
			if out.String() != tt.wantOut {
				t.Errorf("stdout = %q, want %q", out.String(), tt.wantOut)
			}
			if errOut.Len() != 0 {
				t.Errorf("stderr = %q", errOut.String())
			}
		})
	}
}

func TestRunErrors(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		stdout     io.Writer
		wantRoute  string
		wantUsage  bool
		wantOrigin error
	}{
		{name: "unknown flag", args: []string{"--nope"}, wantRoute: "ParseFlags.FlagsInvalid", wantUsage: true},
		{name: "unexpected argument", args: []string{"extra"}, wantRoute: "ParseFlags.FlagsUnexpectedArgument", wantUsage: true},
		{name: "a page there is not", args: []string{"--page", "home"}, wantRoute: "ParseFlags.FlagsPageInvalid", wantUsage: true},
		{name: "a port out of range", args: []string{"--port", "0"}, wantRoute: "ParseFlags.FlagsPortInvalid", wantUsage: true},
		{
			name: "version to a closed stream", args: []string{"--version"}, stdout: failingWriter{},
			wantRoute: "printTo.TcaveWriteFailed", wantOrigin: io.ErrClosedPipe,
		},
		{
			name: "help to a closed stream", args: []string{"--help"}, stdout: failingWriter{},
			wantRoute: "printTo.TcaveWriteFailed", wantOrigin: io.ErrClosedPipe,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			var stdout io.Writer = &out
			if tt.stdout != nil {
				stdout = tt.stdout
			}
			err := run(context.Background(), tt.args, testEnv(t), strings.NewReader(""), stdout, &errOut)
			errtest.Route(t, err, tt.wantRoute)
			if app.IsUsageError(err) != tt.wantUsage {
				t.Errorf("IsUsageError = %v, want %v", app.IsUsageError(err), tt.wantUsage)
			}
			if tt.wantOrigin != nil && !errors.Is(err, tt.wantOrigin) {
				t.Errorf("the cause is %v, want %v", errors.Unwrap(err), tt.wantOrigin)
			}
			if out.Len() != 0 || errOut.Len() != 0 {
				t.Errorf("stdout = %q, stderr = %q, want nothing", out.String(), errOut.String())
			}
			// What the user reads is a sentence, not a route.
			if msg := app.Message(err); msg == "" || strings.Contains(msg, "->") {
				t.Errorf("the message is %q", msg)
			}
		})
	}
}

func TestRunWithAStateFolderThatIsAFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "state")
	if err := os.WriteFile(file, []byte("not a folder"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	args := []string{"--config-dir", filepath.Join(dir, "config"), "--state-dir", file}
	err := run(context.Background(), args, testEnv(t), strings.NewReader("q"), &out, &errOut)
	errtest.Route(t, err, "StorageDirCreateFailed")
	if app.IsUsageError(err) {
		t.Error("a folder that cannot be made is not a usage error")
	}
	if !strings.Contains(app.Message(err), file) {
		t.Errorf("the sentence does not name the folder: %q", app.Message(err))
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestRunOpensTheApplicationAndQuits(t *testing.T) {
	dir := t.TempDir()
	config, state := filepath.Join(dir, "config"), filepath.Join(dir, "state")
	var out, errOut bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// "q" is waiting on the input when the program starts, so it quits at once; the server is
	// not started, so that nothing listens.
	args := []string{"--config-dir", config, "--state-dir", state, "--no-start"}
	if err := run(ctx, args, testEnv(t), strings.NewReader("q"), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q", errOut.String())
	}
	// The application was opened and closed: its files are there, and the lock is free again
	// (a second run works).
	for _, p := range []string{filepath.Join(config, "ssh_host_ed25519_key"), filepath.Join(state, app.LogFile)} {
		if _, err := os.Stat(p); err != nil {
			t.Error(err)
		}
	}
	if err := run(ctx, args, testEnv(t), strings.NewReader("q"), &out, &errOut); err != nil {
		t.Fatalf("the second run: %v", err)
	}
}
