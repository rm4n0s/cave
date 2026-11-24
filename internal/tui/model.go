package tui

import (
	"context"
	"io"

	tea "charm.land/bubbletea/v2"
	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/settings"
)

// Options are what Run needs besides the application.
type Options struct {
	// In and Out are the terminal's input and output; nil is the standard input and output.
	In  io.Reader
	Out io.Writer
	// Env is the environment the terminal's capabilities are read from; nil is the process's.
	Env []string
	// Clock is the clock of the relative times and the tick; nil is the system's.
	Clock clock.Clock
	// Width and Height are the size of the terminal when the output is not one (a test); 0 asks
	// the terminal.
	Width, Height int
	// Page is the page shown first, one of app.Pages; "" is the Overview.
	Page string
	// ConfigDir, StateDir and Version are shown on the Settings page.
	ConfigDir, StateDir, Version string
}

// changedMsg says that the application's snapshots changed: they are read again.
type changedMsg struct{}

// tickMsg says that time has passed: the relative times ("3 min ago") are drawn again.
type tickMsg struct{}

// doneMsg is the end of a command of the application. A nil err clears the message line; an
// error is shown on it as a sentence.
type doneMsg struct {
	err error
}

// asking is a question that is open and what happens when the answer is yes.
type asking struct {
	q   question
	yes tea.Cmd
}

// Model is the root model of the TUI.
type Model struct {
	ctx   context.Context
	app   App
	clock clock.Clock
	opts  Options
	st    styles

	// width and height are the terminal's size, 0 until it is told.
	width, height int
	page          page
	help          bool
	// ask is the question that is open, nil when there is none. It gets every key.
	ask *asking
	// prompt is the text field that is open, nil when there is none. It gets every key too.
	prompt    *prompt
	area      *areaInput
	promptSeq int

	// The People page: the selection, the person it is on (so that it stays on them when the list
	// changes) and the person to select as soon as they are in the list.
	peopleList   list
	peopleSel    string
	peopleWanted string
	// personID is the person whose keys the People page shows, "" for the list of people; keyList
	// and keySel are the selection among their keys, kept by the key's id.
	personID string
	keyList  list
	keySel   string

	// The Attempts page: the filter (0 all, 1 accepted, 2 refused), the search, the selection
	// kept by the entry's id, and whether the details of the selected entry are shown.
	attemptFilter int
	attemptSearch string
	attemptList   list
	attemptSel    string
	attemptDetail bool
	// message is the text of the message line; messageErr says that it is an error.
	message    string
	messageErr bool

	// The snapshots of the application, read again when it says that they changed.
	server      app.ServerSnapshot
	people      []people.Person
	attempts    app.AttemptsSnapshot
	connections []server.Connection
	settings    settings.Snapshot
}

// NewModel makes the model of the TUI over an application. ctx is the context its commands run
// with: they stop when it is cancelled.
func NewModel(ctx context.Context, a App, clk clock.Clock, opts Options) Model {
	m := Model{ctx: ctx, app: a, clock: clk, opts: opts, st: newStyles(), page: pageNamed(opts.Page)}
	m.refresh()
	return m
}

// refresh reads the snapshots of the application again.
func (m *Model) refresh() {
	m.server = m.app.Server()
	m.people = m.app.People()
	m.attempts = m.app.Attempts()
	m.connections = m.app.Connections()
	m.settings = m.app.Settings()
	m.syncPeople()
	m.syncKeys()
	m.syncAttempts()
}

// Init starts nothing: the program sends the size by itself.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles a message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.peopleList.setHeight(m.height - chrome - 1)
		m.keyList.setHeight(m.height - chrome - 2)
		m.attemptList.setHeight(m.height - chrome - 2)
	case changedMsg, tickMsg:
		m.refresh()
	case doneMsg:
		if msg.err == nil {
			m.message, m.messageErr = "", false
		} else {
			m.message, m.messageErr = app.Message(msg.err), true
		}
	case promptDone:
		m = m.promptEnded(msg)
	case tea.PasteMsg:
		switch {
		case m.area != nil:
			m = m.pasteArea(msg.Content)
		case m.prompt != nil:
			m = m.pastePrompt(msg.Content)
		}
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// promptEnded is what the end of a prompt's command does: close the prompt, or show why it
// failed. The end of a prompt that was closed meanwhile is ignored.
func (m Model) promptEnded(done promptDone) Model {
	if m.area != nil && m.area.id == done.id {
		if done.err != nil {
			in := *m.area
			in.err, in.busy = app.Message(done.err), false
			m.area = &in
		} else {
			m.area = nil
		}
		return m
	}
	if m.prompt == nil || m.prompt.id != done.id {
		return m
	}
	if done.err != nil {
		p := *m.prompt
		p.err, p.busy = app.Message(done.err), false
		m.prompt = &p
		return m
	}
	m.prompt = nil
	if done.selectID != "" {
		m.peopleWanted = done.selectID
		m.syncPeople()
	}
	return m
}

// key handles a key press.
func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	name := msg.String()
	if m.ask != nil {
		return m.keyAsked(msg)
	}
	if m.area != nil {
		return m.keyArea(msg)
	}
	if m.prompt != nil {
		return m.keyPrompt(msg)
	}
	m.message, m.messageErr = "", false
	if name == "ctrl+c" {
		m.help = false
		return m.quit()
	}
	if m.help {
		m.help = false // any key closes the help
		return m, nil
	}
	switch name {
	case "q":
		return m.quit()
	case "?":
		m.help = true
	case "1", "2", "3", "4", "5":
		m.page = page(name[0] - '1')
	case "tab":
		m.page = (m.page + 1) % pageCount
	case "shift+tab":
		m.page = (m.page + pageCount - 1) % pageCount
	case "s":
		return m, m.toggleServer()
	default:
		switch m.page {
		case pagePeople:
			return m.keyPeople(msg)
		case pageAttempts:
			return m.keyAttempts(msg)
		case pageOverview, pageConnections, pageSettings, pageCount:
		}
	}
	return m, nil
}

// toggleServer returns the command that stops the server when it runs or is starting, and
// starts it otherwise.
func (m Model) toggleServer() tea.Cmd {
	ctx, a := m.ctx, m.app
	if state := m.server.State; state == server.Running || state == server.Starting {
		return func() tea.Msg { return doneMsg{err: a.StopServer(ctx)} }
	}
	return func() tea.Msg { return doneMsg{err: a.StartServer(ctx)} }
}

// keyAsked handles a key while a question is open: it is the question's, except that ctrl+c quits
// at once, so that a second ctrl+c always ends the program.
func (m Model) keyAsked(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	decided, yes := m.ask.q.key(msg)
	if !decided {
		return m, nil
	}
	cmd := m.ask.yes
	m.ask = nil
	if !yes {
		return m, nil
	}
	return m, cmd
}

// quit quits at once when nobody is connected, and otherwise asks, because quitting
// disconnects them.
func (m Model) quit() (tea.Model, tea.Cmd) {
	if text := app.QuitQuestion(m.connections); text != "" {
		m.ask = &asking{q: question{text: text, yes: "Quit"}, yes: tea.Quit}
		return m, nil
	}
	return m, tea.Quit
}
