package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/rm4n0s/cave/internal/app/apptest"
)

func TestQuestionKeys(t *testing.T) {
	tests := []struct {
		key         string
		wantDecided bool
		wantYes     bool
	}{
		{"y", true, true},
		{"Y", true, true},
		{"n", true, false},
		{"N", true, false},
		{"esc", true, false},
		{"enter", true, false}, // the default is no
		{"x", false, false},
		{"a", false, false},
		{"space", false, false},
		{"tab", false, false},
		{"up", false, false},
		{"1", false, false},
	}
	q := question{text: "Remove alice?", yes: "Remove"}
	for _, tt := range tests {
		decided, yes := q.key(keyPress(tt.key))
		if decided != tt.wantDecided || yes != tt.wantYes {
			t.Errorf("%s: decided %v yes %v, want %v and %v", tt.key, decided, yes, tt.wantDecided, tt.wantYes)
		}
	}
}

func TestGoldenQuestion(t *testing.T) {
	st := newStyles()
	tests := []struct {
		name          string
		q             question
		width, height int
	}{
		{"short-100x12", question{text: "Remove alice and their 2 keys? This closes 1 connection.", yes: "Remove"}, 100, 12},
		{"long-60x10", question{text: "3 people are connected. Quit and disconnect them? Everything they have open ends, including transfers that are under way.", yes: "Quit"}, 60, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := tt.q.view(tt.width, tt.height, st)
			if len(lines) != tt.height {
				t.Fatalf("%d lines, want %d", len(lines), tt.height)
			}
			for i, line := range lines {
				if lipgloss.Width(line) > tt.width {
					t.Errorf("line %d is %d cells wide", i, lipgloss.Width(line))
				}
			}
			goldenText(t, "question-"+tt.name, strings.Join(stripAll(lines), "\n"))
		})
	}
}

func TestQuestionOnATinyScreen(t *testing.T) {
	st := newStyles()
	q := question{text: strings.Repeat("word ", 30), yes: "Quit"}
	lines := q.view(20, 5, st)
	if len(lines) != 5 {
		t.Errorf("%d lines, want 5", len(lines))
	}
	for _, line := range lines {
		if lipgloss.Width(line) > 20 {
			t.Errorf("a line is %d cells wide", lipgloss.Width(line))
		}
	}
}

func TestQuitQuestion(t *testing.T) {
	t.Run("with a connection q asks", func(t *testing.T) {
		h := newFakeHarness(t, apptest.Sample(t), 100, 30)
		h.key("q")
		if h.quit || h.m().ask == nil {
			t.Fatalf("quit %v, ask %v", h.quit, h.m().ask)
		}
		if !strings.Contains(h.screen(), "1 person is connected. Quit and disconnect them?") {
			t.Errorf("the question is not on the screen:\n%s", h.screen())
		}
		h.golden("quit-question-100x30")
	})
	t.Run("n closes it", func(t *testing.T) {
		h := newFakeHarness(t, apptest.Sample(t), 100, 30)
		h.keys("q", "n")
		if h.quit || h.m().ask != nil {
			t.Errorf("quit %v, ask %v", h.quit, h.m().ask)
		}
		if strings.Contains(h.screen(), "Quit and disconnect") {
			t.Error("the question is still drawn")
		}
	})
	t.Run("enter is no", func(t *testing.T) {
		h := newFakeHarness(t, apptest.Sample(t), 100, 30)
		h.keys("q", "enter")
		if h.quit || h.m().ask != nil {
			t.Errorf("quit %v, ask %v", h.quit, h.m().ask)
		}
	})
	t.Run("esc is no", func(t *testing.T) {
		h := newFakeHarness(t, apptest.Sample(t), 100, 30)
		h.keys("q", "esc")
		if h.quit || h.m().ask != nil {
			t.Errorf("quit %v, ask %v", h.quit, h.m().ask)
		}
	})
	t.Run("y quits", func(t *testing.T) {
		h := newFakeHarness(t, apptest.Sample(t), 100, 30)
		h.keys("q", "y")
		if !h.quit {
			t.Error("y did not quit")
		}
	})
	t.Run("other keys leave it open and do nothing else", func(t *testing.T) {
		f := apptest.Sample(t)
		h := newFakeHarness(t, f, 100, 30)
		h.keys("q", "3", "s", "tab", "?", "x")
		if h.quit || h.m().ask == nil || h.m().page != pageOverview || len(f.Calls()) != 0 || h.m().help {
			t.Errorf("quit %v, ask %v, page %d, calls %q, help %v", h.quit, h.m().ask, h.m().page, f.Calls(), h.m().help)
		}
	})
	t.Run("ctrl+c asks, and a second ctrl+c quits", func(t *testing.T) {
		h := newFakeHarness(t, apptest.Sample(t), 100, 30)
		h.key("ctrl+c")
		if h.quit || h.m().ask == nil {
			t.Fatalf("the first ctrl+c: quit %v, ask %v", h.quit, h.m().ask)
		}
		h.key("ctrl+c")
		if !h.quit {
			t.Error("the second ctrl+c did not quit")
		}
	})
	t.Run("from the help", func(t *testing.T) {
		h := newFakeHarness(t, apptest.Sample(t), 100, 30)
		h.keys("?", "ctrl+c")
		if h.quit || h.m().ask == nil || h.m().help {
			t.Errorf("quit %v, ask %v, help %v", h.quit, h.m().ask, h.m().help)
		}
	})
	t.Run("nobody connected: q quits at once", func(t *testing.T) {
		h := newFakeHarness(t, apptest.Empty(t), 100, 30)
		h.key("q")
		if !h.quit || h.m().ask != nil {
			t.Errorf("quit %v, ask %v", h.quit, h.m().ask)
		}
	})
	t.Run("the question is about people, not connections", func(t *testing.T) {
		f := apptest.Sample(t)
		f.ConnectionList = append(f.ConnectionList, f.ConnectionList[0])
		h := newFakeHarness(t, f, 100, 30)
		h.key("q")
		if !strings.Contains(h.screen(), "1 person is connected") {
			t.Errorf("two connections of one person:\n%s", h.screen())
		}
	})
}
