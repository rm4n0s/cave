package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// typeIntoArea sends key names to an area.
func typeIntoArea(a *area, names ...string) {
	for _, name := range names {
		a.key(keyPress(name))
	}
}

func TestAreaPaste(t *testing.T) {
	tests := []struct {
		name  string
		paste string
		want  string
		lines int
	}{
		{"one line", "ssh-ed25519 AAAA laptop", "ssh-ed25519 AAAA laptop", 1},
		{"lines with newlines", "a\nb\nc", "a\nb\nc", 3},
		{"windows line ends", "a\r\nb\r\nc", "a\nb\nc", 3},
		{"old Mac line ends", "a\rb\rc", "a\nb\nc", 3},
		{"mixed line ends", "a\r\nb\rc\nd", "a\nb\nc\nd", 4},
		{"a final newline", "a\nb\n", "a\nb\n", 3},
		{"blank lines", "a\n\nb", "a\n\nb", 3},
		{"control characters", "a\x1b[31mb\tc", "a [31mb c", 1},
		{"invalid UTF-8", "a\xffb", "a\uFFFDb", 1},
		{"nothing", "", "", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newArea()
			a.paste(tt.paste)
			if got := a.text(); got != tt.want || len(a.lines) != tt.lines {
				t.Errorf("text %q with %d lines, want %q with %d", got, len(a.lines), tt.want, tt.lines)
			}
		})
	}
}

func TestAreaPasteInTheMiddle(t *testing.T) {
	a := newArea()
	a.paste("ad")
	a.key(keyPress("left"))
	a.paste("b\nc")
	if a.text() != "ab\ncd" || a.row != 1 || a.col != 1 {
		t.Errorf("text %q, cursor %d,%d; want %q at 1,1", a.text(), a.row, a.col, "ab\ncd")
	}
}

func TestAreaLimits(t *testing.T) {
	// Too many lines: the rest is left out.
	a := newArea()
	a.paste(strings.Repeat("x\n", 500))
	if len(a.lines) != areaMaxLines {
		t.Errorf("%d lines, want %d", len(a.lines), areaMaxLines)
	}
	// Too many bytes: the text stops at the limit, and never inside a character.
	b := newArea()
	b.paste(strings.Repeat("日本語 ", 4000))
	if got := len(b.text()); got > areaMaxBytes || got < areaMaxBytes-4 {
		t.Errorf("%d bytes, want about %d and not more", got, areaMaxBytes)
	}
	if strings.ContainsRune(b.text(), '\uFFFD') {
		t.Error("the text was cut in the middle of a character")
	}
	// Typing after the limit takes nothing.
	size := len(b.text())
	typeIntoArea(&b, "a", "enter", "b")
	if len(b.text()) > areaMaxBytes || len(b.text()) < size {
		t.Errorf("after typing the text has %d bytes (was %d)", len(b.text()), size)
	}
}

func TestAreaKeys(t *testing.T) {
	tests := []struct {
		name         string
		keys         []string
		want         string
		wantRow, col int
	}{
		{"typing and enter", []string{"a", "b", "enter", "c"}, "ab\nc", 1, 1},
		{"enter in the middle splits the line", []string{"a", "b", "left", "enter"}, "a\nb", 1, 0},
		{"backspace joins the lines", []string{"a", "enter", "b", "home", "backspace"}, "ab", 0, 1},
		{"backspace in a line", []string{"a", "b", "backspace"}, "a", 0, 1},
		{"backspace at the very start", []string{"backspace"}, "", 0, 0},
		{"delete joins the lines", []string{"a", "enter", "b", "up", "end", "delete"}, "ab", 0, 1},
		{"delete in a line", []string{"a", "b", "home", "delete"}, "b", 0, 0},
		{"left at the start of a line goes to the end of the one above", []string{"a", "b", "enter", "left"}, "ab\n", 0, 2},
		{"right at the end of a line goes to the next", []string{"a", "enter", "b", "up", "end", "right"}, "a\nb", 1, 0},
		{"up keeps the column when it can", []string{"a", "b", "c", "enter", "d", "up"}, "abc\nd", 0, 1},
		{"down", []string{"a", "enter", "b", "up", "down"}, "a\nb", 1, 1},
		{"up at the top", []string{"a", "up"}, "a", 0, 1},
		{"down at the bottom", []string{"a", "down"}, "a", 0, 1},
		{"home and end", []string{"a", "b", "home", "x", "end", "y"}, "xaby", 0, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newArea()
			typeIntoArea(&a, tt.keys...)
			if a.text() != tt.want || a.row != tt.wantRow || a.col != tt.col {
				t.Errorf("text %q cursor %d,%d; want %q at %d,%d", a.text(), a.row, a.col, tt.want, tt.wantRow, tt.col)
			}
		})
	}
	a := newArea()
	for _, name := range []string{"esc", "tab", "ctrl+c", "ctrl+x", "pgup"} {
		if a.key(keyPress(name)) {
			t.Errorf("the area used %s", name)
		}
	}
}

func TestAreaView(t *testing.T) {
	st := newStyles()
	a := newArea()
	for i := 1; i <= 8; i++ {
		a.paste("line " + itoa(i))
		if i < 8 {
			a.key(keyPress("enter"))
		}
	}
	view := func(width, height int) []string { return stripAll(a.view(width, height, st)) }
	lines := view(20, 4)
	if len(lines) != 4 {
		t.Fatalf("%d lines, want 4", len(lines))
	}
	// The cursor is on the last line: it is in view.
	if !strings.HasPrefix(lines[3], "line 8") {
		t.Errorf("the view does not end with the cursor's line:\n%s", strings.Join(lines, "\n"))
	}
	for _, line := range lines {
		if lipgloss.Width(line) != 20 {
			t.Errorf("a line is %d cells wide: %q", lipgloss.Width(line), line)
		}
	}
	// A short text is padded to the height.
	short := newArea()
	short.paste("only")
	got := stripAll(short.view(10, 3, st))
	if len(got) != 3 || !strings.HasPrefix(got[0], "only") || strings.TrimSpace(got[1]) != "" {
		t.Errorf("a short text: %q", got)
	}
	// A long line is cut for the eye, and the text is whole.
	long := newArea()
	long.paste(strings.Repeat("k", 100))
	long.key(keyPress("home"))
	if got := ansi.Strip(long.view(30, 1, st)[0]); !strings.HasSuffix(strings.TrimRight(got, " "), "…") || lipgloss.Width(got) != 30 {
		t.Errorf("a long line: %q", got)
	}
	if len(long.text()) != 100 {
		t.Errorf("the text has %d bytes, want 100", len(long.text()))
	}
}
