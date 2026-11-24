package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// wantRoute fails the test unless err is an *errors.Error whose route has every given part, and
// returns the error's metadata.
func wantRoute(t *testing.T, err error, parts ...string) map[string]any {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	for _, part := range parts {
		if !appErr.HasRoute(part) {
			t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), part)
		}
	}
	return appErr.Metadata
}

// cancelled returns a context that is already cancelled.
func cancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// checkMode fails the test unless the file or folder has the mode, on platforms with mode bits.
func checkMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode of %s = %o, want %o", path, got, want)
	}
}

func TestResolveRefusesRelativeFolders(t *testing.T) {
	noEnv := func(string) (string, bool) { return "", false }
	tests := []struct {
		name string
		opts Options
		env  Env
	}{
		{"a relative --config-dir", Options{ConfigDir: "relative/config"}, noEnv},
		{"a relative --state-dir", Options{StateDir: "relative/state"}, noEnv},
		{"a relative CAVE_CONFIG_DIR", Options{}, func(name string) (string, bool) {
			if name == EnvConfigDir {
				return "relative", true
			}
			return "", false
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Resolve(tt.opts, tt.env)
			wantRoute(t, err, "Resolve.StorageDirNotAbsolute")
		})
	}
}

func TestEnsure(t *testing.T) {
	t.Run("creates the folders with mode 0700", func(t *testing.T) {
		root := t.TempDir()
		dirs := Dirs{Config: filepath.Join(root, "a", "config"), State: filepath.Join(root, "b", "state")}
		if runtime.GOOS != "windows" {
			dirs.Runtime = filepath.Join(root, "c", "runtime")
		}
		if err := dirs.Ensure(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, dir := range []string{dirs.Config, dirs.State, dirs.Runtime} {
			if dir != "" {
				checkMode(t, dir, 0o700)
			}
		}
		// A second call on folders that exist is fine.
		if err := dirs.Ensure(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a cancelled context", func(t *testing.T) {
		root := t.TempDir()
		dirs := Dirs{Config: filepath.Join(root, "config"), State: filepath.Join(root, "state")}
		err := dirs.Ensure(cancelled())
		wantRoute(t, err, "Ensure.StorageEnsureCancelled")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("the cause is %v", errors.Unwrap(err))
		}
		if _, err := os.Stat(dirs.Config); !os.IsNotExist(err) {
			t.Error("the folder was created")
		}
	})
	t.Run("a file where a folder should be", func(t *testing.T) {
		root := t.TempDir()
		file := filepath.Join(root, "file")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		dirs := Dirs{Config: filepath.Join(file, "config"), State: filepath.Join(root, "state")}
		meta := wantRoute(t, dirs.Ensure(context.Background()), "Ensure.StorageDirCreateFailed")
		if meta["dir"] != dirs.Config {
			t.Errorf("metadata = %v", meta)
		}
	})
}

// failingFS is the real file system with steps that fail on demand.
type failingFS struct {
	osFS
	failWrite, failSync, failClose, failRename, failSyncDir bool
	// removed collects the names given to Remove.
	removed []string
}

// failingFile is a temporary file whose steps fail on demand.
type failingFile struct {
	tempFile
	fs *failingFS
}

var errInjected = &errors.Error{Tag: "Injected", Message: "injected failure"}

func (f *failingFS) CreateTemp(dir, pattern string) (tempFile, error) {
	file, err := f.osFS.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	return &failingFile{tempFile: file, fs: f}, nil
}

func (f *failingFS) Rename(from, to string) error {
	if f.failRename {
		return errInjected
	}
	return f.osFS.Rename(from, to)
}

func (f *failingFS) Remove(name string) error {
	f.removed = append(f.removed, name)
	return f.osFS.Remove(name)
}

func (f *failingFS) SyncDir(dir string) error {
	if f.failSyncDir {
		return errInjected
	}
	return f.osFS.SyncDir(dir)
}

func (f *failingFile) Write(p []byte) (int, error) {
	if f.fs.failWrite {
		return 0, errInjected
	}
	return f.tempFile.Write(p)
}

func (f *failingFile) Sync() error {
	if f.fs.failSync {
		return errInjected
	}
	return f.tempFile.Sync()
}

func (f *failingFile) Close() error {
	err := f.tempFile.Close()
	if f.fs.failClose {
		return errInjected
	}
	return err
}

// entries lists the names in a folder.
func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

func TestWriteFileAtomic(t *testing.T) {
	ctx := context.Background()
	t.Run("writes a new file with mode 0600 and replaces it", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "people.json")
		for _, content := range []string{"first", "second, longer", ""} {
			if err := WriteFileAtomic(ctx, path, []byte(content)); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != content {
				t.Errorf("content = %q, want %q", got, content)
			}
			checkMode(t, path, 0o600)
		}
		if names := entries(t, dir); len(names) != 1 || names[0] != "people.json" {
			t.Errorf("the folder has %v, want only people.json", names)
		}
	})
	t.Run("a cancelled context", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "x.json")
		err := WriteFileAtomic(cancelled(), path, []byte("x"))
		wantRoute(t, err, "WriteFileAtomic->writeFileAtomic.StorageWriteCancelled")
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("the file was written")
		}
	})
	t.Run("a folder that does not exist", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing", "x.json")
		meta := wantRoute(t, WriteFileAtomic(ctx, path, []byte("x")), "WriteFileAtomic->writeFileAtomic.StorageWriteCreateFailed")
		if meta["path"] != path {
			t.Errorf("metadata = %v", meta)
		}
	})

	failures := []struct {
		name      string
		fs        *failingFS
		wantRoute string
		// renamed says that the failure comes after the new content is in place.
		renamed bool
	}{
		{"the write fails", &failingFS{failWrite: true}, "writeFileAtomic.StorageWriteFailed", false},
		{"the sync fails", &failingFS{failSync: true}, "writeFileAtomic.StorageWriteSyncFailed", false},
		{"the close fails", &failingFS{failClose: true}, "writeFileAtomic.StorageWriteCloseFailed", false},
		{"the rename fails", &failingFS{failRename: true}, "writeFileAtomic.StorageWriteRenameFailed", false},
		{"the folder's sync fails", &failingFS{failSyncDir: true}, "writeFileAtomic.StorageWriteDirSyncFailed", true},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")
			if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			err := writeFileAtomic(ctx, tt.fs, path, []byte("new"))
			wantRoute(t, err, tt.wantRoute)
			if !errors.Is(err, errInjected) {
				t.Errorf("the cause is %v", errors.Unwrap(err))
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := "old"
			if tt.renamed {
				want = "new"
			}
			if string(got) != want {
				t.Errorf("content after the failure = %q, want %q", got, want)
			}
			if names := entries(t, dir); len(names) != 1 {
				t.Errorf("the folder has %v: the temporary file was left", names)
			}
			if !tt.renamed && len(tt.fs.removed) != 1 {
				t.Errorf("Remove was called %d times, want 1", len(tt.fs.removed))
			}
		})
	}
}

func TestReadFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	content := []byte("0123456789")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("a file within the limit", func(t *testing.T) {
		for _, limit := range []int64{10, 11, 1 << 20} {
			got, err := ReadFile(ctx, path, limit)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, content) {
				t.Errorf("limit %d: content = %q", limit, got)
			}
		}
	})
	t.Run("a file over the limit", func(t *testing.T) {
		_, err := ReadFile(ctx, path, 9)
		meta := wantRoute(t, err, "ReadFile.FileTooLarge")
		if meta["path"] != path || meta["maxBytes"] != int64(9) {
			t.Errorf("metadata = %v", meta)
		}
	})
	t.Run("a file that does not exist", func(t *testing.T) {
		_, err := ReadFile(ctx, filepath.Join(dir, "missing.json"), 100)
		wantRoute(t, err, "ReadFile.StorageFileNotExist")
		if !errors.Is(err, ErrNotExist) {
			t.Error("errors.Is(err, ErrNotExist) is false")
		}
		if errors.Is(err, ErrAlreadyRunning) {
			t.Error("the error matches another sentinel")
		}
	})
	t.Run("a cancelled context", func(t *testing.T) {
		_, err := ReadFile(cancelled(), path, 100)
		wantRoute(t, err, "ReadFile.StorageReadCancelled")
	})
}

