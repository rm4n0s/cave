package tui

import (
	"strings"
	"testing"

	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/server"
)

func TestGoldenOverview(t *testing.T) {
	stopped := apptest.Sample(t)
	stopped.ServerSnapshot.State = server.Stopped
	failed := apptest.Sample(t)
	failed.ServerSnapshot.State = server.Stopped
	failed.ServerSnapshot.Err = listenFailed()
	starting := apptest.Sample(t)
	starting.ServerSnapshot.State = server.Starting
	noPeople := apptest.Sample(t)
	noPeople.PeopleList = nil
	onlyBob := apptest.Sample(t)
	onlyBob.PeopleList = onlyBob.PeopleList[1:2] // bob is disabled: nobody to name
	tests := []struct {
		name string
		fake *apptest.Fake
	}{
		{"sample", apptest.Sample(t)},
		{"empty", apptest.Empty(t)},
		{"stopped", stopped},
		{"error", failed},
		{"starting", starting},
		{"nobody", noPeople},
		{"nobody-enabled", onlyBob},
	}
	for _, size := range [][2]int{{100, 30}, {60, 16}} {
		for _, tt := range tests {
			name := "overview-" + tt.name + "-" + itoa(size[0]) + "x" + itoa(size[1])
			t.Run(name, func(t *testing.T) {
				newFakeHarness(t, tt.fake, size[0], size[1]).golden(name)
			})
		}
	}
}

func TestOverviewCommandLines(t *testing.T) {
	f := apptest.Sample(t)
	f.ServerSnapshot.Port = 2200
	h := newFakeHarness(t, f, 100, 30)
	screen := h.screen()
	for _, address := range f.ServerSnapshot.Addresses {
		want := f.ServerSnapshot.Command("alice", address)
		if !strings.Contains(screen, "  "+want+"\n") && !strings.Contains(screen, "  "+want+" ") {
			t.Errorf("the screen has no line %q:\n%s", want, screen)
		}
	}
	if !strings.Contains(screen, "ssh -p 2200 alice@192.0.2.10") {
		t.Errorf("the port is not in the lines:\n%s", screen)
	}
	// Port 22 is not written.
	f.ServerSnapshot.Port = 22
	h.send(changedMsg{})
	screen = h.screen()
	if strings.Contains(screen, "-p 22") || !strings.Contains(screen, "ssh alice@192.0.2.10") {
		t.Errorf("port 22 is written:\n%s", screen)
	}
}

func TestOverviewNamesTheFirstEnabledPerson(t *testing.T) {
	f := apptest.Sample(t)
	f.PeopleList[0].Enabled = false // alice is disabled: carol is the first enabled one
	f.PeopleList[1].Enabled = false
	h := newFakeHarness(t, f, 100, 30)
	if screen := h.screen(); !strings.Contains(screen, "for a person named carol:") || !strings.Contains(screen, "carol@192.0.2.10") {
		t.Errorf("the page does not use carol:\n%s", screen)
	}
	f.PeopleList = nil
	h.send(changedMsg{})
	if screen := h.screen(); !strings.Contains(screen, "name@192.0.2.10") || !strings.Contains(screen, "Nobody can connect yet. Press 2 to add a person.") {
		t.Errorf("with nobody the page does not say so:\n%s", screen)
	}
}

func TestOverviewShowsTheErrorOverTwoLinesAtMost(t *testing.T) {
	f := apptest.Sample(t)
	f.ServerSnapshot.State = server.Stopped
	f.ServerSnapshot.Err = listenFailed()
	h := newFakeHarness(t, f, 60, 16)
	lines := strings.Split(h.screen(), "\n")
	errorLines := 0
	for _, line := range lines {
		if strings.Contains(line, "Cave cannot") || strings.Contains(line, "bind:") || strings.Contains(line, "Settings") && strings.Contains(line, "port") {
			errorLines++
		}
	}
	if errorLines == 0 || errorLines > 2 {
		t.Errorf("the error is on %d lines:\n%s", errorLines, h.screen())
	}
	if !strings.Contains(h.screen(), "not running") {
		t.Errorf("the state is not 'not running':\n%s", h.screen())
	}
	// A running server shows no error, even if an old one is still in the snapshot.
	f.ServerSnapshot.State = server.Running
	h.send(changedMsg{})
	if strings.Contains(h.screen(), "Cave cannot listen") {
		t.Error("the error is shown while the server runs")
	}
}

func TestOverviewHostKeyIsNeverCut(t *testing.T) {
	f := apptest.Sample(t)
	fingerprint := f.ServerSnapshot.HostKeyFingerprint
	for _, width := range []int{60, 63, 64, 80, 100} {
		h := newFakeHarness(t, f, width, 30)
		if !strings.Contains(h.screen(), fingerprint) {
			t.Errorf("width %d: the fingerprint is cut:\n%s", width, h.screen())
		}
	}
}

func TestOverviewFootersAndHelp(t *testing.T) {
	h := newFakeHarness(t, apptest.Sample(t), 100, 30)
	lines := strings.Split(h.screen(), "\n")
	if footer := lines[len(lines)-1]; !strings.HasPrefix(footer, "s start or stop the server") {
		t.Errorf("the footer of the Overview is %q", footer)
	}
	h.key("2")
	lines = strings.Split(h.screen(), "\n")
	if footer := lines[len(lines)-1]; strings.Contains(footer, "start or stop") {
		t.Errorf("the footer of another page has the Overview's key: %q", footer)
	}
	// The help does not list s twice.
	h.keys("1", "?")
	lines = strings.Split(h.screen(), "\n")
	body := strings.Join(lines[:len(lines)-1], "\n") // the footer has the key too
	if n := strings.Count(body, "start or stop the server"); n != 1 {
		t.Errorf("the help lists s %d times:\n%s", n, h.screen())
	}
}

func TestOverviewFitsEveryShortPage(t *testing.T) {
	// Even a page too short for everything keeps the first address and the counts when it can.
	f := apptest.Sample(t)
	h := newFakeHarness(t, f, 60, 16)
	screen := h.screen()
	if !strings.Contains(screen, "ssh -p 2222 alice@192.0.2.10") || !strings.Contains(screen, "… and 2 more") {
		t.Errorf("a short page:\n%s", screen)
	}
	if !strings.Contains(screen, "People 3   Connected now 1   Attempts in the list 9") {
		t.Errorf("a short page lost the counts:\n%s", screen)
	}
}
