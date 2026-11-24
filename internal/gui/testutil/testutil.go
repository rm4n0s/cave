// Package testutil holds what the tests of the desktop UI share. It does not import
// internal/gui: the tests of internal/gui import it.
package testutil

import (
	"bytes"
	"flag"
	"image"
	"image/png"
	"os"
	"path/filepath"
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

// MatchesGoldenImage compares a picture that is not a screenshot (an icon) with the golden
// testdata/goldens/<name>.png of the package under test, pixel for pixel. With -update-goldens, the
// flag that gluttertest defines for its own goldens, it writes the picture instead; the changed
// file is then looked at, as every golden is.
func MatchesGoldenImage(t testing.TB, name string, img image.Image) {
	t.Helper()
	path := filepath.Join("testdata", "goldens", name+".png")
	if flagValue("update-goldens") {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated golden %s", path)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("the golden %s cannot be read: %v (run with -update-goldens to make it)", path, err)
	}
	defer func() { _ = file.Close() }()
	want, err := png.Decode(file)
	if err != nil {
		t.Fatalf("the golden %s is not a PNG: %v", path, err)
	}
	if want.Bounds() != img.Bounds() {
		t.Fatalf("the golden %s is %v, the picture is %v", path, want.Bounds(), img.Bounds())
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			wr, wg, wb, wa := want.At(x, y).RGBA()
			gr, gg, gb, ga := img.At(x, y).RGBA()
			if wr != gr || wg != gg || wb != gb || wa != ga {
				t.Fatalf("the picture differs from the golden %s at (%d, %d)", path, x, y)
			}
		}
	}
}

// flagValue reads a boolean flag of the test binary by name.
func flagValue(name string) bool {
	f := flag.Lookup(name)
	return f != nil && f.Value.String() == "true"
}