func TestLock(t *testing.T) {
	ctx := context.Background()
	newDirs := func(t *testing.T) Dirs {
		root := t.TempDir()
		dirs := Dirs{Config: filepath.Join(root, "config"), State: filepath.Join(root, "state")}
		if err := dirs.Ensure(ctx); err != nil {
			t.Fatal(err)
		}
		return dirs
	}
	release := func(t *testing.T, lock *Lock) {
		t.Helper()
		if err := lock.Release(); err != nil {
			t.Errorf("Release: %v", err)
		}
	}

	t.Run("one holder at a time", func(t *testing.T) {
		dirs := newDirs(t)
		first, err := Acquire(ctx, dirs, "cave")
		if err != nil {
			t.Fatal(err)
		}
		lockPath := filepath.Join(dirs.State, "lock")
		checkMode(t, lockPath, 0o600)

		_, err = Acquire(ctx, dirs, "tcave")
		meta := wantRoute(t, err, "Acquire->acquire.AlreadyRunning")
		if !errors.Is(err, ErrAlreadyRunning) {
			t.Error("errors.Is(err, ErrAlreadyRunning) is false")
		}
		if meta["program"] != "cave" || meta["pid"] != os.Getpid() {
			t.Errorf("the holder in the metadata is %v %v, want cave %d", meta["program"], meta["pid"], os.Getpid())
		}

		release(t, first)
		release(t, first) // a second release does nothing

		second, err := Acquire(ctx, dirs, "tcave")
		if err != nil {
			t.Fatalf("after the release: %v", err)
		}
		defer release(t, second)
		_, err = Acquire(ctx, dirs, "cave")
		meta = wantRoute(t, err, "acquire.AlreadyRunning")
		if meta["program"] != "tcave" {
			t.Errorf("the holder is %v, want tcave", meta["program"])
		}
	})
	t.Run("different folders do not conflict", func(t *testing.T) {
		a, err := Acquire(ctx, newDirs(t), "cave")
		if err != nil {
			t.Fatal(err)
		}
		defer release(t, a)
		b, err := Acquire(ctx, newDirs(t), "cave")
		if err != nil {
			t.Fatal(err)
		}
		defer release(t, b)
	})
	t.Run("a cancelled context", func(t *testing.T) {
		_, err := Acquire(cancelled(), newDirs(t), "cave")
		wantRoute(t, err, "Acquire->acquire.StorageLockCancelled")
	})
	t.Run("a state folder that does not exist", func(t *testing.T) {
		_, err := Acquire(ctx, Dirs{State: filepath.Join(t.TempDir(), "missing")}, "cave")
		wantRoute(t, err, "Acquire->acquire.StorageLockOpenFailed")
	})

	real := lockOps{tryLock: tryLock, unlock: unlock, writeHolder: writeHolder}
	t.Run("the lock fails", func(t *testing.T) {
		ops := real
		ops.tryLock = func(*os.File) (bool, error) { return false, errInjected }
		_, err := acquire(ctx, newDirs(t), "cave", ops)
		wantRoute(t, err, "acquire.StorageLockFailed")
		if !errors.Is(err, errInjected) {
			t.Errorf("the cause is %v", errors.Unwrap(err))
		}
	})
	t.Run("the holder cannot be written", func(t *testing.T) {
		dirs := newDirs(t)
		ops := real
		ops.writeHolder = func(*os.File, string, int) error { return errInjected }
		_, err := acquire(ctx, dirs, "cave", ops)
		wantRoute(t, err, "acquire.StorageLockWriteFailed")
		// The failed attempt left the lock free.
		lock, err := Acquire(ctx, dirs, "cave")
		if err != nil {
			t.Fatalf("the lock was left held: %v", err)
		}
		release(t, lock)
	})
	t.Run("the unlock fails", func(t *testing.T) {
		ops := real
		ops.unlock = func(*os.File) error { return errInjected }
		lock, err := acquire(ctx, newDirs(t), "cave", ops)
		if err != nil {
			t.Fatal(err)
		}
		wantRoute(t, lock.Release(), "Release.StorageLockReleaseFailed")
		release(t, lock) // already released: nothing to do, no error
	})
	t.Run("the close fails", func(t *testing.T) {
		ops := real
		ops.unlock = func(f *os.File) error { return f.Close() } // the lock's own close then fails
		lock, err := acquire(ctx, newDirs(t), "cave", ops)
		if err != nil {
			t.Fatal(err)
		}
		wantRoute(t, lock.Release(), "Release.StorageLockCloseFailed")
	})
}

func TestReadHolder(t *testing.T) {
	tests := []struct {
		content     string
		wantProgram string
		wantPID     int
	}{
		{"cave 1234\n", "cave", 1234},
		{"tcave 7", "tcave", 7},
		{"", "", 0},
		{"cave", "", 0},
		{"cave notanumber\n", "", 0},
		{"cave -5\n", "", 0},
		{"cave 12 extra\n", "", 0},
		{strings.Repeat("x", 1000) + " 12\n", "", 0}, // only the start of the file is read
	}
	for _, tt := range tests {
		path := filepath.Join(t.TempDir(), "lock")
		if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		program, pid := readHolder(f)
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if program != tt.wantProgram || pid != tt.wantPID {
			t.Errorf("readHolder(%q) = %q, %d, want %q, %d", tt.content, program, pid, tt.wantProgram, tt.wantPID)
		}
	}
}

// The seams are the real ones unless a test replaces them.
var _ atomicFS = osFS{}
var _ io.Writer = (*failingFile)(nil)
