package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// question is a yes or no question, drawn as a box in the middle of the page. The answer is no
// unless the user says yes: Enter never destroys anything.
type question struct {
	text string
	// yes is the name of the action ("Quit", "Remove"), shown beside the key y.
	yes string
}

// key handles a key press: "y" answers yes; "n", esc and enter answer no; any other key does
// nothing and leaves the question open.
func (q question) key(msg tea.KeyPressMsg) (decided, yes bool) {
	switch msg.String() {
	case "y", "Y":
		return true, true
	case "n", "N", "esc", "enter":
		return true, false
	}
	return false, false
}

// view draws the box in the middle of width by height cells, in exactly height lines.
func (q question) view(width, height int, st styles) []string {
	hint := st.key.Render("y") + " " + q.yes + "   " + st.key.Render("n") + " Cancel"
	inner := min(max(lipgloss.Width(q.text), lipgloss.Width(hint)), max(width-6, 10))
	body := strings.Split(ansi.Wrap(q.text, inner, ""), "\n")
	body = append(body, "", hint)
	for i, line := range body {
		body[i] = fit(line, inner)
	}
	boxWidth := inner + 4
	box := []string{"┌" + strings.Repeat("─", boxWidth-2) + "┐"}
	for _, line := range body {
		box = append(box, "│ "+line+" │")
	}
	box = append(box, "└"+strings.Repeat("─", boxWidth-2)+"┘")

	top := max((height-len(box))/2, 0)
	left := strings.Repeat(" ", max((width-boxWidth)/2, 0))
	lines := make([]string, 0, height)
	for i := 0; i < top; i++ {
		lines = append(lines, "")
	}
	for _, line := range box {
		lines = append(lines, left+line)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}
