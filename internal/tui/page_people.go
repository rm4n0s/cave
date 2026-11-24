package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/people"
)

// maxNameLength is how many characters the field of a person's name takes (docs/SPEC.md §7.1).
const maxNameLength = 32

// syncPeople keeps the selection of the People page on the same person when the list changes: by
// the person's id, not by the row. A person that was just added is selected as soon as they are
// in the list.
func (m *Model) syncPeople() {
	m.peopleList.setLen(len(m.people))
	if m.peopleWanted != "" {
		if i := m.personIndex(m.peopleWanted); i >= 0 {
			m.peopleSel, m.peopleWanted = m.peopleWanted, ""
			m.peopleList.selectIndex(i)
			return
		}
	}
	if i := m.personIndex(m.peopleSel); i >= 0 {
		m.peopleList.selectIndex(i)
		return
	}
	// The person is gone (removed, or nobody was selected yet): the row stays where it is.
	m.peopleSel = ""
	if len(m.people) > 0 {
		m.peopleSel = m.people[m.peopleList.selected].ID
	}
}

// personIndex returns the row of the person with the id, or -1.
func (m Model) personIndex(id string) int {
	if id == "" {
		return -1
	}
	for i, p := range m.people {
		if p.ID == id {
			return i
		}
	}
	return -1
}

// selectedPerson returns the person the People page is on.
func (m Model) selectedPerson() (people.Person, bool) {
	if len(m.people) == 0 || m.peopleList.selected >= len(m.people) {
		return people.Person{}, false
	}
	return m.people[m.peopleList.selected], true
}

// peopleLines draws the People page: the table of people.
func (m Model) peopleLines(height int) []string {
	rows := make([][]string, len(m.people))
	for i, p := range m.people {
		connected := ""
		if n := app.CountConnections(m.connections, p.ID, ""); n > 0 {
			connected = strconv.Itoa(n)
		}
		enabled := "no"
		if p.Enabled {
			enabled = "yes"
		}
		rows[i] = []string{p.Name, strconv.Itoa(len(p.Keys)), enabled, connected}
	}
	t := table{
		columns: []column{
			{title: "Name", flex: true},
			{title: "Keys", width: 4, right: true},
			{title: "Enabled", width: 7},
			{title: "Connected", width: 9, right: true},
		},
		rows:  rows,
		empty: "Nobody yet. Press a to add a person.",
	}
	return t.view(m.width, height, m.peopleList, m.st)
}

// keyPeople handles a key of the People page that is not one of the keys of every page.
func (m Model) keyPeople(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.personID != "" {
		return m.keyPerson(msg)
	}
	name := msg.String()
	m.peopleList.setHeight(m.height - chrome - 1)
	if m.peopleList.key(name) {
		if p, ok := m.selectedPerson(); ok {
			m.peopleSel = p.ID
		}
		return m, nil
	}
	ctx, a := m.ctx, m.app
	switch name {
	case "a":
		// A person needs a key to connect, so there is no person without one: the name is
		// asked first and checked, then the keys, and the person is added with both.
		m.openPrompt("Name of the new person:", "", maxNameLength, func(id int, text string) tea.Cmd {
			return func() tea.Msg {
				if err := a.CheckPersonName(text); err != nil {
					return promptDone{id: id, err: err}
				}
				return promptDone{id: id, next: func(m Model) Model { return m.askKeysOfNewPerson(text) }}
			}
		})
	case "e":
		if p, ok := m.selectedPerson(); ok {
			m.openPrompt("New name of "+p.Name+":", p.Name, maxNameLength, func(id int, text string) tea.Cmd {
				return func() tea.Msg {
					return promptDone{id: id, err: a.RenamePerson(ctx, p.ID, text), selectID: p.ID}
				}
			})
		}
	case "enter":
		if p, ok := m.selectedPerson(); ok {
			m.personID, m.keyList, m.keySel = p.ID, list{}, ""
			m.keyList.setHeight(m.height - chrome - 2)
			m.syncKeys()
		}
	case "space":
		return m.togglePerson()
	case "d":
		return m.askRemovePerson()
	}
	return m, nil
}

// askKeysOfNewPerson opens the field for the public keys of the person who is being added, and
// adds the person with them. The person is not added when the keys are not acceptable: the field
// stays open with the sentence, and Esc leaves nobody added.
func (m Model) askKeysOfNewPerson(name string) Model {
	ctx, a := m.ctx, m.app
	m.openArea("Paste the public keys of "+name+" (one per line), then press ctrl+d. esc cancels.", func(id int, text string) tea.Cmd {
		return func() tea.Msg {
			p, err := a.AddPerson(ctx, name, text)
			return promptDone{id: id, err: err, selectID: p.ID}
		}
	})
	return m
}

// togglePerson enables a person who is disabled, and disables one who is enabled: after asking
// when they are connected, because disabling disconnects them.
func (m Model) togglePerson() (tea.Model, tea.Cmd) {
	p, ok := m.selectedPerson()
	if !ok {
		return m, nil
	}
	ctx, a := m.ctx, m.app
	cmd := func() tea.Msg { return doneMsg{err: a.SetPersonEnabled(ctx, p.ID, !p.Enabled)} }
	if n := app.CountConnections(m.connections, p.ID, ""); p.Enabled && n > 0 {
		m.ask = &asking{
			q:   question{text: p.Name + " is connected (" + plural(n, "connection") + "). Disable and disconnect?", yes: "Disable"},
			yes: cmd,
		}
		return m, nil
	}
	return m, cmd
}

// askRemovePerson asks whether to remove the person, and says what it will close.
func (m Model) askRemovePerson() (tea.Model, tea.Cmd) {
	p, ok := m.selectedPerson()
	if !ok {
		return m, nil
	}
	text := "Remove " + p.Name
	if len(p.Keys) > 0 {
		text += " and their " + plural(len(p.Keys), "key")
	}
	text += "?"
	if n := app.CountConnections(m.connections, p.ID, ""); n > 0 {
		text += " This closes " + plural(n, "connection") + "."
	}
	ctx, a := m.ctx, m.app
	m.ask = &asking{
		q:   question{text: text, yes: "Remove"},
		yes: func() tea.Msg { return doneMsg{err: a.RemovePerson(ctx, p.ID)} },
	}
	return m, nil
}

// plural writes a count and a noun: "1 key", "2 keys".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
