package gui

import (
	"context"

	"github.com/rm4n0s/errors"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/ui"
)

// RunStartError opens a small window that says why Cave could not start, with a Quit button. A
// desktop app has no terminal, so this is how the user learns that Cave is already running or
// that a file is broken. It returns when the window is closed.
func RunStartError(ctx context.Context, message string) error {
	err := m.RunAppContext(ctx, newStartErrorApp(message), m.RunAppOpts{
		AppID:  appID,
		Window: m.WindowConfig{Title: "Cave could not start", Size: ui.Size{Width: 480, Height: 240}},
	})
	if err != nil {
		return errors.NewErr("GuiStartErrorFailed", err)
	}
	return nil
}

// startErrorApp is the content of that window.
type startErrorApp struct {
	m.StatelessWidget
	message string
}

func newStartErrorApp(message string) *startErrorApp {
	return &startErrorApp{StatelessWidget: m.NewStatelessWidgetBase(), message: message}
}

// Build shows the message, as it is, above the Quit button.
func (a *startErrorApp) Build(m.BuildContext) m.Widget {
	return m.NewMaterialApp(m.MaterialAppOpts{
		Title:                      "Cave could not start",
		Theme:                      lightTheme(),
		DarkTheme:                  darkTheme(),
		DebugShowCheckedModeBanner: m.Some(false),
		Home:                       newStartErrorPage(a.message),
	})
}

// startErrorPage is the scaffold of the window.
type startErrorPage struct {
	m.StatelessWidget
	message string
}

func newStartErrorPage(message string) *startErrorPage {
	return &startErrorPage{StatelessWidget: m.NewStatelessWidgetBase(), message: message}
}

// Build lays out the message and the button.
func (p *startErrorPage) Build(bc m.BuildContext) m.Widget {
	return m.NewScaffold(m.ScaffoldOpts{
		Body: m.NewPadding(m.NewEdgeInsetsAll(24), m.NewColumn([]m.Widget{
			m.NewExpanded(m.NewSingleChildScrollView(
				m.NewText(p.message, m.TextOpts{Style: m.ThemeOf(bc).TextTheme.BodyLarge}))),
			m.NewSizedBox(nil, m.SizedBoxOpts{Height: m.Some(16.0)}),
			m.NewRow([]m.Widget{
				m.NewFilledButton(func() { m.WidgetsBindingOf(bc).PlatformDispatcher().RequestAppExit() }, m.NewText("Quit")),
			}, m.RowOpts{MainAxisAlignment: m.MainAxisAlignmentEnd}),
		}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch})),
	})
}
