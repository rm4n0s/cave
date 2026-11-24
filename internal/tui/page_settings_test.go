package tui

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"github.com/rm4n0s/errors"
)

// selectSetting moves the selection of the Settings page to the setting.
func selectSetting(t *testing.T, h *harness, key string) {
	t.Helper()
	h.key("5")
	h.key("home")
	for i := 0; i < 40; i++ {
		if item, ok := h.m().selectedSetting(); ok && item.kind == itemSetting && item.def.Key == key {
			return
		}
		h.key("down")
	}
	t.Fatalf("the selection never came to %s", key)
}

func portOutOfRange() error {
	return errors.New("SettingInvalid", "the value is not acceptable", "key", "server.port", "reason", "it must be between 1 and 65535")
}

func TestGoldenSettings(t *testing.T) {
	tests := []struct {
		name          string
		fake          func() *apptest.Fake
		width, height int
		setup         func(t *testing.T, h *harness)
	}{
		{"top-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, func(*testing.T, *harness) {}},
		{"top-60x16", func() *apptest.Fake { return apptest.Sample(t) }, 60, 16, func(*testing.T, *harness) {}},
		{"last-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, func(_ *testing.T, h *harness) { h.key("end") }},
		{"last-60x16", func() *apptest.Fake { return apptest.Sample(t) }, 60, 16, func(_ *testing.T, h *harness) { h.key("end") }},
		{"tall-100x50", func() *apptest.Fake { return apptest.Sample(t) }, 100, 50, func(*testing.T, *harness) {}},
		{"desktop-only-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, func(t *testing.T, h *harness) {
			selectSetting(t, h, "ui.theme")
		}},
		{"agent-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, func(t *testing.T, h *harness) {
			selectSetting(t, h, "forwarding.agent")
		}},
		{"number-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, func(_ *testing.T, h *harness) { h.key("enter") }},
		{"number-error-100x30", func() *apptest.Fake {
			f := apptest.Sample(t)
			f.Fail = map[string]error{"Set": portOutOfRange()}
			return f
		}, 100, 30, func(_ *testing.T, h *harness) {
			h.key("enter")
			h.key("ctrl+u")
			h.typeText("70000")
			h.key("enter")
		}},
		{"not-a-number-60x16", func() *apptest.Fake { return apptest.Sample(t) }, 60, 16, func(_ *testing.T, h *harness) {
			h.key("enter")
			h.key("ctrl+u")
			h.typeText("abc")
			h.key("enter")
		}},
		{"host-key-100x30", func() *apptest.Fake { return apptest.Sample(t) }, 100, 30, func(_ *testing.T, h *harness) {
			h.key("end")
			h.key("enter")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.fake()
			h := newFakeHarness(t, f, tt.width, tt.height)
			h.model = func() Model {
				m := h.m()
				m.opts.ConfigDir, m.opts.StateDir, m.opts.Version = "/home/ada/.config/cave", "/home/ada/.local/state/cave", "0.6.0"
				return m
			}()
			h.key("5")
			tt.setup(t, h)
			h.golden("settings-" + tt.name)
		})
	}
}

func TestEverySettingIsOnThePage(t *testing.T) {
	h := newFakeHarness(t, apptest.Sample(t), 100, 50)
	h.key("5")
	screen := h.screen()
	for _, def := range settings.Schema() {
		var label string
		for _, text := range app.SettingTexts() {
			if text.Key == def.Key {
				label = text.Label
			}
		}
		if label == "" || !strings.Contains(screen, label) {
			t.Errorf("%s: the label %q is not on the page", def.Key, label)
		}
	}
	for _, group := range []string{"Server", "Attempts list", "Sessions", "File transfer and forwarding", "Notifications", "Appearance"} {
		if !strings.Contains(screen, group) {
			t.Errorf("the group %q is not on the page", group)
		}
	}
	// Under the settings: the host key, the folders and the version.
	for _, want := range []string{"Host key", "SHA256:", "Config folder", "State folder", "Version"} {
		if !strings.Contains(screen, want) {
			t.Errorf("%q is not on the page:\n%s", want, screen)
		}
	}
}

func TestSettingValuesAreWordedByKind(t *testing.T) {
	f := apptest.Sample(t)
	h := newFakeHarness(t, f, 100, 50)
	h.key("5")
	lines := strings.Split(h.screen(), "\n")
	row := func(label string) string {
		for _, l := range lines {
			if strings.Contains(l, label) {
				return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), ">"))
			}
		}
		t.Fatalf("no row %q", label)
		return ""
	}
	for label, want := range map[string]string{
		"Port":                          "2222",
		"Address":                       "—",
		"Start when Cave starts":        "on",
		"Keep-alive (seconds)":          "60",
		"Clear the list":                "weekly",
		"Shell":                         "—",
		"Variables clients may set":     "LANG, LC_*",
		"File transfer (SFTP)":          "on",
		"Forwarded ports open to the":   "off",
		"Notify when somebody connects": "on (desktop app)",
		"Theme":                         "system (desktop app)",
	} {
		if got := row(label); !strings.HasSuffix(got, want) {
			t.Errorf("%s: the row is %q, want it to end with %q", label, got, want)
		}
	}
}

