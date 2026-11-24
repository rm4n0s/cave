package gui

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	m "github.com/rm4n0s/glutter/material"
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"github.com/rm4n0s/cave/internal/gui/testutil"
)

// ctx is the context of the commands that a test calls itself.
var ctx = context.Background()

const (
	aliceKey1 = "k_00000000000001a1"
	aliceKey2 = "k_00000000000001a2"
)

func keyIsPrivate() error { return errors.New("KeyIsPrivate", "this is a private key") }

// openPerson pumps the People page and selects the person (on a narrow window that shows their page).
func openPerson(t *testing.T, f *apptest.Fake, _ any, id string, wide bool) *gluttertest.Tester {
	t.Helper()
	sz := testutil.Narrow
	if wide {
		sz = testutil.Wide
	}
	tester := pump(t, f, sz, "people")
	tapKey(tester, personRowKey(id))
	return tester
}

func TestGoldenPerson(t *testing.T) {
	light := func(f *apptest.Fake, t *testing.T) *apptest.Fake {
		f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.theme": "light"})
		return f
	}
	t.Run("alice light wide", func(t *testing.T) {
		openPerson(t, light(apptest.Sample(t), t), nil, apptest.AliceID, true).MatchesGolden("person-alice-light-wide")
	})
	t.Run("alice dark narrow", func(t *testing.T) {
		f := apptest.Sample(t)
		f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.theme": "dark"})
		openPerson(t, f, nil, apptest.AliceID, false).MatchesGolden("person-alice-dark-narrow")
	})
	t.Run("carol light wide", func(t *testing.T) {
		openPerson(t, light(apptest.Sample(t), t), nil, apptest.CarolID, true).MatchesGolden("person-carol-light-wide")
	})
	t.Run("add dialog with a private key light wide", func(t *testing.T) {
		f := light(apptest.Sample(t), t)
		f.Fail = map[string]error{"AddKeys": keyIsPrivate()}
		tester := openPerson(t, f, nil, apptest.AliceID, true)
		tapKey(tester, addKeysKey)
		tester.EnterText(gluttertest.ByKey(keysFieldKey), "-----BEGIN OPENSSH PRIVATE KEY-----")
		tapKey(tester, formActionKey)
		tester.MatchesGolden("person-add-key-error-light-wide")
	})
}

func TestThePaneShowsThePerson(t *testing.T) {
	f := apptest.Sample(t)
	tester := openPerson(t, f, nil, apptest.AliceID, true)
	for _, want := range []string{"Enabled · 1 connection", "laptop", "work desktop", "ssh-ed25519", "ssh-rsa"} {
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(want)); err != nil {
			t.Error(err)
		}
	}
	for _, id := range []string{aliceKey1, aliceKey2} {
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByKey(keyRowKey(id))); err != nil {
			t.Error(err)
		}
	}
	// A key with no comment says so, and carol has no key at all.
	tester = openPerson(t, f, nil, apptest.BobID, true)
	for _, want := range []string{"Disabled", "No comment"} {
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(want)); err != nil {
			t.Error(err)
		}
	}
	tester = openPerson(t, f, nil, apptest.CarolID, true)
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("No key yet, so carol cannot connect.")); err != nil {
		t.Error(err)
	}
}

func TestFingerprintsAreCutInTheMiddle(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width float64
		want  string
	}{
		{"it fits", "SHA256:abcdefghij", 400, "SHA256:abcdefghij"},
		{"too narrow to cut", "SHA256:abcdefghij", 30, "SHA256:abcdefghij"},
		{"cut", "SHA256:abcdefghijklmnopqrstuvwxyz", 160, "SHA256:abcdefg…tuvwxyz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cutMiddle(tt.text, tt.width)
			if got != tt.want && tt.name != "cut" {
				t.Errorf("cutMiddle = %q, want %q", got, tt.want)
			}
			if tt.name == "cut" {
				if !strings.Contains(got, "…") || !strings.HasPrefix(got, "SHA256:") || !strings.HasSuffix(got, "xyz") || len([]rune(got)) > int(tt.width/monoCharWidth) {
					t.Errorf("cutMiddle = %q", got)
				}
			}
		})
	}
}

func TestTheFullFingerprintIsInTheTooltip(t *testing.T) {
	f := apptest.Sample(t)
	tester := openPerson(t, f, nil, apptest.AliceID, false)
	full := f.PeopleList[0].Keys[0].Fingerprint
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByTooltip(full)); err != nil {
		t.Error(err)
	}
}

