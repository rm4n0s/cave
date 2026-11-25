package gui

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/history"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/glutter/foundation"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/painting"
)

// maxHistoryRows is the most rows of a table of the history that are built; the search finds the
// others.
const maxHistoryRows = 500

// maxPathCell is how many characters of a path the table shows; the beginning goes first, so
// that the name of the file stays. The details show it whole.
const maxPathCell = 48

// The keys of what the tests tap on the History page.
var (
	historyTabsKey        = foundation.NewKey("history-tabs")
	historySearchKey      = foundation.NewKey("history-search")
	clearHistoryKey       = foundation.NewKey("clear-history")
	changeHistoryClearKey = foundation.NewKey("change-history-clear")
	historyConnectionsKey = foundation.NewKey("history-connections")
	historyTransfersKey   = foundation.NewKey("history-transfers")
	historyDetailsKey     = foundation.NewKey("history-details")
)

// historyTab is which records the page shows.
type historyTab int

const (
	historyTabConnections historyTab = iota
	historyTabTransfers
)

// historyPage is the History page (docs/SPEC.md §11.8): the connections that were made and the
// files that were moved.
type historyPage struct {
	m.StatefulWidget
	v *view
}

func newHistoryPage(v *view) *historyPage {
	return &historyPage{StatefulWidget: m.NewStatefulWidgetBase(), v: v}
}

// CreateState creates the state.
func (p *historyPage) CreateState() m.State { return &historyState{} }

type historyState struct {
	m.StateOf[*historyPage]
	tab    historyTab
	search *m.TextEditingController
	// selectedConnection and selectedTransfer are the ids of the records whose details are shown
	// on each tab; "" when there is none.
	selectedConnection, selectedTransfer string
}

// InitState makes the controller of the search field.
func (s *historyState) InitState() { s.search = m.NewTextEditingController() }

// Dispose frees the controller.
func (s *historyState) Dispose() { s.search.Dispose() }

// connections returns the connections the search lets through (who and address), newest first.
func (s *historyState) connections(v *view) []history.Connection {
	needle := strings.ToLower(s.search.Text())
	var out []history.Connection
	for _, c := range v.history.Connections {
		if needle == "" || strings.Contains(strings.ToLower(c.PersonName+"\n"+c.Address), needle) {
			out = append(out, c)
		}
	}
	return out
}

// transfers returns the transfers the search lets through (who and file), newest first.
func (s *historyState) transfers(v *view) []history.Transfer {
	needle := strings.ToLower(s.search.Text())
	var out []history.Transfer
	for _, t := range v.history.Transfers {
		if needle == "" || strings.Contains(strings.ToLower(t.PersonName+"\n"+t.Path), needle) {
			out = append(out, t)
		}
	}
	return out
}

// Build shows the controls, then the table of the tab and the details of its selected record.
func (s *historyState) Build(bc m.BuildContext) m.Widget {
	v := s.Widget().v
	children := []m.Widget{s.controls(bc, v), gap(8), s.info(bc, v), gap(8)}
	var count, total int
	var content m.Widget
	empty := "No connections yet."
	if s.tab == historyTabTransfers {
		visible := s.transfers(v)
		shown := visible[:min(len(visible), maxHistoryRows)]
		count, total, empty = len(visible), len(v.history.Transfers), "No files moved yet."
		if len(shown) > 0 {
			content = s.withDetails(bc, s.transferTable(bc, v, shown), s.transferDetails(bc, v, shown))
		}
	} else {
		visible := s.connections(v)
		shown := visible[:min(len(visible), maxHistoryRows)]
		count, total = len(visible), len(v.history.Connections)
		if len(shown) > 0 {
			content = s.withDetails(bc, s.connectionTable(bc, v, shown), s.connectionDetails(bc, v, shown))
		}
	}
	headline := m.ThemeOf(bc).TextTheme.HeadlineSmall
	switch {
	case total == 0:
		children = append(children, gap(32), m.NewCenter(m.NewText(empty, m.TextOpts{Style: headline})))
	case count == 0:
		children = append(children, gap(32), m.NewCenter(m.NewText("Nothing matches.", m.TextOpts{Style: headline})))
	default:
		children = append(children, content)
		if count > maxHistoryRows {
			children = append(children, gap(8), m.NewText(
				"Showing "+strconv.Itoa(maxHistoryRows)+" of "+strconv.Itoa(count)+". Search to see others.",
				m.TextOpts{Key: foundation.NewKey("history-cap"), Style: m.ThemeOf(bc).TextTheme.BodyMedium}))
		}
	}
	return m.NewSingleChildScrollView(m.NewPadding(m.NewEdgeInsetsAll(16), m.NewColumn(children, m.ColumnOpts{
		CrossAxisAlignment: m.CrossAxisAlignmentStart,
	})))
}

