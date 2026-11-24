package session

import (
	"testing"

	"github.com/rm4n0s/cave/internal/core/errtest"
)

func TestSessionsAreNotBuiltForWindowsYet(t *testing.T) {
	_, err := newCommand(`C:\Windows\System32\cmd.exe`, "ver", false, false)
	errtest.Route(t, err, "newCommand.SessionUnsupported")
}
