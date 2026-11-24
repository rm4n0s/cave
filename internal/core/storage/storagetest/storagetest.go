// Package storagetest keeps tests away from the user's own files (docs/SPEC.md §14.1).
package storagetest

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rm4n0s/cave/internal/core/storage"
)

// runtimeBase is where Isolate makes the runtime folders: the temporary folder the process
// started with, so that an Isolate after another (which changed TMPDIR) does not nest them, and
// /tmp on macOS, whose temporary folder has a path too long for the sockets in them.
var runtimeBase = func() string {
	if runtime.GOOS == "darwin" {
		return "/tmp"
	}
	return os.TempDir()
}()

// Isolate points Cave's folders, the home folder and the XDG folders at temporary folders that
// the test owns, takes the user's SSH agent out of the environment, and returns Cave's folders,
// created. The environment is restored and the folders are removed when the test ends. It
// cannot be used in a parallel test, because it changes the environment.
func Isolate(t testing.TB) storage.Dirs {
	t.Helper()
	// The folders are named by their real paths: on macOS the temporary folder is under /var,
	// a link to /private/var, and a shell or an SFTP client names the real path.
	root := realPath(t, t.TempDir())
	// Sockets live in the runtime folder, and a socket's path must be short: t.TempDir() has
	// the test's name in it and can be too long.
	runtimeDir, err := os.MkdirTemp(runtimeBase, "cave-rt-")
	if err != nil {
		t.Fatalf("storagetest: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(runtimeDir); err != nil {
			t.Errorf("storagetest: %v", err)
		}
	})
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("storagetest: %v", err)
	}
	for name, value := range map[string]string{
		"HOME":            home,
		"USERPROFILE":     home,
		"AppData":         filepath.Join(root, "appdata"),
		"LocalAppData":    filepath.Join(root, "localappdata"),
		"XDG_CONFIG_HOME": filepath.Join(root, "xdg-config"),
		"XDG_STATE_HOME":  filepath.Join(root, "xdg-state"),
		"XDG_CACHE_HOME":  filepath.Join(root, "xdg-cache"),
		"XDG_DATA_HOME":   filepath.Join(root, "xdg-data"),
		"XDG_RUNTIME_DIR": runtimeDir,
		// macOS has no XDG_RUNTIME_DIR: its runtime folder is in the temporary folder.
		"TMPDIR":             runtimeDir,
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

// realPath returns the path with its links resolved.
func realPath(t testing.TB, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("storagetest: %v", err)
	}
	return real
}
