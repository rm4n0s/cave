//go:build !darwin

package session

// directoryShell asks the system's directory service for the shell of the account: only macOS
// has one that Cave reads, so elsewhere the shell is not known.
func directoryShell(string) string { return "" }
