package storage

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/rm4n0s/errors"
)

// resolvePlatform fills in the folders that were not given, by the XDG base directory
// specification: a variable that is empty or not an absolute path is ignored.
func resolvePlatform(config, state string, lookup func(string) string) (Dirs, error) {
	xdg := func(name string) string {
		if dir := lookup(name); filepath.IsAbs(dir) {
			return dir
		}
		return ""
	}
	home := lookup("HOME")
	if config == "" {
		switch dir := xdg("XDG_CONFIG_HOME"); {
		case dir != "":
			config = filepath.Join(dir, "cave")
		case filepath.IsAbs(home):
			config = filepath.Join(home, ".config", "cave")
		}
	}
	if state == "" {
		switch dir := xdg("XDG_STATE_HOME"); {
		case dir != "":
			state = filepath.Join(dir, "cave")
		case filepath.IsAbs(home):
			state = filepath.Join(home, ".local", "state", "cave")
		}
	}
	if config == "" || state == "" {
		return Dirs{}, errors.New(TagHomeUnknown, "HOME is not set, so Cave does not know where to keep its files; use --config-dir and --state-dir")
	}
	var runtime string
	if dir := xdg("XDG_RUNTIME_DIR"); dir != "" {
		runtime = filepath.Join(dir, "cave")
	} else {
		tmp := lookup("TMPDIR")
		if !filepath.IsAbs(tmp) {
			tmp = "/tmp"
		}
		// The temporary folder is shared with other users, so the name says whose it is, and
		// ensureRuntime refuses a folder that somebody else made.
		runtime = filepath.Join(tmp, "cave-"+strconv.Itoa(os.Geteuid()))
	}
	return Dirs{Config: config, State: state, Runtime: runtime}, nil
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
