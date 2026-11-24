package icon

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rm4n0s/cave/internal/gui/testutil"
)

func TestIconGolden(t *testing.T) {
	testutil.MatchesGoldenImage(t, "icon-16", pixels(Draw(16)))
	testutil.MatchesGoldenImage(t, "icon-48", pixels(Draw(48)))
	testutil.MatchesGoldenImage(t, "icon-256", pixels(Draw(256)))
	testutil.MatchesGoldenImage(t, "tray-32-running", pixels(Tray(32, true)))
	testutil.MatchesGoldenImage(t, "tray-32-stopped", pixels(Tray(32, false)))
}

func TestTheIconIsSquareTransparentAtTheCornersAndOpaqueInside(t *testing.T) {
	for _, size := range append([]int{1, 2, 5}, Sizes...) {
		img := Draw(size)
		if b := img.Bounds(); b != image.Rect(0, 0, size, size) {
			t.Errorf("Draw(%d) is %v", size, b)
		}
	}
	for _, size := range Sizes {
		img := Draw(size)
		if a := img.RGBAAt(0, 0).A; a != 0 {
			t.Errorf("Draw(%d): the corner has the alpha %d, want it transparent", size, a)
		}
		mid := img.RGBAAt(size/2, size*3/4)
		if mid.A != 0xFF {
			t.Errorf("Draw(%d): the arch is not opaque: %v", size, mid)
		}
		// The arch is light and the body is dark: the icon has contrast at every size.
		body := img.RGBAAt(size/2, size/10+1)
		if int(mid.R)+int(mid.G)+int(mid.B) < int(body.R)+int(body.G)+int(body.B)+300 {
			t.Errorf("Draw(%d): the arch %v and the body %v do not differ enough", size, mid, body)
		}
	}
}

// pixels returns the picture as non-premultiplied colors, as a PNG file holds them.
func pixels(img image.Image) *image.NRGBA {
	out := image.NewNRGBA(img.Bounds())
	for y := out.Bounds().Min.Y; y < out.Bounds().Max.Y; y++ {
		for x := out.Bounds().Min.X; x < out.Bounds().Max.X; x++ {
			out.SetNRGBA(x, y, color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA))
		}
	}
	return out
}

func TestTheTrayIconShowsWhetherTheServerRuns(t *testing.T) {
	on, off := Tray(32, true), Tray(32, false)
	if string(on.Pix) == string(off.Pix) {
		t.Fatal("the tray icon is the same whether the server runs or not")
	}
	// The dot is green, at the lower right; without it that corner is the body's.
	dot := on.RGBAAt(24, 24)
	if dot.G <= dot.R+40 || dot.G <= dot.B+40 {
		t.Errorf("the dot is %v, want green", dot)
	}
	if off.RGBAAt(24, 24) != Draw(32).RGBAAt(24, 24) {
		t.Error("a stopped tray icon is not the plain icon")
	}
	// Away from the corner the two are the same.
	if on.RGBAAt(8, 8) != off.RGBAAt(8, 8) {
		t.Error("the running icon differs from the stopped one far from the dot")
	}
}

func TestTheGeneratedIconsAreCurrent(t *testing.T) {
	for _, size := range Sizes {
		path := filepath.Join("..", "..", "..", "packaging", "icons", "cave-"+strconv.Itoa(size)+".png")
		file, err := os.Open(path)
		if err != nil {
			t.Fatalf("%v (run go generate ./internal/gui/icon)", err)
		}
		got, err := png.Decode(file)
		_ = file.Close() // only read
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		want := pixels(Draw(size))
		have := pixels(got)
		if have.Bounds() != want.Bounds() || string(have.Pix) != string(want.Pix) {
			t.Errorf("%s is not what Draw(%d) draws: run go generate ./internal/gui/icon", path, size)
		}
	}
}
