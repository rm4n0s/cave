package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// nameTaken is the error of a name that another person has, as the store gives it.
func nameTaken() error {
	return errors.New("PersonNameTaken", "another person has this name", "name", "dave", "takenBy", "alice")
}

func TestGoldenPeople(t *testing.T) {
	type setup func(h *harness)
	tests := []struct {
		name          string
		fake          func() *apptest.Fake
		width, height int
		setup         setup
	}{
		{"sample-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, func(*harness) {}},
		{"sample-60x16", func() *apptest.Fake { return apptest.Sample(t) }, 60, 16, func(*harness) {}},
		{"second-row-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, func(h *harness) { h.key("down") }},
		{"empty-100x30", func() *apptest.Fake { return apptest.Empty(t) }, 100, 30, func(*harness) {}},
		{"add-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, func(h *harness) { h.key("a"); h.typeText("da") }},
		{"add-60x16", func() *apptest.Fake { return apptest.Sample(t) }, 60, 16, func(h *harness) { h.key("a"); h.typeText("da") }},
		{"add-error-100x30", func() *apptest.Fake {
			f := apptest.Sample(t)
			f.Fail = map[string]error{"AddPerson": nameTaken()}
			return f
		}, 100, 30, func(h *harness) { h.key("a"); h.typeText("dave"); h.key("enter") }},
		{"rename-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, func(h *harness) { h.key("e") }},
		{"remove-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, func(h *harness) { h.key("d") }},
		{"disable-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, func(h *harness) { h.key("space") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newFakeHarness(t, tt.fake(), tt.width, tt.height)
			h.key("2")
			tt.setup(h)
			h.golden("people-" + tt.name)
		})
	}
}

func TestPeopleTable(t *testing.T) {
	h := newFakeHarness(t, apptest.Sample(t), 100, 30)
	h.key("2")
	lines := strings.Split(h.screen(), "\n")
	var alice, bob, carol string
	for _, l := range lines {
		switch {
		case strings.Contains(l, "alice"):
			alice = l
		case strings.Contains(l, "bob"):
			bob = l
		case strings.Contains(l, "carol"):
			carol = l
		}
	}
	for name, want := range map[string][]string{alice: {"alice", "2", "yes", "1"}, bob: {"bob", "1", "no"}, carol: {"carol", "0", "yes"}} {
		fields := strings.Fields(strings.TrimPrefix(name, "> "))
		if len(fields) != len(want) {
			t.Errorf("the row %q has %d fields, want %v", name, len(fields), want)
			continue
		}
		for i := range want {
			if fields[i] != want[i] {
				t.Errorf("the row %q: field %d is %q, want %q", name, i, fields[i], want[i])
			}
		}
	}
	if !strings.HasPrefix(alice, "> alice") {
		t.Errorf("the first row is not selected: %q", alice)
	}
}

func TestAddAPerson(t *testing.T) {
	f := apptest.Sample(t)
	h := newFakeHarness(t, f, 100, 30)
	h.keys("2", "a")
	h.typeText("dave")
	if !strings.Contains(h.screen(), "Name of the new person: dave") {
		t.Errorf("the field:\n%s", h.screen())
	}
	h.key("enter")
	if got := f.Calls(); len(got) != 1 || got[0] != "AddPerson(dave)" {
		t.Errorf("calls = %q", got)
	}
	if h.m().prompt != nil {
		t.Error("the field stayed open after the person was added")
	}
	if h.m().peopleWanted != "p_ffffffffffffffff" {
		t.Errorf("the new person is not waiting to be selected: %q", h.m().peopleWanted)
	}
	// The person shows up in the list: they are selected.
	f.PeopleList = append(f.PeopleList, apptest.Sample(t).PeopleList[2])
	f.PeopleList[3].ID, f.PeopleList[3].Name = "p_ffffffffffffffff", "dave"
	h.send(changedMsg{})
	if h.m().peopleList.selected != 3 || h.m().peopleWanted != "" {
		t.Errorf("selected %d, wanted %q; want the new person, last row", h.m().peopleList.selected, h.m().peopleWanted)
	}
}

func TestAddAPersonThatFails(t *testing.T) {
	f := apptest.Sample(t)
	f.Fail = map[string]error{"AddPerson": nameTaken()}
	h := newFakeHarness(t, f, 100, 30)
	h.keys("2", "a")
	h.typeText("dave")
	h.key("enter")
	screen := h.screen()
	if h.m().prompt == nil || h.m().prompt.f.value() != "dave" {
		t.Fatalf("the field did not stay open with its text:\n%s", screen)
	}
	if !strings.Contains(screen, "There is already a person named alice.") || !strings.Contains(screen, "Name of the new person: dave") {
		t.Errorf("the sentence and the field are not both there:\n%s", screen)
	}
	// The sentence is above the field.
	lines := strings.Split(screen, "\n")
	sentence, field := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "There is already") {
			sentence = i
		}
		if strings.Contains(l, "Name of the new person:") {
			field = i
		}
	}
	if sentence != field-1 {
		t.Errorf("the sentence is on line %d and the field on %d", sentence, field)
	}
	// The text can be corrected and sent again; Esc closes it without another call.
	h.keys("backspace", "e")
	f.Fail = nil
	h.key("enter")
	if got := f.Calls(); len(got) != 2 || got[1] != "AddPerson(dave)" {
		t.Errorf("calls = %q", got)
	}
	h.keys("a", "esc")
	if h.m().prompt != nil || len(f.Calls()) != 2 {
		t.Errorf("esc: prompt %v, calls %q", h.m().prompt, f.Calls())
	}
}

func TestTypingANameDoesNotChangePage(t *testing.T) {
	f := apptest.Sample(t)
	h := newFakeHarness(t, f, 100, 30)
	h.keys("2", "a")
	h.typeText("1sq?3")
	if h.quit || h.m().page != pagePeople || h.m().help || len(f.Calls()) != 0 {
		t.Errorf("quit %v, page %d, help %v, calls %q", h.quit, h.m().page, h.m().help, f.Calls())
	}
	if got := h.m().prompt.f.value(); got != "1sq?3" {
		t.Errorf("the field has %q", got)
	}
	// Tab and the arrows do not move the page either; ctrl+c still quits (it asks, with a connection).
	h.keys("tab", "down")
	if h.m().page != pagePeople || h.m().peopleList.selected != 0 {
		t.Error("tab or down acted while the field was open")
	}
	h.key("ctrl+c")
	if h.quit || h.m().ask == nil {
		t.Errorf("ctrl+c with the field open: quit %v, ask %v", h.quit, h.m().ask)
	}
	h.key("ctrl+c")
	if !h.quit {
		t.Error("the second ctrl+c did not quit")
	}
}

func TestPasteIntoThePrompt(t *testing.T) {
	h := newFakeHarness(t, apptest.Sample(t), 100, 30)
	h.keys("2", "a")
	h.paste("al\nice\x1b")
	if got := h.m().prompt.f.value(); got != "al ice " {
		t.Errorf("the field has %q", got)
	}
	// Without a field a paste does nothing.
	h.key("esc")
	h.paste("anything")
	if h.m().prompt != nil {
		t.Error("a paste opened a field")
	}
}

func TestRenameAPerson(t *testing.T) {
	f := apptest.Sample(t)
	h := newFakeHarness(t, f, 100, 30)
	h.keys("2", "e")
	if got := h.m().prompt.f.value(); got != "alice" {
		t.Fatalf("the field starts with %q, want the current name", got)
	}
	h.key("ctrl+u")
	h.typeText("alicia")
	h.key("enter")
	if got := f.Calls(); len(got) != 1 || got[0] != "RenamePerson("+apptest.AliceID+", alicia)" {
		t.Errorf("calls = %q", got)
	}
	// A failure keeps the field.
	f.Fail = map[string]error{"RenamePerson": nameTaken()}
	h.key("e")
	h.key("enter")
	if h.m().prompt == nil || !strings.Contains(h.screen(), "There is already a person") {
		t.Errorf("the failure of a rename:\n%s", h.screen())
	}
}

func TestEnableAndDisable(t *testing.T) {
	t.Run("a connected person is asked first", func(t *testing.T) {
		f := apptest.Sample(t)
		h := newFakeHarness(t, f, 100, 30)
		h.keys("2", "space")
		if !strings.Contains(h.screen(), "alice is connected (1 connection). Disable and disconnect?") || len(f.Calls()) != 0 {
			t.Fatalf("calls %q:\n%s", f.Calls(), h.screen())
		}
		h.key("n")
		if len(f.Calls()) != 0 {
			t.Errorf("no: calls %q", f.Calls())
		}
		h.keys("space", "y")
		if got := f.Calls(); len(got) != 1 || got[0] != "SetPersonEnabled("+apptest.AliceID+", false)" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("a disabled person is enabled at once", func(t *testing.T) {
		f := apptest.Sample(t)
		h := newFakeHarness(t, f, 100, 30)
		h.keys("2", "down", "space")
		if got := f.Calls(); len(got) != 1 || got[0] != "SetPersonEnabled("+apptest.BobID+", true)" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("a person who is not connected is disabled at once", func(t *testing.T) {
		f := apptest.Sample(t)
		h := newFakeHarness(t, f, 100, 30)
		h.keys("2", "down", "down", "space")
		if got := f.Calls(); len(got) != 1 || got[0] != "SetPersonEnabled("+apptest.CarolID+", false)" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("two connections", func(t *testing.T) {
		f := apptest.Sample(t)
		f.ConnectionList = append(f.ConnectionList, f.ConnectionList[0])
		h := newFakeHarness(t, f, 100, 30)
		h.keys("2", "space")
		if !strings.Contains(h.screen(), "(2 connections)") {
			t.Errorf("the question:\n%s", h.screen())
		}
	})
}

func TestRemoveAPerson(t *testing.T) {
	tests := []struct {
		name string
		down int
		want string
		id   string
	}{
		{"keys and a connection", 0, "Remove alice and their 2 keys? This closes 1 connection.", apptest.AliceID},
		{"one key", 1, "Remove bob and their 1 key?", apptest.BobID},
		{"no key", 2, "Remove carol?", apptest.CarolID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := apptest.Sample(t)
			h := newFakeHarness(t, f, 100, 30)
			h.key("2")
			for i := 0; i < tt.down; i++ {
				h.key("down")
			}
			h.key("d")
			if !strings.Contains(h.screen(), tt.want) {
				t.Fatalf("the question is not %q:\n%s", tt.want, h.screen())
			}
			h.key("enter") // the default is no
			h.key("d")
			h.key("esc")
			if len(f.Calls()) != 0 {
				t.Fatalf("a no removed somebody: %q", f.Calls())
			}
			h.keys("d", "y")
			if got := f.Calls(); len(got) != 1 || got[0] != "RemovePerson("+tt.id+")" {
				t.Errorf("calls = %q", got)
			}
		})
	}
	t.Run("a failure is shown on the message line", func(t *testing.T) {
		f := apptest.Sample(t)
		f.Fail = map[string]error{"RemovePerson": errors.New("PersonNotFound", "x")}
		h := newFakeHarness(t, f, 100, 30)
		h.keys("2", "d", "y")
		if !strings.Contains(h.screen(), "There is no such person any more.") {
			t.Errorf("the screen:\n%s", h.screen())
		}
	})
}

func TestNothingToChangeInAnEmptyList(t *testing.T) {
	f := apptest.Empty(t)
	h := newFakeHarness(t, f, 100, 30)
	h.keys("2", "e", "space", "d", "down", "up", "enter")
	if len(f.Calls()) != 0 || h.m().prompt != nil || h.m().ask != nil {
		t.Errorf("calls %q, prompt %v, ask %v", f.Calls(), h.m().prompt, h.m().ask)
	}
	if !strings.Contains(h.screen(), "Nobody yet. Press a to add a person.") {
		t.Errorf("the page:\n%s", h.screen())
	}
}

func TestPeopleKeepsTheSelection(t *testing.T) {
	f := apptest.Sample(t)
	h := newFakeHarness(t, f, 100, 30)
	h.keys("2", "down") // bob
	if h.m().peopleSel != apptest.BobID {
		t.Fatalf("selected %q", h.m().peopleSel)
	}
	// A new person comes first in the list: the selection stays on bob.
	first := apptest.Sample(t).PeopleList[2]
	first.ID, first.Name = "p_00000000000000f0", "aaron"
	f.PeopleList = append([]people.Person{first}, f.PeopleList...)
	h.send(changedMsg{})
	if p, _ := h.m().selectedPerson(); p.ID != apptest.BobID || h.m().peopleList.selected != 2 {
		t.Errorf("selected %s at row %d, want bob at row 2", p.ID, h.m().peopleList.selected)
	}
	// Bob goes: the row stays where it was.
	f.PeopleList = append(f.PeopleList[:2:2], f.PeopleList[3:]...)
	h.send(changedMsg{})
	if p, _ := h.m().selectedPerson(); p.ID != apptest.CarolID {
		t.Errorf("after bob was removed the selection is on %s", p.ID)
	}
	// Everybody goes.
	f.PeopleList = nil
	h.send(changedMsg{})
	if _, ok := h.m().selectedPerson(); ok || h.m().peopleSel != "" {
		t.Error("a person is selected in an empty list")
	}
}

func TestThePeopleListScrolls(t *testing.T) {
	f := apptest.Sample(t)
	template := f.PeopleList[2]
	for i := 0; i < 30; i++ {
		p := template
		p.ID = "p_" + strings.Repeat("0", 14) + string(rune('a'+i/16)) + string("0123456789abcdef"[i%16])
		p.Name = "user" + itoa(i)
		f.PeopleList = append(f.PeopleList, p)
	}
	h := newFakeHarness(t, f, 100, 20)
	h.key("2")
	h.keys("end")
	screen := h.screen()
	if !strings.Contains(screen, "> user29") {
		t.Errorf("the last row is not in view:\n%s", screen)
	}
	h.key("home")
	if !strings.Contains(h.screen(), "> alice") {
		t.Errorf("the first row is not in view:\n%s", h.screen())
	}
	for i := 0; i < 8; i++ {
		h.key("pgdown")
	}
	if p, _ := h.m().selectedPerson(); h.m().peopleList.selected < 20 || !strings.Contains(h.screen(), "> "+p.Name) {
		t.Errorf("after pgdown the selection is not in view:\n%s", h.screen())
	}
}

func TestEveryLineFitsWithAPrompt(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {100, 30}} {
		f := apptest.Sample(t)
		f.Fail = map[string]error{"AddPerson": nameTaken()}
		h := newFakeHarness(t, f, size[0], size[1])
		h.keys("2", "a")
		h.typeText(strings.Repeat("x", 40))
		h.key("enter")
		lines := strings.Split(h.screen(), "\n")
		if len(lines) != size[1] {
			t.Fatalf("%v: %d lines", size, len(lines))
		}
		for i, l := range lines {
			if w := len([]rune(l)); w > size[0] {
				t.Errorf("%v: line %d has %d characters", size, i, w)
			}
		}
	}
}

// ---- with the real application

func TestPeopleFlowsWithTheRealApplication(t *testing.T) {
	r := apptest.Open(t, nil)
	h := newHarness(t, r.App, r.Clock, 100, 30)
	h.key("2")

	// Add alice.
	h.key("a")
	h.typeText("alice")
	h.key("enter")
	h.send(changedMsg{})
	list := r.App.People()
	if len(list) != 1 || list[0].Name != "alice" || h.m().prompt != nil {
		t.Fatalf("people %+v, prompt %v", list, h.m().prompt)
	}
	if h.m().peopleSel != list[0].ID || !strings.Contains(h.screen(), "> alice") {
		t.Errorf("alice is not selected:\n%s", h.screen())
	}

	// A name that is not valid: the sentence of the real error, and the field stays.
	h.key("a")
	h.typeText("-bad")
	h.key("enter")
	if h.m().prompt == nil || !strings.Contains(h.screen(), "A name has 1 to 32 letters") {
		t.Errorf("a bad name:\n%s", h.screen())
	}
	h.typeText("!") // the field is still the user's: the sentence stays until the next try
	h.key("esc")

	// Rename her.
	h.key("e")
	h.key("ctrl+u")
	h.typeText("alicia")
	h.key("enter")
	h.send(changedMsg{})
	if got := r.App.People()[0].Name; got != "alicia" {
		t.Errorf("the name is %q", got)
	}

	// Disable her (she is not connected: no question), then enable her again.
	h.key("space")
	h.send(changedMsg{})
	if r.App.People()[0].Enabled || !strings.Contains(h.screen(), "no") {
		t.Errorf("alicia is still enabled:\n%s", h.screen())
	}
	h.key("space")
	if !r.App.People()[0].Enabled {
		t.Error("alicia was not enabled again")
	}

	// Remove her.
	h.keys("d", "y")
	h.send(changedMsg{})
	if n := len(r.App.People()); n != 0 {
		t.Errorf("%d people are left", n)
	}
	if !strings.Contains(h.screen(), "Nobody yet.") {
		t.Errorf("the page:\n%s", h.screen())
	}
}

// closed waits until a client's connection has ended.
func closed(t *testing.T, client *ssh.Client) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		_ = client.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the connection is still open")
	}
}

func TestRemovingAConnectedPersonClosesTheirConnection(t *testing.T) {
	r := apptest.Open(t, nil)
	key := r.Person("bob")
	client := r.Connect("bob", key)
	h := newHarness(t, r.App, r.Clock, 100, 30)
	h.key("2")
	if !strings.Contains(h.screen(), "bob") {
		t.Fatalf("the page:\n%s", h.screen())
	}
	h.key("d")
	if !strings.Contains(h.screen(), "Remove bob and their 1 key? This closes 1 connection.") {
		t.Fatalf("the question:\n%s", h.screen())
	}
	h.key("y")
	closed(t, client)
	h.send(changedMsg{})
	if len(r.App.People()) != 0 || len(r.App.Connections()) != 0 {
		t.Errorf("people %d, connections %d", len(r.App.People()), len(r.App.Connections()))
	}
}

func TestDisablingAConnectedPersonAsksAndDisconnects(t *testing.T) {
	r := apptest.Open(t, nil)
	key := r.Person("bob")
	client := r.Connect("bob", key)
	h := newHarness(t, r.App, r.Clock, 100, 30)
	h.keys("2", "space")
	if !strings.Contains(h.screen(), "bob is connected (1 connection). Disable and disconnect?") {
		t.Fatalf("the question:\n%s", h.screen())
	}
	h.key("y")
	closed(t, client)
	if r.App.People()[0].Enabled {
		t.Error("bob is still enabled")
	}
}