func TestChangingEachKindOfSetting(t *testing.T) {
	tests := []struct {
		name string
		key  string
		keys []string
		text string
		then []string
		want string
	}{
		{"a switch is flipped", "sftp.enabled", []string{"enter"}, "", nil, "Set(sftp.enabled, false)"},
		{"a switch with the space", "forwarding.local", []string{"space"}, "", nil, "Set(forwarding.local, false)"},
		{"a switch that is off goes on", "forwarding.gatewayPorts", []string{"enter"}, "", nil, "Set(forwarding.gatewayPorts, true)"},
		{"a choice goes to the next", "attempts.clear", []string{"enter"}, "", nil, "Set(attempts.clear, monthly)"},
		{"a number", "server.port", []string{"enter", "ctrl+u"}, "2200", []string{"enter"}, "Set(server.port, 2200)"},
		{"a number with spaces around it", "server.keepAliveSeconds", []string{"enter", "ctrl+u"}, " 30 ", []string{"enter"}, "Set(server.keepAliveSeconds, 30)"},
		{"a text", "server.address", []string{"enter"}, "127.0.0.1", []string{"enter"}, "Set(server.address, 127.0.0.1)"},
		{"a text that is emptied", "session.shell", []string{"enter"}, "/bin/sh", []string{"ctrl+u", "enter"}, "Set(session.shell, )"},
		{"a list", "session.acceptEnv", []string{"enter", "ctrl+u"}, "LANG, LC_*, ,TERM,", []string{"enter"}, "Set(session.acceptEnv, [LANG LC_* TERM])"},
		{"a list that is emptied", "session.acceptEnv", []string{"enter", "ctrl+u"}, "", []string{"enter"}, "Set(session.acceptEnv, [])"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := apptest.Sample(t)
			h := newFakeHarness(t, f, 100, 30)
			selectSetting(t, h, tt.key)
			h.keys(tt.keys...)
			h.typeText(tt.text)
			h.keys(tt.then...)
			if got := f.Calls(); len(got) != 1 || got[0] != tt.want {
				t.Errorf("calls = %q, want [%s]", got, tt.want)
			}
		})
	}
}

func TestAChoiceGoesAroundTheEnd(t *testing.T) {
	choices := []string{"never", "onStart", "hourly", "daily", "weekly", "monthly"}
	for i, current := range choices {
		g := apptest.Empty(t)
		g.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"attempts.clear": current})
		h := newFakeHarness(t, g, 100, 30)
		selectSetting(t, h, "attempts.clear")
		h.key("enter")
		want := "Set(attempts.clear, " + choices[(i+1)%len(choices)] + ")"
		if got := g.Calls(); len(got) != 1 || got[0] != want {
			t.Errorf("from %s: calls = %q, want [%s]", current, got, want)
		}
	}
}

