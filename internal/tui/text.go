package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// fit cuts s to width terminal cells, with "…" as the last cell when it was longer, and pads it
// with spaces to exactly width cells. A cell is what the terminal draws: a wide character takes
// two and a combining character none. Styles in s are kept.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) > width {
		s = ansi.Truncate(s, width, "…")
	}
	if w := lipgloss.Width(s); w < width {
		s += strings.Repeat(" ", width-w)
	}
	return s
}
