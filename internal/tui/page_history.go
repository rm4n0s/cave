package tui

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/history"
	"github.com/rm4n0s/cave/internal/core/server"
)

// The tabs of the History page, in the order the key t goes through them.
const (
	tabConnections = iota
	tabTransfers
	tabCount
)

// visibleHistoryConnections are the connections of the history that the search lets through
// (who and address), the newest first.
func (m Model) visibleHistoryConnections() []history.Connection {
	needle := strings.ToLower(m.histSearch)
	var out []history.Connection
	for _, c := range m.history.Connections {
		if needle == "" || strings.Contains(strings.ToLower(c.PersonName+"\n"+c.Address), needle) {
			out = append(out, c)
		}
	}
	return out
}

// visibleTransfers are the transfers that the search lets through (who and file), the newest
// first.
func (m Model) visibleTransfers() []history.Transfer {
	needle := strings.ToLower(m.histSearch)
	var out []history.Transfer
	for _, t := range m.history.Transfers {
		if needle == "" || strings.Contains(strings.ToLower(t.PersonName+"\n"+t.Path), needle) {
			out = append(out, t)
		}
	}
	return out
}

// keepSelection puts the list on the record with the id when it is still there, else keeps the
// row and returns the id of the record on it ("" when there are none). Records that arrive while
// the user reads do not move the row under the cursor.
func keepSelection(l *list, ids []string, selected string) string {
	l.setLen(len(ids))
	for i, id := range ids {
		if id == selected {
			l.selectIndex(i)
			return id
		}
	}
	if len(ids) == 0 {
		return ""
	}
	return ids[l.selected]
}

// syncHistory keeps the selection of each tab on the same record when the history or the search
// changes. The details close when their record is not in view any more.
func (m *Model) syncHistory() {
	conns, transfers := m.visibleHistoryConnections(), m.visibleTransfers()
	connIDs := make([]string, len(conns))
	for i, c := range conns {
		connIDs[i] = c.ID
	}
	trIDs := make([]string, len(transfers))
	for i, t := range transfers {
		trIDs[i] = t.ID
	}
	connBefore, trBefore := m.histConnSel, m.histTrSel
	m.histConnSel = keepSelection(&m.histConnList, connIDs, m.histConnSel)
	m.histTrSel = keepSelection(&m.histTrList, trIDs, m.histTrSel)
	if (m.histTab == tabConnections && m.histConnSel != connBefore) || (m.histTab == tabTransfers && m.histTrSel != trBefore) {
		m.histDetail = false
	}
}

// selectedHistoryConnection returns the connection the Connections tab is on.
func (m Model) selectedHistoryConnection() (history.Connection, bool) {
	visible := m.visibleHistoryConnections()
	if len(visible) == 0 || m.histConnList.selected >= len(visible) {
		return history.Connection{}, false
	}
	return visible[m.histConnList.selected], true
}

// selectedTransfer returns the transfer the Transfers tab is on.
func (m Model) selectedTransfer() (history.Transfer, bool) {
	visible := m.visibleTransfers()
	if len(visible) == 0 || m.histTrList.selected >= len(visible) {
		return history.Transfer{}, false
	}
	return visible[m.histTrList.selected], true
}

// historyLines draws the History page: the line of the tabs, the search and the clearing, then
// the table of the tab, or the details of the selected record.
func (m Model) historyLines(height int) []string {
	lines := []string{fit(m.historyInfo(), m.width)}
	if m.histDetail {
		if m.histTab == tabTransfers {
			if t, ok := m.selectedTransfer(); ok {
				return append(lines, cutLines(m.transferDetailLines(t), height-1)...)
			}
		} else if c, ok := m.selectedHistoryConnection(); ok {
			return append(lines, cutLines(m.connectionDetailLines(c), height-1)...)
		}
	}
	if m.histTab == tabTransfers {
		return append(lines, m.transferTable().view(m.width, height-1, m.histTrList, m.st)...)
	}
	return append(lines, m.historyConnectionTable().view(m.width, height-1, m.histConnList, m.st)...)
}

// cutLines keeps at most height lines.
func cutLines(lines []string, height int) []string {
	if len(lines) > height {
		return lines[:max(height, 0)]
	}
	return lines
}

