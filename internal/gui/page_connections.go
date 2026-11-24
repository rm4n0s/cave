package gui

import (
	"context"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/glutter/foundation"
	m "github.com/rm4n0s/glutter/material"
)

// disconnectAllKey is the key of the button that disconnects everybody.
var disconnectAllKey = foundation.NewKey("disconnect-all")

func disconnectKey(id string) foundation.Key { return foundation.NewKey("disconnect-" + id) }

// connectionsPage is the fourth page (docs/SPEC.md §11.4): who is connected now.
type connectionsPage struct {
	m.StatelessWidget
	v *view
}

func newConnectionsPage(v *view) *connectionsPage {
	return &connectionsPage{StatelessWidget: m.NewStatelessWidgetBase(), v: v}
}

// Build shows the button that disconnects everybody and one card per connection.
func (p *connectionsPage) Build(bc m.BuildContext) m.Widget {
	v := p.v
	theme := m.ThemeOf(bc)
	var onAll func()
	if len(v.connections) > 0 {
		onAll = func() { askDisconnectAll(bc, v) }
	}
	children := []m.Widget{
		m.NewRow([]m.Widget{
			m.NewFilledButton(onAll, m.NewText("Disconnect all"), m.FilledButtonOpts{Key: disconnectAllKey}),
		}),
		gap(16),
	}
	if len(v.connections) == 0 {
		children = append(children, gap(32), m.NewCenter(m.NewText("Nobody is connected.", m.TextOpts{Style: theme.TextTheme.HeadlineSmall})))
	}
	for _, c := range v.connections {
		children = append(children, connectionCard(bc, v, c), gap(8))
	}
	return column(children)
}

// connectionCard is one connection: who, from where, since when, what is open, and the button.
func connectionCard(bc m.BuildContext, v *view, c server.Connection) m.Widget {
	theme := m.ThemeOf(bc)
	return m.NewCard(m.NewPadding(m.NewEdgeInsetsAll(12), m.NewRow([]m.Widget{
		m.NewExpanded(m.NewColumn([]m.Widget{
			m.NewText(c.PersonName, m.TextOpts{Style: theme.TextTheme.TitleMedium}),
			m.NewText(c.Address+" · Since "+app.Ago(v.now, c.Since), m.TextOpts{
				Key: foundation.NewKey("since-" + c.ID), Style: theme.TextTheme.BodyMedium,
			}),
			m.NewText(app.ActivitiesText(c.Activities), m.TextOpts{Style: theme.TextTheme.BodyMedium}),
		}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStart})),
		hgap(12),
		m.NewOutlinedButton(func() { askDisconnect(bc, v, c) }, m.NewText("Disconnect"), m.OutlinedButtonOpts{Key: disconnectKey(c.ID)}),
	})), m.CardOpts{Margin: m.NewEdgeInsetsAll(0)})
}

// askDisconnect asks, and closes the connection.
func askDisconnect(bc m.BuildContext, v *view, c server.Connection) {
	answer := confirm(bc, "Disconnect", "Disconnect "+c.PersonName+" ("+c.Address+")?", "Disconnect", true)
	waitYes(bc, answer, func() {
		runCommand(bc, v, func(ctx context.Context) error { return v.app.Disconnect(ctx, c.ID) })
	})
}

// askDisconnectAll asks, and closes every connection.
func askDisconnectAll(bc m.BuildContext, v *view) {
	text := "Disconnect the connection?"
	if n := len(v.connections); n > 1 {
		text = "Disconnect all " + plural(n, "connection") + "?"
	}
	answer := confirm(bc, "Disconnect all", text, "Disconnect", true)
	waitYes(bc, answer, func() {
		runCommand(bc, v, func(context.Context) error {
			v.app.DisconnectAll()
			return nil
		})
	})
}