// controls is the first row: the tabs, the search and the button that clears the history.
func (s *historyState) controls(bc m.BuildContext, v *view) m.Widget {
	tabs := m.NewSegmentedButton([]m.ButtonSegment[historyTab]{
		{Value: historyTabConnections, Label: m.NewText("Connections")},
		{Value: historyTabTransfers, Label: m.NewText("Transfers")},
	}, []historyTab{s.tab}, func(sel []historyTab) {
		if len(sel) == 1 {
			s.SetState(func() { s.tab = sel[0] })
		}
	}, m.SegmentedButtonOpts{Key: historyTabsKey})
	search := m.NewSizedBox(m.NewTextField(m.TextFieldOpts{
		Key: historySearchKey, Controller: s.search,
		Decoration: &m.InputDecoration{LabelText: foundation.Some("Search who, address or file"), IsDense: foundation.Some(true)},
		OnChanged:  func(string) { s.SetState(func() {}) },
	}), m.SizedBoxOpts{Width: foundation.Some(280.0)})
	clear := m.NewOutlinedButton(func() { s.askClear(bc, v) }, m.NewText("Clear history"), m.OutlinedButtonOpts{Key: clearHistoryKey})
	return m.NewWrap([]m.Widget{tabs, search, clear}, m.WrapOpts{Spacing: 16, RunSpacing: 8, CrossAxisAlignment: m.WrapCrossAlignmentCenter})
}

// info is the line that says when the history was cleared and when it will be, with the link to
// the setting that decides it.
func (s *historyState) info(bc m.BuildContext, v *view) m.Widget {
	text := "Never cleared"
	if !v.history.LastClearedAt.IsZero() {
		text = "Cleared " + app.Ago(v.now, v.history.LastClearedAt)
	}
	if !v.history.NextClearAt.IsZero() {
		text += " · next clear " + app.Until(v.now, v.history.NextClearAt)
	}
	return m.NewRow([]m.Widget{
		m.NewText(text, m.TextOpts{Key: foundation.NewKey("history-info"), Style: m.ThemeOf(bc).TextTheme.BodyMedium}),
		hgap(8),
		m.NewTextButton(func() { v.showSettings(app.GroupAttempts) }, m.NewText("Change…"), m.TextButtonOpts{Key: changeHistoryClearKey}),
	})
}

// askClear asks, and clears the history.
func (s *historyState) askClear(bc m.BuildContext, v *view) {
	if len(v.history.Connections) == 0 && len(v.history.Transfers) == 0 {
		return
	}
	answer := confirm(bc, "Clear the history", "Clear the history? Connections that are open stay.", "Clear", true)
	waitYes(bc, answer, func() {
		runCommand(bc, v, func(ctx context.Context) error { return v.app.ClearHistory(ctx) })
	})
}

// withDetails is the table, with the details of the selected record (nil for none) beside it on
// a wide window and under it on a narrow one.
func (s *historyState) withDetails(bc m.BuildContext, table, details m.Widget) m.Widget {
	scrolled := m.NewSingleChildScrollView(table, m.SingleChildScrollViewOpts{ScrollDirection: painting.AxisHorizontal})
	if details == nil {
		return scrolled
	}
	if m.MediaQuerySizeOf(bc).Width >= wideFrom {
		return m.NewRow([]m.Widget{m.NewFlexible(scrolled), hgap(16), m.NewSizedBox(details, m.SizedBoxOpts{Width: foundation.Some(320.0)})},
			m.RowOpts{CrossAxisAlignment: m.CrossAxisAlignmentStart})
	}
	return m.NewColumn([]m.Widget{scrolled, gap(16), details}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStart})
}

// heading is the title of a column; the tables of the history are in the order they were
// recorded, newest first, and are not sorted.
func heading(label string, numeric bool) m.DataColumn {
	return m.NewDataColumn(m.NewText(label), m.DataColumnOpts{Numeric: numeric})
}

