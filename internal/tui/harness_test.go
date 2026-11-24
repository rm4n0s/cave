package tui

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rm4n0s/cave/internal/app/apptest"
)

// updateGoldens rewrites the golden files instead of comparing with them:
// go test ./internal/tui -run Golden -update-goldens. Read every file that changes.
var updateGoldens = flag.Bool("update-goldens", false, "rewrite the golden files")

var ctx = context.Background()

// harness drives the model the way the program does: it sends a message, then every message
// that the returned commands produce.
type harness struct {
	t     *testing.T
	model tea.Model
	// quit is set when a command produced tea.QuitMsg.
	quit bool
}

// newHarness makes the model over the application and gives it the size of the terminal.
func newHarness(t *testing.T, a App, clockOf *apptest.Fake, width, height int) *harness {
	t.Helper()
	h := &harness{t: t, model: NewModel(ctx, a, clockOf.Clock, Options{})}
	h.resize(width, height)
	return h
}

// newFakeHarness is newHarness for a fake application.
func newFakeHarness(t *testing.T, f *apptest.Fake, width, height int) *harness {
	t.Helper()
	return newHarness(t, f, f, width, height)
}

// m returns the model as its own type.
func (h *harness) m() Model {
	h.t.Helper()
	m, ok := h.model.(Model)
	if !ok {
		h.t.Fatalf("the model is a %T", h.model)
	}
	return m
}

// send gives the model a message and everything the commands it returns produce.
func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	queue := []tea.Msg{msg}
	for len(queue) > 0 {
		msg, queue = queue[0], queue[1:]
		switch msg := msg.(type) {
		case nil:
			continue
		case tea.QuitMsg:
			h.quit = true
			continue
		case tea.BatchMsg:
			for _, cmd := range msg {
				if cmd != nil {
					queue = append(queue, cmd())
				}
			}
			continue
		}
		var cmd tea.Cmd
		h.model, cmd = h.model.Update(msg)
		if cmd != nil {
			queue = append(queue, cmd())
		}
	}
}

// keyPress makes the message of a key by its name, as Bubble Tea names them.
func keyPress(name string) tea.KeyPressMsg {
	special := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "space": tea.KeySpace, "backspace": tea.KeyBackspace,
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd, "delete": tea.KeyDelete,
	}
	switch {
	case name == "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case name == "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case strings.HasPrefix(name, "ctrl+") && len(name) == 6:
		return tea.KeyPressMsg{Code: rune(name[5]), Mod: tea.ModCtrl}
	case special[name] != 0:
		msg := tea.KeyPressMsg{Code: special[name]}
		if name == "space" {
			msg.Text = " "
		}
		return msg
	}
	r := []rune(name)
	if len(r) != 1 {
		panic("keyPress: not a key: " + name)
	}
	msg := tea.KeyPressMsg{Code: r[0], Text: name}
	if r[0] >= 'A' && r[0] <= 'Z' {
		msg.Mod = tea.ModShift
	}
	return msg
}

// key sends one key press.
func (h *harness) key(name string) {
	h.t.Helper()
	h.send(keyPress(name))
}

// keys sends several key presses.
func (h *harness) keys(names ...string) {
	h.t.Helper()
	for _, name := range names {
		h.key(name)
	}
}

// resize sends the size of the terminal.
func (h *harness) resize(width, height int) {
	h.t.Helper()
	h.send(tea.WindowSizeMsg{Width: width, Height: height})
}

// raw is the view's text as the terminal would get it, with its styles.
func (h *harness) raw() string {
	h.t.Helper()
	return h.model.View().Content
}

// screen is what the user reads: the view without its styles.
func (h *harness) screen() string {
	h.t.Helper()
	return ansi.Strip(h.raw())
}

// golden compares the screen with testdata/goldens/<name>.txt, or writes the file when the
// tests run with -update-goldens.
func (h *harness) golden(name string) {
	h.t.Helper()
	goldenText(h.t, name, h.screen())
}

// goldenText is golden for any text.
func goldenText(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "goldens", name+".txt")
	got += "\n"
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (write it with -update-goldens)", err)
	}
	if string(want) != got {
		t.Errorf("the screen differs from %s:\n--- want\n%s--- got\n%s", path, want, got)
	}
}
