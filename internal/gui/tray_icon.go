package gui

import (
	"image"
	"image/draw"
	"log/slog"
	"sync"

	"github.com/rm4n0s/cave/internal/gui/icon"
	"github.com/rm4n0s/glutter/desktop"
	"github.com/rm4n0s/glutter/ui"
)

// trayIconSize is the size of the picture the tray icon is drawn at; the tray scales it.
const trayIconSize = 32

// nonPremultiplied returns the picture as the non-premultiplied colors that Glutter's icons take.
func nonPremultiplied(img image.Image) *image.NRGBA {
	out := image.NewNRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	return out
}

// trayIcon draws the icon of the tray: Cave's icon, with a green dot while the server runs.
func trayIcon(running bool) image.Image {
	return nonPremultiplied(icon.Tray(trayIconSize, running))
}

// windowIcon is the icon of the window: Cave's icon in every size of the packaging, and its name
// in the icon theme, which a desktop that has installed it (packaging/cave.desktop) prefers.
func windowIcon() ui.WindowIcon {
	win := ui.WindowIcon{Name: appID}
	for _, size := range icon.Sizes {
		win.Images = append(win.Images, ui.WindowIconImage{Size: size, RGBA: nonPremultiplied(icon.Draw(size)).Pix})
	}
	return win
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
