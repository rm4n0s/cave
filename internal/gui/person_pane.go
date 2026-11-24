package gui

import (
	"context"
	"strings"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/glutter/desktop"
	"github.com/rm4n0s/glutter/foundation"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/rendering"
)

// The keys of what the tests tap in a person's pane.
var (
	addKeysKey   = foundation.NewKey("add-keys")
	importKeyKey = foundation.NewKey("import-keys")
	keysFieldKey = foundation.NewKey("field-pasted-keys")
	commentKey   = foundation.NewKey("field-comment")
	pathFieldKey = foundation.NewKey("field-path")
)

func keyRowKey(id string) foundation.Key          { return foundation.NewKey("key-" + id) }
func editCommentKey(id string) foundation.Key     { return foundation.NewKey("edit-comment-" + id) }
func removeKeyButtonKey(id string) foundation.Key { return foundation.NewKey("remove-key-" + id) }

// personPane is one person: who they are, and their keys. It reads the view itself, because on a
// narrow window it is a page of its own, made by the navigator.
type personPane struct {
	m.StatelessWidget
	id string
	// page says that the pane is a page of its own, with a bar and a back button.
	page bool
}

func newPersonPane(id string, page bool) *personPane {
	return &personPane{StatelessWidget: m.NewStatelessWidgetBase(), id: id, page: page}
}

// Build shows the person, or that they are gone.
func (w *personPane) Build(bc m.BuildContext) m.Widget {
	v := viewOf(bc)
	theme := m.ThemeOf(bc)
	p, ok := findPerson(v, w.id)
	title, body := "Person", m.Widget(m.NewCenter(m.NewText("This person is gone.", m.TextOpts{Style: theme.TextTheme.BodyLarge})))
	if ok {
		title = p.Name
		body = column(w.content(bc, v, p))
	}
	if !w.page {
		return body
	}
	return m.NewScaffold(m.ScaffoldOpts{
		AppBar: m.NewAppBar(m.AppBarOpts{Title: m.NewText(title)}),
		Body:   body,
	})
}

// content is the pane of a person that exists.
func (w *personPane) content(bc m.BuildContext, v *view, p people.Person) []m.Widget {
	theme := m.ThemeOf(bc)
	status := "Disabled"
	if p.Enabled {
		status = "Enabled"
	}
	if n := app.CountConnections(v.connections, p.ID, ""); n > 0 {
		status += " · " + plural(n, "connection")
	}
	children := []m.Widget{
		m.NewText(p.Name, m.TextOpts{Key: foundation.NewKey("pane-name"), Style: theme.TextTheme.HeadlineMedium}),
		gap(4),
		m.NewText(status, m.TextOpts{Key: foundation.NewKey("pane-status"), Style: theme.TextTheme.BodyMedium}),
		gap(16),
		m.NewRow([]m.Widget{
			m.NewOutlinedButton(func() { renamePerson(bc, v, p) }, m.NewText("Rename"), m.OutlinedButtonOpts{Key: renamePersonKey}),
			hgap(12),
			m.NewOutlinedButton(func() { removePerson(bc, v, p, w.page) }, m.NewText("Remove"), m.OutlinedButtonOpts{
				Key: removePersonKey,
				Style: m.OutlinedButtonStyleFrom(m.ButtonStyleFromOpts{
					ForegroundColor: foundation.Some(theme.ColorScheme.Error()),
				}),
			}),
		}),
		gap(24),
		m.NewText("Keys", m.TextOpts{Style: theme.TextTheme.TitleLarge}),
		gap(8),
		m.NewWrap([]m.Widget{
			m.NewFilledButton(func() { addKeys(bc, v, p) }, m.NewText("Add keys"), m.FilledButtonOpts{Key: addKeysKey}),
			m.NewOutlinedButton(func() { importKeys(bc, v, p) }, m.NewText("Import from file…"), m.OutlinedButtonOpts{Key: importKeyKey}),
		}, m.WrapOpts{Spacing: 12, RunSpacing: 8}),
		gap(12),
	}
	if len(p.Keys) == 0 {
		return append(children, m.NewText("No key yet, so "+p.Name+" cannot connect.", m.TextOpts{
			Key: foundation.NewKey("no-key"), Style: theme.TextTheme.BodyMedium,
		}))
	}
	for _, k := range p.Keys {
		children = append(children, keyRow(bc, v, p, k), gap(8))
	}
	return children
}

// keyRow is one key: its type, its fingerprint, its comment, when it was added, and the buttons.
func keyRow(bc m.BuildContext, v *view, p people.Person, k people.Key) m.Widget {
	theme := m.ThemeOf(bc)
	scheme := theme.ColorScheme
	comment := m.NewText("No comment", m.TextOpts{Style: theme.TextTheme.BodyMedium.CopyWith(m.TextStyleCopy{Color: foundation.Some(scheme.OnSurfaceVariant())})})
	if k.Comment != "" {
		comment = m.NewText(k.Comment, m.TextOpts{Key: foundation.NewKey("comment-" + k.ID), Style: theme.TextTheme.BodyMedium})
	}
	return m.NewCard(m.NewPadding(m.NewEdgeInsetsAll(12), m.NewColumn([]m.Widget{
		m.NewRow([]m.Widget{
			m.NewExpanded(m.NewText(k.Type, m.TextOpts{Style: theme.TextTheme.TitleSmall})),
			m.NewIconButton(func() { editComment(bc, v, p, k) }, m.NewIcon(m.Icons.Edit()), m.IconButtonOpts{
				Key: editCommentKey(k.ID), Tooltip: "Edit the comment",
			}),
			m.NewIconButton(func() { removeKey(bc, v, p, k) }, m.NewIcon(m.Icons.Delete()), m.IconButtonOpts{
				Key: removeKeyButtonKey(k.ID), Tooltip: "Remove the key",
			}),
		}),
		m.NewLayoutBuilder(func(_ m.BuildContext, c rendering.BoxConstraints) m.Widget {
			return m.NewTooltip(m.NewText(cutMiddle(k.Fingerprint, c.MaxWidth), m.TextOpts{
				Key:   foundation.NewKey("fingerprint-" + k.ID),
				Style: monoStyle(theme.TextTheme.BodyMedium),
			}), m.TooltipOpts{Message: k.Fingerprint})
		}),
		gap(4),
		comment,
		m.NewText("Added "+k.AddedAt.Format("2006-01-02"), m.TextOpts{Style: theme.TextTheme.BodySmall}),
	}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStart})), m.CardOpts{Margin: m.NewEdgeInsetsAll(0), Key: keyRowKey(k.ID)})
}

