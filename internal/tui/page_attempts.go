package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/attempts"
)

// The filters of the Attempts page, in the order the key f goes through them.
const (
	filterAll = iota
	filterAccepted
	filterRefused
	filterCount
)

// filterName is the filter as the first line of the page words it.
func filterName(filter int) string {
	switch filter {
	case filterAccepted:
		return "Accepted"
	case filterRefused:
		return "Refused"
	}
	return "All"
}

// maxSearchLength is how many characters the search field takes.
const maxSearchLength = 64

// visibleAttempts are the entries that the filter and the search let through, in the order of the
// snapshot (the newest first).
func (m Model) visibleAttempts() []attempts.Entry {
	needle := strings.ToLower(m.attemptSearch)
	var out []attempts.Entry
	for _, e := range m.attempts.Entries {
		switch m.attemptFilter {
		case filterAccepted:
			if e.Outcome != attempts.Accepted {
				continue
			}
		case filterRefused:
			if e.Outcome == attempts.Accepted {
				continue
			}
		}
		if needle != "" && !strings.Contains(strings.ToLower(app.Who(e)+"\n"+e.LoginName+"\n"+e.Address), needle) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// syncAttempts keeps the selection on the same entry (by id) when the list, the filter or the
// search changes: an attempt that arrives while the user reads does not move the row under the
// cursor. The details close when their entry is not in view any more.
func (m *Model) syncAttempts() {
	visible := m.visibleAttempts()
	m.attemptList.setLen(len(visible))
	for i, e := range visible {
		if e.ID == m.attemptSel {
			m.attemptList.selectIndex(i)
			return
		}
	}
	m.attemptSel, m.attemptDetail = "", false
	if len(visible) > 0 {
		m.attemptSel = visible[m.attemptList.selected].ID
	}
}

// selectedAttempt returns the entry the Attempts page is on.
func (m Model) selectedAttempt() (attempts.Entry, bool) {
	visible := m.visibleAttempts()
	if len(visible) == 0 || m.attemptList.selected >= len(visible) {
		return attempts.Entry{}, false
	}
	return visible[m.attemptList.selected], true
}

// attemptLines draws the Attempts page: the line of the filter, the search and the clearing, then
// the table, or the details of the selected entry.
func (m Model) attemptLines(height int) []string {
	lines := []string{fit(m.attemptInfo(), m.width)}
	if m.attemptDetail {
		if e, ok := m.selectedAttempt(); ok {
			return append(lines, m.detailLines(e, height-1)...)
		}
	}
	return append(lines, m.attemptTable().view(m.width, height-1, m.attemptList, m.st)...)
}

// attemptInfo is the first line: "Filter: All   Search: —   Cleared 3 d ago · next clear in 4 d".
func (m Model) attemptInfo() string {
	now := m.clock.Now()
	search := "—"
	if m.attemptSearch != "" {
		search = m.attemptSearch
	}
	cleared := "never cleared"
	if !m.attempts.LastClearedAt.IsZero() {
		cleared = "Cleared " + app.Ago(now, m.attempts.LastClearedAt)
	}
	if !m.attempts.NextClearAt.IsZero() {
		cleared += " · next clear " + app.Until(now, m.attempts.NextClearAt)
	}
	return "Filter: " + filterName(m.attemptFilter) + "   Search: " + search + "   " + cleared
}

// attemptTable is the table of the visible entries. A narrow screen leaves out the login name.
func (m Model) attemptTable() table {
	now := m.clock.Now()
	wide := m.width >= 80
	resultWidth, whoWidth := 16, 8
	if m.width >= 100 {
		resultWidth = 27
	}
	if wide {
		whoWidth = 10
	}
	columns := []column{{title: "Who", width: whoWidth}}
	if wide {
		columns = append(columns, column{title: "Login name", flex: true})
	}
	columns = append(columns,
		column{title: "Address", flex: true},
		column{title: "Result", width: resultWidth},
		column{title: "Count", width: 5, right: true},
		column{title: "Last", width: 10},
	)
	visible := m.visibleAttempts()
	rows := make([][]string, len(visible))
	for i, e := range visible {
		row := []string{app.Who(e)}
		if wide {
			row = append(row, e.LoginName)
		}
		rows[i] = append(row, e.Address, app.ResultText(e.Outcome), strconv.FormatInt(e.Count, 10), app.Ago(now, e.LastAt))
	}
	empty := "Nobody tried to connect yet."
	if len(m.attempts.Entries) > 0 {
		empty = "Nothing matches."
	}
	return table{columns: columns, rows: rows, empty: empty}
}

// detailLines draws every field of an entry (docs/SPEC.md §8.2), one to a line.
func (m Model) detailLines(e attempts.Entry, height int) []string {
	zone := m.clock.Now().Location()
	row := func(label, value string) string {
		return m.st.title.Render(fit(label, 12)) + value
	}
	lines := []string{
		row("Result", app.ResultText(e.Outcome)),
		row("Who", app.Who(e)),
		row("Login name", e.LoginName),
		row("Address", e.Address),
		row("Count", strconv.FormatInt(e.Count, 10)),
		row("First", e.FirstAt.In(zone).Format("2006-01-02 15:04:05 MST")),
		row("Last", e.LastAt.In(zone).Format("2006-01-02 15:04:05 MST")),
		row("Client", e.ClientVersion),
	}
	if len(e.Keys) == 0 {
		lines = append(lines, row("Keys", "none offered"))
	}
	for i, k := range e.Keys {
		label := ""
		if i == 0 {
			label = "Keys"
		}
		lines = append(lines, row(label, k))
	}
	if e.KeyOwner != "" {
		lines = append(lines, row("Key owner", "This key belongs to "+e.KeyOwner+"."))
	}
	if len(lines) > height {
		lines = lines[:max(height, 0)]
	}
	return lines
}

// keyAttempts handles a key of the Attempts page that is not one of the keys of every page.
func (m Model) keyAttempts(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	name := msg.String()
	if m.attemptDetail {
		if name == "esc" || name == "enter" {
			m.attemptDetail = false
		}
		return m, nil
	}
	m.attemptList.setHeight(m.height - chrome - 2)
	if m.attemptList.key(name) {
		if e, ok := m.selectedAttempt(); ok {
			m.attemptSel = e.ID
		}
		return m, nil
	}
	ctx, a := m.ctx, m.app
	switch name {
	case "f":
		m.attemptFilter = (m.attemptFilter + 1) % filterCount
		m.syncAttempts()
	case "/":
		m.openLivePrompt("Search:", m.attemptSearch, maxSearchLength,
			func(id int, _ string) tea.Cmd { return func() tea.Msg { return promptDone{id: id} } },
			func(m Model, text string) Model {
				m.attemptSearch = text
				m.syncAttempts()
				return m
			},
			func(m Model) Model {
				m.attemptSearch = ""
				m.syncAttempts()
				return m
			})
	case "enter":
		if _, ok := m.selectedAttempt(); ok {
			m.attemptDetail = true
		}
	case "d":
		if e, ok := m.selectedAttempt(); ok {
			return m, func() tea.Msg { return doneMsg{err: a.RemoveAttempt(ctx, e.ID)} }
		}
	case "C":
		if n := len(m.attempts.Entries); n > 0 {
			m.ask = &asking{
				q:   question{text: "Clear the list of " + plural(n, "attempt") + "?", yes: "Clear"},
				yes: func() tea.Msg { return doneMsg{err: a.ClearAttempts(ctx)} },
			}
		}
	}
	return m, nil
}
