package pty

import (
	"testing"

	"github.com/rm4n0s/cave/internal/core/errtest"
)

func TestPseudoTerminalsAreNotBuiltForWindowsYet(t *testing.T) {
	_, err := Open(Size{Cols: 80, Rows: 24}, nil)
	errtest.Route(t, err, "Open.PtyUnsupported")
}