// connectionDuration is how long a connection lasted, as the page words it.
func connectionDuration(c history.Connection) string {
	if c.Open() {
		return "Connected now"
	}
	if d, ok := c.Duration(); ok {
		return app.DurationText(d)
	}
	return "—"
}

// connectionEnded is how a connection ended, as the table words it; an open one has not.
func connectionEnded(c history.Connection) string {
	if c.Open() {
		return "—"
	}
	return app.ReasonText(c.Reason)
}

// connectionTable is the DataTable of the connections.
func (s *historyState) connectionTable(bc m.BuildContext, v *view, shown []history.Connection) m.Widget {
	columns := []m.DataColumn{heading("Who", false), heading("Address", false), heading("Connected", false),
		heading("Duration", false), heading("Ended", false)}
	rows := make([]m.DataRow, len(shown))
	for i, c := range shown {
		id := c.ID
		rows[i] = m.NewDataRow([]m.DataCell{
			m.NewDataCell(cellText(c.PersonName, 140)),
			m.NewDataCell(cellText(c.Address, 160)),
			m.NewDataCell(m.NewText(app.Ago(v.now, c.ConnectedAt))),
			m.NewDataCell(m.NewText(connectionDuration(c))),
			m.NewDataCell(cellText(connectionEnded(c), 200)),
		}, m.DataRowOpts{
			Key:             foundation.NewValueKey(id),
			Selected:        id == s.selectedConnection,
			OnSelectChanged: func(foundation.Opt[bool]) { s.SetState(func() { s.selectedConnection = id }) },
		})
	}
	return m.NewDataTable(columns, rows, m.DataTableOpts{
		Key: historyConnectionsKey, ColumnSpacing: foundation.Some(20.0), HorizontalMargin: foundation.Some(12.0), HideCheckboxColumn: true,
	})
}

// pathCell is a path as the table shows it: at most maxPathCell characters, its beginning
// replaced by "…" when it is longer.
func pathCell(path string) string {
	runes := []rune(path)
	if len(runes) <= maxPathCell {
		return path
	}
	return "…" + string(runes[len(runes)-maxPathCell+1:])
}

// transferSize is the bytes of a transfer in its direction; both for one that went both ways.
func transferSize(t history.Transfer) string {
	switch t.Direction() {
	case history.Download:
		return app.SizeText(t.Downloaded)
	case history.Both:
		return app.SizeText(t.Downloaded) + " / " + app.SizeText(t.Uploaded)
	}
	return app.SizeText(t.Uploaded)
}

// transferTable is the DataTable of the transfers.
func (s *historyState) transferTable(bc m.BuildContext, v *view, shown []history.Transfer) m.Widget {
	columns := []m.DataColumn{heading("Who", false), heading("File", false), heading("Direction", false),
		heading("Size", true), heading("When", false), heading("Result", false)}
	rows := make([]m.DataRow, len(shown))
	for i, t := range shown {
		id := t.ID
		rows[i] = m.NewDataRow([]m.DataCell{
			m.NewDataCell(cellText(t.PersonName, 120)),
			m.NewDataCell(m.NewText(pathCell(t.Path))),
			m.NewDataCell(m.NewText(app.DirectionText(t.Direction()))),
			m.NewDataCell(m.NewText(transferSize(t))),
			m.NewDataCell(m.NewText(app.Ago(v.now, t.ClosedAt))),
			m.NewDataCell(m.NewText(app.ResultWord(t.Result))),
		}, m.DataRowOpts{
			Key:             foundation.NewValueKey(id),
			Selected:        id == s.selectedTransfer,
			OnSelectChanged: func(foundation.Opt[bool]) { s.SetState(func() { s.selectedTransfer = id }) },
		})
	}
	return m.NewDataTable(columns, rows, m.DataTableOpts{
		Key: historyTransfersKey, ColumnSpacing: foundation.Some(20.0), HorizontalMargin: foundation.Some(12.0), HideCheckboxColumn: true,
	})
}

// detailRow is one field of the details: its label over its value.
func detailRow(bc m.BuildContext, label, value string) m.Widget {
	theme := m.ThemeOf(bc)
	return m.NewPadding(m.NewEdgeInsetsSymmetric(2, 0), m.NewColumn([]m.Widget{
		m.NewText(label, m.TextOpts{Style: theme.TextTheme.LabelMedium}),
		m.NewText(value, m.TextOpts{Style: theme.TextTheme.BodyMedium}),
	}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStart}))
}

