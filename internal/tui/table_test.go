package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// sampleTable is a table with a flexible column, a right column of numbers and a fixed one.
func sampleTable() table {
	return table{
		columns: []column{
			{title: "Name", flex: true},
			{title: "Keys", width: 4, right: true},
			{title: "Enabled", width: 7},
			{title: "Last seen", flex: true},
		},
		rows: [][]string{
			{"alice", "2", "yes", "3 min ago"},
			{"bob-with-a-very-long-name-that-does-not-fit-its-column-at-all", "1204", "no", "never"},
			{"日本語の名前", "1", "yes", "2 h ago"},
			{"carol", "0", "yes", "5 d ago"},
		},
		empty: "Nobody yet.",
	}
}

func stripAll(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

func TestGoldenTable(t *testing.T) {
	st := newStyles()
	var l list
	l.setLen(4)
	l.selectIndex(1)
	for _, width := range []int{60, 100} {
		lines := sampleTable().view(width, 6, l, st)
		goldenText(t, "table-"+itoa(width), strings.Join(stripAll(lines), "\n"))
		for i, line := range lines {
			if w := lipgloss.Width(line); w != width && line != "" {
				t.Errorf("width %d: line %d is %d cells wide: %q", width, i, w, line)
			}
		}
	}
	// A short window shows the selected row.
	l.selectIndex(3)
	lines := stripAll(sampleTable().view(100, 3, l, st))
	if len(lines) != 3 || !strings.HasPrefix(lines[2], "> carol") {
		t.Errorf("a window of two rows with the last row selected:\n%s", strings.Join(lines, "\n"))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func TestTableEmpty(t *testing.T) {
	st := newStyles()
	tb := sampleTable()
	tb.rows = nil
	lines := stripAll(tb.view(60, 4, list{}, st))
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "  Name") || strings.TrimSpace(lines[1]) != "Nobody yet." {
		t.Errorf("an empty table:\n%s", strings.Join(lines, "\n"))
	}
	if got := tb.view(60, 0, list{}, st); got != nil {
		t.Errorf("a table of height 0 drew %q", got)
	}
	// With one line there is only room for the titles.
	if got := stripAll(tb.view(60, 1, list{}, st)); len(got) != 1 || !strings.HasPrefix(got[0], "  Name") {
		t.Errorf("a table of height 1: %q", got)
	}
}

func TestTableCutsAndAligns(t *testing.T) {
	st := newStyles()
	var l list
	l.setLen(4)
	lines := stripAll(sampleTable().view(60, 6, l, st))
	long := lines[2] // the second row
	if !strings.Contains(long, "…") || strings.Contains(long, "at-all") {
		t.Errorf("a long cell is not cut with …: %q", long)
	}
	// The numbers end at the same cell, whatever their length: the next column starts after them.
	two, big := lines[1], lines[2]
	i, j := strings.Index(two, " yes"), strings.Index(big, " no")
	if i < 0 || j < 0 || utf8.RuneCountInString(two[:i]) != utf8.RuneCountInString(big[:j]) {
		t.Errorf("numbers are not right-aligned:\n%s\n%s", two, big)
	}
	// The selected row has the marker and the others have none.
	if !strings.HasPrefix(lines[1], "> alice") || !strings.HasPrefix(lines[2], "  bob") {
		t.Errorf("the markers:\n%s", strings.Join(lines, "\n"))
	}
}

func TestTableWidths(t *testing.T) {
	tb := table{columns: []column{{title: "a", flex: true}, {title: "b", width: 5}, {title: "c", flex: true}}}
	tests := []struct {
		total int
		want  []int
	}{
		{40, []int{16, 5, 17}}, // 40 - 5 - 2 gaps = 33: 16 each, the last has the rest
		{15, []int{4, 5, 4}},   // too narrow: the least for each flexible column
		{12, []int{4, 5, 4}},
	}
	for _, tt := range tests {
		got := tb.widths(tt.total)
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("widths(%d) = %v, want %v", tt.total, got, tt.want)
				break
			}
		}
	}
	fixed := table{columns: []column{{title: "a", width: 3}, {title: "b", width: 6}}}
	if got := fixed.widths(80); got[0] != 3 || got[1] != 6 {
		t.Errorf("fixed widths = %v", got)
	}
}
