package main

import (
	"bytes"
	"io"
	"testing"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/errors"
)

// failingWriter is a stream that cannot be written to.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		stdout     io.Writer
		wantOut    string
		wantRoute  string
		wantOrigin error
	}{
		{name: "version", args: []string{"--version"}, wantOut: "cave-tui " + app.Version() + "\n"},
		{name: "version with one dash", args: []string{"-version"}, wantOut: "cave-tui " + app.Version() + "\n"},
		{name: "unknown flag", args: []string{"--nope"}, wantRoute: "run.CaveTuiUsage"},
		{name: "unexpected argument", args: []string{"extra"}, wantRoute: "run.CaveTuiUsage"},
		{
			name: "version to a closed stream", args: []string{"--version"}, stdout: failingWriter{},
			wantRoute: "run.CaveTuiVersionWriteFailed", wantOrigin: io.ErrClosedPipe,
		},
		{name: "no flag", wantRoute: "run.CaveTuiNotBuiltYet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			stdout := tt.stdout
			if stdout == nil {
				stdout = &out
			}
			err := run(tt.args, stdout, &errOut)
			if tt.wantRoute == "" {
				if err != nil {
					t.Fatalf("run(%v) failed: %v", tt.args, err)
				}
				if out.String() != tt.wantOut {
					t.Errorf("stdout = %q, want %q", out.String(), tt.wantOut)
				}
				return
			}
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T: %v", err, err)
			}
			if !appErr.HasRoute(tt.wantRoute) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if tt.wantOrigin != nil && !errors.Is(err, tt.wantOrigin) {
				t.Errorf("the cause is %v, want %v", errors.Unwrap(err), tt.wantOrigin)
			}
			if out.Len() != 0 {
				t.Errorf("stdout = %q, want nothing", out.String())
			}
		})
	}
}
