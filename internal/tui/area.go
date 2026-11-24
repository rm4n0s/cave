package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// The limits of an area: text beyond them is not taken.
const (
	areaMaxLines = 200
	areaMaxBytes = 16 << 10
)

// area is a multi-line text input, for pasting public keys: a list of lines and a cursor in it.
type area struct {
	lines [][]rune
	// row and col are the cursor's line and place in it.
	row, col int
}

// newArea makes an empty area.
func newArea() area {
	return area{lines: [][]rune{nil}}
}

// text returns the lines joined with newlines.
func (a area) text() string {
	parts := make([]string, len(a.lines))
	for i, l := range a.lines {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

// size is how many bytes the text has, newlines included.
func (a area) size() int {
	n := len(a.lines) - 1
	for _, l := range a.lines {
		n += len(string(l))
	}
	return n
}

// insert puts text at the cursor. A newline starts a new line (a carriage return counts as one,
// and so does "\r\n"); every line is made safe as a field's text is. What would pass the limits
// of lines or bytes is left out.
func (a *area) insert(text string) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	for i, part := range strings.Split(text, "\n") {
		if i > 0 && !a.newline() {
			return
		}
		for _, r := range clean(part) {
			if a.size()+len(string(r)) > areaMaxBytes {
				return
			}
			line := a.lines[a.row]
			a.lines[a.row] = append(append(append([]rune(nil), line[:a.col]...), r), line[a.col:]...)
			a.col++
		}
	}
}

// newline splits the line at the cursor, and says whether there was room for it.
func (a *area) newline() bool {
	if len(a.lines) >= areaMaxLines || a.size()+1 > areaMaxBytes {
		return false
	}
	line := a.lines[a.row]
	rest := append([]rune(nil), line[a.col:]...)
	a.lines[a.row] = append([]rune(nil), line[:a.col]...)
	a.lines = append(a.lines[:a.row+1], append([][]rune{rest}, a.lines[a.row+1:]...)...)
	a.row++
	a.col = 0
	return true
}

// paste is what pasted text does: it is inserted, line by line.
func (a *area) paste(text string) {
	a.insert(text)
}

// key handles a key press and says whether the area used it: a typed character, enter (a new
// line), the arrows, home and end, backspace and delete.
func (a *area) key(msg tea.KeyPressMsg) bool {
	switch name := msg.String(); name {
	case "enter":
		a.newline()
	case "left":
		if a.col > 0 {
			a.col--
		} else if a.row > 0 {
			a.row--
			a.col = len(a.lines[a.row])
		}
	case "right":
		if a.col < len(a.lines[a.row]) {
			a.col++
		} else if a.row < len(a.lines)-1 {
			a.row++
			a.col = 0
		}
	case "up":
		if a.row > 0 {
			a.row--
			a.col = min(a.col, len(a.lines[a.row]))
		}
	case "down":
		if a.row < len(a.lines)-1 {
			a.row++
			a.col = min(a.col, len(a.lines[a.row]))
		}
	case "home", "ctrl+a":
		a.col = 0
	case "end", "ctrl+e":
		a.col = len(a.lines[a.row])
	case "backspace":
		switch {
		case a.col > 0:
			line := a.lines[a.row]
			a.lines[a.row] = append(line[:a.col-1], line[a.col:]...)
			a.col--
		case a.row > 0:
			prev := a.lines[a.row-1]
			a.col = len(prev)
			a.lines[a.row-1] = append(append([]rune(nil), prev...), a.lines[a.row]...)
			a.lines = append(a.lines[:a.row], a.lines[a.row+1:]...)
			a.row--
		}
	case "delete":
		line := a.lines[a.row]
		switch {
		case a.col < len(line):
			a.lines[a.row] = append(line[:a.col], line[a.col+1:]...)
		case a.row < len(a.lines)-1:
			a.lines[a.row] = append(append([]rune(nil), line...), a.lines[a.row+1]...)
			a.lines = append(a.lines[:a.row+1], a.lines[a.row+2:]...)
		}
	default:
		if msg.Text == "" || msg.Mod&(tea.ModCtrl|tea.ModAlt) != 0 {
			return false
		}
		a.insert(msg.Text)
	}
	return true
}

// view draws the lines around the cursor in exactly height lines of width cells, with the cursor
// reversed. A line that is longer than the width is cut for the eye; the text is whole.
func (a area) view(width, height int, st styles) []string {
	first := 0
	if len(a.lines) > height {
		first = max(min(a.row-height/2, len(a.lines)-height), 0)
	}
	out := make([]string, 0, height)
	for i := first; i < len(a.lines) && len(out) < height; i++ {
		if i == a.row {
			out = append(out, drawLine(a.lines[i], a.col, true, "", width, st))
		} else {
			out = append(out, fit(string(a.lines[i]), width))
		}
	}
	for len(out) < height {
		out = append(out, "")
	}
	return out
}
