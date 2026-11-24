package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// typeInto sends a key sequence to a field: names of keys, or the text of a character.
func typeInto(f *field, names ...string) {
	for _, name := range names {
		f.key(keyPress(name))
	}
}

func TestFieldEditing(t *testing.T) {
	tests := []struct {
		name       string
		start      string
		keys       []string
		want       string
		wantCursor int
	}{
		{"typing", "", []string{"a", "l", "i", "c", "e"}, "alice", 5},
		{"a space", "", []string{"a", "space", "b"}, "a b", 3},
		{"left and typing in the middle", "ac", []string{"left", "b"}, "abc", 2},
		{"right at the end stays", "ab", []string{"right", "right"}, "ab", 2},
		{"left at the start stays", "ab", []string{"home", "left"}, "ab", 0},
		{"home and end", "abc", []string{"home", "x", "end", "y"}, "xabcy", 5},
		{"ctrl+a and ctrl+e", "abc", []string{"ctrl+a", "x", "ctrl+e", "y"}, "xabcy", 5},
		{"backspace", "abc", []string{"backspace"}, "ab", 2},
		{"backspace in the middle", "abc", []string{"left", "backspace"}, "ac", 1},
		{"backspace at the start", "abc", []string{"home", "backspace"}, "abc", 0},
		{"delete", "abc", []string{"home", "delete"}, "bc", 0},
		{"delete at the end", "abc", []string{"delete"}, "abc", 3},
		{"ctrl+u empties it", "abc def", []string{"ctrl+u"}, "", 0},
		{"ctrl+w deletes a word", "hello big world", []string{"ctrl+w"}, "hello big ", 10},
		{"ctrl+w skips the spaces before the word", "hello big   ", []string{"ctrl+w"}, "hello ", 6},
		{"ctrl+w in the middle", "one two three", []string{"left", "left", "left", "left", "left", "left", "ctrl+w"}, "one  three", 4},
		{"ctrl+w with one word", "word", []string{"ctrl+w"}, "", 0},
		{"wide characters", "", []string{"日", "本"}, "日本", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newField(tt.start, 0)
			typeInto(&f, tt.keys...)
			if f.value() != tt.want || f.cursor != tt.wantCursor {
				t.Errorf("value %q cursor %d, want %q and %d", f.value(), f.cursor, tt.want, tt.wantCursor)
			}
		})
	}
}

func TestFieldUsesOnlyItsKeys(t *testing.T) {
	f := newField("ab", 0)
	for _, name := range []string{"enter", "esc", "tab", "up", "down", "ctrl+c", "ctrl+x", "pgup"} {
		if f.key(keyPress(name)) {
			t.Errorf("the field used %s", name)
		}
	}
	if f.value() != "ab" {
		t.Errorf("a key that is not the field's changed it: %q", f.value())
	}
	// A key with alt is not text.
	if f.key(tea.KeyPressMsg{Code: 'x', Text: "x", Mod: tea.ModAlt}) {
		t.Error("alt+x was taken as text")
	}
}

func TestFieldLimit(t *testing.T) {
	f := newField("", 5)
	typeInto(&f, "a", "b", "c", "d", "e", "f", "g")
	if f.value() != "abcde" {
		t.Errorf("value %q, want the first five", f.value())
	}
	f.paste("xyz")
	if f.value() != "abcde" {
		t.Errorf("a paste past the limit gave %q", f.value())
	}
	typeInto(&f, "backspace", "backspace")
	f.paste("123456")
	if f.value() != "abc123" && f.value() != "abc12" {
		t.Errorf("a paste that fits in part gave %q", f.value())
	}
	if len([]rune(f.value())) != 5 {
		t.Errorf("%q has %d characters, want 5", f.value(), len([]rune(f.value())))
	}
	// The limit counts characters, not bytes.
	w := newField("", 2)
	w.paste("日本語")
	if w.value() != "日本" {
		t.Errorf("wide characters: %q", w.value())
	}
}

func TestFieldPasteIsCleaned(t *testing.T) {
	f := newField("", 0)
	f.paste("a\nb\x1b[31m\tc\xff")
	if want := "a b [31m c\uFFFD"; f.value() != want {
		t.Errorf("value %q, want %q", f.value(), want)
	}
	// Pasted in the middle.
	g := newField("ad", 0)
	g.key(keyPress("left"))
	g.paste("bc")
	if g.value() != "abcd" || g.cursor != 3 {
		t.Errorf("value %q cursor %d", g.value(), g.cursor)
	}
}

func TestFieldView(t *testing.T) {
	st := newStyles()
	view := func(f field, width int) string { return ansi.Strip(f.view(width, st)) }
	tests := []struct {
		name  string
		f     field
		width int
		want  string
	}{
		{"short text, the cursor after it", newField("ab", 0), 6, "ab    "},
		{"text longer than the width, the cursor at the end", newField("abcdefghij", 0), 5, "ghij "},
		{"the cursor at the start of a long text", func() field { f := newField("abcdefghij", 0); f.cursor = 0; return f }(), 5, "abcd…"},
		{"the cursor in the middle", func() field { f := newField("abcdefghij", 0); f.cursor = 5; return f }(), 5, "bcde…"},
		{"a placeholder", field{placeholder: "name"}, 10, " name     "},
		{"no placeholder once there is text", func() field { f := newField("x", 0); f.placeholder = "name"; return f }(), 10, "x         "},
		{"wide characters", newField("日本語", 0), 5, "本語 "},
		{"width 1", newField("abc", 0), 1, " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := view(tt.f, tt.width)
			if lipgloss.Width(got) != tt.width {
				t.Errorf("the view is %d cells wide, want %d: %q", lipgloss.Width(got), tt.width, got)
			}
			if got != tt.want {
				t.Errorf("view = %q, want %q", got, tt.want)
			}
		})
	}
	// The cursor is drawn reversed: the styled text differs from the plain one.
	f := newField("ab", 0)
	if raw := f.view(6, st); raw == ansi.Strip(raw) {
		t.Error("the cursor has no style")
	}
	if got := f.view(0, st); got != "" {
		t.Errorf("a field of width 0 drew %q", got)
	}
}
