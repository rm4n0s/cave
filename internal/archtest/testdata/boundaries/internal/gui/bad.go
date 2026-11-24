// Package gui breaks rule 3 twice and imports what it may.
package gui

import (
	_ "charm.land/bubbletea/v2"
	_ "github.com/rm4n0s/cave/internal/app"
	_ "github.com/rm4n0s/cave/internal/core/ok"
	_ "github.com/rm4n0s/cave/internal/gui/widgets"
	_ "github.com/rm4n0s/cave/internal/tui"
	_ "github.com/rm4n0s/glutter/material"
)
