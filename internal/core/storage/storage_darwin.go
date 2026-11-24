package storage

import (
	"path/filepath"

	"github.com/rm4n0s/errors"
)

// resolvePlatform fills in the folders that were not given, in the places of macOS: the
// configuration in ~/Library/Application Support/Cave, the state in its folder State, and the
// runtime folder under the temporary folder, which macOS makes for each user (docs/SPEC.md §5.1).
func resolvePlatform(config, state string, lookup func(string) string) (Dirs, error) {
	home := lookup("HOME")
	support := ""
	if filepath.IsAbs(home) {
		support = filepath.Join(home, "Library", "Application Support", "Cave")
	}
	if config == "" {
		config = support
	}
	if state == "" && support != "" {
		state = filepath.Join(support, "State")
	}
	if config == "" || state == "" {
		return Dirs{}, errors.New(TagHomeUnknown, "HOME is not set, so Cave does not know where to keep its files; use --config-dir and --state-dir")
	}
	return Dirs{Config: config, State: state, Runtime: tempRuntime(lookup)}, nil
}
