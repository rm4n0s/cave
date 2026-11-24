package tui

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFit(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
		want  string
	}{
		{"shorter", "abc", 5, "abc  "},
		{"equal", "abcde", 5, "abcde"},
		{"longer", "abcdef", 5, "abcd…"},
		{"empty", "", 3, "   "},
		{"wide characters, cut", "日本語", 5, "日本…"},
		{"wide characters that fit", "日本語", 6, "日本語"},
		{"a wide character that does not fit the last cell", "ab日本", 3, "ab…"},
		{"a combining character takes no cell", "ébc", 4, "ébc "},
		{"width 1", "abc", 1, "…"},
		{"width 0", "abc", 0, ""},
		{"a negative width", "abc", -3, ""},
		{"one cell, nothing to cut", "a", 1, "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fit(tt.in, tt.width)
			if got != tt.want {
				t.Errorf("fit(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
			}
			if w := lipgloss.Width(got); tt.width > 0 && w != tt.width {
				t.Errorf("fit(%q, %d) is %d cells wide", tt.in, tt.width, w)
			}
		})
	}
	// Styles are kept, and the result is as wide as asked.
	st := newStyles()
	for _, width := range []int{3, 6, 12} {
		got := fit(st.title.Render("abcdef"), width)
		if lipgloss.Width(got) != width {
			t.Errorf("a styled text cut to %d is %d cells wide", width, lipgloss.Width(got))
		}
	}
	if got := ansi.Strip(fit(st.title.Render("abcdef"), 4)); got != "abc…" {
		t.Errorf("a styled text cut to 4 is %q", got)
	}
}

func TestClean(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "alice laptop", "alice laptop"},
		{"empty", "", ""},
		{"an escape sequence", "a\x1b[31mb", "a [31mb"},
		{"newlines and tabs", "a\nb\r\nc\td", "a b  c d"},
		{"DEL", "a\x7fb", "a b"},
		{"a C1 control", "x\u0085y", "x y"},
		{"invalid UTF-8", "a\xffb\xfe", "a\uFFFDb\uFFFD"},
		{"a cut multi-byte character", "ab\xe6\x97", "ab\uFFFD\uFFFD"},
		{"letters of other scripts", "café 日本語", "café 日本語"},
	}
	for _, tt := range tests {
		if got := clean(tt.in); got != tt.want {
			t.Errorf("%s: clean(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestFitAlign(t *testing.T) {
	tests := []struct {
		in    string
		width int
		right bool
		want  string
	}{
		{"7", 4, true, "   7"},
		{"7", 4, false, "7   "},
		{"1204", 4, true, "1204"},
		{"123456", 4, true, "123…"},
		{"日", 4, true, "  日"},
	}
	for _, tt := range tests {
		if got := fitAlign(tt.in, tt.width, tt.right); got != tt.want {
			t.Errorf("fitAlign(%q, %d, %v) = %q, want %q", tt.in, tt.width, tt.right, got, tt.want)
		}
	}
}
