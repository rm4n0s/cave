package tui

// defaultPage is how far PgUp and PgDn move before a page tells the list its height.
const defaultPage = 10

// list is the selection of a scrolling list of rows: how many rows there are, which is selected
// and which is the first one shown. It draws nothing; a page draws its rows with window.
type list struct {
	n, selected, first int
	// height is how many rows the page shows, as the page last told it with setHeight.
	height int
}

// setLen says how many rows there are and keeps the selection inside them.
func (l *list) setLen(n int) {
	l.n = max(n, 0)
	l.selected = max(min(l.selected, l.n-1), 0)
	l.first = l.scrolled(l.first, l.pageSize())
}

// setHeight says how many rows the page shows.
func (l *list) setHeight(h int) {
	l.height = max(h, 0)
	l.first = l.scrolled(l.first, l.pageSize())
}

// pageSize is how many rows PgUp and PgDn move: a page less one, so that one row stays in sight.
func (l list) pageSize() int {
	if l.height > 0 {
		return l.height
	}
	return defaultPage
}

// key moves the selection for up and k, down and j, pgup, pgdown, home and end, and says whether
// the key was one of them.
func (l *list) key(name string) bool {
	switch name {
	case "up", "k":
		l.selected--
	case "down", "j":
		l.selected++
	case "pgup":
		l.selected -= max(l.pageSize()-1, 1)
	case "pgdown":
		l.selected += max(l.pageSize()-1, 1)
	case "home":
		l.selected = 0
	case "end":
		l.selected = l.n - 1
	default:
		return false
	}
	l.selected = max(min(l.selected, l.n-1), 0)
	l.first = l.scrolled(l.first, l.pageSize())
	return true
}

// selectIndex selects a row.
func (l *list) selectIndex(i int) {
	l.selected = max(min(i, l.n-1), 0)
	l.first = l.scrolled(l.first, l.pageSize())
}

// scrolled returns the first row to show, given the one that was first, so that the selected row
// is shown among height rows and the window does not stop short of the end.
func (l list) scrolled(first, height int) int {
	if height <= 0 {
		return 0
	}
	if l.selected < first {
		first = l.selected
	}
	if l.selected >= first+height {
		first = l.selected - height + 1
	}
	return max(min(first, l.n-height), 0)
}

// window returns the rows to draw when height rows fit: from first up to, but not including,
// last. The selected row is among them. It changes nothing.
func (l list) window(height int) (first, last int) {
	first = l.scrolled(l.first, height)
	return first, min(first+max(height, 0), l.n)
}
