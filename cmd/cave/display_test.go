//go:build cave_display

package main

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/app"
)

// TestStartsInAWindowAndEndsOnSIGTERM runs the real program in a window of the compositor that the
// test was started in (scripts/headless-wayland.sh), draws it on the CPU, and ends it with
// SIGTERM. It needs a compositor, so it has the tag cave_display.
func TestStartsInAWindowAndEndsOnSIGTERM(t *testing.T) {
	display := os.Getenv("WAYLAND_DISPLAY")
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if display == "" || runtime == "" {
		t.Skip("no compositor: run this through scripts/headless-wayland.sh")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "cave")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	config, state, home, run := filepath.Join(dir, "config"), filepath.Join(dir, "state"), filepath.Join(dir, "home"), filepath.Join(dir, "run")
	for _, d := range []string{home, run} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bin, "--no-start")
	// A clean environment: the folders are the test's own, and only the compositor's socket
	// (by its whole path) and the CPU renderer come from outside.
	cmd.Env = []string{
		"CAVE_CONFIG_DIR=" + config, "CAVE_STATE_DIR=" + state,
		"HOME=" + home, "XDG_RUNTIME_DIR=" + run,
		"WAYLAND_DISPLAY=" + filepath.Join(runtime, display),
		"GLUTTER_RENDERER=soft", "PATH=" + os.Getenv("PATH"),
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	select {
	case err := <-exited:
		t.Fatalf("cave ended by itself within 3 seconds: %v", err)
	case <-time.After(3 * time.Second):
	}
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Errorf("cave ended with %v after SIGTERM, want status 0", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cave did not end within 10 seconds of SIGTERM")
	}

	f, err := os.Open(filepath.Join(state, app.LogFile))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var line struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
		}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Errorf("a line of the log is not JSON: %q", sc.Text())
			continue
		}
		if line.Level == "ERROR" {
			t.Errorf("the log has an error: %s", sc.Text())
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
}
