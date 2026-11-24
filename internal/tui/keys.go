package tui

import "strings"

// page is one of the five pages.
type page int

// The pages, in the order of the keys 1 to 5.
const (
	pageOverview page = iota
	pagePeople
	pageAttempts
	pageConnections
	pageSettings
	pageCount
)

// title is the page's name as the header shows it.
func (p page) title() string {
	switch p {
	case pageOverview:
		return "Overview"
	case pagePeople:
		return "People"
	case pageAttempts:
		return "Attempts"
	case pageConnections:
		return "Connections"
	case pageSettings:
		return "Settings"
	}
	return ""
}

// pageNamed returns the page that app.Pages calls name ("" and unknown names are the Overview).
func pageNamed(name string) page {
	for p := page(0); p < pageCount; p++ {
		if strings.ToLower(p.title()) == name {
			return p
		}
	}
	return pageOverview
}

// binding is one key, or one group of keys, and what it does.
type binding struct {
	keys, what string
}

// globalBindings are the keys that work on every page. The help overlay lists them all; the
// footer shows only the last two.
func globalBindings() []binding {
	return []binding{
		{"1-5", "go to a page"},
		{"tab", "next page"},
		{"shift+tab", "previous page"},
		{"s", "start or stop the server"},
		{"?", "show or hide the keys"},
		{"q", "quit"},
	}
}

// pageBindings are the keys of one page. The footer shows them first and the help overlay lists
// them under the page's name. Both are drawn from these lists, so they cannot disagree. The
// pages add their rows here as they are built.
func pageBindings(_ page) []binding {
	return nil
}
