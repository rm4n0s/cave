package gui

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/glutter/foundation"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/painting"
)

// maxAttemptRows is the most rows of the table that are built: 5 000 rows of widgets are too many,
// and nobody reads them. The search finds the others.
const maxAttemptRows = 500

// The keys of what the tests tap on the Attempts page.
var (
	attemptSearchKey = foundation.NewKey("attempt-search")
	clearListKey     = foundation.NewKey("clear-list")
	changeClearKey   = foundation.NewKey("change-clear")
	removeAttemptKey = foundation.NewKey("remove-attempt")
	attemptsTableKey = foundation.NewKey("attempts-table")
)

// attemptFilter is which results the table shows.
type attemptFilter int

const (
	filterAll attemptFilter = iota
	filterAccepted
	filterRefused
)

// The columns of the table, in order.
const (
	colWho = iota
	colLogin
	colAddress
	colResult
	colCount
	colFirst
	colLast
)

// attemptsPage is the third page (docs/SPEC.md §11.3): the table of who tried to connect.
type attemptsPage struct {
	m.StatefulWidget
	v *view
}

func newAttemptsPage(v *view) *attemptsPage {
	return &attemptsPage{StatefulWidget: m.NewStatefulWidgetBase(), v: v}
}

// CreateState creates the state.
func (p *attemptsPage) CreateState() m.State { return &attemptsState{sortColumn: colLast} }

type attemptsState struct {
	m.StateOf[*attemptsPage]
	filter     attemptFilter
	search     *m.TextEditingController
	sortColumn int
	// ascending is the order of the sort; the default, newest first, is descending.
	ascending bool
	// selected is the ID of the entry whose details are shown; "" when there is none.
	selected string
}

// InitState makes the controller of the search field.
func (s *attemptsState) InitState() { s.search = m.NewTextEditingController() }

// Dispose frees the controller.
func (s *attemptsState) Dispose() { s.search.Dispose() }

// matches says whether the entry passes the filter and the search. The search ignores case and
// looks at who, the login name and the address.
func (s *attemptsState) matches(e attempts.Entry, needle string) bool {
	switch s.filter {
	case filterAccepted:
		if e.Outcome != attempts.Accepted {
			return false
		}
	case filterRefused:
		if e.Outcome == attempts.Accepted {
			return false
		}
	}
	return needle == "" || strings.Contains(strings.ToLower(app.Who(e)+"\n"+e.LoginName+"\n"+e.Address), needle)
}

// visible returns the entries that match, in the order of the sort.
func (s *attemptsState) visible(v *view) []attempts.Entry {
	needle := strings.ToLower(s.search.Text())
	var out []attempts.Entry
	for _, e := range v.attempts.Entries {
		if s.matches(e, needle) {
			out = append(out, e)
		}
	}
	slices.SortStableFunc(out, func(a, b attempts.Entry) int {
		c := s.compare(a, b)
		if !s.ascending {
			c = -c
		}
		return c
	})
	return out
}

// compare orders two entries by the column that is sorted.
func (s *attemptsState) compare(a, b attempts.Entry) int {
	switch s.sortColumn {
	case colWho:
		return cmp.Compare(strings.ToLower(app.Who(a)), strings.ToLower(app.Who(b)))
	case colLogin:
		return cmp.Compare(a.LoginName, b.LoginName)
	case colAddress:
		return cmp.Compare(a.Address, b.Address)
	case colResult:
		return cmp.Compare(app.ResultText(a.Outcome), app.ResultText(b.Outcome))
	case colCount:
		return cmp.Compare(a.Count, b.Count)
	case colFirst:
		return a.FirstAt.Compare(b.FirstAt)
	}
	return a.LastAt.Compare(b.LastAt)
}

// Build shows the controls, the table and the details of the selected entry.
func (s *attemptsState) Build(bc m.BuildContext) m.Widget {
	v := s.Widget().v
	theme := m.ThemeOf(bc)
	visible := s.visible(v)
	shown := visible[:min(len(visible), maxAttemptRows)]
	children := []m.Widget{s.controls(bc, v), gap(8), s.info(bc, v), gap(8)}
	switch {
	case len(v.attempts.Entries) == 0:
		children = append(children, gap(32), m.NewCenter(m.NewText("Nobody tried to connect yet.", m.TextOpts{Style: theme.TextTheme.HeadlineSmall})))
	case len(visible) == 0:
		children = append(children, gap(32), m.NewCenter(m.NewText("Nothing matches.", m.TextOpts{Style: theme.TextTheme.HeadlineSmall})))
	default:
		children = append(children, s.tableAndDetails(bc, v, shown))
		if len(visible) > len(shown) {
			children = append(children, gap(8), m.NewText(
				"Showing "+strconv.Itoa(len(shown))+" of "+strconv.Itoa(len(visible))+". Search to see others.",
				m.TextOpts{Key: foundation.NewKey("attempts-cap"), Style: theme.TextTheme.BodyMedium}))
		}
	}
	return m.NewSingleChildScrollView(m.NewPadding(m.NewEdgeInsetsAll(16), m.NewColumn(children, m.ColumnOpts{
		CrossAxisAlignment: m.CrossAxisAlignmentStart,
	})))
}

