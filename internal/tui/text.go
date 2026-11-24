package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

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

// clean makes pasted or typed text safe to put in one line: every control character (C0, DEL,
// C1), newline and tab becomes a space, and invalid UTF-8 becomes U+FFFD.
func clean(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			b.WriteRune('\uFFFD')
		case unicode.IsControl(r):
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
		i += size
	}
	return b.String()
}

// fitAlign is fit with the text at the right end of the cells when right is true: for numbers.
// Text that is too long is cut as fit cuts it.
func fitAlign(s string, width int, right bool) string {
	if !right || lipgloss.Width(s) >= width {
		return fit(s, width)
	}
	return strings.Repeat(" ", width-lipgloss.Width(s)) + s
}

// cellWidth is how many cells a character takes in the terminal.
func cellWidth(r rune) int {
	return lipgloss.Width(string(r))
}
