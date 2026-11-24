package gui

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"github.com/rm4n0s/cave/internal/gui/testutil"
)

func portOutOfRange() error {
	return errors.New("SettingInvalid", "the value is not acceptable", "key", "server.port", "reason", "it must be between 1 and 65535")
}

// pumpSettings pumps the Settings page with a version and the folders to show.
func pumpSettings(t *testing.T, a App, size ui.Size) *gluttertest.Tester {
	t.Helper()
	opts := Options{Page: "settings", Version: "0.7.0", ConfigDir: "/home/ada/.config/cave", StateDir: "/home/ada/.local/state/cave"}
	if f, ok := a.(*apptest.Fake); ok {
		opts.Clock = f.Clock
	}
	return pumpOptions(t, a, size, opts, syncRunner)
}

// reveal scrolls the page until the widget with the key is on the screen, so that a tap reaches it.
func reveal(tester *gluttertest.Tester, key foundation.Key) {
	m.ScrollableEnsureVisible(tester.Element(gluttertest.ByKey(key)))
	tester.PumpAndSettle()
}

func scrollToEnd(tester *gluttertest.Tester) {
	tester.Scroll(gluttertest.ByType[*m.SingleChildScrollView](), ui.Offset{Dy: 5000})
	tester.PumpAndSettle()
}

func TestGoldenSettings(t *testing.T) {
	t.Run("top light wide", func(t *testing.T) {
		pumpSettings(t, light(apptest.Sample(t), t), testutil.Wide).MatchesGolden("settings-top-light-wide")
	})
	t.Run("end light wide", func(t *testing.T) {
		tester := pumpSettings(t, light(apptest.Sample(t), t), testutil.Wide)
		scrollToEnd(tester)
		tester.MatchesGolden("settings-end-light-wide")
	})
	t.Run("top dark narrow", func(t *testing.T) {
		f := apptest.Sample(t)
		f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.theme": "dark"})
		pumpSettings(t, f, testutil.Narrow).MatchesGolden("settings-top-dark-narrow")
	})
	t.Run("a port out of range light wide", func(t *testing.T) {
		f := light(apptest.Sample(t), t)
		f.Fail = map[string]error{"Set": portOutOfRange()}
		tester := pumpSettings(t, f, testutil.Wide)
		tester.EnterText(gluttertest.ByKey(settingKey("server.port")), "70000")
		tester.SendKeyEvent(services.LogicalKeyboardKeyEnter)
		tester.PumpAndSettle()
		tester.MatchesGolden("settings-port-error-light-wide")
	})
}

func TestEverySettingIsOnThePage(t *testing.T) {
	tester := pumpSettings(t, apptest.Sample(t), testutil.Wide)
	for _, def := range settings.Schema() {
		var label string
		for _, text := range app.SettingTexts() {
			if text.Key == def.Key {
				label = text.Label
			}
		}
		if label == "" {
			t.Errorf("%s has no text", def.Key)
			continue
		}
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(label)); err != nil {
			t.Errorf("%s: %v", def.Key, err)
		}
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByKey(settingKey(def.Key))); err != nil {
			t.Errorf("%s: no control: %v", def.Key, err)
		}
	}
	for _, group := range []string{"Server", "Attempts list", "Sessions", "File transfer and forwarding", "Notifications", "Appearance", "Host key", "About"} {
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(group)); err != nil {
			t.Errorf("the group %s: %v", group, err)
		}
	}
}

func TestSettingsAboutAndHostKey(t *testing.T) {
	f := apptest.Sample(t)
	tester := pumpSettings(t, f, testutil.Wide)
	for key, want := range map[string]string{
		"settings-fingerprint": f.ServerSnapshot.HostKeyFingerprint,
		"settings-config-dir":  "/home/ada/.config/cave",
		"settings-state-dir":   "/home/ada/.local/state/cave",
		"settings-version":     "0.7.0",
	} {
		w := gluttertest.WidgetAs[*m.Text](tester, gluttertest.ByKey(foundation.NewKey(key)))
		if w.PlainText() != want {
			t.Errorf("%s says %q, want %q", key, w.PlainText(), want)
		}
	}
	// The copy buttons copy exactly what is shown.
	for key, want := range map[foundation.Key]string{
		copyHostKeyKey:   f.ServerSnapshot.HostKeyFingerprint,
		copyConfigDirKey: "/home/ada/.config/cave",
		copyStateDirKey:  "/home/ada/.local/state/cave",
	} {
		tester.ClearClipboard()
		reveal(tester, key)
		tester.Tap(gluttertest.ByKey(key))
		tester.Pump()
		if got, _ := tester.ClipboardText(); got != want {
			t.Errorf("the clipboard has %q, want %q", got, want)
		}
		tester.PumpAndSettle()
	}
}

