package gui

import (
	"image"
	"image/color"
	"log/slog"
	"math"
	"sync"

	"github.com/rm4n0s/glutter/desktop"
	"github.com/rm4n0s/glutter/ui"
)

// trayIconSize is the size of the picture the tray icon is drawn at; the tray scales it.
const trayIconSize = 32

// The colors of the icon: green while the server runs, grey while it does not, and a white key.
var (
	trayRunning = color.NRGBA{R: 0x2E, G: 0x7D, B: 0x32, A: 0xFF}
	trayStopped = color.NRGBA{R: 0x75, G: 0x75, B: 0x75, A: 0xFF}
	trayKey     = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
)

// trayIcon draws the icon of the tray: a rounded square with a keyhole, green when the server
// runs and grey when it does not. It is drawn here, so that Cave has no image file; step 8.2 puts
// the real icon in its place.
func trayIcon(running bool) image.Image {
	fill := trayStopped
	if running {
		fill = trayRunning
	}
	img := image.NewNRGBA(image.Rect(0, 0, trayIconSize, trayIconSize))
	const radius = 7.0
	for y := 0; y < trayIconSize; y++ {
		for x := 0; x < trayIconSize; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			switch {
			case inRoundedSquare(px, py, trayIconSize, radius):
				if inKeyhole(px, py) {
					img.SetNRGBA(x, y, trayKey)
				} else {
					img.SetNRGBA(x, y, fill)
				}
			}
		}
	}
	return img
}

// inRoundedSquare says whether the point is inside a square of the size with rounded corners.
func inRoundedSquare(x, y, size, radius float64) bool {
	// The distance to the nearest corner circle's centre, for a point in a corner.
	cx := math.Min(math.Max(x, radius), size-radius)
	cy := math.Min(math.Max(y, radius), size-radius)
	return math.Hypot(x-cx, y-cy) <= radius
}

// inKeyhole says whether the point is in the keyhole: a disc with a slot below it.
func inKeyhole(x, y float64) bool {
	if math.Hypot(x-16, y-13) <= 4.5 {
		return true
	}
	return x >= 14 && x <= 18 && y >= 14 && y <= 24
}

// trayWindowIcon is the icon of the tray in the form the tray takes.
var trayWindowIcon = func() func(running bool) ui.WindowIcon {
	// The two icons are made once, when first asked for: drawing and scaling them is not free, and the
	// menu is built again with every change of the application.
	build := func(running bool) func() ui.WindowIcon {
		return sync.OnceValue(func() ui.WindowIcon {
			pix := trayIcon(running).(*image.NRGBA).Pix
			icon, err := desktop.TrayIconFromRGBA(trayIconSize, pix)
			if err != nil {
				// The picture is made here with the right length, so this cannot happen; if it does,
				// the tray has no picture and the rest of Cave goes on.
				slog.Error("the tray icon could not be made", "err", err)
				return ui.WindowIcon{}
			}
			return icon
		})
	}
	on, off := build(true), build(false)
	return func(running bool) ui.WindowIcon {
		if running {
			return on()
		}
		return off()
	}
}()