func TestKeyCommands(t *testing.T) {
	t.Run("add keys", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := openPerson(t, f, nil, apptest.AliceID, true)
		tapKey(tester, addKeysKey)
		tester.EnterText(gluttertest.ByKey(keysFieldKey), "ssh-ed25519 AAAA one\nssh-ed25519 BBBB two")
		tapKey(tester, formActionKey)
		want := "AddKeys(" + apptest.AliceID + ", " + strconv.Quote("ssh-ed25519 AAAA one\nssh-ed25519 BBBB two") + ")"
		if got := f.Calls(); len(got) != 1 || got[0] != want {
			t.Errorf("calls = %q, want [%s]", got, want)
		}
	})
	t.Run("an error of the keys shows as it is and the dialog stays", func(t *testing.T) {
		f := apptest.Sample(t)
		bad := errors.New("KeyParseFailed", "x", "line", 2)
		f.Fail = map[string]error{"AddKeys": bad}
		tester := openPerson(t, f, nil, apptest.AliceID, true)
		tapKey(tester, addKeysKey)
		tester.EnterText(gluttertest.ByKey(keysFieldKey), "a\nb")
		tapKey(tester, formActionKey)
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Line 2: this is not a public key. Paste the contents of a .pub file.")); err != nil {
			t.Error(err)
		}
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByKey(formActionKey)); err != nil {
			t.Errorf("the dialog closed: %v", err)
		}
	})
	t.Run("the comment", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := openPerson(t, f, nil, apptest.AliceID, true)
		tapKey(tester, editCommentKey(aliceKey1))
		field := gluttertest.WidgetAs[*m.TextField](tester, gluttertest.ByKey(commentKey))
		if got := field.Controller().Text(); got != "laptop" {
			t.Errorf("the field starts with %q, want laptop", got)
		}
		tester.EnterText(gluttertest.ByKey(commentKey), "old laptop")
		tapKey(tester, formActionKey)
		want := "SetKeyComment(" + apptest.AliceID + ", " + aliceKey1 + ", \"old laptop\")"
		if got := f.Calls(); len(got) != 1 || got[0] != want {
			t.Errorf("calls = %q, want [%s]", got, want)
		}
	})
	t.Run("remove a key says what it closes", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := openPerson(t, f, nil, apptest.AliceID, true)
		tapKey(tester, removeKeyButtonKey(aliceKey1))
		fp := f.PeopleList[0].Keys[0].Fingerprint
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Remove this key? "+fp+" This closes 1 connection.")); err != nil {
			t.Fatal(err)
		}
		tapKey(tester, confirmCancelKey)
		eventually(t, tester, "the dialog closes", func() bool { return !tester.Exists(gluttertest.ByKey(confirmYesKey)) })
		if got := f.Calls(); len(got) != 0 {
			t.Fatalf("Cancel made the calls %q", got)
		}
		tapKey(tester, removeKeyButtonKey(aliceKey1))
		tapKey(tester, confirmYesKey)
		eventually(t, tester, "the call", func() bool { return len(f.Calls()) == 1 })
		if got, want := f.Calls()[0], "RemoveKey("+apptest.AliceID+", "+aliceKey1+")"; got != want {
			t.Errorf("calls = %q, want %q", got, want)
		}
	})
	t.Run("a key nobody uses is removed without the sentence", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := openPerson(t, f, nil, apptest.AliceID, true)
		tapKey(tester, removeKeyButtonKey(aliceKey2))
		fp := f.PeopleList[0].Keys[1].Fingerprint
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Remove this key? "+fp)); err != nil {
			t.Error(err)
		}
	})
}

