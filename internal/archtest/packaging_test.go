package archtest

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// desktopFile reads the keys of the [Desktop Entry] group of packaging/cave.desktop.
func desktopFile(t *testing.T) map[string]string {
	t.Helper()
	file, err := os.Open(filepath.Join(moduleRoot(t), "packaging", "cave.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }() // only read
	keys := map[string]string{}
	inEntry := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "["):
			inEntry = line == "[Desktop Entry]"
		case inEntry:
			if key, value, ok := strings.Cut(line, "="); ok {
				keys[key] = value
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestTheDesktopFile(t *testing.T) {
	want := map[string]string{
		"Type":           "Application",
		"Name":           "Cave",
		"Comment":        "An SSH server you start, watch and stop",
		"Exec":           "cave",
		"Icon":           "io.github.rm4n0s.cave",
		"Categories":     "Network;",
		"Terminal":       "false",
		"StartupWMClass": "io.github.rm4n0s.cave",
	}
	got := desktopFile(t)
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s is %q, want %q", key, got[key], value)
		}
	}
	for key := range got {
		if _, known := want[key]; !known {
			t.Errorf("the desktop file has the key %s, which nobody asked for", key)
		}
	}
	// The icon's name and the class of the window are the application ID that the window gives
	// (internal/gui/run.go).
	source, err := os.ReadFile(filepath.Join(moduleRoot(t), "internal", "gui", "run.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), `const appID = "`+want["Icon"]+`"`) {
		t.Errorf("internal/gui/run.go has another application ID than %s", want["Icon"])
	}
	path, err := exec.LookPath("desktop-file-validate")
	if err != nil {
		t.Log("desktop-file-validate is not installed: only the keys were checked")
		return
	}
	if out, err := exec.Command(path, filepath.Join(moduleRoot(t), "packaging", "cave.desktop")).CombinedOutput(); err != nil {
		t.Errorf("desktop-file-validate: %v\n%s", err, out)
	}
}
