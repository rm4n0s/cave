//go:build linux && cave_wine

package main

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCaveExeDrawsItsFirstFrameUnderWine builds cave.exe for Windows (a debug build: only those
// read the self-capture of Glutter's GLUTTER_SCREENSHOT) and runs it under Wine
// (scripts/wine-exec.sh, Cave's private prefix, a headless X server) with isolated folders and
// --no-start, so that nothing listens. The app draws its first frame on the CPU, saves it, and
// ends by itself with status 0: the log has the engine's "first frame presented" line at the
// level info and no panic, and the picture has many colors (a drawn window). It needs Wine and
// Xvfb, and has the tag cave_wine; run it through scripts/heavy.sh --alone, as Glutter's
// examples/windows_wine_test.go is.
func TestCaveExeDrawsItsFirstFrameUnderWine(t *testing.T) {
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
	exe := filepath.Join(dir, "cave.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "run", "github.com/rm4n0s/glutter/cmd/glutter-windows", "build",
		"-debug", "-arch", "amd64", "-name", "Cave", "-app-id", "org.rm4n0s.cave", "-o", exe, "./cmd/cave")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("glutter-windows build: %v\n%s", err, out)
	}

	config, state, shot := filepath.Join(dir, "config"), filepath.Join(dir, "state"), filepath.Join(dir, "frame.png")
	runtime := filepath.Join(dir, "run")
	for _, d := range []string{config, state, runtime} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// Wine maps the whole file system to the drive Z:.
	windowsPath := func(p string) string { return "Z:" + strings.ReplaceAll(p, "/", `\`) }
	cmd := exec.CommandContext(ctx, filepath.Join(root, "scripts", "wine-exec.sh"), exe, "--no-start")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "XDG_RUNTIME_DIR=" + runtime,
		"CAVE_CONFIG_DIR=" + windowsPath(config), "CAVE_STATE_DIR=" + windowsPath(state),
		"GLUTTER_RENDERER=soft", "GLUTTER_SCREENSHOT=" + windowsPath(shot),
	}
	var log bytes.Buffer
	cmd.Stdout, cmd.Stderr = &log, &log
	if err := cmd.Run(); err != nil {
		t.Fatalf("cave.exe: %v\n%s", err, log.String())
	}
	for _, bad := range []string{"panic:", "fatal error:", "goroutine "} {
		if strings.Contains(log.String(), bad) {
			t.Errorf("the log has %q:\n%s", bad, log.String())
		}
	}
	var first string
	for _, line := range strings.Split(log.String(), "\n") {
		if strings.Contains(line, "first frame presented") {
			first = line
			break
		}
	}
	if first == "" || !strings.Contains(first, "INFO") {
		t.Fatalf("cave.exe logged no first frame at the level info:\n%s", log.String())
	}
	file, err := os.Open(shot)
	if err != nil {
		t.Fatalf("cave.exe saved no frame: %v\n%s", err, log.String())
	}
	defer func() { _ = file.Close() }()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	colors := map[[4]uint32]bool{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y && len(colors) < 8; y += 3 {
		for x := b.Min.X; x < b.Max.X && len(colors) < 8; x += 3 {
			r, g, bl, a := img.At(x, y).RGBA()
			colors[[4]uint32{r, g, bl, a}] = true
		}
	}
	if len(colors) < 8 {
		t.Errorf("the frame (%v) has %d colors in a sample, want a drawn window", b, len(colors))
	}
	t.Logf("first frame: %s", first)
}
