package gui

import (
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/services"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"github.com/rm4n0s/cave/internal/gui/testutil"
)

// fieldName and the others are the fields of the dialogs.
var (
	fieldNameKey = foundation.NewKey("field-name")
	fieldKeysKey = foundation.NewKey("field-keys")
)

func nameTaken() error {
	return errors.New("PersonNameTaken", "that name is taken", "takenBy", "alice")
}

func keyNotParsed() error {
	return errors.New("KeyParseFailed", "not a key", "line", 1)
}

func TestGoldenPeople(t *testing.T) {
	light := func(f *apptest.Fake, t *testing.T) *apptest.Fake {
		f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.theme": "light"})
		return f
	}
	t.Run("sample light wide", func(t *testing.T) {
		pump(t, light(apptest.Sample(t), t), testutil.Wide, "people").MatchesGolden("people-light-wide")
	})
	t.Run("selected light wide", func(t *testing.T) {
		tester := pump(t, light(apptest.Sample(t), t), testutil.Wide, "people")
		tapKey(tester, personRowKey(apptest.AliceID))
		tester.MatchesGolden("people-selected-light-wide")
	})
	t.Run("sample dark narrow", func(t *testing.T) {
		f := apptest.Sample(t)
		f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.theme": "dark"})
		pump(t, f, testutil.Narrow, "people").MatchesGolden("people-dark-narrow")
	})
	t.Run("nobody light wide", func(t *testing.T) {
		pump(t, light(apptest.Empty(t), t), testutil.Wide, "people").MatchesGolden("people-empty-light-wide")
	})
	t.Run("add dialog with an error light wide", func(t *testing.T) {
		f := light(apptest.Sample(t), t)
		f.Fail = map[string]error{"AddPerson": nameTaken()}
		tester := pump(t, f, testutil.Wide, "people")
		tapKey(tester, addPersonButtonKey)
		tester.EnterText(gluttertest.ByKey(fieldNameKey), "alice")
		tapKey(tester, formActionKey)
		tester.MatchesGolden("people-add-error-light-wide")
	})
	t.Run("add dialog with keys that failed light wide", func(t *testing.T) {
		f := light(apptest.Sample(t), t)
		f.Fail = map[string]error{"AddPerson": keyNotParsed()}
		tester := pump(t, f, testutil.Wide, "people")
		tapKey(tester, addPersonButtonKey)
		tester.EnterText(gluttertest.ByKey(fieldNameKey), "dave")
		tester.EnterText(gluttertest.ByKey(fieldKeysKey), "not a key")
		tapKey(tester, formActionKey)
		tester.MatchesGolden("people-add-keys-error-light-wide")
	})
	t.Run("remove question light wide", func(t *testing.T) {
		tester := pump(t, light(apptest.Sample(t), t), testutil.Wide, "people")
		tapKey(tester, personRowKey(apptest.AliceID))
		tapKey(tester, removePersonKey)
		tester.MatchesGolden("people-remove-light-wide")
	})
}

func TestTheRows(t *testing.T) {
	tester := pump(t, apptest.Sample(t), testutil.Wide, "people")
	for _, want := range []string{"alice", "bob", "carol", "2 keys", "1 key", "No key"} {
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(want)); err != nil {
			t.Error(err)
		}
	}
	// Only alice is connected.
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByKey(connectedMarkKey(apptest.AliceID))); err != nil {
		t.Error(err)
	}
	for _, id := range []string{apptest.BobID, apptest.CarolID} {
		if err := gluttertest.FindsNothing(tester, gluttertest.ByKey(connectedMarkKey(id))); err != nil {
			t.Error(err)
		}
	}
	// The switches say who is enabled.
	for id, want := range map[string]bool{apptest.AliceID: true, apptest.BobID: false, apptest.CarolID: true} {
		sw := gluttertest.WidgetAs[*m.Switch](tester, gluttertest.ByKey(personSwitchKey(id)))
		if sw.Value() != want {
			t.Errorf("the switch of %s is %v, want %v", id, sw.Value(), want)
		}
	}
}

