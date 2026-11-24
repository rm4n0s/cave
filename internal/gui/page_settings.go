package gui

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/glutter/foundation"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/services"
)

// The keys of what the tests tap on the Settings page.
var (
	newHostKeyKey         = foundation.NewKey("new-host-key")
	copyHostKeyKey        = foundation.NewKey("copy-host-key")
	copyConfigDirKey      = foundation.NewKey("copy-config-dir")
	copyStateDirKey       = foundation.NewKey("copy-state-dir")
	settingsVersionKey    = foundation.NewKey("settings-version")
	settingsConfigDirText = foundation.NewKey("settings-config-dir")
	settingsStateDirText  = foundation.NewKey("settings-state-dir")
)

// settingKey is the key of the control of a setting, for the tests.
func settingKey(name string) foundation.Key { return foundation.NewKey("setting-" + name) }

// choiceWords are the words the choices of the settings are shown with.
var choiceWords = map[string]string{
	settings.ClearNever:   "Never",
	settings.ClearOnStart: "Each time Cave starts",
	settings.ClearHourly:  "Every hour",
	settings.ClearDaily:   "Every day",
	settings.ClearWeekly:  "Every week",
	settings.ClearMonthly: "Every 30 days",
	settings.ThemeSystem:  "Follow the system",
	settings.ThemeLight:   "Light",
	settings.ThemeDark:    "Dark",
}

// choiceWord is the word of a choice; a choice that has none is shown as it is.
func choiceWord(choice string) string {
	if word, ok := choiceWords[choice]; ok {
		return word
	}
	return choice
}

