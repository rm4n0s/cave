// Package storage is where Cave keeps its files: it resolves the configuration, state and
// runtime folders, writes files atomically, reads them with a size limit, and holds the lock
// that keeps a second instance from using the same folders (docs/SPEC.md §5).
package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/rm4n0s/errors"
)

// The Tags of this package's errors.
const (
	TagDirNotAbsolute         = "StorageDirNotAbsolute"
	TagEnsureCancelled        = "StorageEnsureCancelled"
	TagDirCreateFailed        = "StorageDirCreateFailed"
	TagWriteCancelled         = "StorageWriteCancelled"
	TagWriteCreateFailed      = "StorageWriteCreateFailed"
	TagWriteFailed            = "StorageWriteFailed"
	TagWriteSyncFailed        = "StorageWriteSyncFailed"
	TagWriteCloseFailed       = "StorageWriteCloseFailed"
	TagWriteRenameFailed      = "StorageWriteRenameFailed"
	TagWriteDirSyncFailed     = "StorageWriteDirSyncFailed"
	TagReadCancelled          = "StorageReadCancelled"
	TagFileNotExist           = "StorageFileNotExist"
	TagFileOpenFailed         = "StorageFileOpenFailed"
	TagFileReadFailed         = "StorageFileReadFailed"
	TagFileTooLarge           = "FileTooLarge"
	TagLockCancelled          = "StorageLockCancelled"
	TagLockOpenFailed         = "StorageLockOpenFailed"
	TagLockFailed             = "StorageLockFailed"
	TagLockWriteFailed        = "StorageLockWriteFailed"
	TagLockReleaseFailed      = "StorageLockReleaseFailed"
	TagLockCloseFailed        = "StorageLockCloseFailed"
	TagAlreadyRunning         = "AlreadyRunning"
	TagHomeUnknown            = "StorageHomeUnknown"
	TagAppDataUnknown         = "StorageAppDataUnknown"
	TagLocalAppDataUnknown    = "StorageLocalAppDataUnknown"
	TagRuntimeDirStatFailed   = "StorageRuntimeDirStatFailed"
	TagRuntimeDirCreateFailed = "StorageRuntimeDirCreateFailed"
	TagRuntimeDirUnsafe       = "StorageRuntimeDirUnsafe"
)

// ErrNotExist matches, with errors.Is, the error ReadFile returns for a file that does not exist.
var ErrNotExist = &errors.Error{Tag: TagFileNotExist}

// ErrAlreadyRunning matches, with errors.Is, the error Acquire returns when another instance of
// Cave holds the lock.
var ErrAlreadyRunning = &errors.Error{Tag: TagAlreadyRunning}

// The names of the environment variables that replace the folders (docs/SPEC.md §5.1).
const (
	EnvConfigDir = "CAVE_CONFIG_DIR"
	EnvStateDir  = "CAVE_STATE_DIR"
)

// Dirs are the folders Cave uses.
type Dirs struct {
	// Config holds the settings, the people and the host key.
	Config string
	// State holds the attempts list, the log and the lock.
	State string
	// Runtime holds the sockets of forwarded agents. It is empty on Windows, which uses named
	// pipes.
	Runtime string
}

// Options are the folders given on the command line; an empty one is not given.
type Options struct {
	// ConfigDir is --config-dir. It must be an absolute path.
	ConfigDir string
	// StateDir is --state-dir. It must be an absolute path.
	StateDir string
}

// Env looks up an environment variable, as os.LookupEnv does.
type Env func(name string) (string, bool)

// Resolve decides the folders by docs/SPEC.md §5.1: a folder of the options wins over its
// CAVE_ variable, which wins over the platform's own place. It creates nothing.
func Resolve(opts Options, env Env) (Dirs, error) {
	lookup := func(name string) string {
		value, _ := env(name)
		return value
	}
	config, state := opts.ConfigDir, opts.StateDir
	if config == "" {
		config = lookup(EnvConfigDir)
	}
	if state == "" {
		state = lookup(EnvStateDir)
	}
	for _, dir := range []string{config, state} {
		if dir != "" && !filepath.IsAbs(dir) {
			return Dirs{}, errors.New(TagDirNotAbsolute, "a folder given by a flag or a CAVE_ variable must be an absolute path", "dir", dir)
		}
	}
	return resolvePlatform(config, state, lookup)
}