// monoCharWidth is about how wide a character of the monospace text is, in logical pixels.
const monoCharWidth = 8.0

// cutMiddle shortens a text that does not fit the width (about) by putting … in the middle,
// so that both its start and its end can be read.
func cutMiddle(text string, width float64) string {
	room := int(width / monoCharWidth)
	runes := []rune(text)
	if room >= len(runes) || room < 8 {
		return text
	}
	keep := room - 1
	head := keep / 2
	return string(runes[:head]) + "…" + string(runes[len(runes)-(keep-head):])
}

// renamePerson opens the dialog that changes a person's name.
func renamePerson(bc m.BuildContext, v *view, p people.Person) {
	showForm(bc, v.run, "Rename "+p.Name, "Rename", []fieldSpec{nameField(p.Name)},
		textAction(func(ctx context.Context, name string) error { return v.app.RenamePerson(ctx, p.ID, name) }))
}

// removePerson asks, and removes the person. On a page of its own it goes back afterwards.
func removePerson(bc m.BuildContext, v *view, p people.Person, page bool) {
	answer := confirm(bc, "Remove person", removeQuestion(v, p), "Remove", true)
	waitYes(bc, answer, func() {
		v.run(func(ctx context.Context) error { return v.app.RemovePerson(ctx, p.ID) }, func(err error) {
			if err != nil {
				showSnack(bc, app.Message(err))
				return
			}
			if page {
				m.NavigatorPop(bc)
			}
		})
	})
}

// addKeys opens the dialog that adds the keys that the user pastes.
func addKeys(bc m.BuildContext, v *view, p people.Person) {
	showForm(bc, v.run, "Add keys to "+p.Name, "Add", []fieldSpec{
		{key: keysFieldKey, label: "Paste public keys, one per line", lines: 6},
	}, textAction(func(ctx context.Context, text string) error {
		_, err := v.app.AddKeys(ctx, p.ID, text)
		return err
	}))
}

// importKeys asks for a file with the system's file dialog and adds the keys in it. A desktop
// that has no file dialog gets a dialog that asks for the path instead.
func importKeys(bc m.BuildContext, v *view, p people.Person) {
	var noDialog bool
	v.run(func(ctx context.Context) error {
		res, err := desktop.OpenFiles(ctx, bc, desktop.OpenFilesOpts{
			Title: "Import public keys", AcceptLabel: "Import",
			Filters: []desktop.FileFilter{
				{Name: "Public keys", Patterns: []string{"*.pub", "authorized_keys"}},
				{Name: "All files", Patterns: []string{"*"}},
			},
		})
		if err != nil {
			noDialog = true
			return nil
		}
		if res.Cancelled || len(res.Paths) == 0 {
			return nil
		}
		_, err = v.app.ImportKeys(ctx, p.ID, res.Paths[0])
		return err
	}, func(err error) {
		switch {
		case noDialog:
			importByPath(bc, v, p)
		case err != nil:
			showSnack(bc, app.Message(err))
		}
	})
}

// importByPath asks for the path of the file to import.
func importByPath(bc m.BuildContext, v *view, p people.Person) {
	showForm(bc, v.run, "Import keys", "Import", []fieldSpec{
		{key: pathFieldKey, label: "Path of the file", lines: 1},
	}, textAction(func(ctx context.Context, path string) error {
		_, err := v.app.ImportKeys(ctx, p.ID, strings.TrimSpace(path))
		return err
	}))
}

// editComment opens the dialog that changes the comment of a key.
func editComment(bc m.BuildContext, v *view, p people.Person, k people.Key) {
	showForm(bc, v.run, "Comment of the key", "Save", []fieldSpec{
		{key: commentKey, label: "Comment", initial: k.Comment, lines: 1},
	}, textAction(func(ctx context.Context, comment string) error {
		return v.app.SetKeyComment(ctx, p.ID, k.ID, comment)
	}))
}

// removeKey asks, and removes the key. The question says how many connections it closes.
func removeKey(bc m.BuildContext, v *view, p people.Person, k people.Key) {
	text := "Remove this key? " + k.Fingerprint
	if n := app.CountConnections(v.connections, p.ID, k.ID); n > 0 {
		text += " This closes " + plural(n, "connection") + "."
	}
	answer := confirm(bc, "Remove key", text, "Remove", true)
	waitYes(bc, answer, func() {
		runCommand(bc, v, func(ctx context.Context) error { return v.app.RemoveKey(ctx, p.ID, k.ID) })
	})
}
