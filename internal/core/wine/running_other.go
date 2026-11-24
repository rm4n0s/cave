//go:build !windows

package wine

// Running reports whether the program runs under Wine: never, off Windows.
func Running() bool { return false }
