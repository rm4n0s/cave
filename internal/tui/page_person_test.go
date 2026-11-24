package tui

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

const (
	aliceKey1 = "k_00000000000001a1"
	aliceKey2 = "k_00000000000001a2"
)

// openPersonHarness opens the People page and the person at the row.
func openPersonHarness(t *testing.T, f *apptest.Fake, width, height, row int) *harness {
	t.Helper()
	h := newFakeHarness(t, f, width, height)
	h.key("2")
	for i := 0; i < row; i++ {
		h.key("down")
	}
	h.key("enter")
	return h
}

func keyIsPrivate() error {
	return errors.New("KeyIsPrivate", "this is a private key")
}

func TestGoldenPerson(t *testing.T) {
	tests := []struct {
		name          string
		fake          func() *apptest.Fake
		width, height int
		row           int
		then          func(h *harness)
	}{
		{"alice-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, 0, func(*harness) {}},
		{"alice-60x16", func() *apptest.Fake { return apptest.Sample(t) }, 60, 16, 0, func(*harness) {}},
		{"alice-second-key-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, 0, func(h *harness) { h.key("down") }},
		{"bob-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, 1, func(*harness) {}},
		{"carol-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, 2, func(*harness) {}},
		{"area-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, 0, func(h *harness) {
			h.key("a")
			h.paste("ssh-ed25519 AAAA first\nssh-ed25519 BBBB second")
		}},
		{"area-60x16", func() *apptest.Fake { return apptest.Sample(t) }, 60, 16, 0, func(h *harness) {
			h.key("a")
			h.paste("ssh-ed25519 AAAA first\nssh-ed25519 BBBB second")
		}},
		{"area-error-100x30", func() *apptest.Fake {
			f := apptest.Sample(t)
			f.Fail = map[string]error{"AddKeys": keyIsPrivate()}
			return f
		}, 100, 30, 0, func(h *harness) {
			h.key("a")
			h.paste("-----BEGIN OPENSSH PRIVATE KEY-----")
			h.key("ctrl+d")
		}},
		{"import-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, 0, func(h *harness) { h.key("i"); h.typeText("~/keys.pub") }},
		{"comment-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, 0, func(h *harness) { h.key("c") }},
		{"remove-key-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, 0, func(h *harness) { h.key("d") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := openPersonHarness(t, tt.fake(), tt.width, tt.height, tt.row)
			tt.then(h)
			h.golden("person-" + tt.name)
		})
	}
}

func TestOpenAPerson(t *testing.T) {
	f := apptest.Sample(t)
	h := openPersonHarness(t, f, 100, 30, 0)
	screen := h.screen()
	for _, want := range []string{"alice   enabled   1 connection", "ssh-ed25519", "laptop", "work desktop", "2026-10-26", "SHA256:"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the page lacks %q:\n%s", want, screen)
		}
	}
	// A disabled person who is not connected.
	h = openPersonHarness(t, f, 100, 30, 1)
	if !strings.Contains(h.screen(), "bob   disabled   not connected") {
		t.Errorf("the page of bob:\n%s", h.screen())
	}
	// Nobody has no key: the empty text.
	h = openPersonHarness(t, f, 100, 30, 2)
	if !strings.Contains(h.screen(), "No key yet, so carol cannot connect. Press a to add one.") {
		t.Errorf("the page of carol:\n%s", h.screen())
	}
	// The page's keys are in the footer, the help has them under "a person".
	if lines := strings.Split(h.screen(), "\n"); !strings.HasPrefix(lines[len(lines)-1], "a add") {
		t.Errorf("the footer: %q", lines[len(lines)-1])
	}
	h.key("?")
	if !strings.Contains(h.screen(), "Keys of a person") || !strings.Contains(h.screen(), "import") {
		t.Errorf("the help:\n%s", h.screen())
	}
}

func TestEscGoesBackToTheList(t *testing.T) {
	h := openPersonHarness(t, apptest.Sample(t), 100, 30, 1)
	h.key("esc")
	if h.m().personID != "" || !strings.Contains(h.screen(), "Connected") {
		t.Errorf("esc did not go back:\n%s", h.screen())
	}
	if p, _ := h.m().selectedPerson(); p.Name != "bob" {
		t.Errorf("the list is on %s, want bob", p.Name)
	}
	// Enter on an empty list opens nobody.
	e := newFakeHarness(t, apptest.Empty(t), 100, 30)
	e.keys("2", "enter")
	if e.m().personID != "" {
		t.Error("enter opened a person in an empty list")
	}
}

func TestThePersonLeavesWhileTheirPageIsOpen(t *testing.T) {
	f := apptest.Sample(t)
	h := openPersonHarness(t, f, 100, 30, 0)
	f.PeopleList = f.PeopleList[1:] // alice is removed elsewhere
	h.send(changedMsg{})
	if h.m().personID != "" || !strings.Contains(h.screen(), "Connected") {
		t.Errorf("the page of a person who is gone is still open:\n%s", h.screen())
	}
}

func TestAddKeysByPaste(t *testing.T) {
	f := apptest.Sample(t)
	h := openPersonHarness(t, f, 100, 30, 0)
	h.key("a")
	if !strings.Contains(h.screen(), "Paste public keys (one per line), then press ctrl+d. esc cancels.") {
		t.Fatalf("the area:\n%s", h.screen())
	}
	h.paste("ssh-ed25519 AAAA one\nssh-ed25519 BBBB two")
	h.key("ctrl+d")
	want := "AddKeys(" + apptest.AliceID + `, "ssh-ed25519 AAAA one\nssh-ed25519 BBBB two")`
	if got := f.Calls(); len(got) != 1 || got[0] != want {
		t.Errorf("calls = %q, want [%s]", got, want)
	}
	if h.m().area != nil {
		t.Error("the area stayed open after the keys were added")
	}
}

func TestAPasteWithAnEscapeReachesTheCallCleaned(t *testing.T) {
	f := apptest.Sample(t)
	h := openPersonHarness(t, f, 100, 30, 0)
	h.key("a")
	h.paste("ssh-ed25519 AAAA\x1b[31m laptop\r\nssh-ed25519 BBBB\x00")
	h.key("ctrl+d")
	got := f.Calls()
	if len(got) != 1 || strings.Contains(got[0], `\x1b`) || strings.Contains(got[0], `\x00`) || !strings.Contains(got[0], `\n`) {
		t.Fatalf("calls = %q", got)
	}
	if want := `, "ssh-ed25519 AAAA [31m laptop\nssh-ed25519 BBBB ")`; !strings.HasSuffix(got[0], want) {
		t.Errorf("call = %s, want it to end with %s", got[0], want)
	}
}

func TestTypingInTheAreaKeepsEveryKey(t *testing.T) {
	f := apptest.Sample(t)
	h := openPersonHarness(t, f, 100, 30, 0)
	h.key("a")
	h.typeText("1sq?")
	h.keys("enter", "tab", "x")
	if h.quit || h.m().page != pagePeople || h.m().help || len(f.Calls()) != 0 {
		t.Errorf("quit %v, page %d, help %v, calls %q", h.quit, h.m().page, h.m().help, f.Calls())
	}
	if got := h.m().area.a.text(); got != "1sq?\nx" {
		t.Errorf("the area has %q (tab is the area's, not a key it takes)", got)
	}
	h.key("esc")
	if h.m().area != nil || h.m().personID == "" {
		t.Error("esc closed more than the area")
	}
}

func TestAddKeysThatFail(t *testing.T) {
	f := apptest.Sample(t)
	f.Fail = map[string]error{"AddKeys": keyIsPrivate()}
	h := openPersonHarness(t, f, 100, 30, 0)
	h.key("a")
	h.paste("secret")
	h.key("ctrl+d")
	want := "This is a private key. Paste the public key instead: the file that ends in .pub."
	if h.m().area == nil || !strings.Contains(h.screen(), want) || !strings.Contains(h.screen(), "secret") {
		t.Fatalf("the area did not stay with its text and the sentence:\n%s", h.screen())
	}
	// It can be corrected and sent again.
	f.Fail = nil
	h.keys("ctrl+u")
	h.typeText("ok")
	h.key("ctrl+d")
	if len(f.Calls()) != 2 || h.m().area != nil {
		t.Errorf("calls %q, area %v", f.Calls(), h.m().area)
	}
}

func TestImportKeys(t *testing.T) {
	f := apptest.Sample(t)
	h := openPersonHarness(t, f, 100, 30, 0)
	h.key("i")
	h.typeText("/tmp/keys/authorized_keys")
	h.key("enter")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	h.keys("i")
	h.typeText("~/keys.pub")
	h.key("enter")
	h.keys("i")
	h.typeText("~bob/keys.pub")
	h.key("enter")
	want := []string{
		"ImportKeys(" + apptest.AliceID + ", /tmp/keys/authorized_keys)",
		"ImportKeys(" + apptest.AliceID + ", " + home + "/keys.pub)",
		"ImportKeys(" + apptest.AliceID + ", ~bob/keys.pub)",
	}
	if got := f.Calls(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls = %q, want %q", got, want)
	}
}

func TestEditAComment(t *testing.T) {
	f := apptest.Sample(t)
	h := openPersonHarness(t, f, 100, 30, 0)
	h.keys("down", "c") // the second key
	if got := h.m().prompt.f.value(); got != "work desktop" {
		t.Fatalf("the field starts with %q", got)
	}
	h.key("ctrl+u")
	h.typeText("desk")
	h.key("enter")
	if got := f.Calls(); len(got) != 1 || got[0] != "SetKeyComment("+apptest.AliceID+", "+aliceKey2+`, "desk")` {
		t.Errorf("calls = %q", got)
	}
	// The comment is cut at its limit.
	h.key("c")
	h.key("ctrl+u")
	h.typeText(strings.Repeat("x", 100))
	if n := len(h.m().prompt.f.text); n != maxCommentLength {
		t.Errorf("the comment field took %d characters, want %d", n, maxCommentLength)
	}
	// With no key there is nothing to comment.
	c := openPersonHarness(t, apptest.Sample(t), 100, 30, 2)
	c.key("c")
	if c.m().prompt != nil {
		t.Error("a comment field opened for a person without a key")
	}
}

func TestRemoveAKey(t *testing.T) {
	f := apptest.Sample(t)
	h := openPersonHarness(t, f, 100, 30, 0)
	h.key("d")
	if h.m().ask == nil || !strings.HasPrefix(h.m().ask.q.text, "Remove this key (SHA256:") || !strings.HasSuffix(h.m().ask.q.text, ")? This closes 1 connection.") {
		t.Fatalf("the question:\n%s", h.screen())
	}
	h.key("enter")
	h.keys("d", "esc")
	if len(f.Calls()) != 0 {
		t.Fatalf("calls = %q", f.Calls())
	}
	h.keys("d", "y")
	if got := f.Calls(); len(got) != 1 || got[0] != "RemoveKey("+apptest.AliceID+", "+aliceKey1+")" {
		t.Errorf("calls = %q", got)
	}
	// The second key has no connection: the question does not say so.
	h.keys("down", "d")
	if h.m().ask == nil || strings.Contains(h.m().ask.q.text, "This closes") {
		t.Errorf("a key without a connection:\n%s", h.screen())
	}
}

func TestPersonKeepsTheSelectedKey(t *testing.T) {
	f := apptest.Sample(t)
	h := openPersonHarness(t, f, 100, 30, 0)
	h.key("down") // the second key
	// A key is added before it: the selection stays on the key.
	first := f.PeopleList[0].Keys[0]
	first.ID = "k_00000000000001a0"
	f.PeopleList[0].Keys = append([]people.Key{first}, f.PeopleList[0].Keys...)
	h.send(changedMsg{})
	if k, _ := h.m().selectedKey(); k.ID != aliceKey2 {
		t.Errorf("the selection moved to %s", k.ID)
	}
}

// ---- with the real application

// textOf returns the lines of text that a key text of the error cases is made of.
func weakRSAKey(t *testing.T) string {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	public, err := ssh.NewPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public)))
}

