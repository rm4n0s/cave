package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/rm4n0s/cave/internal/core/server"
)

// The least size the TUI draws in (docs/SPEC.md §12).
const (
	minWidth  = 60
	minHeight = 16
)

// chrome is how many lines the frame uses besides the page: the header, the pages, a rule above
// and below the page, the message line and the footer.
const chrome = 6

// View draws the screen. It only reads the model.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

// render returns the whole screen as text: exactly m.height lines of at most m.width cells.
func (m Model) render() string {
	if m.width <= 0 || m.height <= 0 {
		return "" // the terminal's size is not known yet
	}
	if m.width < minWidth || m.height < minHeight {
		return "Terminal too small (needs " + strconv.Itoa(minWidth) + "×" + strconv.Itoa(minHeight) + ")"
	}
	rule := strings.Repeat("─", m.width)
	lines := make([]string, 0, m.height)
	lines = append(lines, m.header(), m.tabs(), m.st.dim.Render(rule))
	lines = append(lines, m.body(m.height-chrome)...)
	lines = append(lines, m.st.dim.Render(rule), m.messageLine(), m.footer())
	for i, line := range lines {
		lines[i] = fit(line, m.width)
	}
	return strings.Join(lines, "\n")
}

// header is the first line: the name and the state of the server on the left, the counts on the
// right.
func (m Model) header() string {
	left := m.st.title.Render("Cave") + "   " + m.stateText()
	right := strconv.Itoa(len(m.connections)) + " connected · " + plural(len(m.attempts.Entries), "attempt")
	if gap := m.width - lipgloss.Width(left) - lipgloss.Width(right); gap >= 2 {
		return left + strings.Repeat(" ", gap) + right
	}
	return left
}

// stateText says what the server is doing, in words, colored besides.
func (m Model) stateText() string {
	switch m.server.State {
	case server.Running:
		return m.st.good.Render("running on port " + strconv.Itoa(m.server.Port))
	case server.Starting:
		return "starting…"
	case server.Stopping:
		return "stopping…"
	}
	if m.server.Err != nil {
		return m.st.bad.Render("not running")
	}
	return m.st.dim.Render("stopped")
}

// tabs is the second line: the pages, with the current one in brackets and reversed.
func (m Model) tabs() string {
	parts := make([]string, pageCount)
	for p := page(0); p < pageCount; p++ {
		label := strconv.Itoa(int(p)+1) + " " + p.title()
		if p == m.page {
			parts[p] = m.st.current.Render("[" + label + "]")
		} else {
			parts[p] = label
		}
	}
	separator := "  "
	if lipgloss.Width(strings.Join(parts, separator)) > m.width {
		separator = " "
	}
	return strings.Join(parts, separator)
}

// body is the part between the rules: the help, or the current page. It has exactly height
// lines.
func (m Model) body(height int) []string {
	var lines []string
	switch {
	case m.ask != nil:
		lines = m.ask.q.view(m.width, height, m.st)
	case m.help:
		lines = m.helpLines()
	case m.area != nil:
		lines = m.area.view(m.width, height, m.st)
	case m.prompt != nil:
		// The page is drawn above the prompt, which has the last lines of the body.
		own := m.prompt.view(m.width, m.st)
		lines = m.pageLines(height - len(own))
		for len(lines) < height-len(own) {
			lines = append(lines, "")
		}
		lines = append(lines[:height-len(own)], own...)
	default:
		lines = m.pageLines(height)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

// pageLines draws the current page into at most height lines.
func (m Model) pageLines(height int) []string {
	switch m.page {
	case pageOverview:
		return m.overviewLines(height)
	case pageAttempts:
		return m.attemptLines(height)
	case pageConnections:
		return m.connectionLines(height)
	case pagePeople:
		if m.personID != "" {
			return m.personLines(height)
		}
		return m.peopleLines(height)
	}
	return []string{m.st.title.Render(m.page.title())}
}

// helpLines lists every key: the ones that work everywhere, then the current page's.
func (m Model) helpLines() []string {
	lines := []string{m.st.title.Render("Keys everywhere")}
	lines = append(lines, m.bindingLines(globalBindings())...)
	if rows := ownBindings(m.pageKeys()); len(rows) > 0 {
		lines = append(lines, "", m.st.title.Render("Keys of "+m.keysTitle()))
		lines = append(lines, m.bindingLines(rows)...)
	}
	return lines
}

// bindingLines draws one line for each binding: the keys in a column of their own.
func (m Model) bindingLines(rows []binding) []string {
	width := 0
	for _, r := range rows {
		width = max(width, lipgloss.Width(r.keys))
	}
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = "  " + m.st.key.Render(fit(r.keys, width)) + "  " + r.what
	}
	return lines
}

// messageLine is the line above the footer: the error of the last command, or nothing.
func (m Model) messageLine() string {
	if m.message == "" {
		return ""
	}
	if m.messageErr {
		return m.st.err.Render(m.message)
	}
	return m.message
}

// footer is the last line: the keys of the page, then the two that are always there.
func (m Model) footer() string {
	if m.area != nil {
		return m.st.key.Render("ctrl+d") + " add   " + m.st.key.Render("esc") + " cancel"
	}
	if m.prompt != nil {
		return m.st.key.Render("enter") + " ok   " + m.st.key.Render("esc") + " cancel"
	}
	if m.ask != nil {
		return m.st.key.Render("y") + " " + m.ask.q.yes + "   " + m.st.key.Render("n") + " cancel"
	}
	rows := append(append([]binding(nil), m.pageKeys()...), binding{"?", "keys"}, binding{"q", "quit"})
	parts := make([]string, len(rows))
	for i, r := range rows {
		parts[i] = m.st.key.Render(r.keys) + " " + r.what
	}
	// The keys are spaced as widely as the width allows.
	for _, gap := range []string{"   ", "  ", " "} {
		if line := strings.Join(parts, gap); lipgloss.Width(line) <= m.width {
			return line
		}
	}
	return strings.Join(parts, " ")
}
