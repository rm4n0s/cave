package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/server"
)

// syncConnections keeps the selection of the Connections page on the same connection when the
// list changes: by the connection's id, not by the row.
func (m *Model) syncConnections() {
	m.connList.setLen(len(m.connections))
	for i, c := range m.connections {
		if c.ID == m.connSel {
			m.connList.selectIndex(i)
			return
		}
	}
	m.connSel = ""
	if len(m.connections) > 0 {
		m.connSel = m.connections[m.connList.selected].ID
	}
}

// selectedConnection returns the connection the Connections page is on.
func (m Model) selectedConnection() (server.Connection, bool) {
	if len(m.connections) == 0 || m.connList.selected >= len(m.connections) {
		return server.Connection{}, false
	}
	return m.connections[m.connList.selected], true
}

// connectionLines draws the Connections page: who is connected now, from where, since when and
// what each has open.
func (m Model) connectionLines(height int) []string {
	now := m.clock.Now()
	rows := make([][]string, len(m.connections))
	for i, c := range m.connections {
		rows[i] = []string{c.PersonName, c.Address, app.Ago(now, c.Since), app.ActivitiesText(c.Activities)}
	}
	// The address has the room of an IPv4 address and a little more; what a connection has open
	// can be long and gets the rest.
	addressWidth := 16
	if m.width >= 80 {
		addressWidth = 20
	}
	t := table{
		columns: []column{
			{title: "Person", width: 12},
			{title: "Address", width: addressWidth},
			{title: "Since", width: 10},
			{title: "Open", flex: true},
		},
		rows:  rows,
		empty: "Nobody is connected.",
	}
	return t.view(m.width, height, m.connList, m.st)
}

// keyConnections handles a key of the Connections page that is not one of the keys of every page.
func (m Model) keyConnections(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	name := msg.String()
	m.connList.setHeight(m.height - chrome - 1)
	if m.connList.key(name) {
		if c, ok := m.selectedConnection(); ok {
			m.connSel = c.ID
		}
		return m, nil
	}
	ctx, a := m.ctx, m.app
	switch name {
	case "d":
		if c, ok := m.selectedConnection(); ok {
			m.ask = &asking{
				q:   question{text: "Disconnect " + c.PersonName + " (" + c.Address + ")?", yes: "Disconnect"},
				yes: func() tea.Msg { return doneMsg{err: a.Disconnect(ctx, c.ID)} },
			}
		}
	case "D":
		n := len(m.connections)
		if n == 0 {
			return m, nil
		}
		text := "Disconnect the connection?"
		if n > 1 {
			text = "Disconnect all " + plural(n, "connection") + "?"
		}
		m.ask = &asking{
			q: question{text: text, yes: "Disconnect all"},
			yes: func() tea.Msg {
				a.DisconnectAll()
				return doneMsg{}
			},
		}
	}
	return m, nil
}
