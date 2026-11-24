package gui

import (
	"context"

	"github.com/rm4n0s/glutter/foundation"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/services"
)

// maxContentWidth is the widest a page's column grows on a wide window, in logical pixels.
const maxContentWidth = 720.0

// gap is empty space of a height.
func gap(height float64) m.Widget {
	return m.NewSizedBox(nil, m.SizedBoxOpts{Height: foundation.Some(height)})
}

// hgap is empty space of a width.
func hgap(width float64) m.Widget {
	return m.NewSizedBox(nil, m.SizedBoxOpts{Width: foundation.Some(width)})
}

// monoStyle is the style with the system's monospace font, for fingerprints and commands. A
// text in it is drawn as it is.
func monoStyle(style *m.TextStyle) *m.TextStyle {
	return style.CopyWith(m.TextStyleCopy{FontFamily: "monospace"})
}

// card is the card every page uses for a group of things that belong together.
func card(children []m.Widget, key ...foundation.Key) m.Widget {
	var k foundation.Key
	if len(key) > 0 {
		k = key[0]
	}
	return m.NewCard(
		m.NewPadding(m.NewEdgeInsetsAll(16), m.NewColumn(children, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStart})),
		m.CardOpts{Key: k, Margin: m.NewEdgeInsetsAll(0)})
}

// column is the body of a page: a scroll view with a column of the children that is at most
// maxContentWidth wide, in the middle.
func column(children []m.Widget) m.Widget {
	// The height is the child's: a Center in a scroll view, whose height has no end, would be
	// infinitely high and put its child at an offset that is not a number.
	return m.NewSingleChildScrollView(m.NewCenter(m.NewConstrainedBox(
		m.NewBoxConstraints(m.BoxConstraintsOpts{MaxWidth: foundation.Some(maxContentWidth)}),
		m.NewPadding(m.NewEdgeInsetsAll(16), m.NewColumn(children, m.ColumnOpts{
			CrossAxisAlignment: m.CrossAxisAlignmentStretch,
		}))), m.CenterOpts{HeightFactor: foundation.Some(1.0)}))
}

// showSnack shows a message at the bottom of the window.
func showSnack(bc m.BuildContext, text string) {
	m.ScaffoldMessengerOf(bc).ShowSnackBar(m.NewSnackBar(m.NewText(text)))
}

// copyText puts the text on the clipboard and says so. ctx ends the request when the widget that
// asked goes away.
func copyText(bc m.BuildContext, ctx context.Context, text string) {
	m.WidgetsBindingOf(bc).Clipboard().SetData(ctx, services.NewClipboardData(text), func(err error) {
		if err != nil {
			showSnack(bc, "Could not copy: "+err.Error())
			return
		}
		showSnack(bc, "Copied")
	})
}

// copyButton is a small button that copies the text.
func copyButton(bc m.BuildContext, ctx context.Context, key string, text string) m.Widget {
	return m.NewIconButton(func() { copyText(bc, ctx, text) }, m.NewIcon(m.Icons.ContentCopy()), m.IconButtonOpts{
		Key: foundation.NewKey(key), Tooltip: "Copy",
	})
}