func TestAddAPerson(t *testing.T) {
	t.Run("a name and keys", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pump(t, f, testutil.Wide, "people")
		tapKey(tester, addPersonButtonKey)
		tester.EnterText(gluttertest.ByKey(fieldNameKey), "dave")
		tester.EnterText(gluttertest.ByKey(fieldKeysKey), "ssh-ed25519 AAAA dave@laptop")
		tapKey(tester, formActionKey)
		want := []string{`AddPerson(dave, "ssh-ed25519 AAAA dave@laptop")`}
		if got := f.Calls(); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("calls = %q, want %q", got, want)
		}
		if err := gluttertest.FindsNothing(tester, gluttertest.ByKey(formActionKey)); err != nil {
			t.Errorf("the dialog is still open: %v", err)
		}
	})
	t.Run("the keys are not optional", func(t *testing.T) {
		tester := pump(t, apptest.Sample(t), testutil.Wide, "people")
		tapKey(tester, addPersonButtonKey)
		if err := gluttertest.FindsNothing(tester, gluttertest.ByTextContaining("optional")); err != nil {
			t.Errorf("the dialog says something is optional: %v", err)
		}
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Public keys")); err != nil {
			t.Error(err)
		}
	})
	t.Run("a name that is taken keeps the dialog", func(t *testing.T) {
		f := apptest.Sample(t)
		f.Fail = map[string]error{"AddPerson": nameTaken()}
		tester := pump(t, f, testutil.Wide, "people")
		tapKey(tester, addPersonButtonKey)
		tester.EnterText(gluttertest.ByKey(fieldNameKey), "alice")
		tester.EnterText(gluttertest.ByKey(fieldKeysKey), "ssh-ed25519 AAAA alice@laptop")
		tapKey(tester, formActionKey)
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(app.Message(nameTaken()))); err != nil {
			t.Errorf("the sentence under the field: %v", err)
		}
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByKey(formActionKey)); err != nil {
			t.Errorf("the dialog closed: %v", err)
		}
	})
	t.Run("keys that fail keep the dialog, and nobody is added", func(t *testing.T) {
		f := apptest.Sample(t)
		f.Fail = map[string]error{"AddPerson": keyNotParsed()}
		tester := pump(t, f, testutil.Wide, "people")
		tapKey(tester, addPersonButtonKey)
		tester.EnterText(gluttertest.ByKey(fieldNameKey), "dave")
		tester.EnterText(gluttertest.ByKey(fieldKeysKey), "not a key")
		tapKey(tester, formActionKey)
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(app.Message(keyNotParsed()))); err != nil {
			t.Error(err)
		}
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByKey(formActionKey)); err != nil {
			t.Errorf("the dialog closed: %v", err)
		}
		// Corrected and sent again, the person is added with the keys, in one call.
		delete(f.Fail, "AddPerson")
		tester.EnterText(gluttertest.ByKey(fieldKeysKey), "ssh-ed25519 AAAA dave@laptop")
		tapKey(tester, formActionKey)
		want := []string{`AddPerson(dave, "not a key")`, `AddPerson(dave, "ssh-ed25519 AAAA dave@laptop")`}
		if got := f.Calls(); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("calls = %q, want %q", got, want)
		}
		if err := gluttertest.FindsNothing(tester, gluttertest.ByKey(formActionKey)); err != nil {
			t.Errorf("the dialog is still open: %v", err)
		}
	})
	t.Run("no keys is refused by the application, under the keys", func(t *testing.T) {
		r := apptest.Open(t, nil)
		tester := pump(t, r.App, testutil.Wide, "people")
		tapKey(tester, addPersonButtonKey)
		tester.EnterText(gluttertest.ByKey(fieldNameKey), "dave")
		tapKey(tester, formActionKey)
		eventually(t, tester, "the sentence of the empty keys", func() bool {
			return tester.Exists(gluttertest.ByTextContaining("There is no public key in the text."))
		})
		if len(r.App.People()) != 0 {
			t.Errorf("dave was added without a key: %+v", r.App.People())
		}
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByKey(formActionKey)); err != nil {
			t.Errorf("the dialog closed: %v", err)
		}
	})
	t.Run("cancel makes no call", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pump(t, f, testutil.Wide, "people")
		tapKey(tester, addPersonButtonKey)
		tester.EnterText(gluttertest.ByKey(fieldNameKey), "dave")
		tapKey(tester, formCancelKey)
		if got := f.Calls(); len(got) != 0 {
			t.Errorf("calls = %q", got)
		}
		if err := gluttertest.FindsNothing(tester, gluttertest.ByKey(formActionKey)); err != nil {
			t.Errorf("the dialog is still open: %v", err)
		}
	})
	t.Run("nobody yet", func(t *testing.T) {
		f := apptest.Empty(t)
		tester := pump(t, f, testutil.Wide, "people")
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Nobody yet.")); err != nil {
			t.Fatal(err)
		}
		tapKey(tester, addPersonButtonKey)
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByKey(formActionKey)); err != nil {
			t.Errorf("the button did not open the dialog: %v", err)
		}
	})
}