// historyInfo is the first line: "[Connections]  Transfers   Search: —   Cleared 20 d ago".
func (m Model) historyInfo() string {
	tabs := "[Connections]  Transfers"
	if m.histTab == tabTransfers {
		tabs = " Connections  [Transfers]"
	}
	now := m.clock.Now()
	search := "—"
	if m.histSearch != "" {
		search = m.histSearch
	}
	cleared := "never cleared"
	if !m.history.LastClearedAt.IsZero() {
		cleared = "Cleared " + app.Ago(now, m.history.LastClearedAt)
	}
	if !m.history.NextClearAt.IsZero() {
		cleared += " · next clear " + app.Until(now, m.history.NextClearAt)
	}
	return tabs + "   Search: " + search + "   " + cleared
}

// durationCell is how long a connection lasted, as the table and the details word it.
func durationCell(c history.Connection) string {
	if c.Open() {
		return "connected now"
	}
	if d, ok := c.Duration(); ok {
		return app.DurationText(d)
	}
	return "—"
}

// endedCell is how a connection ended, as the table words it; an open one has not.
func endedCell(c history.Connection) string {
	if c.Open() {
		return "—"
	}
	return app.ReasonText(c.Reason)
}

// historyConnectionTable is the table of the Connections tab. A narrow screen leaves out when
// the connection began.
func (m Model) historyConnectionTable() table {
	now := m.clock.Now()
	wide := m.width >= 80
	endedWidth := 18
	if m.width >= 100 {
		endedWidth = 27
	}
	columns := []column{{title: "Who", width: 8}, {title: "Address", flex: true}}
	if wide {
		columns[0].width = 10
		columns = append(columns, column{title: "Connected", width: 10})
	}
	columns = append(columns, column{title: "Duration", width: 13}, column{title: "Ended", width: endedWidth})
	visible := m.visibleHistoryConnections()
	rows := make([][]string, len(visible))
	for i, c := range visible {
		row := []string{c.PersonName, c.Address}
		if wide {
			row = append(row, app.Ago(now, c.ConnectedAt))
		}
		rows[i] = append(row, durationCell(c), endedCell(c))
	}
	empty := "No connections yet."
	if len(m.history.Connections) > 0 {
		empty = "Nothing matches."
	}
	return table{columns: columns, rows: rows, empty: empty}
}

// sizeCell is the bytes of a transfer in its direction; both for one that went both ways.
func sizeCell(t history.Transfer) string {
	switch t.Direction() {
	case history.Download:
		return app.SizeText(t.Downloaded)
	case history.Both:
		return app.SizeText(t.Downloaded) + " / " + app.SizeText(t.Uploaded)
	}
	return app.SizeText(t.Uploaded)
}

// transferTable is the table of the Transfers tab. A narrow screen leaves out who, when and the
// result.
func (m Model) transferTable() table {
	now := m.clock.Now()
	wide := m.width >= 80
	var columns []column
	if wide {
		columns = append(columns, column{title: "Who", width: 10})
	}
	columns = append(columns,
		column{title: "File", flex: true, cutAtStart: true},
		column{title: "Direction", width: 9},
		column{title: "Size", width: 17, right: true},
	)
	if wide {
		columns = append(columns, column{title: "When", width: 10}, column{title: "Result", width: 11})
	}
	visible := m.visibleTransfers()
	rows := make([][]string, len(visible))
	for i, t := range visible {
		var row []string
		if wide {
			row = append(row, t.PersonName)
		}
		row = append(row, t.Path, app.DirectionText(t.Direction()), sizeCell(t))
		if wide {
			row = append(row, app.Ago(now, t.ClosedAt), app.ResultWord(t.Result))
		}
		rows[i] = row
	}
	empty := "No files moved yet."
	if len(m.history.Transfers) > 0 {
		empty = "Nothing matches."
	}
	return table{columns: columns, rows: rows, empty: empty}
}

// labelWidth is the width of the labels of the details.
const labelWidth = 13

// detailRow is one line of details: the label, then the value.
func (m Model) detailRow(label, value string) string {
	return m.st.title.Render(fit(label, labelWidth)) + value
}

// when writes a time of the details in the clock's zone; the zero time is "—".
func (m Model) when(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.In(m.clock.Now().Location()).Format("2006-01-02 15:04:05 MST")
}