func TestChangingEachKindOfSetting(t *testing.T) {
	t.Run("a switch", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pumpSettings(t, f, testutil.Wide)
		reveal(tester, settingKey("sftp.enabled"))
		tester.Tap(gluttertest.ByKey(settingKey("sftp.enabled")))
		tester.PumpAndSettle()
		if got := f.Calls(); len(got) != 1 || got[0] != "Set(sftp.enabled, false)" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("a choice", func(t *testing.T) {
		f := apptest.Sample(t) // attempts.clear is weekly
		tester := pumpSettings(t, f, testutil.Wide)
		reveal(tester, settingKey("attempts.clear"))
		tester.Tap(gluttertest.ByKey(settingKey("attempts.clear")))
		tester.PumpAndSettle()
		tester.Tap(last{gluttertest.ByText("Every 30 days")})
		tester.PumpAndSettle()
		if got := f.Calls(); len(got) != 1 || got[0] != "Set(attempts.clear, monthly)" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("a number typed and Enter", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pumpSettings(t, f, testutil.Wide)
		tester.EnterText(gluttertest.ByKey(settingKey("server.port")), "2200")
		tester.SendKeyEvent(services.LogicalKeyboardKeyEnter)
		tester.PumpAndSettle()
		if got := f.Calls(); len(got) != 1 || got[0] != "Set(server.port, 2200)" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("a text", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pumpSettings(t, f, testutil.Wide)
		tester.EnterText(gluttertest.ByKey(settingKey("server.address")), "127.0.0.1")
		tester.SendKeyEvent(services.LogicalKeyboardKeyEnter)
		tester.PumpAndSettle()
		if got := f.Calls(); len(got) != 1 || got[0] != "Set(server.address, 127.0.0.1)" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("a list", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pumpSettings(t, f, testutil.Wide)
		tester.EnterText(gluttertest.ByKey(settingKey("session.acceptEnv")), "LANG, LC_*, ,TERM,")
		tester.SendKeyEvent(services.LogicalKeyboardKeyEnter)
		tester.PumpAndSettle()
		if got := f.Calls(); len(got) != 1 || got[0] != "Set(session.acceptEnv, [LANG LC_* TERM])" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("a list emptied", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pumpSettings(t, f, testutil.Wide)
		tester.EnterText(gluttertest.ByKey(settingKey("session.acceptEnv")), "")
		tester.SendKeyEvent(services.LogicalKeyboardKeyEnter)
		tester.PumpAndSettle()
		if got := f.Calls(); len(got) != 1 || got[0] != "Set(session.acceptEnv, [])" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("leaving the field sends it", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pumpSettings(t, f, testutil.Wide)
		tester.EnterText(gluttertest.ByKey(settingKey("server.port")), "2300")
		reveal(tester, settingKey("server.keepAliveSeconds"))
		tester.Tap(gluttertest.ByKey(settingKey("server.keepAliveSeconds")))
		tester.PumpAndSettle()
		if got := f.Calls(); len(got) != 1 || got[0] != "Set(server.port, 2300)" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("a text that did not change sends nothing", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pumpSettings(t, f, testutil.Wide)
		tester.EnterText(gluttertest.ByKey(settingKey("server.port")), "2222")
		tester.SendKeyEvent(services.LogicalKeyboardKeyEnter)
		tester.PumpAndSettle()
		if got := f.Calls(); len(got) != 0 {
			t.Errorf("calls = %q", got)
		}
	})
}

func TestANumberThatIsNotOne(t *testing.T) {
	f := apptest.Sample(t)
	tester := pumpSettings(t, f, testutil.Wide)
	tester.EnterText(gluttertest.ByKey(settingKey("server.port")), "abc")
	tester.SendKeyEvent(services.LogicalKeyboardKeyEnter)
	tester.PumpAndSettle()
	if got := f.Calls(); len(got) != 0 {
		t.Errorf("calls = %q, want none", got)
	}
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Type a number.")); err != nil {
		t.Error(err)
	}
}

func TestEscapePutsTheOldValueBack(t *testing.T) {
	f := apptest.Sample(t)
	tester := pumpSettings(t, f, testutil.Wide)
	field := func() *m.TextField {
		return gluttertest.WidgetAs[*m.TextField](tester, gluttertest.ByKey(settingKey("server.port")))
	}
	tester.EnterText(gluttertest.ByKey(settingKey("server.port")), "abc")
	tester.SendKeyEvent(services.LogicalKeyboardKeyEnter)
	tester.PumpAndSettle()
	tester.EnterText(gluttertest.ByKey(settingKey("server.port")), "999")
	tester.SendKeyEvent(services.LogicalKeyboardKeyEscape)
	tester.PumpAndSettle()
	if got := field().Controller().Text(); got != "2222" {
		t.Errorf("after Escape the field has %q, want 2222", got)
	}
	if got := f.Calls(); len(got) != 0 {
		t.Errorf("calls = %q, want none", got)
	}
	if err := gluttertest.FindsNothing(tester, gluttertest.ByText("Type a number.")); err != nil {
		t.Errorf("the sentence is still there: %v", err)
	}
}

func TestAnErrorOfSetStaysUnderTheField(t *testing.T) {
	f := apptest.Sample(t)
	f.Fail = map[string]error{"Set": portOutOfRange()}
	tester := pumpSettings(t, f, testutil.Wide)
	tester.EnterText(gluttertest.ByKey(settingKey("server.port")), "70000")
	tester.SendKeyEvent(services.LogicalKeyboardKeyEnter)
	tester.PumpAndSettle()
	want := "The value is not acceptable for server.port: it must be between 1 and 65535."
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(want)); err != nil {
		t.Error(err)
	}
	// The field keeps what the user typed.
	if got := gluttertest.WidgetAs[*m.TextField](tester, gluttertest.ByKey(settingKey("server.port"))).Controller().Text(); got != "70000" {
		t.Errorf("the field has %q, want 70000", got)
	}
	// A refused switch says so under it, and a good value clears the sentence.
	f.Fail = map[string]error{"Set": errors.New("SettingInvalid", "x", "key", "sftp.enabled", "reason", "no")}
	reveal(tester, settingKey("sftp.enabled"))
	tester.Tap(gluttertest.ByKey(settingKey("sftp.enabled")))
	tester.PumpAndSettle()
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByKey(foundation.NewKey("error-sftp.enabled"))); err != nil {
		t.Error(err)
	}
}

func TestNewHostKey(t *testing.T) {
	f := apptest.Sample(t)
	tester := pumpSettings(t, f, testutil.Wide)
	scrollToEnd(tester)
	tapKey(tester, newHostKeyKey)
	want := "Make a new host key? Every person will see a warning that the host key changed, the next time they connect."
	if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(want)); err != nil {
		t.Fatal(err)
	}
	tapKey(tester, confirmCancelKey)
	eventually(t, tester, "the question goes", func() bool { return !tester.Exists(gluttertest.ByKey(confirmYesKey)) })
	if got := f.Calls(); len(got) != 0 {
		t.Fatalf("Cancel made the calls %q", got)
	}
	tapKey(tester, newHostKeyKey)
	tapKey(tester, confirmYesKey)
	eventually(t, tester, "the call", func() bool { return len(f.Calls()) == 1 })
	if got := f.Calls()[0]; got != "NewHostKey()" {
		t.Errorf("calls = %q", f.Calls())
	}
}

func TestChangeFromAttemptsScrollsToTheGroup(t *testing.T) {
	tester := pumpOptions(t, apptest.Sample(t), testutil.Narrow, Options{Page: "attempts", Version: "0.7.0"}, syncRunner)
	tapKey(tester, changeClearKey)
	eventually(t, tester, "the page is Settings", func() bool { return currentPage(t, tester) == pageSettings })
	card := gluttertest.ByKey(foundation.NewKey("group-Attempts list"))
	eventually(t, tester, "the group is in view", func() bool {
		top, height := tester.TopLeft(card).Dy, tester.Size(card).Height
		return top >= 0 && top+height <= testutil.Narrow.Height
	})
	// Choosing Settings by hand does not scroll to a group.
	tester = pumpSettings(t, apptest.Sample(t), testutil.Narrow)
	if top := tester.TopLeft(gluttertest.ByKey(foundation.NewKey("group-Server"))).Dy; top < 0 {
		t.Errorf("the page starts scrolled: the first group is at %v", top)
	}
}

// ---- with the real application

func TestSettingsWithTheRealApplication(t *testing.T) {
	t.Run("the theme", func(t *testing.T) {
		r := apptest.Open(t, map[string]any{"ui.theme": "light"})
		tester := pumpOptions(t, r.App, testutil.Wide, Options{Page: "settings", Clock: r.Clock}, syncRunner)
		brightness := func() ui.Brightness {
			return m.ThemeOf(tester.Element(gluttertest.ByType[*shell]())).Brightness()
		}
		if got := brightness(); got != ui.BrightnessLight {
			t.Fatalf("the theme starts as %v", got)
		}
		reveal(tester, settingKey("ui.theme"))
		tester.Tap(gluttertest.ByKey(settingKey("ui.theme")))
		tester.PumpAndSettle()
		tester.Tap(last{gluttertest.ByText("Dark")})
		tester.PumpAndSettle()
		eventually(t, tester, "the theme turns dark", func() bool {
			r.Clock.Advance(app.CoalesceTime)
			return brightness() == ui.BrightnessDark
		})
		if got := r.App.Settings().String(settings.UITheme); got != settings.ThemeDark {
			t.Errorf("ui.theme is %q", got)
		}
	})
	t.Run("a free port and a client", func(t *testing.T) {
		r := apptest.Open(t, nil)
		key := r.Person("alice")
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
		tester := pumpOptions(t, r.App, testutil.Wide, Options{Page: "settings", Clock: r.Clock}, syncRunner)
		tester.EnterText(gluttertest.ByKey(settingKey("server.port")), strconv.Itoa(port))
		tester.SendKeyEvent(services.LogicalKeyboardKeyEnter)
		tester.PumpAndSettle()
		r.Eventually("the server listens on the new port", func() bool { return r.App.Server().Port == port })
		client := sshtest.Dial(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), "alice", key, r.HostKey())
		if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
			t.Errorf("a client cannot use the new port: %v", err)
		}
	})
	t.Run("a port in use", func(t *testing.T) {
		r := apptest.Open(t, nil)
		held, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = held.Close() }()
		port := held.Addr().(*net.TCPAddr).Port
		tester := pumpOptions(t, r.App, testutil.Wide, Options{Page: "settings", Clock: r.Clock}, syncRunner)
		tester.EnterText(gluttertest.ByKey(settingKey("server.port")), strconv.Itoa(port))
		tester.SendKeyEvent(services.LogicalKeyboardKeyEnter)
		tester.PumpAndSettle()
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByTextContaining("Cave cannot listen on 127.0.0.1:"+strconv.Itoa(port))); err != nil {
			t.Error(err)
		}
		if got := gluttertest.WidgetAs[*m.TextField](tester, gluttertest.ByKey(settingKey("server.port"))).Controller().Text(); got != strconv.Itoa(port) {
			t.Errorf("the field has %q", got)
		}
	})
	t.Run("a new host key", func(t *testing.T) {
		r := apptest.Open(t, nil)
		tester := pumpOptions(t, r.App, testutil.Wide, Options{Page: "settings", Clock: r.Clock}, syncRunner)
		before := r.App.Server().HostKeyFingerprint
		scrollToEnd(tester)
		tapKey(tester, newHostKeyKey)
		tapKey(tester, confirmYesKey)
		eventually(t, tester, "the fingerprint changes", func() bool {
			r.Clock.Advance(app.CoalesceTime)
			after := r.App.Server().HostKeyFingerprint
			return after != before && strings.Contains(textOf(tester, "settings-fingerprint"), after)
		})
	})
}

// textOf is the text of the widget with the key.
func textOf(tester *gluttertest.Tester, key string) string {
	return gluttertest.WidgetAs[*m.Text](tester, gluttertest.ByKey(foundation.NewKey(key))).PlainText()
}
