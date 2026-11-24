package tui

import "charm.land/lipgloss/v2"

// styles are the Lip Gloss styles of the TUI. Bold and reverse mark what is current or selected;
// color only adds to them, so the screen reads the same without color (NO_COLOR, or a terminal
// that has none). Bubble Tea removes what the terminal cannot show.
type styles struct {
	title   lipgloss.Style
	current lipgloss.Style
	key     lipgloss.Style
	dim     lipgloss.Style
	err     lipgloss.Style
	good    lipgloss.Style
	bad     lipgloss.Style
}

func newStyles() styles {
	return styles{
		title:   lipgloss.NewStyle().Bold(true),
		current: lipgloss.NewStyle().Bold(true).Reverse(true),
		key:     lipgloss.NewStyle().Bold(true),
		dim:     lipgloss.NewStyle().Faint(true),
		err:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")),
		good:    lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		bad:     lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
	}
}
