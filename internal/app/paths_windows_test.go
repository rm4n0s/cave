package app

import "path/filepath"

// absolute makes a path that starts at the root, written as a Unix path, absolute on this system:
// it gets a drive.
func absolute(path string) string { return `C:` + filepath.FromSlash(path) }
