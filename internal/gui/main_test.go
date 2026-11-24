package gui

import (
	"os"
	"testing"
)

// TestMain takes the user's session out of reach of every test of this package: a desktop call
// that a test does not fake (a file dialog, a notification) must fail and never reach the user's
// own desktop, so the session bus is an address where nothing listens, and no display is named.
func TestMain(m *testing.M) {
	for name, value := range map[string]string{
		"DBUS_SESSION_BUS_ADDRESS": "unix:path=/nonexistent/cave-tests/no-bus",
		"WAYLAND_DISPLAY":          "",
		"DISPLAY":                  "",
	} {
		if err := os.Setenv(name, value); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}
