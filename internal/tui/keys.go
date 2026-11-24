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
		{"up/down pgup/pgdn", "move in a list"},
		{"s", "start or stop the server"},
		{"?", "show or hide the keys"},
		{"q", "quit"},
	}
}

// pageBindings are the keys of one page. The footer shows them first and the help overlay lists
// them under the page's name. Both are drawn from these lists, so they cannot disagree. The
// pages add their rows here as they are built.
func pageBindings(p page) []binding {
	switch p {
	case pageOverview:
		return []binding{{"s", "start or stop the server"}}
	case pagePeople:
		return []binding{{"enter", "open"}, {"a", "add"}, {"e", "rename"}, {"space", "on/off"}, {"d", "remove"}}
	case pageAttempts:
		return []binding{{"f", "filter"}, {"/", "search"}, {"enter", "show"}, {"d", "remove"}, {"C", "clear"}}
	case pageConnections, pageSettings:
		return nil
	}
	return nil
}

// ownBindings are the keys among all that do not work on every page: what the help lists under
// the page's name (the footer shows all of them).
func ownBindings(all []binding) []binding {
	global := map[string]bool{}
	for _, b := range globalBindings() {
		global[b.keys] = true
	}
	var rows []binding
	for _, b := range all {
		if !global[b.keys] {
			rows = append(rows, b)
		}
	}
	return rows
}

// personBindings are the keys of the People page when a person is open.
func personBindings() []binding {
	return []binding{{"a", "add"}, {"i", "import"}, {"c", "comment"}, {"d", "remove"}, {"esc", "back"}}
}

// pageKeys are the keys of what the current page shows now: a person's keys on the People page
// when one is open.
func (m Model) pageKeys() []binding {
	if m.page == pagePeople && m.personID != "" {
		return personBindings()
	}
	if m.page == pageAttempts && m.attemptDetail {
		return []binding{{"esc", "back"}}
	}
	return pageBindings(m.page)
}

// keysTitle names what pageKeys are the keys of, for the help.
func (m Model) keysTitle() string {
	if m.page == pagePeople && m.personID != "" {
		return "a person"
	}
	if m.page == pageAttempts && m.attemptDetail {
		return "an attempt"
	}
	return m.page.title()
}