func TestTheSwitch(t *testing.T) {
	t.Run("a person who is not connected is disabled at once", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pump(t, f, testutil.Wide, "people")
		tapKey(tester, personSwitchKey(apptest.CarolID))
		if got := f.Calls(); len(got) != 1 || got[0] != "SetPersonEnabled("+apptest.CarolID+", false)" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("a disabled person is enabled at once", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pump(t, f, testutil.Wide, "people")
		tapKey(tester, personSwitchKey(apptest.BobID))
		if got := f.Calls(); len(got) != 1 || got[0] != "SetPersonEnabled("+apptest.BobID+", true)" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("a person who is connected is asked about", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pump(t, f, testutil.Wide, "people")
		tapKey(tester, personSwitchKey(apptest.AliceID))
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("alice is connected. Disable and disconnect?")); err != nil {
			t.Fatal(err)
		}
		if got := f.Calls(); len(got) != 0 {
			t.Fatalf("calls = %q before the answer", got)
		}
		tapKey(tester, confirmCancelKey)
		eventually(t, tester, "the dialog closes", func() bool { return !tester.Exists(gluttertest.ByKey(confirmYesKey)) })
		if got := f.Calls(); len(got) != 0 {
			t.Errorf("Cancel made the calls %q", got)
		}
		tapKey(tester, personSwitchKey(apptest.AliceID))
		tapKey(tester, confirmYesKey)
		eventually(t, tester, "the call", func() bool { return len(f.Calls()) == 1 })
		if got := f.Calls(); got[0] != "SetPersonEnabled("+apptest.AliceID+", false)" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("a failure shows its sentence", func(t *testing.T) {
		f := apptest.Sample(t)
		f.Fail = map[string]error{"SetPersonEnabled": errors.New("PersonNotFound", "gone")}
		tester := pump(t, f, testutil.Wide, "people")
		tapKey(tester, personSwitchKey(apptest.CarolID))
		tester.Pump()
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(app.Message(f.Fail["SetPersonEnabled"]))); err != nil {
			t.Error(err)
		}
	})
}

func TestRenameAndRemove(t *testing.T) {
	t.Run("rename", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pump(t, f, testutil.Wide, "people")
		tapKey(tester, personRowKey(apptest.CarolID))
		tapKey(tester, renamePersonKey)
		tester.EnterText(gluttertest.ByKey(fieldNameKey), "caroline")
		tapKey(tester, formActionKey)
		if got := f.Calls(); len(got) != 1 || got[0] != "RenamePerson("+apptest.CarolID+", caroline)" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("rename starts with the current name and shows an error under the field", func(t *testing.T) {
		f := apptest.Sample(t)
		f.Fail = map[string]error{"RenamePerson": nameTaken()}
		tester := pump(t, f, testutil.Wide, "people")
		tapKey(tester, personRowKey(apptest.CarolID))
		tapKey(tester, renamePersonKey)
		field := gluttertest.WidgetAs[*m.TextField](tester, gluttertest.ByKey(fieldNameKey))
		if got := field.Controller().Text(); got != "carol" {
			t.Errorf("the field starts with %q, want carol", got)
		}
		tapKey(tester, formActionKey)
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(app.Message(nameTaken()))); err != nil {
			t.Error(err)
		}
	})
	t.Run("remove asks, and says what it closes", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pump(t, f, testutil.Wide, "people")
		tapKey(tester, personRowKey(apptest.AliceID))
		tapKey(tester, removePersonKey)
		want := "Remove alice and their 2 keys? This closes 1 connection."
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(want)); err != nil {
			t.Fatal(err)
		}
		tapKey(tester, confirmCancelKey)
		eventually(t, tester, "the dialog closes", func() bool { return !tester.Exists(gluttertest.ByKey(confirmYesKey)) })
		if got := f.Calls(); len(got) != 0 {
			t.Fatalf("Cancel made the calls %q", got)
		}
		tapKey(tester, removePersonKey)
		tapKey(tester, confirmYesKey)
		eventually(t, tester, "the call", func() bool { return len(f.Calls()) == 1 })
		if got := f.Calls(); got[0] != "RemovePerson("+apptest.AliceID+")" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("the question for a person with no key and no connection", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pump(t, f, testutil.Wide, "people")
		tapKey(tester, personRowKey(apptest.CarolID))
		tapKey(tester, removePersonKey)
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Remove carol?")); err != nil {
			t.Error(err)
		}
	})
	t.Run("Enter does not choose Remove", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pump(t, f, testutil.Wide, "people")
		tapKey(tester, personRowKey(apptest.CarolID))
		tapKey(tester, removePersonKey)
		tester.SendKeyEvent(services.LogicalKeyboardKeyEnter)
		tester.PumpAndSettle()
		if got := f.Calls(); len(got) != 0 {
			t.Errorf("Enter made the calls %q", got)
		}
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByKey(confirmYesKey)); err != nil {
			t.Errorf("the question went away: %v", err)
		}
	})
}

