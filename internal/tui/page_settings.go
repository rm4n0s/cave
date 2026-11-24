package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/settings"
)

// maxSettingTextLength is how many characters the field of a text setting takes.
const maxSettingTextLength = 256

// itemKind says what one line of the Settings page is.
type itemKind int

const (
	itemGroup   itemKind = iota // the title of a group of settings
	itemSpacer                  // an empty line between groups
	itemSetting                 // a setting, which can be changed
	itemHostKey                 // the host key, which can be replaced
	itemInfo                    // a folder or the version: shown, not changed
)

// settingsItem is one line of the Settings page. Every item is one line, so the index of an item
// is its line.
type settingsItem struct {
	kind  itemKind
	text  app.SettingText // an itemSetting
	def   settings.Def    // an itemSetting
	label string          // the others
	value string          // an itemInfo
}

// selectable says whether the selection can be on the item.
func (i settingsItem) selectable() bool {
	return i.kind == itemSetting || i.kind == itemHostKey
}

// hostKeyHelp explains the row of the host key.
const hostKeyHelp = "Replaces the host key. Every person's ssh will warn that it changed, the next time they connect."

// settingsItems lists the lines of the page: every setting of app.SettingTexts under the title of
// its group, then the host key, the two folders and the version.
func (m Model) settingsItems() []settingsItem {
	defs := map[string]settings.Def{}
	for _, d := range settings.Schema() {
		defs[d.Key] = d
	}
	var items []settingsItem
	group := ""
	for _, t := range app.SettingTexts() {
		if t.Group != group {
			if group != "" {
				items = append(items, settingsItem{kind: itemSpacer})
			}
			items = append(items, settingsItem{kind: itemGroup, label: t.Group})
			group = t.Group
		}
		items = append(items, settingsItem{kind: itemSetting, text: t, def: defs[t.Key]})
	}
	return append(items,
		settingsItem{kind: itemSpacer},
		settingsItem{kind: itemGroup, label: "Cave"},
		settingsItem{kind: itemHostKey, label: "Host key", value: m.server.HostKeyFingerprint},
		settingsItem{kind: itemInfo, label: "Config folder", value: m.opts.ConfigDir},
		settingsItem{kind: itemInfo, label: "State folder", value: m.opts.StateDir},
		settingsItem{kind: itemInfo, label: "Version", value: m.opts.Version},
	)
}

// selectableRows returns the line of each item that can be selected.
func selectableRows(items []settingsItem) []int {
	var rows []int
	for i, item := range items {
		if item.selectable() {
			rows = append(rows, i)
		}
	}
	return rows
}

// settingsArea is how many lines the rows have: the page less the explanation under them.
func (m Model) settingsArea() int {
	return max(m.height-chrome-settingsHelpLines-1, 1)
}

// settingsHelpLines is how many lines the explanation of the selected row has, and a line is left
// empty above it.
const settingsHelpLines = 2

// syncSettings keeps the selection inside the rows that can be selected and the page scrolled
// so that the selected one is in view. The title of the group stays in view when the first
// row is selected.
func (m *Model) syncSettings() {
	items := m.settingsItems()
	rows := selectableRows(items)
	m.settingsList.setLen(len(rows))
	if len(rows) == 0 {
		m.settingsFirst = 0
		return
	}
	area, line := m.settingsArea(), rows[m.settingsList.selected]
	switch {
	case m.settingsList.selected == 0:
		m.settingsFirst = 0
	case m.settingsList.selected == len(rows)-1:
		// The last row shows what is under it too: the folders and the version.
		m.settingsFirst = len(items)
	case line < m.settingsFirst:
		m.settingsFirst = line
	case line >= m.settingsFirst+area:
		m.settingsFirst = line - area + 1
	}
	m.settingsFirst = max(min(m.settingsFirst, len(items)-area), 0)
}

// selectedSetting returns the item the Settings page is on.
func (m Model) selectedSetting() (settingsItem, bool) {
	items := m.settingsItems()
	rows := selectableRows(items)
	if len(rows) == 0 || m.settingsList.selected >= len(rows) {
		return settingsItem{}, false
	}
	return items[rows[m.settingsList.selected]], true
}

// settingValue is the value of a setting as the page words it: on or off, the number, the
// word of a choice, the text (a dash when empty), a list joined with commas.
func settingValue(item settingsItem, snap settings.Snapshot) string {
	v, _ := snap.Value(item.def.Key)
	switch v := v.(type) {
	case bool:
		if v {
			return "on"
		}
		return "off"
	case int:
		return strconv.Itoa(v)
	case string:
		if v == "" {
			return "—"
		}
		return v
	case []string:
		if len(v) == 0 {
			return "—"
		}
		return strings.Join(v, ", ")
	}
	return "—"
}

