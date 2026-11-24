package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// field is a one-line text input: the text and a cursor in it. The text is kept as runes, so a
// character is always taken or left whole.
type field struct {
	text []rune
	// cursor is the place of the cursor: 0 is before the first character, len(text) after the
	// last.
	cursor int
	// limit is the most characters the field takes; 0 is no limit.
	limit int
	// placeholder is drawn, dim, while the field is empty.
	placeholder string
}

// newField makes a field with a text; the cursor is at its end.
func newField(text string, limit int) field {
	f := field{limit: limit}
	f.set(text)
	return f
}

// set replaces the text and puts the cursor at its end.
func (f *field) set(text string) {
	f.text = nil
	f.cursor = 0
	f.insert(text)
}

// value returns the text.
func (f field) value() string {
	return string(f.text)
}

// insert puts text (made safe for a line) at the cursor, as much of it as the limit allows.
func (f *field) insert(text string) {
	add := []rune(clean(text))
	if f.limit > 0 {
		add = add[:max(min(len(add), f.limit-len(f.text)), 0)]
	}
	rest := append(append([]rune(nil), add...), f.text[f.cursor:]...)
	f.text = append(f.text[:f.cursor], rest...)
	f.cursor += len(add)
}

// paste is what pasted text does: it is inserted as typed text is.
func (f *field) paste(text string) {
	f.insert(text)
}

// key handles a key press and says whether the field used it. A typed character is inserted; the
// editing keys are left, right, home and ctrl+a, end and ctrl+e, backspace, delete, ctrl+u (empty
// the field) and ctrl+w (delete the word before the cursor).
func (f *field) key(msg tea.KeyPressMsg) bool {
	switch name := msg.String(); name {
	case "left":
		f.cursor = max(f.cursor-1, 0)
	case "right":
		f.cursor = min(f.cursor+1, len(f.text))
	case "home", "ctrl+a":
		f.cursor = 0
	case "end", "ctrl+e":
		f.cursor = len(f.text)
	case "backspace":
		if f.cursor > 0 {
			f.text = append(f.text[:f.cursor-1], f.text[f.cursor:]...)
			f.cursor--
		}
	case "delete":
		if f.cursor < len(f.text) {
			f.text = append(f.text[:f.cursor], f.text[f.cursor+1:]...)
		}
	case "ctrl+u":
		f.text, f.cursor = nil, 0
	case "ctrl+w":
		start := f.cursor
		for start > 0 && f.text[start-1] == ' ' {
			start--
		}
		for start > 0 && f.text[start-1] != ' ' {
			start--
		}
		f.text = append(f.text[:start], f.text[f.cursor:]...)
		f.cursor = start
	default:
		if msg.Text == "" || msg.Mod&(tea.ModCtrl|tea.ModAlt) != 0 {
			return false
		}
		f.insert(msg.Text)
	}
	return true
}

// view draws the field in exactly width cells: the part of the text around the cursor, with the
// cursor reversed.
func (f field) view(width int, st styles) string {
	return drawLine(f.text, f.cursor, true, f.placeholder, width, st)
}

// drawLine draws one line of text in exactly width cells. The part around the cursor is shown
// when the text is longer than the width, and the cursor, a reversed cell, only when show is
// true. An empty line shows the placeholder, dim, after the cursor.
func drawLine(text []rune, cursor int, show bool, placeholder string, width int, st styles) string {
	if width <= 0 {
		return ""
	}
	// Scroll right until the cursor's cell fits.
	start, used := 0, 0
	for _, r := range text[:cursor] {
		used += cellWidth(r)
	}
	for start < cursor && used > width-1 {
		used -= cellWidth(text[start])
		start++
	}
	var b strings.Builder
	b.WriteString(string(text[start:cursor]))
	under := " "
	if cursor < len(text) {
		under = string(text[cursor])
	}
	if show {
		b.WriteString(st.current.Render(under))
	} else {
		b.WriteString(under)
	}
	if cursor < len(text) {
		b.WriteString(string(text[cursor+1:]))
	} else if len(text) == 0 && placeholder != "" {
		b.WriteString(st.dim.Render(placeholder))
	}
	line := b.String()
	if lipgloss.Width(line) > width {
		line = fit(line, width)
	}
	return fit(line, width)
}
