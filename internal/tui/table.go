package tui

import "strings"

// minFlexWidth is the least a flexible column is given.
const minFlexWidth = 4

// column is one column of a table: a title, and either a width in cells (fixed) or a share of
// what is left (flex). A right column puts its text at the right end, for numbers.
type column struct {
	title string
	width int
	flex  bool
	right bool
}

// table is a table of text with a title line. It draws rows that a page owns.
type table struct {
	columns []column
	rows    [][]string
	// empty is what is drawn instead of rows when there are none.
	empty string
}

// widths returns the width of each column when the columns have total cells with one space
// between them: fixed columns keep theirs, flexible ones share the rest in equal parts (the last
// has what does not divide), none under minFlexWidth.
func (t table) widths(total int) []int {
	widths := make([]int, len(t.columns))
	fixed, flexible, last := 0, 0, -1
	for i, c := range t.columns {
		if c.flex {
			flexible++
			last = i
		} else {
			widths[i] = c.width
			fixed += c.width
		}
	}
	if flexible == 0 {
		return widths
	}
	rest := total - fixed - (len(t.columns) - 1)
	share := max(rest/flexible, minFlexWidth)
	for i, c := range t.columns {
		if c.flex {
			widths[i] = share
		}
	}
	if rest >= minFlexWidth*flexible {
		widths[last] += rest - share*flexible
	}
	return widths
}

// line draws the cells of one row, cut to the widths.
func (t table) line(cells []string, widths []int) string {
	parts := make([]string, len(t.columns))
	for i, c := range t.columns {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		parts[i] = fitAlign(cell, widths[i], c.right)
	}
	return strings.Join(parts, " ")
}

// view draws the table in exactly height lines of width cells: the title line, then the rows
// that the list shows, the selected one marked with ">" and reversed. Text that does not fit its
// column ends with "…"; nothing wraps.
func (t table) view(width, height int, l list, st styles) []string {
	if height <= 0 {
		return nil
	}
	const prefix = 2 // "> " or two spaces
	widths := t.widths(width - prefix)
	lines := make([]string, 0, height)
	lines = append(lines, st.title.Render(fit("  "+t.line(titles(t.columns), widths), width)))
	if len(t.rows) == 0 {
		if height > 1 {
			lines = append(lines, st.dim.Render(fit("  "+t.empty, width)))
		}
	} else {
		first, last := l.window(height - 1)
		for i := first; i < last; i++ {
			text := t.line(t.rows[i], widths)
			if i == l.selected {
				lines = append(lines, st.current.Render(fit("> "+text, width)))
			} else {
				lines = append(lines, fit("  "+text, width))
			}
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

// titles returns the titles of the columns, right-aligned ones included.
func titles(columns []column) []string {
	out := make([]string, len(columns))
	for i, c := range columns {
		out[i] = c.title
	}
	return out
}