// controls is the first row: the filter, the search and the button that clears the list.
func (s *attemptsState) controls(bc m.BuildContext, v *view) m.Widget {
	filters := m.NewSegmentedButton([]m.ButtonSegment[attemptFilter]{
		{Value: filterAll, Label: m.NewText("All")},
		{Value: filterAccepted, Label: m.NewText("Accepted")},
		{Value: filterRefused, Label: m.NewText("Refused")},
	}, []attemptFilter{s.filter}, func(sel []attemptFilter) {
		if len(sel) == 1 {
			s.SetState(func() { s.filter = sel[0] })
		}
	}, m.SegmentedButtonOpts{Key: foundation.NewKey("attempt-filter")})
	search := m.NewSizedBox(m.NewTextField(m.TextFieldOpts{
		Key: attemptSearchKey, Controller: s.search,
		Decoration: &m.InputDecoration{LabelText: foundation.Some("Search who, login name or address"), IsDense: foundation.Some(true)},
		OnChanged:  func(string) { s.SetState(func() {}) },
	}), m.SizedBoxOpts{Width: foundation.Some(320.0)})
	clear := m.NewOutlinedButton(func() { s.askClear(bc, v) }, m.NewText("Clear list"), m.OutlinedButtonOpts{Key: clearListKey})
	return m.NewWrap([]m.Widget{filters, search, clear}, m.WrapOpts{Spacing: 16, RunSpacing: 8, CrossAxisAlignment: m.WrapCrossAlignmentCenter})
}

// info is the line that says when the list was cleared and when it will be, with the link to the
// setting that decides it.
func (s *attemptsState) info(bc m.BuildContext, v *view) m.Widget {
	text := "Never cleared"
	if !v.attempts.LastClearedAt.IsZero() {
		text = "Cleared " + app.Ago(v.now, v.attempts.LastClearedAt)
	}
	if !v.attempts.NextClearAt.IsZero() {
		text += " · next clear " + app.Until(v.now, v.attempts.NextClearAt)
	}
	return m.NewRow([]m.Widget{
		m.NewText(text, m.TextOpts{Key: foundation.NewKey("attempts-info"), Style: m.ThemeOf(bc).TextTheme.BodyMedium}),
		hgap(8),
		m.NewTextButton(func() { v.selectPage(pageSettings) }, m.NewText("Change…"), m.TextButtonOpts{Key: changeClearKey}),
	})
}

// askClear asks, and clears the list.
func (s *attemptsState) askClear(bc m.BuildContext, v *view) {
	n := len(v.attempts.Entries)
	if n == 0 {
		return
	}
	answer := confirm(bc, "Clear the list", "Clear the list of "+plural(n, "attempt")+"?", "Clear", true)
	waitYes(bc, answer, func() {
		runCommand(bc, v, func(ctx context.Context) error { return v.app.ClearAttempts(ctx) })
	})
}

// selectedEntry returns the entry whose details are shown, if it is still in the list.
func (s *attemptsState) selectedEntry(shown []attempts.Entry) (attempts.Entry, bool) {
	for _, e := range shown {
		if e.ID == s.selected {
			return e, true
		}
	}
	return attempts.Entry{}, false
}

// tableAndDetails is the table, with the details of the selected entry beside it on a wide window
// and under it on a narrow one.
func (s *attemptsState) tableAndDetails(bc m.BuildContext, v *view, shown []attempts.Entry) m.Widget {
	table := m.NewSingleChildScrollView(s.table(bc, v, shown), m.SingleChildScrollViewOpts{ScrollDirection: painting.AxisHorizontal})
	entry, ok := s.selectedEntry(shown)
	if !ok {
		return table
	}
	details := s.details(bc, v, entry)
	if m.MediaQuerySizeOf(bc).Width >= wideFrom {
		return m.NewRow([]m.Widget{m.NewFlexible(table), hgap(16), m.NewSizedBox(details, m.SizedBoxOpts{Width: foundation.Some(320.0)})},
			m.RowOpts{CrossAxisAlignment: m.CrossAxisAlignmentStart})
	}
	return m.NewColumn([]m.Widget{table, gap(16), details}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStart})
}

