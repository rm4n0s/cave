//go:build linux || darwin

package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// envOf makes an Env from a map.
func envOf(vars map[string]string) Env {
	return func(name string) (string, bool) {
		value, ok := vars[name]
		return value, ok
	}
}

// notRoot skips a test that needs the mode bits to stop it.
func notRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root is not stopped by mode bits")
	}
}

// chmod changes a mode for the test and makes the folder removable again afterwards.
func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, 0o700); err != nil {
			t.Error(err)
		}
	})
}

func TestEnsureRuntimeOnUnix(t *testing.T) {
	ctx := context.Background()
	base := func(t *testing.T) (Dirs, string) {
		root := t.TempDir()
		return Dirs{Config: filepath.Join(root, "config"), State: filepath.Join(root, "state")}, root
	}
	t.Run("a folder open to others", func(t *testing.T) {
		dirs, root := base(t)
		dirs.Runtime = filepath.Join(root, "runtime")
		if err := os.Mkdir(dirs.Runtime, 0o755); err != nil {
			t.Fatal(err)
		}
		meta := wantRoute(t, dirs.Ensure(ctx), "Ensure->ensureRuntime.StorageRuntimeDirUnsafe")
		if meta["dir"] != dirs.Runtime {
			t.Errorf("metadata = %v", meta)
		}
	})
	t.Run("a symbolic link to a private folder", func(t *testing.T) {
		dirs, root := base(t)
		target := filepath.Join(root, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		dirs.Runtime = filepath.Join(root, "runtime")
		if err := os.Symlink(target, dirs.Runtime); err != nil {
			t.Fatal(err)
		}
		wantRoute(t, dirs.Ensure(ctx), "ensureRuntime.StorageRuntimeDirUnsafe")
	})
	t.Run("a file", func(t *testing.T) {
		dirs, root := base(t)
		dirs.Runtime = filepath.Join(root, "runtime")
		if err := os.WriteFile(dirs.Runtime, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		wantRoute(t, dirs.Ensure(ctx), "ensureRuntime.StorageRuntimeDirUnsafe")
	})
	t.Run("a private folder that exists", func(t *testing.T) {
		dirs, root := base(t)
		dirs.Runtime = filepath.Join(root, "runtime")
		if err := os.Mkdir(dirs.Runtime, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := dirs.Ensure(ctx); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a parent that cannot be searched", func(t *testing.T) {
		notRoot(t)
		dirs, root := base(t)
		parent := filepath.Join(root, "parent")
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatal(err)
		}
		chmod(t, parent, 0)
		dirs.Runtime = filepath.Join(parent, "runtime")
		wantRoute(t, dirs.Ensure(ctx), "ensureRuntime.StorageRuntimeDirStatFailed")
	})
	t.Run("a parent that cannot be written", func(t *testing.T) {
		notRoot(t)
		dirs, root := base(t)
		parent := filepath.Join(root, "parent")
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatal(err)
		}
		chmod(t, parent, 0o500)
		dirs.Runtime = filepath.Join(parent, "runtime")
		wantRoute(t, dirs.Ensure(ctx), "ensureRuntime.StorageRuntimeDirCreateFailed")
	})
}

func TestReadFileErrorsOnUnix(t *testing.T) {
	ctx := context.Background()
	t.Run("a file that cannot be opened", func(t *testing.T) {
		notRoot(t)
		path := filepath.Join(t.TempDir(), "secret.json")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		chmod(t, path, 0)
		_, err := ReadFile(ctx, path, 100)
		wantRoute(t, err, "ReadFile.StorageFileOpenFailed")
	})
	t.Run("a folder instead of a file", func(t *testing.T) {
		_, err := ReadFile(ctx, t.TempDir(), 100)
		wantRoute(t, err, "ReadFile.StorageFileReadFailed")
	})
}

func TestSyncDir(t *testing.T) {
	if err := syncDir(t.TempDir()); err != nil {
		t.Errorf("syncDir of a folder: %v", err)
	}
	if err := syncDir(filepath.Join(t.TempDir(), "missing")); !os.IsNotExist(err) {
		t.Errorf("syncDir of a missing folder: %v", err)
	}
}
