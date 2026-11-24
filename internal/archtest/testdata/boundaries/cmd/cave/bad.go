// Command cave breaks rule 3 twice and imports what it may.
package main

import (
	_ "charm.land/lipgloss/v2"
	_ "github.com/rm4n0s/cave/internal/app"
	_ "github.com/rm4n0s/cave/internal/gui"
	_ "github.com/rm4n0s/cave/internal/tui"
	_ "github.com/rm4n0s/glutter/widgets"
)
