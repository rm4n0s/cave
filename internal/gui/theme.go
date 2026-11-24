package gui

import (
	"github.com/rm4n0s/cave/internal/core/settings"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/ui"
)

// seedColor is the one color both themes are made from: a calm blue-grey.
var seedColor = ui.NewColor(0xFF4A6572)

// lightTheme is the Material 3 theme of the light look.
func lightTheme() *m.ThemeData {
	return m.NewThemeData(m.ThemeDataOpts{ColorSchemeSeed: m.Some(seedColor)})
}

// darkTheme is the Material 3 theme of the dark look.
func darkTheme() *m.ThemeData {
	return m.NewThemeData(m.ThemeDataOpts{ColorSchemeSeed: m.Some(seedColor), Brightness: m.BrightnessDark})
}

// themeMode turns the setting ui.theme (system, light or dark) into the mode of the app.
func themeMode(snap settings.Snapshot) m.ThemeMode {
	switch snap.String(settings.UITheme) {
	case settings.ThemeLight:
		return m.ThemeModeLight
	case settings.ThemeDark:
		return m.ThemeModeDark
	default:
		return m.ThemeModeSystem
	}
}