// table is the DataTable of the entries.
func (s *attemptsState) table(bc m.BuildContext, v *view, shown []attempts.Entry) m.Widget {
	sort := func(column int, ascending bool) {
		s.SetState(func() { s.sortColumn, s.ascending = column, ascending })
	}
	heading := func(label string, numeric bool) m.DataColumn {
		return m.NewDataColumn(m.NewText(label), m.DataColumnOpts{Numeric: numeric, OnSort: sort})
	}
	columns := []m.DataColumn{
		heading("Who", false), heading("Login name", false), heading("Address", false),
		heading("Result", false), heading("Count", true), heading("First", false), heading("Last", false),
	}
	rows := make([]m.DataRow, len(shown))
	for i, e := range shown {
		id := e.ID
		rows[i] = m.NewDataRow([]m.DataCell{
			m.NewDataCell(cellText(app.Who(e), 140)),
			m.NewDataCell(cellText(e.LoginName, 180)),
			m.NewDataCell(cellText(e.Address, 160)),
			m.NewDataCell(cellText(app.ResultText(e.Outcome), 200)),
			m.NewDataCell(m.NewText(strconv.FormatInt(e.Count, 10))),
			m.NewDataCell(m.NewText(app.Ago(v.now, e.FirstAt))),
			m.NewDataCell(m.NewText(app.Ago(v.now, e.LastAt))),
		}, m.DataRowOpts{
			Key:      foundation.NewValueKey(id),
			Selected: id == s.selected,
			OnSelectChanged: func(foundation.Opt[bool]) {
				s.SetState(func() { s.selected = id })
			},
		})
	}
	return m.NewDataTable(columns, rows, m.DataTableOpts{
		Key:                attemptsTableKey,
		ColumnSpacing:      foundation.Some(20.0),
		HorizontalMargin:   foundation.Some(12.0),
		SortColumnIndex:    foundation.Some(s.sortColumn),
		SortDescending:     !s.ascending,
		HideCheckboxColumn: true,
	})
}

// cellText is the text of a cell, on one line that is at most width wide: a login name that a
// client made very long is cut with "…" in the table, and shown whole in the details.
func cellText(text string, width float64) m.Widget {
	return m.NewConstrainedBox(m.NewBoxConstraints(m.BoxConstraintsOpts{MaxWidth: foundation.Some(width)}),
		m.NewText(text, m.TextOpts{Overflow: painting.TextOverflowEllipsis, MaxLines: foundation.Some(1)}))
}

// details is every field of the selected entry, and the button that removes it.
func (s *attemptsState) details(bc m.BuildContext, v *view, e attempts.Entry) m.Widget {
	theme := m.ThemeOf(bc)
	zone := v.now.Location()
	row := func(label, value string) m.Widget {
		return m.NewPadding(m.NewEdgeInsetsSymmetric(2, 0), m.NewColumn([]m.Widget{
			m.NewText(label, m.TextOpts{Style: theme.TextTheme.LabelMedium}),
			m.NewText(value, m.TextOpts{Style: theme.TextTheme.BodyMedium}),
		}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStart}))
	}
	children := []m.Widget{
		m.NewText("Details", m.TextOpts{Style: theme.TextTheme.TitleMedium}), gap(8),
		row("Result", app.ResultText(e.Outcome)),
		row("Who", app.Who(e)),
		row("Login name", e.LoginName),
		row("Address", e.Address),
		row("Count", strconv.FormatInt(e.Count, 10)),
		row("First attempt", e.FirstAt.In(zone).Format("2006-01-02 15:04:05 MST")),
		row("Last attempt", e.LastAt.In(zone).Format("2006-01-02 15:04:05 MST")),
		row("Client", e.ClientVersion),
	}
	if len(e.Keys) == 0 {
		children = append(children, row("Keys", "none offered"))
	}
	for i, k := range e.Keys {
		label := ""
		if i == 0 {
			label = "Keys"
		}
		children = append(children, row(label, k))
	}
	if e.KeyOwner != "" {
		children = append(children, row("Key owner", "This key belongs to "+e.KeyOwner+"."))
	}
	children = append(children, gap(12), m.NewOutlinedButton(func() {
		runCommand(bc, v, func(ctx context.Context) error { return v.app.RemoveAttempt(ctx, e.ID) })
	}, m.NewText("Remove"), m.OutlinedButtonOpts{Key: removeAttemptKey}))
	return card(children, foundation.NewKey("attempt-details"))
}