func TestTheSelectionFollowsThePerson(t *testing.T) {
	f := apptest.Sample(t)
	tester := pump(t, f, testutil.Wide, "people")
	tapKey(tester, personRowKey(apptest.BobID))
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByKey(foundation.NewKey("pane-name"))); err != nil {
		t.Fatal(err)
	}
	paneName := func() string {
		return gluttertest.WidgetAs[*m.Text](tester, gluttertest.ByKey(foundation.NewKey("pane-name"))).PlainText()
	}
	if paneName() != "bob" {
		t.Fatalf("the pane shows %q", paneName())
	}
	// A person comes in front of bob: bob is still the one shown.
	f.PeopleList = append(f.PeopleList[:1:1], append([]people.Person{{ID: "p_00000000000000d1", Name: "dave", Enabled: true}}, f.PeopleList[1:]...)...)
	f.Emit(app.Changed{Topics: app.TopicPeople})
	eventually(t, tester, "the new person is in the list", func() bool {
		return tester.Exists(gluttertest.ByKey(personRowKey("p_00000000000000d1")))
	})
	if paneName() != "bob" {
		t.Errorf("after a person came, the pane shows %q, want bob", paneName())
	}
	// Bob is removed: nobody is selected.
	f.PeopleList = []people.Person{f.PeopleList[0], f.PeopleList[1], f.PeopleList[3]}
	f.Emit(app.Changed{Topics: app.TopicPeople})
	eventually(t, tester, "the pane goes", func() bool {
		return !tester.Exists(gluttertest.ByKey(foundation.NewKey("pane-name")))
	})
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Select a person.")); err != nil {
		t.Error(err)
	}
}

func TestNarrowPushesThePerson(t *testing.T) {
	tester := pump(t, apptest.Sample(t), testutil.Narrow, "people")
	if err := gluttertest.FindsNothing(tester, gluttertest.ByKey(renamePersonKey)); err != nil {
		t.Fatalf("the list shows a person's pane: %v", err)
	}
	tapKey(tester, personRowKey(apptest.AliceID))
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByKey(renamePersonKey)); err != nil {
		t.Fatalf("the person's pane is not shown: %v", err)
	}
	if err := gluttertest.FindsNothing(tester, gluttertest.ByKey(addPersonButtonKey)); err != nil {
		t.Errorf("the list is still on screen: %v", err)
	}
	tester.Tap(gluttertest.ByTooltip("Back"))
	tester.PumpAndSettle()
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByKey(addPersonButtonKey)); err != nil {
		t.Errorf("back does not show the list: %v", err)
	}
	if err := gluttertest.FindsNothing(tester, gluttertest.ByKey(renamePersonKey)); err != nil {
		t.Errorf("the pane is still there: %v", err)
	}
}

// ---- with the real application

func TestPeopleWithTheRealApplication(t *testing.T) {
	r := apptest.Open(t, nil)
	tester := pump(t, r.App, testutil.Wide, "people")
	sync := func(what string, cond func() bool) {
		t.Helper()
		eventually(t, tester, what, func() bool {
			r.Clock.Advance(app.CoalesceTime)
			return cond()
		})
	}

	// Add dave with a pasted key.
	key := sshtest.NewKey(t, sshtest.Ed25519)
	tapKey(tester, addPersonButtonKey)
	tester.EnterText(gluttertest.ByKey(fieldNameKey), "dave")
	tester.EnterText(gluttertest.ByKey(fieldKeysKey), key.AuthorizedKey)
	tapKey(tester, formActionKey)
	sync("dave is in the list with one key", func() bool {
		list := r.App.People()
		return len(list) == 1 && list[0].Name == "dave" && len(list[0].Keys) == 1 && tester.Exists(gluttertest.ByText("1 key"))
	})

	// A name that is not valid: the sentence of the real error under the field.
	tapKey(tester, addPersonButtonKey)
	tester.EnterText(gluttertest.ByKey(fieldNameKey), "-bad")
	tapKey(tester, formActionKey)
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByTextContaining("A name has 1 to 32 letters")); err != nil {
		t.Error(err)
	}
	tapKey(tester, formCancelKey)

	// dave connects: Remove says what it closes, and after it the client is gone.
	client := r.Connect("dave", key)
	sync("the list shows dave connected", func() bool {
		return tester.Exists(gluttertest.ByKey(connectedMarkKey(r.App.People()[0].ID)))
	})
	tapKey(tester, personRowKey(r.App.People()[0].ID))
	tapKey(tester, removePersonKey)
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Remove dave and their 1 key? This closes 1 connection.")); err != nil {
		t.Fatal(err)
	}
	tapKey(tester, confirmYesKey)
	sync("dave is removed", func() bool { return len(r.App.People()) == 0 })
	closedClient(t, client)
	sync("the page says Nobody yet.", func() bool { return tester.Exists(gluttertest.ByText("Nobody yet.")) })
}
