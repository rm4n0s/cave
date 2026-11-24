package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// prompt is a one-line question with a text field, drawn at the bottom of the page: "Name of the
// new person:" and what the user has typed. Enter runs its command, Esc closes it. When the
// command fails the prompt stays open with the text and the sentence above it, so the user can
// correct it. While a prompt is open it gets every key, so a name can hold a "q".
type prompt struct {
	// id tells the prompt from the next one that opens, so that the end of an old command does
	// not act on a new prompt.
	id    int
	label string
	f     field
	// err is the sentence of the last failure, shown above the field.
	err string
	// busy is true while the command runs: Enter does nothing then.
	busy bool
	// run makes the command for the text. It gives back a promptDone with the same id.
	run func(id int, text string) tea.Cmd
}

// promptDone is the end of a prompt's command: the prompt closes, or shows why it failed.
// selectID, when it is not empty, is the person (or entry) to select once it is in the list.
type promptDone struct {
	id       int
	err      error
	selectID string
}

// openPrompt opens a prompt with a text in its field.
func (m *Model) openPrompt(label, text string, limit int, run func(id int, text string) tea.Cmd) {
	m.promptSeq++
	m.prompt = &prompt{id: m.promptSeq, label: label, f: newField(text, limit), run: run}
}

// keyPrompt handles a key while a prompt is open.
func (m Model) keyPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := *m.prompt
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		m.prompt = nil
		return m, nil
	case "enter":
		if p.busy {
			return m, nil
		}
		p.busy, p.err = true, ""
		m.prompt = &p
		return m, p.run(p.id, p.f.value())
	}
	p.f.key(msg)
	m.prompt = &p
	return m, nil
}

// pastePrompt puts pasted text in the prompt's field.
func (m Model) pastePrompt(text string) Model {
	p := *m.prompt
	p.f.paste(text)
	m.prompt = &p
	return m
}

// view draws the prompt: the sentence of the last failure, if any, then the label and the field.
func (p prompt) view(width int, st styles) []string {
	var lines []string
	for _, line := range wrapLines(p.err, width, 2) {
		lines = append(lines, st.err.Render(line))
	}
	room := max(width-lipgloss.Width(p.label)-1, 1)
	lines = append(lines, st.title.Render(p.label)+" "+p.f.view(room, st))
	return lines
}