// unknownTypeKey is a key line of a type that Cave does not know, which the library cannot parse.
func unknownTypeKey() string {
	data := []byte{0, 0, 0, 7}
	data = append(data, "ssh-foo"...)
	data = append(data, 0, 0, 0, 0)
	return "ssh-foo " + base64.StdEncoding.EncodeToString(data)
}

func TestEveryErrorOfAPasteIsShown(t *testing.T) {
	r := apptest.Open(t, nil)
	r.Person("alice")
	other := r.Person("bob")
	own := sshtest.NewKey(t, sshtest.Ed25519)
	private, err := os.ReadFile(own.PrivatePath)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		text string
	}{
		{"no key in the text", "# nothing here\n"},
		{"text that is not a key", "this is not a key"},
		{"a private key", string(private)},
		{"a type that is not supported", unknownTypeKey()},
		{"a key that is too weak", weakRSAKey(t)},
		{"a key with options", `command="ls" ` + own.AuthorizedKey},
		{"a key that another person has", other.AuthorizedKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, r.App, r.Clock, 140, 30)
			h.keys("2", "enter")
			before := len(r.App.People()[0].Keys)
			h.key("a")
			h.paste(tt.text)
			h.key("ctrl+d")
			_, err := r.App.AddKeys(ctx, r.App.People()[0].ID, tt.text)
			if err == nil {
				t.Fatal("AddKeys accepted the text")
			}
			want := app.Message(err)
			// The sentence is on the screen, whole (it wraps over two lines at most).
			joined := strings.Join(strings.Fields(h.screen()), " ")
			if !strings.Contains(joined, strings.Join(strings.Fields(want), " ")) {
				t.Errorf("the screen does not show %q:\n%s", want, h.screen())
			}
			if h.m().area == nil {
				t.Error("the area closed after an error")
			}
			if after := len(r.App.People()[0].Keys); after != before {
				t.Errorf("the person has %d keys, had %d", after, before)
			}
		})
	}
}

