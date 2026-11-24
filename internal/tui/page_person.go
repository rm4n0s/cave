package tui

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/people"
)

// maxPathLength is how many characters the field of a file's path takes.
const maxPathLength = 4096

// maxCommentLength is how many characters a key's comment has (docs/SPEC.md §7.2).
const maxCommentLength = people.MaxCommentLength

// openPerson returns the person the People page has open.
func (m Model) openPerson() (people.Person, bool) {
	i := m.personIndex(m.personID)
	if i < 0 {
		return people.Person{}, false
	}
	return m.people[i], true
}

// syncKeys keeps the selection among the open person's keys on the same key when the list
// changes, and goes back to the list of people when the person is gone.
func (m *Model) syncKeys() {
	if m.personID == "" {
		return
	}
	p, ok := m.openPerson()
	if !ok {
		m.personID, m.keySel = "", ""
		return
	}
	m.keyList.setLen(len(p.Keys))
	for i, k := range p.Keys {
		if k.ID == m.keySel {
			m.keyList.selectIndex(i)
			return
		}
	}
	m.keySel = ""
	if len(p.Keys) > 0 {
		m.keySel = p.Keys[m.keyList.selected].ID
	}
}

// selectedKey returns the key the open person's list is on.
func (m Model) selectedKey() (people.Key, bool) {
	p, ok := m.openPerson()
	if !ok || len(p.Keys) == 0 || m.keyList.selected >= len(p.Keys) {
		return people.Key{}, false
	}
	return p.Keys[m.keyList.selected], true
}

// personLines draws the open person: who they are and the table of their keys.
func (m Model) personLines(height int) []string {
	p, ok := m.openPerson()
	if !ok {
		return nil
	}
	state := "enabled"
	if !p.Enabled {
		state = "disabled"
	}
	connected := "not connected"
	if n := app.CountConnections(m.connections, p.ID, ""); n > 0 {
		connected = plural(n, "connection")
	}
	head := m.st.title.Render(p.Name) + "   " + state + "   " + connected
	rows := make([][]string, len(p.Keys))
	for i, k := range p.Keys {
		rows[i] = []string{k.Type, k.Fingerprint, k.Comment, k.AddedAt.Format("2006-01-02")}
	}
	t := table{
		columns: []column{
			{title: "Type", width: 11},
			{title: "Fingerprint", flex: true},
			{title: "Comment", width: 16},
			{title: "Added", width: 10},
		},
		rows:  rows,
		empty: "No key yet, so " + p.Name + " cannot connect. Press a to add one.",
	}
	return append([]string{fit(head, m.width)}, t.view(m.width, height-1, m.keyList, m.st)...)
}

// keyPerson handles a key of the People page while a person is open.
func (m Model) keyPerson(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	name := msg.String()
	m.keyList.setHeight(m.height - chrome - 2)
	if m.keyList.key(name) {
		if k, ok := m.selectedKey(); ok {
			m.keySel = k.ID
		}
		return m, nil
	}
	p, ok := m.openPerson()
	if !ok {
		return m, nil
	}
	ctx, a := m.ctx, m.app
	switch name {
	case "esc":
		m.personID, m.keySel = "", ""
	case "a":
		m.openArea("Paste public keys (one per line), then press ctrl+d. esc cancels.", func(id int, text string) tea.Cmd {
			return func() tea.Msg {
				_, err := a.AddKeys(ctx, p.ID, text)
				return promptDone{id: id, err: err}
			}
		})
	case "i":
		m.openPrompt("Path of a .pub or authorized_keys file:", "", maxPathLength, func(id int, text string) tea.Cmd {
			return func() tea.Msg {
				_, err := a.ImportKeys(ctx, p.ID, expandHome(text))
				return promptDone{id: id, err: err}
			}
		})
	case "c":
		if k, ok := m.selectedKey(); ok {
			m.openPrompt("Comment:", k.Comment, maxCommentLength, func(id int, text string) tea.Cmd {
				return func() tea.Msg {
					return promptDone{id: id, err: a.SetKeyComment(ctx, p.ID, k.ID, text)}
				}
			})
		}
	case "d":
		if k, ok := m.selectedKey(); ok {
			text := "Remove this key (" + k.Fingerprint + ")?"
			if n := app.CountConnections(m.connections, p.ID, k.ID); n > 0 {
				text += " This closes " + plural(n, "connection") + "."
			}
			m.ask = &asking{
				q:   question{text: text, yes: "Remove"},
				yes: func() tea.Msg { return doneMsg{err: a.RemoveKey(ctx, p.ID, k.ID)} },
			}
		}
	}
	return m, nil
}

// expandHome turns a leading "~/" into the user's home folder, and changes nothing else: no
// "~user", no variables.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path // the path is tried as it is, and the file's error says what is wrong
	}
	return home + path[1:]
}
