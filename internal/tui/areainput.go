package tui

import (
	tea "charm.land/bubbletea/v2"
)

// areaInput is a multi-line text input that takes the whole page's part, with a title and, after
// a failure, the sentence above the text. Ctrl+D runs its command and Esc closes it. It is for
// pasting public keys. While it is open it gets every key and every paste.
type areaInput struct {
	// id tells it from the next one that opens, as a prompt's id does.
	id    int
	title string
	a     area
	err   string
	busy  bool
	run   func(id int, text string) tea.Cmd
}

// openArea opens an area input.
func (m *Model) openArea(title string, run func(id int, text string) tea.Cmd) {
	m.promptSeq++
	m.area = &areaInput{id: m.promptSeq, title: title, a: newArea(), run: run}
}

// keyArea handles a key while an area input is open.
func (m Model) keyArea(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	in := *m.area
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		m.area = nil
		return m, nil
	case "ctrl+d":
		if in.busy {
			return m, nil
		}
		in.busy, in.err = true, ""
		m.area = &in
		return m, in.run(in.id, in.a.text())
	}
	in.a.key(msg)
	m.area = &in
	return m, nil
}

// pasteArea puts pasted text in the area.
func (m Model) pasteArea(text string) Model {
	in := *m.area
	in.a.paste(text)
	m.area = &in
	return m
}

// view draws the title, the sentence of the last failure if there is one, and the text, in
// exactly height lines.
func (in areaInput) view(width, height int, st styles) []string {
	lines := []string{st.title.Render(fit(in.title, width))}
	for _, line := range wrapLines(in.err, width, 2) {
		lines = append(lines, st.err.Render(line))
	}
	return append(lines, in.a.view(width, max(height-len(lines), 0), st)...)
}