func TestAddAKeyByPasteThenConnectAndRemoveIt(t *testing.T) {
	r := apptest.Open(t, nil)
	p := r.Keyless("carol")
	key := sshtest.NewKey(t, sshtest.Ed25519)
	h := newHarness(t, r.App, r.Clock, 120, 30)
	h.send(changedMsg{})
	h.keys("2", "enter")
	h.key("a")
	h.paste(key.AuthorizedKey + " laptop\n")
	h.key("ctrl+d")
	h.send(changedMsg{})
	if h.m().area != nil || len(r.App.People()[0].Keys) != 1 || r.App.People()[0].ID != p.ID {
		t.Fatalf("the key was not added: area %v, people %+v", h.m().area, r.App.People())
	}
	if !strings.Contains(h.screen(), "laptop") {
		t.Errorf("the key is not on the page:\n%s", h.screen())
	}
	// The person connects with it, and removing the key closes the connection.
	client := r.Connect("carol", key)
	h.send(changedMsg{})
	h.key("d")
	if h.m().ask == nil || !strings.HasSuffix(h.m().ask.q.text, "This closes 1 connection.") {
		t.Fatalf("the question:\n%s", h.screen())
	}
	h.key("y")
	closed(t, client)
	h.send(changedMsg{})
	if len(r.App.People()[0].Keys) != 0 || !strings.Contains(h.screen(), "No key yet, so carol cannot connect.") {
		t.Errorf("the key is still there:\n%s", h.screen())
	}
}