// Ensure creates the folders that do not exist, with mode 0700, and checks that the runtime
// folder is private to the user.
func (d Dirs) Ensure(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errors.NewErr(TagEnsureCancelled, err)
	}
	for _, dir := range []string{d.Config, d.State} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return errors.NewErr(TagDirCreateFailed, err).SetMetadata("dir", dir)
		}
	}
	return ensureRuntime(d.Runtime)
}

// atomicFS is the part of the file system WriteFileAtomic uses, so that tests can make each step
// fail.
type atomicFS interface {
	// CreateTemp creates a new file in dir with mode 0600, as os.CreateTemp does.
	CreateTemp(dir, pattern string) (tempFile, error)
	// Rename moves a file over another, as os.Rename does.
	Rename(from, to string) error
	// Remove deletes a file.
	Remove(name string) error
	// SyncDir makes a folder's entries durable.
	SyncDir(dir string) error
}

// tempFile is the part of *os.File WriteFileAtomic uses.
type tempFile interface {
	io.Writer
	Sync() error
	Close() error
	Name() string
}

// osFS is the real file system.
type osFS struct{}

func (osFS) CreateTemp(dir, pattern string) (tempFile, error) { return os.CreateTemp(dir, pattern) }
func (osFS) Rename(from, to string) error                     { return os.Rename(from, to) }
func (osFS) Remove(name string) error                         { return os.Remove(name) }
func (osFS) SyncDir(dir string) error                         { return syncDir(dir) }

// WriteFileAtomic replaces the file at path with data so that a reader, or a crash, sees the old
// content or the new, never a part: it writes a temporary file with mode 0600 in the same
// folder, makes it durable, and renames it over path (docs/SPEC.md §5.3). When it fails, the old
// file is untouched and the temporary file is removed.
func WriteFileAtomic(ctx context.Context, path string, data []byte) error {
	return writeFileAtomic(ctx, osFS{}, path, data)
}

// writeFileAtomic is WriteFileAtomic over a given file system.
func writeFileAtomic(ctx context.Context, fsys atomicFS, path string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return errors.NewErr(TagWriteCancelled, err).SetMetadata("path", path)
	}
	dir := filepath.Dir(path)
	tmp, err := fsys.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return errors.NewErr(TagWriteCreateFailed, err).SetMetadata("path", path)
	}
	// From here on, a failure removes the temporary file. The removal is best effort: its own
	// failure would hide the error that matters, and leaves only a hidden temporary file.
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = fsys.Remove(tmp.Name())
		return errors.NewErr(TagWriteFailed, err).SetMetadata("path", path)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = fsys.Remove(tmp.Name())
		return errors.NewErr(TagWriteSyncFailed, err).SetMetadata("path", path)
	}
	if err := tmp.Close(); err != nil {
		_ = fsys.Remove(tmp.Name())
		return errors.NewErr(TagWriteCloseFailed, err).SetMetadata("path", path)
	}
	if err := fsys.Rename(tmp.Name(), path); err != nil {
		_ = fsys.Remove(tmp.Name())
		return errors.NewErr(TagWriteRenameFailed, err).SetMetadata("path", path)
	}
	if err := fsys.SyncDir(dir); err != nil {
		return errors.NewErr(TagWriteDirSyncFailed, err).SetMetadata("path", path)
	}
	return nil
}

// ReadFile reads the file at path when it is at most maxBytes long. A file that does not exist
// is reported with an error that errors.Is matches with ErrNotExist; a longer file with the Tag
// FileTooLarge.
func ReadFile(ctx context.Context, path string, maxBytes int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.NewErr(TagReadCancelled, err).SetMetadata("path", path)
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, errors.NewErr(TagFileNotExist, err).SetMetadata("path", path)
	}
	if err != nil {
		return nil, errors.NewErr(TagFileOpenFailed, err).SetMetadata("path", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		_ = f.Close() // the read's error is the one that matters
		return nil, errors.NewErr(TagFileReadFailed, err).SetMetadata("path", path)
	}
	_ = f.Close() // a file that was only read loses nothing when its close fails
	if int64(len(data)) > maxBytes {
		return nil, errors.New(TagFileTooLarge, "the file is larger than the limit", "path", path, "maxBytes", maxBytes)
	}
	return data, nil
}
