package gui

import (
	"context"
	"strconv"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	m "github.com/rm4n0s/glutter/material"
)

// listWidth is the width of the list of people when the page has two panes.
const listWidth = 320.0

// The keys of what the tests tap on the People page.
var (
	addPersonButtonKey = foundation.NewKey("add-person")
	renamePersonKey    = foundation.NewKey("rename-person")
	removePersonKey    = foundation.NewKey("remove-person")
)

func personRowKey(id string) foundation.Key     { return foundation.NewKey("person-" + id) }
func personSwitchKey(id string) foundation.Key  { return foundation.NewKey("switch-" + id) }
func connectedMarkKey(id string) foundation.Key { return foundation.NewKey("connected-" + id) }

// plural writes a count and a noun: "1 key", "2 keys".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// keysText is how many keys a person has, for a row of the list.
func keysText(n int) string {
	if n == 0 {
		return "No key"
	}
	return plural(n, "key")
}

// findPerson returns the person with the ID.
func findPerson(v *view, id string) (people.Person, bool) {
	for _, p := range v.people {
		if p.ID == id {
			return p, true
		}
	}
	return people.Person{}, false
}

// runCommand runs a command of the application away from the UI thread. A failure is shown in a
// snack bar: a command that has no dialog has nowhere else to say it.
func runCommand(bc m.BuildContext, v *view, work func(ctx context.Context) error) {
	v.run(work, func(err error) {
		if err != nil {
			showSnack(bc, app.Message(err))
		}
	})
}

// peoplePage is the second page (docs/SPEC.md §11.2): the list of people and, beside it on a
// wide window, the person who is selected.
type peoplePage struct {
	m.StatefulWidget
	v *view
}

func newPeoplePage(v *view) *peoplePage {
	return &peoplePage{StatefulWidget: m.NewStatefulWidgetBase(), v: v}
}

// CreateState creates the state.
func (p *peoplePage) CreateState() m.State { return &peopleState{} }

type peopleState struct {
	m.StateOf[*peoplePage]
	// selected is the ID of the person who is selected; "" when nobody is. It is kept by ID, so
	// that the selection follows the person when the list changes.
	selected string
}

// Build shows the list, with the person beside it when the window is wide.
func (s *peopleState) Build(bc m.BuildContext) m.Widget {
	v := s.Widget().v
	if len(v.people) == 0 {
		return s.nobody(bc, v)
	}
	list := s.list(bc, v)
	if m.MediaQuerySizeOf(bc).Width < wideFrom {
		return list
	}
	var pane m.Widget
	if _, ok := findPerson(v, s.selected); ok {
		pane = newPersonPane(s.selected, false)
	} else {
		pane = m.NewCenter(m.NewText("Select a person.", m.TextOpts{Style: m.ThemeOf(bc).TextTheme.BodyLarge}))
	}
	return m.NewRow([]m.Widget{
		m.NewSizedBox(list, m.SizedBoxOpts{Width: foundation.Some(listWidth)}),
		m.NewVerticalDivider(m.VerticalDividerOpts{Width: foundation.Some(1.0)}),
		m.NewExpanded(pane),
	}, m.RowOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch})
}

// nobody is the page when there is nobody yet.
func (s *peopleState) nobody(bc m.BuildContext, v *view) m.Widget {
	return m.NewCenter(m.NewColumn([]m.Widget{
		m.NewText("Nobody yet.", m.TextOpts{Style: m.ThemeOf(bc).TextTheme.HeadlineSmall}),
		gap(16),
		m.NewFilledButton(func() { addPerson(bc, v) }, m.NewText("Add person"), m.FilledButtonOpts{Key: addPersonButtonKey}),
	}, m.ColumnOpts{MainAxisSize: m.MainAxisSizeMin}))
}

// list is the button that adds a person above the rows.
func (s *peopleState) list(bc m.BuildContext, v *view) m.Widget {
	return m.NewColumn([]m.Widget{
		m.NewPadding(m.NewEdgeInsetsAll(12), m.NewRow([]m.Widget{
			m.NewExpanded(m.NewFilledButton(func() { addPerson(bc, v) }, m.NewText("Add person"), m.FilledButtonOpts{Key: addPersonButtonKey})),
		})),
		m.NewExpanded(m.NewListViewBuilder(func(bc m.BuildContext, i int) m.Widget {
			return s.row(bc, v, v.people[i])
		}, m.ListViewOpts{ItemCount: foundation.Some(len(v.people))})),
	})
}

