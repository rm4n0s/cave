// Package tui breaks rule 3 twice and imports what it may.
package tui

import (
	_ "charm.land/bubbletea/v2"
	_ "charm.land/lipgloss/v2"
	_ "github.com/rm4n0s/cave/internal/app"
	_ "github.com/rm4n0s/cave/internal/gui"
	_ "github.com/rm4n0s/glutter/widgets"
)