func TestImportAFileWithTheRealApplication(t *testing.T) {
	r := apptest.Open(t, nil)
	r.Keyless("dave")
	key := sshtest.NewKey(t, sshtest.Ed25519)
	path := filepath.Join(t.TempDir(), "dave.pub")
	if err := os.WriteFile(path, []byte(key.AuthorizedKey+" from a file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Wide enough for the sentence about a missing file to show its long temporary path whole
	// (macOS has long ones).
	h := newHarness(t, r.App, r.Clock, 200, 30)
	h.keys("2", "enter", "i")
	h.typeText(path)
	h.key("enter")
	h.send(changedMsg{})
	if keys := r.App.People()[0].Keys; len(keys) != 1 || keys[0].Comment != "from a file" {
		t.Fatalf("keys: %+v", keys)
	}
	// A file that is not there: the sentence, and the field stays.
	h.key("i")
	h.typeText(filepath.Join(t.TempDir(), "missing.pub"))
	h.key("enter")
	if h.m().prompt == nil || !strings.Contains(h.screen(), "does not exist") {
		t.Errorf("a missing file:\n%s", h.screen())
	}
	// Comment it.
	h.key("esc")
	h.key("c")
	h.key("ctrl+u")
	h.typeText("renamed")
	h.key("enter")
	h.send(changedMsg{})
	if got := r.App.People()[0].Keys[0].Comment; got != "renamed" {
		t.Errorf("the comment is %q", got)
	}
}
