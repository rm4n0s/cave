// Package icon draws Cave's icon with the standard library only (no font, no file): a rounded
// square in the theme's seed color with a light arch, a cave's mouth, and a small keyhole in it.
// Every pixel is anti-aliased by the share of it that the shapes cover. Tray draws the same icon
// with a green dot when the server runs. The packaging's PNG files are drawn by it
// (`go generate ./internal/gui/icon`), and the window and the tray use it directly.
package icon

//go:generate go run gen.go

import (
	"image"
	"image/color"
	"math"
)

// The colors of the icon. Seed is the seed color of the themes (internal/gui/theme.go).
var (
	seed    = color.NRGBA{R: 0x4A, G: 0x65, B: 0x72, A: 0xFF}
	arch    = color.NRGBA{R: 0xEC, G: 0xF1, B: 0xF3, A: 0xFF}
	running = color.NRGBA{R: 0x2E, G: 0x7D, B: 0x32, A: 0xFF}
)

// Sizes are the sizes of the PNG files of the packaging, in pixels.
var Sizes = []int{16, 32, 48, 64, 128, 256}

// samples is how many points of a pixel are tried in each direction to find how much of it a shape
// covers: 8 by 8, so 64 levels of coverage.
const samples = 8

// A shape says whether a point of the unit square (0 to 1 in each direction, y downwards) is in it.
type shape func(x, y float64) bool

// roundedSquare is the body of the icon: the unit square with a small margin and round corners.
func roundedSquare(x, y float64) bool {
	const margin, radius = 0.03, 0.22
	lo, hi := margin+radius, 1-margin-radius
	cx, cy := math.Min(math.Max(x, lo), hi), math.Min(math.Max(y, lo), hi)
	return math.Hypot(x-cx, y-cy) <= radius && x >= margin && x <= 1-margin && y >= margin && y <= 1-margin
}

// mouth is the arch: a half circle on a rectangle, as wide as half of the icon.
func mouth(x, y float64) bool {
	const left, right, top, bottom, radius = 0.25, 0.75, 0.50, 0.80, 0.25
	switch {
	case y > bottom || x < left || x > right:
		return false
	case y >= top:
		return true
	}
	return math.Hypot(x-0.5, y-top) <= radius
}

// keyhole is a small disc with a slot under it, in the arch.
func keyhole(x, y float64) bool {
	if math.Hypot(x-0.5, y-0.52) <= 0.075 {
		return true
	}
	// The slot widens a little downwards.
	if y < 0.53 || y > 0.69 {
		return false
	}
	half := 0.025 + (y-0.53)*0.06
	return math.Abs(x-0.5) <= half
}

// dot returns a disc at the lower right corner of the icon, of the radius.
func dot(radius float64) shape {
	return func(x, y float64) bool { return math.Hypot(x-0.77, y-0.77) <= radius }
}

// coverage returns the share of the pixel (px, py) of an image of the size that the shape covers.
func coverage(s shape, px, py, size int) float64 {
	hit := 0
	for sy := 0; sy < samples; sy++ {
		for sx := 0; sx < samples; sx++ {
			x := (float64(px) + (float64(sx)+0.5)/samples) / float64(size)
			y := (float64(py) + (float64(sy)+0.5)/samples) / float64(size)
			if s(x, y) {
				hit++
			}
		}
	}
	return float64(hit) / (samples * samples)
}

// A layer is a shape and its color; layers are painted in order, each over the ones before.
type layer struct {
	shape shape
	color color.NRGBA
}

// mix returns a color part of the way from a to b.
func mix(a, b [3]float64, part float64) [3]float64 {
	return [3]float64{a[0] + (b[0]-a[0])*part, a[1] + (b[1]-a[1])*part, a[2] + (b[2]-a[2])*part}
}

// render paints the layers into an image of the size. The first layer is the body: its coverage
// is the alpha of the pixel, so the corners are transparent and their edge is smooth. Each
// layer after it is painted over what is below by its own coverage.
func render(size int, layers []layer) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			alpha := coverage(layers[0].shape, x, y, size)
			if alpha == 0 {
				continue
			}
			rgb := [3]float64{float64(layers[0].color.R), float64(layers[0].color.G), float64(layers[0].color.B)}
			for _, l := range layers[1:] {
				if part := coverage(l.shape, x, y, size); part > 0 {
					rgb = mix(rgb, [3]float64{float64(l.color.R), float64(l.color.G), float64(l.color.B)}, part)
				}
			}
			// An image.RGBA holds the color multiplied by the alpha.
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(math.Round(rgb[0] * alpha)), G: uint8(math.Round(rgb[1] * alpha)),
				B: uint8(math.Round(rgb[2] * alpha)), A: uint8(math.Round(alpha * 255)),
			})
		}
	}
	return img
}

// Draw draws Cave's icon at size by size pixels. It reads at 16 pixels and at 256.
func Draw(size int) *image.RGBA {
	return render(size, []layer{{roundedSquare, seed}, {mouth, arch}, {keyhole, seed}})
}

// Tray draws the icon of the tray at size by size pixels: the icon, with a green dot at the
// corner while the server runs.
func Tray(size int, serverRuns bool) *image.RGBA {
	layers := []layer{{roundedSquare, seed}, {mouth, arch}, {keyhole, seed}}
	if serverRuns {
		// A light ring under the dot keeps it apart from the arch and the body.
		layers = append(layers, layer{dot(0.215), arch}, layer{dot(0.165), running})
	}
	return render(size, layers)
}
