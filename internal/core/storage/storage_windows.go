package storage

import (
	"os"
	"path/filepath"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/windows"
)

// lockOffset is where the locked byte of the lock file is. A lock of Windows keeps other handles
// from reading the locked bytes, so it is far from the holder's line at the start of the file.
const lockOffset = 1 << 30

// resolvePlatform fills in the folders that were not given: Cave in %AppData% and in
// %LocalAppData%. Windows has no runtime folder; forwarded agents use named pipes.
func resolvePlatform(config, state string, lookup func(string) string) (Dirs, error) {
	if config == "" {
		appData := lookup("AppData")
		if !filepath.IsAbs(appData) {
			return Dirs{}, errors.New(TagAppDataUnknown, "%AppData% is not set, so Cave does not know where to keep its settings; use --config-dir")
		}
		config = filepath.Join(appData, "Cave")
	}
	if state == "" {
		local := lookup("LocalAppData")
		if !filepath.IsAbs(local) {
			return Dirs{}, errors.New(TagLocalAppDataUnknown, "%LocalAppData% is not set, so Cave does not know where to keep its state; use --state-dir")
		}
		state = filepath.Join(local, "Cave")
	}
	return Dirs{Config: config, State: state}, nil
}

// ensureRuntime does nothing: Windows has no runtime folder.
func ensureRuntime(string) error {
	return nil
}

// syncDir does nothing: Windows cannot flush a folder, and a rename is durable with the file.
func syncDir(string) error {
	return nil
}

// lockRange describes the one byte at lockOffset.
func lockRange() *windows.Overlapped {
	return &windows.Overlapped{Offset: lockOffset & 0xffffffff, OffsetHigh: lockOffset >> 32}
}

// tryLock takes an exclusive lock on one byte of the file without waiting.
func tryLock(f *os.File) (held bool, err error) {
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, lockRange())
	if err == windows.ERROR_LOCK_VIOLATION || err == windows.ERROR_IO_PENDING {
		return true, nil
	}
	return false, err
}

// unlock drops the lock on the byte.
func unlock(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, lockRange())
}