// at writes a time of the details in the zone of the view's clock; the zero time is "—".
func at(v *view, t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.In(v.now.Location()).Format("2006-01-02 15:04:05 MST")
}

// connectionDetails is every field of the selected connection, then the files it moved; nil when
// no connection of the table is selected.
func (s *historyState) connectionDetails(bc m.BuildContext, v *view, shown []history.Connection) m.Widget {
	var c history.Connection
	found := false
	for _, candidate := range shown {
		if candidate.ID == s.selectedConnection {
			c, found = candidate, true
		}
	}
	if !found {
		return nil
	}
	theme := m.ThemeOf(bc)
	children := []m.Widget{
		m.NewText("Details", m.TextOpts{Style: theme.TextTheme.TitleMedium}), gap(8),
		detailRow(bc, "Who", c.PersonName),
		detailRow(bc, "Address", c.Address),
		detailRow(bc, "Key", c.KeyFingerprint),
		detailRow(bc, "Client", c.ClientVersion),
		detailRow(bc, "Connected", at(v, c.ConnectedAt)),
		detailRow(bc, "Ended", at(v, c.EndedAt)),
		detailRow(bc, "Duration", connectionDuration(c)),
		detailRow(bc, "How it ended", app.ReasonText(c.Reason)),
	}
	if !c.Open() && c.Reason != history.ReasonUnknown {
		children = append(children, detailRow(bc, "Data", "in "+app.SizeText(c.BytesIn)+" · out "+app.SizeText(c.BytesOut)))
	}
	activities := make([]server.Activity, len(c.Activities))
	for i, a := range c.Activities {
		activities[i] = server.Activity{Kind: server.ActivityKind(a.Kind), Detail: a.Detail}
	}
	opened := app.ActivitiesText(activities)
	if c.MoreActivities > 0 {
		opened += " and " + strconv.Itoa(c.MoreActivities) + " more"
	}
	children = append(children, detailRow(bc, "Opened", opened), gap(8), m.NewText("Files", m.TextOpts{Style: theme.TextTheme.TitleSmall}))
	n := 0
	for _, t := range v.history.Transfers {
		if t.ConnectionID != c.ID {
			continue
		}
		n++
		children = append(children, m.NewPadding(m.NewEdgeInsetsSymmetric(2, 0), m.NewText(
			app.DirectionText(t.Direction())+" · "+transferSize(t)+" · "+app.ResultWord(t.Result)+"\n"+t.Path,
			m.TextOpts{Style: theme.TextTheme.BodySmall})))
	}
	if n == 0 {
		children = append(children, m.NewText("No files moved.", m.TextOpts{Style: theme.TextTheme.BodySmall}))
	}
	return card(children, historyDetailsKey)
}

// transferDetails is every field of the selected transfer, with its whole path; nil when no
// transfer of the table is selected.
func (s *historyState) transferDetails(bc m.BuildContext, v *view, shown []history.Transfer) m.Widget {
	var t history.Transfer
	found := false
	for _, candidate := range shown {
		if candidate.ID == s.selectedTransfer {
			t, found = candidate, true
		}
	}
	if !found {
		return nil
	}
	connection := t.ConnectionID
	for _, c := range v.history.Connections {
		if c.ID == t.ConnectionID {
			connection += " from " + c.Address
		}
	}
	return card([]m.Widget{
		m.NewText("Details", m.TextOpts{Style: m.ThemeOf(bc).TextTheme.TitleMedium}), gap(8),
		detailRow(bc, "Who", t.PersonName),
		detailRow(bc, "File", t.Path),
		detailRow(bc, "Direction", app.DirectionText(t.Direction())),
		detailRow(bc, "Downloaded", app.SizeText(t.Downloaded)),
		detailRow(bc, "Uploaded", app.SizeText(t.Uploaded)),
		detailRow(bc, "Opened", at(v, t.OpenedAt)),
		detailRow(bc, "Closed", at(v, t.ClosedAt)),
		detailRow(bc, "Duration", app.DurationText(t.ClosedAt.Sub(t.OpenedAt))),
		detailRow(bc, "Result", app.ResultWord(t.Result)),
		detailRow(bc, "Connection", connection),
	}, historyDetailsKey)
}
