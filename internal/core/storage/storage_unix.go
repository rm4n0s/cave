//go:build linux || darwin

package storage

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/rm4n0s/errors"
)

// tempRuntime is the runtime folder under the temporary folder (TMPDIR, else /tmp). The
// temporary folder may be shared with other users, so the name says whose it is, and
// ensureRuntime refuses a folder that somebody else made.
func tempRuntime(lookup func(string) string) string {
	tmp := lookup("TMPDIR")
	if !filepath.IsAbs(tmp) {
		tmp = "/tmp"
	}
	return filepath.Join(tmp, "cave-"+strconv.Itoa(os.Geteuid()))
}

// ensureRuntime creates the runtime folder with mode 0700 when it does not exist, and refuses
// one that is not a real folder of this user closed to everybody else: forwarded agents' sockets
// live in it. Dirs without a runtime folder have nothing to ensure.
func ensureRuntime(dir string) error {
	if dir == "" {
		return nil
	}
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return errors.NewErr(TagRuntimeDirCreateFailed, err).SetMetadata("dir", dir)
		}
		return nil
	}
	if err != nil {
		return errors.NewErr(TagRuntimeDirStatFailed, err).SetMetadata("dir", dir)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 || !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New(TagRuntimeDirUnsafe, "the runtime folder is not a folder of this user with mode 0700",
			"dir", dir, "mode", info.Mode().String())
	}
	return nil
}

// syncDir makes the entries of a folder durable.
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close() // the sync's error is the one that matters
		return err
	}
	return f.Close()
}

// tryLock takes an exclusive flock on the file without waiting.
func tryLock(f *os.File) (held bool, err error) {
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == syscall.EWOULDBLOCK {
		return true, nil
	}
	return false, err
}

// unlock drops the flock.
func unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