// connectionDetailLines are every field of a connection of the history (docs/SPEC.md §9.11),
// then the files it moved.
func (m Model) connectionDetailLines(c history.Connection) []string {
	lines := []string{
		m.detailRow("Who", c.PersonName),
		m.detailRow("Address", c.Address),
		m.detailRow("Key", c.KeyFingerprint),
		m.detailRow("Client", c.ClientVersion),
		m.detailRow("Connected", m.when(c.ConnectedAt)),
		m.detailRow("Ended", m.when(c.EndedAt)),
		m.detailRow("Duration", durationCell(c)),
		m.detailRow("How it ended", app.ReasonText(c.Reason)),
	}
	if !c.Open() && c.Reason != history.ReasonUnknown {
		lines = append(lines, m.detailRow("Data", "in "+app.SizeText(c.BytesIn)+" · out "+app.SizeText(c.BytesOut)))
	}
	activities := make([]server.Activity, len(c.Activities))
	for i, a := range c.Activities {
		activities[i] = server.Activity{Kind: server.ActivityKind(a.Kind), Detail: a.Detail}
	}
	opened := app.ActivitiesText(activities)
	if c.MoreActivities > 0 {
		opened += " and " + strconv.Itoa(c.MoreActivities) + " more"
	}
	lines = append(lines, m.detailRow("Opened", opened), "", m.st.title.Render("Files"))
	n := 0
	for _, t := range m.history.Transfers {
		if t.ConnectionID != c.ID {
			continue
		}
		n++
		lines = append(lines, "  "+fit(app.DirectionText(t.Direction()), 9)+" "+fitAlign(sizeCell(t), 17, true)+"  "+
			fit(app.ResultWord(t.Result), 11)+" "+t.Path)
	}
	if n == 0 {
		lines = append(lines, m.st.dim.Render("  No files moved."))
	}
	return lines
}

// transferDetailLines are every field of a transfer (docs/SPEC.md §9.11); the path is written
// whole, over as many lines as it needs.
func (m Model) transferDetailLines(t history.Transfer) []string {
	lines := []string{m.detailRow("Who", t.PersonName)}
	pathWidth := max(m.width-labelWidth, 8)
	runes := []rune(t.Path)
	for i := 0; i < len(runes) || i == 0; i += pathWidth {
		label := ""
		if i == 0 {
			label = "File"
		}
		lines = append(lines, m.detailRow(label, string(runes[i:min(i+pathWidth, len(runes))])))
	}
	connection := t.ConnectionID
	for _, c := range m.history.Connections {
		if c.ID == t.ConnectionID {
			connection += " from " + c.Address
		}
	}
	return append(lines,
		m.detailRow("Direction", app.DirectionText(t.Direction())),
		m.detailRow("Downloaded", app.SizeText(t.Downloaded)),
		m.detailRow("Uploaded", app.SizeText(t.Uploaded)),
		m.detailRow("Opened", m.when(t.OpenedAt)),
		m.detailRow("Closed", m.when(t.ClosedAt)),
		m.detailRow("Duration", app.DurationText(t.ClosedAt.Sub(t.OpenedAt))),
		m.detailRow("Result", app.ResultWord(t.Result)),
		m.detailRow("Connection", connection),
	)
}

// keyHistory handles a key of the History page that is not one of the keys of every page.
func (m Model) keyHistory(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	name := msg.String()
	if m.histDetail {
		if name == "esc" || name == "enter" {
			m.histDetail = false
		}
		return m, nil
	}
	m.histConnList.setHeight(m.height - chrome - 2)
	m.histTrList.setHeight(m.height - chrome - 2)
	if m.histTab == tabTransfers {
		if m.histTrList.key(name) {
			if t, ok := m.selectedTransfer(); ok {
				m.histTrSel = t.ID
			}
			return m, nil
		}
	} else if m.histConnList.key(name) {
		if c, ok := m.selectedHistoryConnection(); ok {
			m.histConnSel = c.ID
		}
		return m, nil
	}
	ctx, a := m.ctx, m.app
	switch name {
	case "t":
		m.histTab = (m.histTab + 1) % tabCount
	case "/":
		m.openLivePrompt("Search:", m.histSearch, maxSearchLength,
			func(id int, _ string) tea.Cmd { return func() tea.Msg { return promptDone{id: id} } },
			func(m Model, text string) Model {
				m.histSearch = text
				m.syncHistory()
				return m
			},
			func(m Model) Model {
				m.histSearch = ""
				m.syncHistory()
				return m
			})
	case "enter":
		if m.histTab == tabTransfers {
			_, m.histDetail = m.selectedTransfer()
		} else {
			_, m.histDetail = m.selectedHistoryConnection()
		}
	case "C":
		if len(m.history.Connections) > 0 || len(m.history.Transfers) > 0 {
			m.ask = &asking{
				q:   question{text: "Clear the history? Connections that are open stay.", yes: "Clear"},
				yes: func() tea.Msg { return doneMsg{err: a.ClearHistory(ctx)} },
			}
		}
	}
	return m, nil
}