func TestImportFromAFile(t *testing.T) {
	t.Run("the file the dialog answers", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pumpGoroutines(t, f, testutil.Wide, "people")
		desktop := tester.FakeDesktop()
		tapKey(tester, personRowKey(apptest.AliceID))
		tapKey(tester, importKeyKey)
		eventually(t, tester, "the file dialog opens", func() bool { return desktop.PendingFileChoosers() == 1 })
		if asked := desktop.FileChoosers()[0]; asked.Multiple || asked.Directory {
			t.Errorf("the dialog asked for %+v, want one file", asked)
		}
		desktop.AnswerFileChooser("/tmp/keys/laptop.pub")
		eventually(t, tester, "the call", func() bool { return len(f.Calls()) == 1 })
		if got, want := f.Calls()[0], "ImportKeys("+apptest.AliceID+", /tmp/keys/laptop.pub)"; got != want {
			t.Errorf("calls = %q, want %q", got, want)
		}
	})
	t.Run("a cancelled dialog makes no call", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pumpGoroutines(t, f, testutil.Wide, "people")
		desktop := tester.FakeDesktop()
		tapKey(tester, personRowKey(apptest.AliceID))
		tapKey(tester, importKeyKey)
		eventually(t, tester, "the file dialog opens", func() bool { return desktop.PendingFileChoosers() == 1 })
		desktop.CancelFileChooser()
		eventually(t, tester, "the dialog is answered", func() bool { return desktop.PendingFileChoosers() == 0 })
		time.Sleep(50 * time.Millisecond)
		tester.FlushPosted()
		tester.PumpAndSettle()
		if got := f.Calls(); len(got) != 0 {
			t.Errorf("calls = %q", got)
		}
		if err := gluttertest.FindsNothing(tester, gluttertest.ByKey(pathFieldKey)); err != nil {
			t.Errorf("the path dialog opened after Cancel: %v", err)
		}
	})
	t.Run("without a file dialog the path is asked", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pumpGoroutines(t, f, testutil.Wide, "people")
		tapKey(tester, personRowKey(apptest.AliceID))
		tapKey(tester, importKeyKey)
		eventually(t, tester, "the path dialog opens", func() bool { return tester.Exists(gluttertest.ByKey(pathFieldKey)) })
		tester.EnterText(gluttertest.ByKey(pathFieldKey), " /home/ada/laptop.pub ")
		tapKey(tester, formActionKey)
		eventually(t, tester, "the call", func() bool { return len(f.Calls()) == 1 })
		if got, want := f.Calls()[0], "ImportKeys("+apptest.AliceID+", /home/ada/laptop.pub)"; got != want {
			t.Errorf("calls = %q, want %q", got, want)
		}
	})
}

// ---- with the real application

// weakRSAKey is an RSA key of 1024 bits, which Cave refuses.
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
		{"text that is not a key", "this is not a key"},
		{"a private key", string(private)},
		{"a type that is not supported", unknownTypeKey()},
		{"a key that is too weak", weakRSAKey(t)},
		{"a key with options", `command="ls" ` + own.AuthorizedKey},
		{"a key that another person has", other.AuthorizedKey},
	}
	aliceID := r.App.People()[0].ID
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tester := pump(t, r.App, testutil.Wide, "people")
			tapKey(tester, personRowKey(aliceID))
			before := len(r.App.People()[0].Keys)
			tapKey(tester, addKeysKey)
			tester.EnterText(gluttertest.ByKey(keysFieldKey), tt.text)
			tapKey(tester, formActionKey)
			_, err := r.App.AddKeys(ctx, aliceID, tt.text)
			if err == nil {
				t.Fatal("AddKeys accepted the text")
			}
			want := app.Message(err)
			if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(want)); err != nil {
				t.Errorf("the dialog does not show %q: %v", want, err)
			}
			if err := gluttertest.FindsOneWidget(tester, gluttertest.ByKey(formActionKey)); err != nil {
				t.Errorf("the dialog closed after an error: %v", err)
			}
			if after := len(r.App.People()[0].Keys); after != before {
				t.Errorf("the person has %d keys, had %d", after, before)
			}
		})
	}
}

func TestAddAKeyConnectWithItAndRemoveIt(t *testing.T) {
	r := apptest.Open(t, nil)
	p := r.Keyless("carol")
	key := sshtest.NewKey(t, sshtest.Ed25519)
	tester := pump(t, r.App, testutil.Wide, "people")
	sync := func(what string, cond func() bool) {
		t.Helper()
		eventually(t, tester, what, func() bool {
			r.Clock.Advance(app.CoalesceTime)
			return cond()
		})
	}
	sync("carol is in the list", func() bool { return tester.Exists(gluttertest.ByKey(personRowKey(p.ID))) })
	tapKey(tester, personRowKey(p.ID))
	tapKey(tester, addKeysKey)
	tester.EnterText(gluttertest.ByKey(keysFieldKey), key.AuthorizedKey+" laptop")
	tapKey(tester, formActionKey)
	sync("the key is on the page", func() bool {
		return len(r.App.People()[0].Keys) == 1 && tester.Exists(gluttertest.ByText("laptop"))
	})
	client := r.Connect("carol", key)
	keyID := r.App.People()[0].Keys[0].ID
	sync("the connection is on the page", func() bool { return tester.Exists(gluttertest.ByText("Enabled · 1 connection")) })
	tapKey(tester, removeKeyButtonKey(keyID))
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByTextContaining("This closes 1 connection.")); err != nil {
		t.Fatal(err)
	}
	tapKey(tester, confirmYesKey)
	sync("the key is removed", func() bool { return len(r.App.People()[0].Keys) == 0 })
	closedClient(t, client)
	sync("the page says there is no key", func() bool {
		return tester.Exists(gluttertest.ByText("No key yet, so carol cannot connect."))
	})
}