func TestANumberThatIsNotOne(t *testing.T) {
	f := apptest.Sample(t)
	h := newFakeHarness(t, f, 100, 30)
	selectSetting(t, h, "server.port")
	h.keys("enter", "ctrl+u")
	h.typeText("abc")
	h.key("enter")
	if len(f.Calls()) != 0 || h.m().prompt == nil || !strings.Contains(h.screen(), "Type a number.") {
		t.Fatalf("calls %q, prompt %v:\n%s", f.Calls(), h.m().prompt, h.screen())
	}
	// It can be corrected and sent again.
	h.keys("ctrl+u")
	h.typeText("2300")
	h.key("enter")
	if got := f.Calls(); len(got) != 1 || got[0] != "Set(server.port, 2300)" || h.m().prompt != nil {
		t.Errorf("calls = %q, prompt %v", got, h.m().prompt)
	}
	// An empty field is not a number either.
	h.key("enter")
	h.key("ctrl+u")
	h.key("enter")
	if len(f.Calls()) != 1 || !strings.Contains(h.screen(), "Type a number.") {
		t.Errorf("an empty number: calls %q", f.Calls())
	}
	h.key("esc")
	if h.m().prompt != nil {
		t.Error("esc did not close the field")
	}
}

func TestAnErrorOfSetKeepsTheField(t *testing.T) {
	f := apptest.Sample(t)
	f.Fail = map[string]error{"Set": portOutOfRange()}
	h := newFakeHarness(t, f, 100, 30)
	selectSetting(t, h, "server.port")
	h.keys("enter", "ctrl+u")
	h.typeText("70000")
	h.key("enter")
	if h.m().prompt == nil || h.m().prompt.f.value() != "70000" {
		t.Fatalf("the field did not stay open with its text:\n%s", h.screen())
	}
	if !strings.Contains(h.screen(), "The value is not acceptable for server.port: it must be between 1 and 65535.") {
		t.Errorf("the sentence:\n%s", h.screen())
	}
	h.key("esc")
	if h.m().prompt != nil {
		t.Error("esc did not close the field")
	}
}

func TestDesktopOnlySettingsAreNotChanged(t *testing.T) {
	for _, key := range []string{"notify.accepted", "notify.rejected", "ui.theme", "ui.closeToTray"} {
		f := apptest.Sample(t)
		h := newFakeHarness(t, f, 100, 30)
		selectSetting(t, h, key)
		h.keys("enter", "space")
		if len(f.Calls()) != 0 || h.m().prompt != nil || h.m().ask != nil {
			t.Errorf("%s: calls %q, prompt %v, ask %v", key, f.Calls(), h.m().prompt, h.m().ask)
		}
	}
}

func TestTheHostKey(t *testing.T) {
	f := apptest.Sample(t)
	h := newFakeHarness(t, f, 100, 30)
	h.keys("5", "end")
	if item, _ := h.m().selectedSetting(); item.kind != itemHostKey {
		t.Fatalf("the last row is %v, want the host key", item.kind)
	}
	h.key("enter")
	if !strings.Contains(h.screen(), "Make a new host key? Every person will see a warning that the host key changed") {
		t.Fatalf("the question:\n%s", h.screen())
	}
	h.key("enter") // the default is no
	h.keys("space", "n")
	if len(f.Calls()) != 0 {
		t.Fatalf("calls = %q", f.Calls())
	}
	h.keys("enter", "y")
	if got := f.Calls(); len(got) != 1 || got[0] != "NewHostKey()" {
		t.Errorf("calls = %q", got)
	}
}

func TestOnlyTheRowsThatCanChangeAreSelected(t *testing.T) {
	h := newFakeHarness(t, apptest.Sample(t), 100, 30)
	h.key("5")
	seen := 0
	for i := 0; i < 40; i++ {
		item, ok := h.m().selectedSetting()
		if !ok || (item.kind != itemSetting && item.kind != itemHostKey) {
			t.Fatalf("step %d: a row that cannot change is selected", i)
		}
		seen++
		h.key("down")
	}
	// 16 settings and the host key.
	last, _ := h.m().selectedSetting()
	if last.kind != itemHostKey {
		t.Errorf("the selection ended on %v", last.kind)
	}
	h.key("home")
	first, _ := h.m().selectedSetting()
	if first.text.Key != "server.port" || seen != 40 {
		t.Errorf("the first row is %s", first.text.Key)
	}
}

