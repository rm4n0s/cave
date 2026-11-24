//go:build linux && cave_wine

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/app"
)

// TestTcaveExePrintsItsVersionUnderWine builds tcave.exe for Windows and runs it under Wine
// (scripts/wine-exec.sh, Cave's private prefix, a headless X server): --version prints the version
// and the program ends with status 0. It needs Wine and Xvfb, and has the tag cave_wine; run it
// through scripts/heavy.sh --alone.
func TestTcaveExePrintsItsVersionUnderWine(t *testing.T) {
	for _, tool := range []string{"wine", "Xvfb"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "tcave.exe")
	build := exec.Command("go", "build", "-o", exe, "./cmd/tcave")
	build.Dir = root
	build.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(root, "scripts", "wine-exec.sh"), exe, "--version")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "XDG_RUNTIME_DIR=" + t.TempDir()}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("tcave.exe --version: %v", err)
	}
	// Windows ends the line with \r\n when a console does the writing; through a pipe it is \n.
	if want := "tcave " + app.Version() + "\n"; string(out) != want {
		t.Errorf("tcave.exe --version printed %q, want %q", out, want)
	}
}
