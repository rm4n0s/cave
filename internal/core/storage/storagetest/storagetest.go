// Package storagetest keeps tests away from the user's own files (docs/SPEC.md §14.1).
package storagetest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/cave/internal/core/storage"
)

// Isolate points Cave's folders, the home folder and the XDG folders at temporary folders that
// the test owns, takes the user's SSH agent out of the environment, and returns Cave's folders,
// created. The environment is restored and the folders are removed when the test ends. It
// cannot be used in a parallel test, because it changes the environment.
func Isolate(t testing.TB) storage.Dirs {
	t.Helper()
	root := t.TempDir()
	// Sockets live in the runtime folder, and a socket's path must be short: t.TempDir() has
	// the test's name in it and can be too long.
	runtime, err := os.MkdirTemp("", "cave-rt-")
	if err != nil {
		t.Fatalf("storagetest: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(runtime); err != nil {
			t.Errorf("storagetest: %v", err)
		}
	})
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("storagetest: %v", err)
	}
	for name, value := range map[string]string{
		"HOME":               home,
		"USERPROFILE":        home,
		"AppData":            filepath.Join(root, "appdata"),
		"LocalAppData":       filepath.Join(root, "localappdata"),
		"XDG_CONFIG_HOME":    filepath.Join(root, "xdg-config"),
		"XDG_STATE_HOME":     filepath.Join(root, "xdg-state"),
		"XDG_CACHE_HOME":     filepath.Join(root, "xdg-cache"),
		"XDG_DATA_HOME":      filepath.Join(root, "xdg-data"),
		"XDG_RUNTIME_DIR":    runtime,
		storage.EnvConfigDir: filepath.Join(root, "config"),
		storage.EnvStateDir:  filepath.Join(root, "state"),
	} {
		t.Setenv(name, value)
	}
	for _, name := range []string{"SSH_AUTH_SOCK", "SSH_AGENT_PID"} {
		// Setenv registers the restore of the old value; the variable is then removed.
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("storagetest: %v", err)
		}
	}
	dirs, err := storage.Resolve(storage.Options{}, os.LookupEnv)
	if err != nil {
		t.Fatalf("storagetest: %v", err)
	}
	if err := dirs.Ensure(context.Background()); err != nil {
		t.Fatalf("storagetest: %v", err)
	}
	return dirs
}