// row is one person: the name, the number of keys, a mark when they are connected, and the switch.
func (s *peopleState) row(bc m.BuildContext, v *view, p people.Person) m.Widget {
	theme := m.ThemeOf(bc)
	sub := []m.Widget{m.NewText(keysText(len(p.Keys)))}
	if app.CountConnections(v.connections, p.ID, "") > 0 {
		sub = append(sub, hgap(8), m.NewText("connected", m.TextOpts{
			Key:   connectedMarkKey(p.ID),
			Style: theme.TextTheme.LabelMedium.CopyWith(m.TextStyleCopy{Color: foundation.Some(runningColor)}),
		}))
	}
	return m.NewListTile(m.ListTileOpts{
		Key:      personRowKey(p.ID),
		Title:    m.NewText(p.Name),
		Subtitle: m.NewRow(sub),
		Selected: p.ID == s.selected,
		Trailing: m.NewSwitch(p.Enabled, func(on bool) { setEnabled(bc, v, p, on) }, m.SwitchOpts{Key: personSwitchKey(p.ID)}),
		OnTap:    func() { s.selectPerson(bc, p.ID) },
	})
}

// selectPerson selects a person. On a narrow window the person has a page of their own.
func (s *peopleState) selectPerson(bc m.BuildContext, id string) {
	s.SetState(func() { s.selected = id })
	if m.MediaQuerySizeOf(bc).Width >= wideFrom {
		return
	}
	m.NavigatorPush(bc, m.NewMaterialPageRoute(func(m.BuildContext) m.Widget { return newPersonPane(id, true) }))
}

// setEnabled enables or disables a person. Disabling a person who is connected asks first,
// because it disconnects them.
func setEnabled(bc m.BuildContext, v *view, p people.Person, on bool) {
	do := func() {
		runCommand(bc, v, func(ctx context.Context) error { return v.app.SetPersonEnabled(ctx, p.ID, on) })
	}
	if on || app.CountConnections(v.connections, p.ID, "") == 0 {
		do()
		return
	}
	answer := confirm(bc, "Disable person", p.Name+" is connected. Disable and disconnect?", "Disable", true)
	waitYes(bc, answer, do)
}

// waitYes waits for the answer of a confirm away from the UI thread and calls then on the UI
// thread when it is yes. It is for code that has no state of its own to wait with.
func waitYes(bc m.BuildContext, answer <-chan bool, then func()) {
	post := m.WidgetsBindingOf(bc).Post
	go func() {
		if yes, ok := <-answer; ok && yes {
			post(then)
		}
	}()
}

// nameField is the field of a person's name.
func nameField(initial string) fieldSpec {
	return fieldSpec{key: foundation.NewKey("field-name"), label: "Name", initial: initial, lines: 1}
}

// addPerson opens the dialog that adds a person. A person needs a key to connect, so the dialog
// asks for the name and the keys together and adds nobody unless both are acceptable: the
// sentence of a refusal goes under the field it belongs to.
func addPerson(bc m.BuildContext, v *view) {
	showForm(bc, v.run, "Add person", "Add", []fieldSpec{
		nameField(""),
		{key: foundation.NewKey("field-keys"), label: "Public keys", lines: 5},
	}, func(ctx context.Context, values []string) formResult {
		if _, err := v.app.AddPerson(ctx, values[0], values[1]); err != nil {
			field := 1
			if appErr, ok := errors.FromError(err); ok && (appErr.Tag == people.TagPersonNameInvalid || appErr.Tag == people.TagPersonNameTaken) {
				field = 0
			}
			return formResult{errors: map[int]string{field: app.Message(err)}}
		}
		return formResult{done: true}
	})
}

// removeQuestion is what the dialog says before a person is removed: what goes with them, and how
// many connections it closes.
func removeQuestion(v *view, p people.Person) string {
	text := "Remove " + p.Name
	if len(p.Keys) > 0 {
		text += " and their " + plural(len(p.Keys), "key")
	}
	text += "?"
	if n := app.CountConnections(v.connections, p.ID, ""); n > 0 {
		text += " This closes " + plural(n, "connection") + "."
	}
	return text
}
