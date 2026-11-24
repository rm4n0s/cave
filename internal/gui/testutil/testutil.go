// Package testutil holds what the tests of the desktop UI share. It does not import
// internal/gui: the tests of internal/gui import it.
package testutil

import (
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
)

// Wide and Narrow are the sizes of the goldens: Wide shows the navigation rail and Narrow, which
// is below 800 logical pixels, the bar at the bottom.
var (
	Wide   = ui.Size{Width: 960, Height: 640}
	Narrow = ui.Size{Width: 720, Height: 480}
)

// NewTester makes a tester of the given size. It draws the text with the fonts that Glutter
// bundles (Roboto and the Material icons) and never with the system's, so that a golden is the
// same on every machine, and its clock is a fake one that Pump moves.
func NewTester(t testing.TB, size ui.Size) *gluttertest.Tester {
	t.Helper()
	return gluttertest.New(t, gluttertest.Opts{Size: size})
}