// splitList turns the names typed with commas ("LANG, LC_*") into a list: the spaces around a name
// are taken off and an empty name is left out.
func splitList(text string) []string {
	names := []string{}
	for _, name := range strings.Split(text, ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// settingText is the value of a setting as the text of its field.
func settingText(snap settings.Snapshot, key string) string {
	v, _ := snap.Value(key)
	switch v := v.(type) {
	case int:
		return strconv.Itoa(v)
	case string:
		return v
	case []string:
		return strings.Join(v, ", ")
	}
	return ""
}

// settingsPage is the fifth page (docs/SPEC.md §11.5): every setting in its group, the host key,
// and where Cave keeps its files.
type settingsPage struct {
	m.StatelessWidget
	v *view
}

func newSettingsPage(v *view) *settingsPage {
	return &settingsPage{StatelessWidget: m.NewStatelessWidgetBase(), v: v}
}

// Build makes a card for each group, in the order of app.SettingTexts, and the cards of the host
// key and of the files.
func (p *settingsPage) Build(bc m.BuildContext) m.Widget {
	v := p.v
	defs := map[string]settings.Def{}
	for _, d := range settings.Schema() {
		defs[d.Key] = d
	}
	var children []m.Widget
	var group string
	var rows []m.Widget
	flush := func() {
		if group == "" {
			return
		}
		children = append(children, groupCard(v, group, rows), gap(16))
		rows = nil
	}
	for _, t := range app.SettingTexts() {
		if t.Group != group {
			flush()
			group = t.Group
		}
		if len(rows) > 0 {
			rows = append(rows, gap(20))
		}
		rows = append(rows, newSettingRow(v, t, defs[t.Key]))
	}
	flush()
	children = append(children, hostKeyCard(bc, v), gap(16), aboutCard(bc, v))
	return column(children)
}

// groupCard is a card with the title of the group and its rows. When the Attempts page sent the
// user here for this group, the card scrolls into view.
func groupCard(v *view, group string, rows []m.Widget) m.Widget {
	return newScrollAnchor(v.settingsGroup == group && group != "", m.NewBuilder(func(bc m.BuildContext) m.Widget {
		children := append([]m.Widget{
			m.NewText(group, m.TextOpts{Style: m.ThemeOf(bc).TextTheme.TitleMedium}), gap(12),
		}, rows...)
		return card(children, foundation.NewKey("group-"+group))
	}))
}

// scrollAnchor scrolls its child into view, once, when it is built with scroll set.
type scrollAnchor struct {
	m.StatefulWidget
	scroll bool
	child  m.Widget
}

func newScrollAnchor(scroll bool, child m.Widget) *scrollAnchor {
	return &scrollAnchor{StatefulWidget: m.NewStatefulWidgetBase(), scroll: scroll, child: child}
}

// CreateState creates the state.
func (a *scrollAnchor) CreateState() m.State { return &anchorState{} }

type anchorState struct {
	m.StateOf[*scrollAnchor]
	done     bool
	disposed bool
}

// InitState scrolls after the first frame, when the card has its place.
func (s *anchorState) InitState() { s.maybeScroll() }

// DidUpdateWidget scrolls when the card is asked for after it was built.
func (s *anchorState) DidUpdateWidget(*scrollAnchor) { s.maybeScroll() }

// Dispose remembers that the card is gone.
func (s *anchorState) Dispose() { s.disposed = true }

func (s *anchorState) maybeScroll() {
	if !s.Widget().scroll || s.done {
		return
	}
	s.done = true
	m.WidgetsBindingOf(s.Context()).RunPostFrame(func(time.Duration) {
		if !s.disposed {
			m.ScrollableEnsureVisible(s.Context())
		}
	})
}

// Build shows the child.
func (s *anchorState) Build(m.BuildContext) m.Widget { return s.Widget().child }

// settingRow is one setting: its label, what it does, the control for its kind and, when
// the last change was refused, the sentence under the control.
type settingRow struct {
	m.StatefulWidget
	v    *view
	text app.SettingText
	def  settings.Def
}

func newSettingRow(v *view, text app.SettingText, def settings.Def) *settingRow {
	return &settingRow{StatefulWidget: m.NewStatefulWidgetBase(m.WidgetOpts{Key: foundation.NewKey("row-" + text.Key)}), v: v, text: text, def: def}
}

// CreateState creates the state.
func (r *settingRow) CreateState() m.State { return &settingState{} }

type settingState struct {
	m.StateOf[*settingRow]
	field *m.TextEditingController
	// shown is the value of the setting, as the text of its field, when the row was last built.
	shown string
	// err is the sentence under the control: why the last change was refused.
	err string
	// sent is the text of a field that was sent and not refused, so that Enter, which also takes
	// the focus out of the field, does not send it twice.
	sent     string
	hasSent  bool
	disposed bool
}

// InitState makes the field, for the kinds that have one, with the value of the setting.
func (s *settingState) InitState() {
	r := s.Widget()
	s.shown = settingText(r.v.settings, r.text.Key)
	s.field = m.NewTextEditingController(s.shown)
}

// DidUpdateWidget puts the new value in the field when the setting changed from elsewhere.
func (s *settingState) DidUpdateWidget(*settingRow) {
	r := s.Widget()
	if now := settingText(r.v.settings, r.text.Key); now != s.shown {
		s.shown = now
		s.field.SetText(now)
	}
}

// Dispose frees the field.
func (s *settingState) Dispose() {
	s.disposed = true
	s.field.Dispose()
}

// set sends a value of the setting. A refusal shows under the control and the control keeps what
// the user made of it.
func (s *settingState) set(value any) {
	r := s.Widget()
	r.v.run(func(ctx context.Context) error { return r.v.app.Set(ctx, r.text.Key, value) }, func(err error) {
		if s.disposed {
			return
		}
		s.SetState(func() {
			s.err = ""
			if err != nil {
				s.err = app.Message(err)
			}
		})
	})
}

// commit sends what the user typed, when it is not what the setting already is. Leaving the field
// (explicit false) does not send a text again that was already sent, also when it was refused,
// because Enter takes the focus out of the field and would send it twice; Enter (explicit true)
// sends it again, so that the user can try once more. A number that is not one is not sent.
func (s *settingState) commit(explicit bool) {
	r := s.Widget()
	text := s.field.Text()
	if text == s.shown || (!explicit && s.hasSent && text == s.sent) {
		return
	}
	switch r.def.Kind {
	case settings.KindInt:
		n, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil {
			s.SetState(func() { s.err = "Type a number." })
			return
		}
		s.sent, s.hasSent = text, true
		s.set(n)
	case settings.KindStrings:
		s.sent, s.hasSent = text, true
		s.set(splitList(text))
	default:
		s.sent, s.hasSent = text, true
		s.set(text)
	}
}

// revert puts the value of the setting back in the field.
func (s *settingState) revert() {
	s.SetState(func() {
		s.err, s.hasSent = "", false
		s.field.SetText(s.shown)
	})
}

// control is the widget for the kind of the setting.
func (s *settingState) control(bc m.BuildContext) m.Widget {
	r := s.Widget()
	key := settingKey(r.text.Key)
	switch r.def.Kind {
	case settings.KindBool:
		v, _ := r.v.settings.Value(r.text.Key)
		on, _ := v.(bool)
		return m.NewSwitch(on, func(value bool) { s.set(value) }, m.SwitchOpts{Key: key})
	case settings.KindChoice:
		entries := make([]m.DropdownMenuEntry[string], len(r.def.Choices))
		for i, c := range r.def.Choices {
			entries[i] = m.NewDropdownMenuEntry(c, choiceWord(c))
		}
		return m.NewDropdownMenu(entries, m.DropdownMenuOpts[string]{
			Key: key, Width: foundation.Some(280.0), SelectOnly: true,
			InitialSelection: foundation.Some(s.shown),
			OnSelected: func(value foundation.Opt[string]) {
				if value.IsSome() && value.Value() != s.shown {
					s.set(value.Value())
				}
			},
		})
	}
	decoration := &m.InputDecoration{IsDense: foundation.Some(true)}
	if s.err != "" {
		decoration.ErrorText = foundation.Some(s.err)
		decoration.ErrorMaxLines = foundation.Some(4)
	}
	opts := m.TextFieldOpts{
		Key: key, Controller: s.field, Decoration: decoration,
		OnSubmitted: func(string) { s.commit(true) },
	}
	if r.def.Kind == settings.KindInt {
		opts.KeyboardType = foundation.Some(services.TextInputTypeNumber)
	}
	return m.NewFocus(m.NewSizedBox(m.NewTextField(opts), m.SizedBoxOpts{Width: foundation.Some(320.0)}), m.FocusOpts{
		OnFocusChange: func(has bool) {
			if !has {
				s.commit(false)
			}
		},
		OnKeyEvent: func(_ *m.FocusNode, event services.KeyEvent) m.KeyEventResult {
			if _, down := event.(*services.KeyDownEvent); down && event.LogicalKey() == services.LogicalKeyboardKeyEscape {
				s.revert()
				return m.KeyEventResultHandled
			}
			return m.KeyEventResultIgnored
		},
	})
}

// Build shows the label and what the setting does, with the control under them.
func (s *settingState) Build(bc m.BuildContext) m.Widget {
	r := s.Widget()
	theme := m.ThemeOf(bc)
	children := []m.Widget{
		m.NewText(r.text.Label, m.TextOpts{Style: theme.TextTheme.BodyLarge}),
		m.NewText(r.text.Help, m.TextOpts{Style: theme.TextTheme.BodySmall}),
		gap(8),
		s.control(bc),
	}
	// A switch and a menu have no field to put the sentence under.
	if s.err != "" && (r.def.Kind == settings.KindBool || r.def.Kind == settings.KindChoice) {
		children = append(children, gap(4), m.NewText(s.err, m.TextOpts{
			Key:   foundation.NewKey("error-" + r.text.Key),
			Style: theme.TextTheme.BodySmall.CopyWith(m.TextStyleCopy{Color: foundation.Some(theme.ColorScheme.Error())}),
		}))
	}
	return m.NewColumn(children, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStart})
}

// hostKeyCard shows the fingerprint and the button that makes a new host key.
func hostKeyCard(bc m.BuildContext, v *view) m.Widget {
	theme := m.ThemeOf(bc)
	return card([]m.Widget{
		m.NewText("Host key", m.TextOpts{Style: theme.TextTheme.TitleMedium}),
		gap(8),
		m.NewRow([]m.Widget{
			m.NewExpanded(m.NewText(v.server.HostKeyFingerprint, m.TextOpts{
				Key: foundation.NewKey("settings-fingerprint"), Style: monoStyle(theme.TextTheme.BodyMedium),
			})),
			copyButton(bc, v.ctx, "copy-host-key", v.server.HostKeyFingerprint),
		}),
		gap(4),
		m.NewText("A person sees this the first time they connect.", m.TextOpts{Style: theme.TextTheme.BodySmall}),
		gap(12),
		m.NewOutlinedButton(func() { askNewHostKey(bc, v) }, m.NewText("New host key…"), m.OutlinedButtonOpts{Key: newHostKeyKey}),
	}, foundation.NewKey("group-host-key"))
}

// askNewHostKey asks, and makes a new host key.
func askNewHostKey(bc m.BuildContext, v *view) {
	answer := confirm(bc, "New host key",
		"Make a new host key? Every person will see a warning that the host key changed, the next time they connect.",
		"Make it", true)
	waitYes(bc, answer, func() {
		runCommand(bc, v, func(ctx context.Context) error { return v.app.NewHostKey(ctx) })
	})
}

// aboutCard shows where Cave keeps its files, and its version.
func aboutCard(bc m.BuildContext, v *view) m.Widget {
	theme := m.ThemeOf(bc)
	path := func(label, dir string, textKey, copyKey foundation.Key) m.Widget {
		return m.NewColumn([]m.Widget{
			m.NewText(label, m.TextOpts{Style: theme.TextTheme.BodySmall}),
			m.NewRow([]m.Widget{
				m.NewExpanded(m.NewText(dir, m.TextOpts{Key: textKey, Style: monoStyle(theme.TextTheme.BodyMedium)})),
				copyButton(bc, v.ctx, copyKey.(foundation.ValueKey[string]).Value(), dir),
			}),
		}, m.ColumnOpts{CrossAxisAlignment: m.CrossAxisAlignmentStretch})
	}
	return card([]m.Widget{
		m.NewText("About", m.TextOpts{Style: theme.TextTheme.TitleMedium}),
		gap(8),
		path("Configuration folder", v.opts.ConfigDir, settingsConfigDirText, copyConfigDirKey),
		gap(8),
		path("State folder", v.opts.StateDir, settingsStateDirText, copyStateDirKey),
		gap(8),
		m.NewText("Version", m.TextOpts{Style: theme.TextTheme.BodySmall}),
		m.NewText(v.opts.Version, m.TextOpts{Key: settingsVersionKey, Style: theme.TextTheme.BodyMedium}),
	}, foundation.NewKey("group-about"))
}
