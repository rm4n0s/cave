//go:build linux || darwin

package app

// absolute makes a path that starts at the root, written as a Unix path, absolute on this system.
func absolute(path string) string { return path }
