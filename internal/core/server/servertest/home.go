package servertest

import (
	"os"
	"testing"
)

// homeDir returns the isolated home folder that storagetest.Isolate made for the test.
func homeDir(t testing.TB) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("servertest: %v", err)
	}
	return home
}