func TestSettingsScrollsToTheSelection(t *testing.T) {
	h := newFakeHarness(t, apptest.Sample(t), 100, 20)
	h.key("5")
	if !strings.Contains(h.screen(), "Server") || strings.Contains(h.screen(), "Version") {
		t.Errorf("the top:\n%s", h.screen())
	}
	h.key("end")
	screen := h.screen()
	if !strings.Contains(screen, "> Host key") || !strings.Contains(screen, "Version") {
		t.Errorf("the end:\n%s", screen)
	}
	// Moving up one row does not scroll: the window stays while the selection is in it.
	before := h.m().settingsFirst
	h.key("up")
	if h.m().settingsFirst != before {
		t.Errorf("the page scrolled from %d to %d for one row up", before, h.m().settingsFirst)
	}
	h.key("home")
	if h.m().settingsFirst != 0 || !strings.Contains(h.screen(), "Server") {
		t.Errorf("home does not show the first group's title:\n%s", h.screen())
	}
}

func TestSettingsFitEverySize(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {80, 24}, {100, 30}, {137, 50}} {
		h := newFakeHarness(t, apptest.Sample(t), size[0], size[1])
		h.key("5")
		for i := 0; i < 20; i++ {
			lines := strings.Split(h.screen(), "\n")
			if len(lines) != size[1] {
				t.Fatalf("%v: %d lines", size, len(lines))
			}
			for j, l := range lines {
				if w := len([]rune(l)); w > size[0] {
					t.Fatalf("%v: line %d has %d characters", size, j, w)
				}
			}
			h.key("down")
		}
	}
}

// ---- with the real application

func TestChangingThePortWithTheRealApplication(t *testing.T) {
	r := apptest.Open(t, nil)
	key := r.Person("alice")
	h := newHarness(t, r.App, r.Clock, 100, 30)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	selectSetting(t, h, "server.port")
	h.keys("enter", "ctrl+u")
	h.typeText(strconv.Itoa(port))
	h.key("enter")
	h.send(changedMsg{})
	if h.m().prompt != nil || !strings.Contains(h.screen(), "running on port "+strconv.Itoa(port)) {
		t.Fatalf("the header does not show the new port:\n%s", h.screen())
	}
	client := sshtest.Dial(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), "alice", key, r.HostKey())
	if !aliveClient(client) {
		t.Error("a client cannot use the new port")
	}
}

func TestAPortInUseWithTheRealApplication(t *testing.T) {
	r := apptest.Open(t, nil)
	h := newHarness(t, r.App, r.Clock, 120, 30)
	held, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })
	port := held.Addr().(*net.TCPAddr).Port

	selectSetting(t, h, "server.port")
	h.keys("enter", "ctrl+u")
	h.typeText(strconv.Itoa(port))
	h.key("enter")
	if h.m().prompt == nil || !strings.Contains(h.screen(), "Cave cannot listen on 127.0.0.1:"+strconv.Itoa(port)) {
		t.Errorf("the sentence and the field:\n%s", h.screen())
	}
	if got := h.m().prompt.f.value(); got != strconv.Itoa(port) {
		t.Errorf("the field has %q", got)
	}
}

func TestTheAttemptsClearingWithTheRealApplication(t *testing.T) {
	r := apptest.Open(t, nil)
	h := newHarness(t, r.App, r.Clock, 100, 30)
	selectSetting(t, h, "attempts.clear")
	h.key("enter") // the next choice after the one it has
	h.send(changedMsg{})
	h.key("enter")
	h.send(changedMsg{})
	if got := r.App.Settings().String(settings.AttemptsClear); got != settings.ClearHourly {
		t.Fatalf("attempts.clear is %q", got)
	}
	h.key("3")
	want := "next clear " + app.Until(r.Clock.Now(), r.App.Attempts().NextClearAt)
	if want == "next clear now" || !strings.Contains(h.screen(), want) {
		t.Errorf("the first line of the Attempts page lacks %q:\n%s", want, h.screen())
	}
}

func TestANewHostKeyWithTheRealApplication(t *testing.T) {
	r := apptest.Open(t, nil)
	h := newHarness(t, r.App, r.Clock, 120, 30)
	h.keys("5", "end")
	before := r.App.Server().HostKeyFingerprint
	if !strings.Contains(h.screen(), before) {
		t.Fatalf("the fingerprint is not on the page:\n%s", h.screen())
	}
	h.keys("enter", "y")
	h.send(changedMsg{})
	after := r.App.Server().HostKeyFingerprint
	if after == before || !strings.Contains(h.screen(), after) || strings.Contains(h.screen(), before) {
		t.Errorf("the fingerprint did not change on the page (%s, then %s):\n%s", before, after, h.screen())
	}
}
