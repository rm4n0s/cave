package gui

import (
	"strconv"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/glutter/foundation"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/rendering"
)

// wideFrom is the width, in logical pixels, from which the navigation is a rail on the left;
// below it, the pages are chosen with a bar at the bottom.
const wideFrom = 800.0

// The indexes of the pages, in the order of app.Pages.
const (
	pageOverview = iota
	pagePeople
	pageAttempts
	pageConnections
	pageSettings
)

// destination is one page in the navigation.
type destination struct {
	label string
	icon  func() m.Widget
}

// destinations are the pages, in the order of app.Pages.
var destinations = [...]destination{
	{"Overview", func() m.Widget { return m.NewIcon(m.Icons.Dashboard()) }},
	{"People", func() m.Widget { return m.NewIcon(m.Icons.People()) }},
	{"Attempts", func() m.Widget { return m.NewIcon(m.Icons.History()) }},
	{"Connections", func() m.Widget { return m.NewIcon(m.Icons.Lan()) }},
	{"Settings", func() m.Widget { return m.NewIcon(m.Icons.Settings()) }},
}

// shell is the scaffold of the window: the navigation and the current page.
type shell struct {
	m.StatelessWidget
}

// newShell makes the shell.
func newShell() *shell { return &shell{StatelessWidget: m.NewStatelessWidgetBase()} }

// Build chooses the rail or the bar by the width the window has.
func (s *shell) Build(bc m.BuildContext) m.Widget {
	v := viewOf(bc)
	return m.NewLayoutBuilder(func(_ m.BuildContext, c rendering.BoxConstraints) m.Widget {
		page := pageFor(v)
		if c.MaxWidth >= wideFrom {
			return m.NewScaffold(m.ScaffoldOpts{
				Body: m.NewRow([]m.Widget{
					m.NewNavigationRail(railDestinations(v), foundation.Some(v.page), m.NavigationRailOpts{
						LabelType:             m.NavigationRailLabelTypeAll,
						OnDestinationSelected: v.selectPage,
					}),
					m.NewVerticalDivider(m.VerticalDividerOpts{Width: foundation.Some(1.0)}),
					m.NewExpanded(page),
				}, m.RowOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch}),
			})
		}
		return m.NewScaffold(m.ScaffoldOpts{
			Body: page,
			BottomNavigationBar: m.NewNavigationBar(barDestinations(v), m.NavigationBarOpts{
				SelectedIndex:         v.page,
				OnDestinationSelected: v.selectPage,
			}),
		})
	})
}

// icon returns the icon of a page, with the number of connections on the Connections one when it
// is not 0.
func icon(v *view, page int) m.Widget {
	ic := destinations[page].icon()
	if app.Pages[page] == "connections" && len(v.connections) > 0 {
		return m.NewBadge(ic, m.BadgeOpts{
			Key:   foundation.NewKey("connections-badge"),
			Label: m.NewText(strconv.Itoa(len(v.connections))),
		})
	}
	return ic
}

func railDestinations(v *view) []m.NavigationRailDestination {
	out := make([]m.NavigationRailDestination, len(destinations))
	for i, d := range destinations {
		out[i] = m.NewNavigationRailDestination(icon(v, i), m.NewText(d.label))
	}
	return out
}

func barDestinations(v *view) []m.Widget {
	out := make([]m.Widget, len(destinations))
	for i, d := range destinations {
		out[i] = m.NewNavigationDestination(icon(v, i), d.label)
	}
	return out
}

// pageFor returns the widget of the page the view is on. A page that has no widget yet is its
// name as a headline.
func pageFor(v *view) m.Widget {
	switch v.page {
	case pageOverview:
		return newOverviewPage(v)
	case pagePeople:
		return newPeoplePage(v)
	case pageAttempts:
		return newAttemptsPage(v)
	}
	return newHeadline(destinations[v.page].label)
}

// headline is the name of a page as a headline, the whole content of a page until its step.
type headline struct {
	m.StatelessWidget
	text string
}

func newHeadline(text string) *headline {
	return &headline{StatelessWidget: m.NewStatelessWidgetBase(), text: text}
}

// Build draws the text in the headline style.
func (h *headline) Build(bc m.BuildContext) m.Widget {
	return m.NewCenter(m.NewText(h.text, m.TextOpts{Style: m.ThemeOf(bc).TextTheme.HeadlineMedium}))
}