// settingsLines draws the Settings page into height lines: the rows, scrolled, and the
// explanation of the selected row under them.
func (m Model) settingsLines(height int) []string {
	items := m.settingsItems()
	rows := selectableRows(items)
	selectedLine := -1
	if len(rows) > 0 && m.settingsList.selected < len(rows) {
		selectedLine = rows[m.settingsList.selected]
	}
	area := max(height-settingsHelpLines-1, 1)
	first := m.settingsFirst
	// A prompt under the rows leaves them fewer lines: the selected row stays in view.
	if selectedLine >= first+area {
		first = selectedLine - area + 1
	}
	first = max(min(first, len(items)-area), 0)
	if selectedLine >= 0 && selectedLine < first {
		first = selectedLine
	}
	labelWidth := min(38, m.width/2)
	lines := make([]string, 0, height)
	for i := first; i < len(items) && len(lines) < area; i++ {
		lines = append(lines, m.settingsLine(items[i], i == selectedLine, labelWidth))
	}
	for len(lines) < area {
		lines = append(lines, "")
	}
	lines = append(lines, "")
	help := ""
	if selectedLine >= 0 {
		help = items[selectedLine].text.Help
		if items[selectedLine].kind == itemHostKey {
			help = hostKeyHelp
		}
	}
	lines = append(lines, wrapLines(help, m.width, settingsHelpLines)...)
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines[:min(len(lines), height)]
}

// settingsLine draws one item.
func (m Model) settingsLine(item settingsItem, selected bool, labelWidth int) string {
	switch item.kind {
	case itemGroup:
		return m.st.title.Render(fit(item.label, m.width))
	case itemSpacer:
		return ""
	}
	label, value := item.label, item.value
	dim := false
	if item.kind == itemSetting {
		label, value = item.text.Label, settingValue(item, m.settings)
		if item.text.DesktopOnly {
			value += " (desktop app)"
			dim = true
		}
	}
	prefix := "  "
	if selected {
		prefix = "> "
	}
	line := fit(prefix+fit(label, labelWidth)+" "+value, m.width)
	switch {
	case selected:
		return m.st.current.Render(line)
	case dim || item.kind == itemInfo:
		return m.st.dim.Render(line)
	}
	return line
}

// keySettings handles a key of the Settings page that is not one of the keys of every page.
func (m Model) keySettings(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	name := msg.String()
	if m.settingsList.key(name) {
		m.syncSettings()
		return m, nil
	}
	if name != "enter" && name != "space" {
		return m, nil
	}
	item, ok := m.selectedSetting()
	if !ok {
		return m, nil
	}
	if item.kind == itemHostKey {
		return m.askNewHostKey()
	}
	if item.text.DesktopOnly {
		return m, nil
	}
	return m.changeSetting(item)
}

// askNewHostKey asks whether to replace the host key.
func (m Model) askNewHostKey() (tea.Model, tea.Cmd) {
	ctx, a := m.ctx, m.app
	m.ask = &asking{
		q: question{
			text: "Make a new host key? Every person will see a warning that the host key changed, the next time they connect.",
			yes:  "Make it",
		},
		yes: func() tea.Msg { return doneMsg{err: a.NewHostKey(ctx)} },
	}
	return m, nil
}

// changeSetting changes a setting by its kind: a switch flips, a choice goes to the next, and a
// number, a text or a list opens a field with what it is now.
func (m Model) changeSetting(item settingsItem) (tea.Model, tea.Cmd) {
	ctx, a := m.ctx, m.app
	key := item.def.Key
	current, _ := m.settings.Value(key)
	set := func(value any) tea.Cmd {
		return func() tea.Msg { return doneMsg{err: a.Set(ctx, key, value)} }
	}
	switch item.def.Kind {
	case settings.KindBool:
		on, _ := current.(bool)
		return m, set(!on)
	case settings.KindChoice:
		choice, _ := current.(string)
		next := item.def.Choices[(slices.Index(item.def.Choices, choice)+1)%len(item.def.Choices)]
		return m, set(next)
	case settings.KindInt:
		n, _ := current.(int)
		m.openPrompt(item.text.Label+":", strconv.Itoa(n), 10, func(id int, text string) tea.Cmd {
			return func() tea.Msg {
				value, err := strconv.Atoi(strings.TrimSpace(text))
				if err != nil {
					return promptDone{id: id, failure: "Type a number."}
				}
				return promptDone{id: id, err: a.Set(ctx, key, value)}
			}
		})
	case settings.KindString:
		text, _ := current.(string)
		m.openPrompt(item.text.Label+":", text, maxSettingTextLength, func(id int, text string) tea.Cmd {
			return func() tea.Msg { return promptDone{id: id, err: a.Set(ctx, key, text)} }
		})
	case settings.KindStrings:
		list, _ := current.([]string)
		m.openPrompt(item.text.Label+" (separated by commas):", strings.Join(list, ", "), maxSettingTextLength, func(id int, text string) tea.Cmd {
			return func() tea.Msg { return promptDone{id: id, err: a.Set(ctx, key, splitList(text))} }
		})
	}
	return m, nil
}

// splitList splits a list typed with commas: the spaces around each name are removed and empty
// names are left out.
func splitList(text string) []string {
	names := []string{}
	for _, name := range strings.Split(text, ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}
