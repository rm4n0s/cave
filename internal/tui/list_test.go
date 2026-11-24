package tui

import "testing"

func TestList(t *testing.T) {
	tests := []struct {
		name         string
		n, height    int
		start        int // the row selected first
		keys         []string
		wantSelected int
		wantFirst    int
	}{
		{"down", 5, 3, 0, []string{"down"}, 1, 0},
		{"j is down", 5, 3, 0, []string{"j", "j"}, 2, 0},
		{"down scrolls", 5, 3, 0, []string{"down", "down", "down"}, 3, 1},
		{"down stops at the end", 5, 3, 0, []string{"end", "down", "down"}, 4, 2},
		{"up", 5, 3, 3, []string{"up"}, 2, 1},
		{"k is up", 5, 3, 2, []string{"k"}, 1, 0},
		{"up stops at the top", 5, 3, 0, []string{"up", "up"}, 0, 0},
		{"up scrolls back", 6, 3, 5, []string{"up", "up", "up", "up"}, 1, 1},
		{"pgdown moves a page less one", 20, 5, 0, []string{"pgdown"}, 4, 0},
		{"pgdown twice", 20, 5, 0, []string{"pgdown", "pgdown"}, 8, 4},
		{"pgdown stops at the end", 6, 5, 0, []string{"pgdown", "pgdown", "pgdown"}, 5, 1},
		{"pgup", 20, 5, 10, []string{"pgup"}, 6, 6},
		{"pgup stops at the top", 20, 5, 2, []string{"pgup", "pgup"}, 0, 0},
		{"end", 20, 5, 0, []string{"end"}, 19, 15},
		{"home", 20, 5, 12, []string{"home"}, 0, 0},
		{"a short list does not scroll", 2, 5, 0, []string{"end"}, 1, 0},
		{"an empty list stays at 0", 0, 5, 0, []string{"down", "end", "pgdown", "up", "home"}, 0, 0},
		{"a height of 1", 4, 1, 0, []string{"down", "down"}, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var l list
			l.setHeight(tt.height)
			l.setLen(tt.n)
			l.selectIndex(tt.start)
			for _, k := range tt.keys {
				if !l.key(k) {
					t.Fatalf("%s was not used", k)
				}
			}
			if l.selected != tt.wantSelected || l.first != tt.wantFirst {
				t.Errorf("selected %d first %d, want %d and %d", l.selected, l.first, tt.wantSelected, tt.wantFirst)
			}
			// The window always holds the selected row and no more than height rows.
			first, last := l.window(tt.height)
			if tt.n > 0 && (l.selected < first || l.selected >= last) {
				t.Errorf("the window [%d,%d) does not hold the selected row %d", first, last, l.selected)
			}
			if last-first > tt.height || last > tt.n {
				t.Errorf("the window [%d,%d) is too big for %d rows and height %d", first, last, tt.n, tt.height)
			}
		})
	}
}

func TestListIgnoresOtherKeys(t *testing.T) {
	var l list
	l.setLen(3)
	for _, k := range []string{"a", "enter", "space", "ctrl+c", "tab", ""} {
		if l.key(k) {
			t.Errorf("%q was used", k)
		}
	}
	if l.selected != 0 {
		t.Error("the selection moved")
	}
}

func TestListSetLen(t *testing.T) {
	var l list
	l.setHeight(3)
	l.setLen(10)
	l.selectIndex(8)
	l.setLen(3) // the rows got fewer: the selection stays on the last one that is left
	if l.selected != 2 || l.first != 0 {
		t.Errorf("selected %d first %d, want 2 and 0", l.selected, l.first)
	}
	l.setLen(0)
	if l.selected != 0 {
		t.Errorf("selected %d in an empty list", l.selected)
	}
	if first, last := l.window(3); first != 0 || last != 0 {
		t.Errorf("window of an empty list = [%d,%d)", first, last)
	}
	l.setLen(-4)
	if l.n != 0 {
		t.Errorf("a negative length gave %d", l.n)
	}
}

func TestListWindowHeights(t *testing.T) {
	var l list
	l.setLen(10)
	l.selectIndex(7)
	// A page may draw fewer rows than it told the list: the selection is still in sight.
	for _, height := range []int{1, 2, 3, 5, 10, 20} {
		first, last := l.window(height)
		if l.selected < first || l.selected >= last {
			t.Errorf("height %d: [%d,%d) does not hold %d", height, first, last, l.selected)
		}
	}
	if first, last := l.window(0); first != last {
		t.Errorf("window(0) = [%d,%d)", first, last)
	}
	// The window does not change the list.
	before := l
	l.window(2)
	if l != before {
		t.Error("window changed the list")
	}
}
