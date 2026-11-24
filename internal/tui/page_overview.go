package tui

import (
	"strconv"
	"strings"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/server"
)

// overviewLabel is the width of the labels in front of the server's state and the host key.
const overviewLabel = 12

// overviewLines draws the Overview page (docs/SPEC.md §11.1) into at most height lines: the
// server's state, the host key, how to connect and the counts. When the page is too short the
// lines of addresses are the first to go.
func (m Model) overviewLines(height int) []string {
	head := m.serverLines()
	head = append(head, m.hostKeyLines()...)
	head = append(head, "", m.connectHeading())
	tail := []string{"", m.countsLine()}
	if len(m.people) == 0 {
		tail = append(tail, "", "Nobody can connect yet. Press 2 to add a person.")
	}
	addresses := m.addressLines()
	if room := height - len(head) - len(tail); len(addresses) > room {
		if hidden := len(addresses) - 1; room >= 2 && hidden > 0 {
			addresses = append(addresses[:1:1], m.st.dim.Render("  … and "+strconv.Itoa(hidden)+" more"))
		}
	}
	lines := append(head, addresses...)
	return append(lines, tail...)
}

// serverLines is the server's state, with what the key s does, and the reason when it is not
// running although it was asked to.
func (m Model) serverLines() []string {
	label := m.st.title.Render(fit("Server", overviewLabel))
	hint := ""
	switch m.server.State {
	case server.Running, server.Starting:
		hint = "   " + m.st.dim.Render("(s stops it)")
	case server.Stopped:
		hint = "   " + m.st.dim.Render("(s starts it)")
	}
	lines := []string{label + m.stateText() + hint}
	if m.server.Err != nil && m.server.State == server.Stopped {
		lines = append(lines, m.errorLines(m.server.Err, 2)...)
	}
	return lines
}

// errorLines draws the sentence of an error in the error style, wrapped to the width and kept to
// at most max lines: what does not fit ends in "…".
func (m Model) errorLines(err error, max int) []string {
	parts := wrapLines(app.Message(err), m.width-overviewLabel, max)
	lines := make([]string, len(parts))
	for i, p := range parts {
		lines[i] = strings.Repeat(" ", overviewLabel) + m.st.err.Render(p)
	}
	return lines
}

// hostKeyLines is the host key's fingerprint, and who sees it. On a narrow screen the
// fingerprint has a line of its own, so that it is never cut.
func (m Model) hostKeyLines() []string {
	fingerprint := m.server.HostKeyFingerprint
	note := m.st.dim.Render("A person sees this the first time they connect.")
	label := m.st.title.Render(fit("Host key", overviewLabel))
	if overviewLabel+len([]rune(fingerprint)) <= m.width {
		return []string{label + fingerprint, strings.Repeat(" ", overviewLabel) + note}
	}
	return []string{m.st.title.Render("Host key"), "  " + fingerprint, "  " + note}
}

// connectHeading introduces the lines that say how to connect.
func (m Model) connectHeading() string {
	if name := m.firstPerson(); name != "" {
		return "How to connect, for a person named " + name + ":"
	}
	return "How to connect:"
}

// firstPerson is the name of the first person who is enabled, or "" when there is none.
func (m Model) firstPerson() string {
	for _, p := range m.people {
		if p.Enabled {
			return p.Name
		}
	}
	return ""
}

// addressLines are the ssh lines to give a person, one for each address of this computer.
func (m Model) addressLines() []string {
	name := m.firstPerson()
	if name == "" {
		name = "name"
	}
	if len(m.server.Addresses) == 0 {
		return []string{m.st.dim.Render("  This computer's addresses are not known.")}
	}
	lines := make([]string, len(m.server.Addresses))
	for i, address := range m.server.Addresses {
		lines[i] = "  " + m.server.Command(name, address)
	}
	return lines
}

// countsLine is how many people, connections and attempts there are.
func (m Model) countsLine() string {
	return "People " + strconv.Itoa(len(m.people)) +
		"   Connected now " + strconv.Itoa(len(m.connections)) +
		"   Attempts in the list " + strconv.Itoa(len(m.attempts.Entries))
}
