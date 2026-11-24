package gui

import (
	"context"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/glutter/foundation"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/ui"
)

// The keys of what the tests tap in a dialog.
var (
	confirmYesKey    = foundation.NewKey("confirm-yes")
	confirmCancelKey = foundation.NewKey("confirm-cancel")
	formActionKey    = foundation.NewKey("form-action")
	formCancelKey    = foundation.NewKey("form-cancel")
)

// destructiveColor is the color of the button that destroys something.
func destructiveColor(bc m.BuildContext) ui.Color { return m.ThemeOf(bc).ColorScheme.Error() }

// confirm shows a dialog with Cancel and the button yes. The channel gets true when the user
// chose yes and false (or is closed) otherwise. A button that destroys something (destructive)
// is in the error color. Enter does not choose yes: only a tap does.
func confirm(bc m.BuildContext, title, text, yes string, destructive bool) <-chan bool {
	return m.ShowDialog[bool](bc, func(bc m.BuildContext) m.Widget {
		var style *m.ButtonStyle
		if destructive {
			style = m.TextButtonStyleFrom(m.ButtonStyleFromOpts{ForegroundColor: foundation.Some(destructiveColor(bc))})
		}
		return m.NewAlertDialog(m.AlertDialogOpts{
			Title:   m.NewText(title),
			Content: m.NewText(text),
			Actions: []m.Widget{
				m.NewTextButton(func() { m.NavigatorPop(bc, false) }, m.NewText("Cancel"), m.TextButtonOpts{Key: confirmCancelKey}),
				m.NewTextButton(func() { m.NavigatorPop(bc, true) }, m.NewText(yes), m.TextButtonOpts{Key: confirmYesKey, Style: style}),
			},
		})
	})
}

// fieldSpec is one text field of a form.
type fieldSpec struct {
	// key is the key of the field, for the tests.
	key foundation.Key
	// label is the text above the field when it has something in it, and in it when it is empty.
	label string
	// initial is the text the field starts with.
	initial string
	// lines is how many lines the field shows; 1 for a name.
	lines int
}

// formResult is what the action of a form says when it is done.
type formResult struct {
	// done closes the dialog.
	done bool
	// errors are the sentences to show under the fields, by the index of the field.
	errors map[int]string
	// notice is a sentence to show above the fields, such as that part of the work was done.
	notice string
}

// formAction does what the form asks with the text of its fields, away from the UI thread.
type formAction func(ctx context.Context, values []string) formResult

// textAction makes the action of a form of one field out of a command that takes the text: its
// error is shown under the field and the dialog stays open, and without one it closes.
func textAction(command func(ctx context.Context, text string) error) formAction {
	return func(ctx context.Context, values []string) formResult {
		if err := command(ctx, values[0]); err != nil {
			return formResult{errors: map[int]string{0: app.Message(err)}}
		}
		return formResult{done: true}
	}
}

// showForm opens a dialog of text fields and an action button. The action runs through the runner;
// while it runs the button is off. When it fails the dialog stays open, with what it says under
// the fields; when it is done the dialog closes.
func showForm(bc m.BuildContext, run runner, title, actionLabel string, fields []fieldSpec, action formAction) {
	m.ShowDialog[bool](bc, func(bc m.BuildContext) m.Widget {
		return newFormDialog(run, title, actionLabel, fields, action)
	})
}

// formDialog is the widget of showForm.
type formDialog struct {
	m.StatefulWidget
	run         runner
	title       string
	actionLabel string
	fields      []fieldSpec
	action      formAction
}

func newFormDialog(run runner, title, actionLabel string, fields []fieldSpec, action formAction) *formDialog {
	return &formDialog{
		StatefulWidget: m.NewStatefulWidgetBase(), run: run, title: title,
		actionLabel: actionLabel, fields: fields, action: action,
	}
}

// CreateState creates the state.
func (d *formDialog) CreateState() m.State { return &formState{} }

type formState struct {
	m.StateOf[*formDialog]
	controllers []*m.TextEditingController
	busy        bool
	disposed    bool
	errors      map[int]string
	notice      string
}

// InitState makes a controller for each field, with its first text.
func (s *formState) InitState() {
	for _, f := range s.Widget().fields {
		s.controllers = append(s.controllers, m.NewTextEditingController(f.initial))
	}
}

// Dispose frees the controllers.
func (s *formState) Dispose() {
	s.disposed = true
	for _, c := range s.controllers {
		c.Dispose()
	}
}

// submit runs the action with the texts of the fields.
func (s *formState) submit() {
	if s.busy {
		return
	}
	d := s.Widget()
	values := make([]string, len(s.controllers))
	for i, c := range s.controllers {
		values[i] = c.Text()
	}
	var result formResult
	s.busy = true
	d.run(func(ctx context.Context) error {
		result = d.action(ctx, values)
		return nil
	}, func(error) {
		if s.disposed {
			return
		}
		if result.done {
			m.NavigatorPop(s.Context(), true)
			return
		}
		s.SetState(func() { s.busy, s.errors, s.notice = false, result.errors, result.notice })
	})
	if s.busy && !s.disposed {
		s.SetState(func() {})
	}
}

// Build shows the fields in an alert dialog.
func (s *formState) Build(bc m.BuildContext) m.Widget {
	d := s.Widget()
	theme := m.ThemeOf(bc)
	var content []m.Widget
	if s.notice != "" {
		content = append(content, m.NewText(s.notice, m.TextOpts{
			Key:   foundation.NewKey("form-notice"),
			Style: theme.TextTheme.BodyMedium.CopyWith(m.TextStyleCopy{Color: foundation.Some(theme.ColorScheme.Error())}),
		}), gap(12))
	}
	for i, f := range d.fields {
		if i > 0 {
			content = append(content, gap(16))
		}
		decoration := &m.InputDecoration{LabelText: foundation.Some(f.label)}
		if msg, ok := s.errors[i]; ok {
			decoration.ErrorText = foundation.Some(msg)
			decoration.ErrorMaxLines = foundation.Some(4)
		}
		opts := m.TextFieldOpts{
			Key: f.key, Controller: s.controllers[i], Decoration: decoration, Autofocus: i == 0,
		}
		if f.lines > 1 {
			opts.MaxLines, opts.MinLines = foundation.Some(f.lines), foundation.Some(f.lines)
		} else {
			opts.OnSubmitted = func(string) { s.submit() }
		}
		content = append(content, m.NewTextField(opts))
	}
	var onPressed func()
	if !s.busy {
		onPressed = s.submit
	}
	return m.NewAlertDialog(m.AlertDialogOpts{
		Title: m.NewText(d.title),
		Content: m.NewSizedBox(m.NewSingleChildScrollView(m.NewColumn(content, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch})),
			m.SizedBoxOpts{Width: foundation.Some(420.0)}),
		Actions: []m.Widget{
			m.NewTextButton(func() { m.NavigatorPop(bc) }, m.NewText("Cancel"), m.TextButtonOpts{Key: formCancelKey}),
			m.NewFilledButton(onPressed, m.NewText(d.actionLabel), m.FilledButtonOpts{Key: formActionKey}),
		},
	})
}
