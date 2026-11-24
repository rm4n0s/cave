package storage

import (
	"path/filepath"

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
	runtime := tempRuntime(lookup)
	if dir := xdg("XDG_RUNTIME_DIR"); dir != "" {
		runtime = filepath.Join(dir, "cave")
	}
	return Dirs{Config: config, State: state, Runtime: runtime}, nil
}
