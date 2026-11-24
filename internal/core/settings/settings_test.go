package settings

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/cave/internal/core/storage/storagetest"
)

var ctx = context.Background()

// open opens the store of the folders, failing the test on an error.
func open(t *testing.T, dirs storage.Dirs) *Store {
	t.Helper()
	s, err := Open(ctx, dirs)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// writeFile writes the settings file of the folders.
func writeFile(t *testing.T, dirs storage.Dirs, content string) string {
	t.Helper()
	path := filepath.Join(dirs.Config, FileName)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaults(t *testing.T) {
	s := open(t, storagetest.Isolate(t))
	snap := s.Snapshot()

	// The table of docs/SPEC.md §6.
	want := map[string]any{
		"server.port":             2222,
		"server.address":          "",
		"server.startOnLaunch":    true,
		"server.keepAliveSeconds": 60,
		"attempts.clear":          "never",
		"session.shell":           "",
		"session.acceptEnv":       []string{"LANG", "LC_*"},
		"sftp.enabled":            true,
		"forwarding.local":        true,
		"forwarding.remote":       true,
		"forwarding.gatewayPorts": false,
		"forwarding.agent":        true,
		"notify.accepted":         true,
		"notify.rejected":         false,
		"ui.theme":                "system",
		"ui.closeToTray":          false,
	}
	defs := Schema()
	if len(defs) != len(want) {
		t.Errorf("the schema has %d settings, want %d", len(defs), len(want))
	}
	for _, def := range defs {
		got, ok := snap.Value(def.Key)
		if !ok {
			t.Errorf("Value(%q) does not exist", def.Key)
		}
		if !reflect.DeepEqual(got, want[def.Key]) {
			t.Errorf("%s = %#v, want %#v", def.Key, got, want[def.Key])
		}
		if !reflect.DeepEqual(def.Default, want[def.Key]) {
			t.Errorf("the default of %s = %#v, want %#v", def.Key, def.Default, want[def.Key])
		}
	}

	// The typed getters.
	if snap.Int(ServerPort) != 2222 || snap.String(AttemptsClear) != ClearNever || !snap.Bool(SFTPEnabled) ||
		!reflect.DeepEqual(snap.Strings(SessionAcceptEnv), []string{"LANG", "LC_*"}) {
		t.Error("a typed getter does not return the default")
	}
	if _, ok := snap.Value("no.such.setting"); ok {
		t.Error("Value of an unknown setting exists")
	}
	// A zero Snapshot has the defaults too.
	if (Snapshot{}).Int(ServerPort) != 2222 {
		t.Error("the zero Snapshot does not have the defaults")
	}
	// No file was written by reading.
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Errorf("opening wrote the file: %v", err)
	}
}

func TestSchemaIsACopy(t *testing.T) {
	defs := Schema()
	for i := range defs {
		if defs[i].Key == string(AttemptsClear) {
			defs[i].Choices[0] = "changed"
		}
		if defs[i].Key == string(SessionAcceptEnv) {
			defs[i].Default.([]string)[0] = "changed"
		}
	}
	for _, def := range Schema() {
		if def.Key == string(AttemptsClear) && def.Choices[0] != ClearNever {
			t.Error("the choices of the schema were changed through a copy")
		}
		if def.Key == string(SessionAcceptEnv) && def.Default.([]string)[0] != "LANG" {
			t.Error("a default of the schema was changed through a copy")
		}
	}
	s := open(t, storagetest.Isolate(t))
	list := s.Snapshot().Strings(SessionAcceptEnv)
	list[0] = "changed"
	if s.Snapshot().Strings(SessionAcceptEnv)[0] != "LANG" {
		t.Error("a list of the snapshot was changed through a copy")
	}
}

func TestSetAndReopen(t *testing.T) {
	dirs := storagetest.Isolate(t)
	s := open(t, dirs)
	before := s.Snapshot()

	changes := []struct {
		key   string
		value any
	}{
		{"server.port", 2022},
		{"server.address", "127.0.0.1"},
		{"server.address", "::1"},
		{"server.startOnLaunch", false},
		{"server.keepAliveSeconds", 0},
		{"attempts.clear", "weekly"},
		{"session.shell", "/bin/zsh"},
		{"session.acceptEnv", []string{"LANG", "LC_*", "TERM_PROGRAM", "*"}},
		{"forwarding.gatewayPorts", true},
		{"ui.theme", "dark"},
	}
	for _, c := range changes {
		if err := s.Set(ctx, c.key, c.value); err != nil {
			t.Fatalf("Set(%s, %v): %v", c.key, c.value, err)
		}
		if got, _ := s.Snapshot().Value(c.key); !reflect.DeepEqual(got, c.value) {
			t.Errorf("after Set, %s = %#v, want %#v", c.key, got, c.value)
		}
	}
	// A snapshot taken before the changes still has the old values.
	if before.Int(ServerPort) != 2222 || before.String(UITheme) != ThemeSystem {
		t.Error("an old snapshot changed")
	}

	if info, err := os.Stat(s.path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 && filepath.Separator == '/' {
		t.Errorf("the file's mode is %o, want 600", info.Mode().Perm())
	}

	reopened := open(t, dirs).Snapshot()
	want := map[string]any{
		"server.port": 2022, "server.address": "::1", "server.startOnLaunch": false, "server.keepAliveSeconds": 0,
		"attempts.clear": "weekly", "session.shell": "/bin/zsh",
		"session.acceptEnv":       []string{"LANG", "LC_*", "TERM_PROGRAM", "*"},
		"forwarding.gatewayPorts": true, "ui.theme": "dark",
		"sftp.enabled": true, // never set: still the default
	}
	for key, value := range want {
		if got, _ := reopened.Value(key); !reflect.DeepEqual(got, value) {
			t.Errorf("after reopening, %s = %#v, want %#v", key, got, value)
		}
	}

	// The file has only what was set, as readable JSON.
	var f struct {
		Version  int            `json:"version"`
		Settings map[string]any `json:"settings"`
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("the file is not JSON: %v\n%s", err, data)
	}
	if f.Version != 1 || len(f.Settings) != 9 {
		t.Errorf("the file has version %d and %d settings, want 1 and 9:\n%s", f.Version, len(f.Settings), data)
	}
	if _, ok := f.Settings["sftp.enabled"]; ok {
		t.Error("a setting that was never set is in the file")
	}
}

func TestSetRefusesBadValues(t *testing.T) {
	tests := []struct {
		key   string
		value any
	}{
		{"server.port", 0},
		{"server.port", 65536},
		{"server.port", -1},
		{"server.port", "2222"},
		{"server.port", 22.5},
		{"server.port", true},
		{"server.address", "localhost"},
		{"server.address", "192.168.1"},
		{"server.address", "fe80::1%eth0"},
		{"server.address", "127.0.0.1:22"},
		{"server.address", 5},
		{"server.startOnLaunch", "yes"},
		{"server.startOnLaunch", 1},
		{"server.keepAliveSeconds", -1},
		{"server.keepAliveSeconds", 3601},
		{"attempts.clear", "yearly"},
		{"attempts.clear", ""},
		{"attempts.clear", "Never"},
		{"attempts.clear", 7},
		{"session.shell", strings.Repeat("x", 4097)},
		{"session.shell", "/bin/sh\x00-evil"},
		{"session.shell", []string{"/bin/sh"}},
		{"session.acceptEnv", "LANG"},
		{"session.acceptEnv", []string{""}},
		{"session.acceptEnv", []string{"LC_*", "BAD NAME"}},
		{"session.acceptEnv", []string{"A=B"}},
		{"session.acceptEnv", []string{strings.Repeat("A", 257)}},
		{"session.acceptEnv", make([]string, 65)},
		{"session.acceptEnv", []any{"LANG", 5}},
		{"sftp.enabled", nil},
		{"forwarding.local", "true"},
		{"forwarding.remote", 0},
		{"forwarding.gatewayPorts", "false"},
		{"forwarding.agent", 1.0},
		{"notify.accepted", "on"},
		{"notify.rejected", "off"},
		{"ui.theme", "blue"},
		{"ui.closeToTray", "no"},
	}
	covered := map[string]bool{}
	s := open(t, storagetest.Isolate(t))
	notified := 0
	s.Subscribe(func(string) { notified++ })
	for _, tt := range tests {
		covered[tt.key] = true
		before, _ := s.Snapshot().Value(tt.key)
		err := s.Set(ctx, tt.key, tt.value)
		meta := errtest.Route(t, err, "Store.Set.SettingInvalid")
		if meta["key"] != tt.key || meta["reason"] == "" {
			t.Errorf("Set(%s, %#v): metadata = %v", tt.key, tt.value, meta)
		}
		if after, _ := s.Snapshot().Value(tt.key); !reflect.DeepEqual(before, after) {
			t.Errorf("Set(%s, %#v) was refused but changed the value to %#v", tt.key, tt.value, after)
		}
	}
	for _, def := range Schema() {
		if !covered[def.Key] {
			t.Errorf("no bad value is tested for %s", def.Key)
		}
	}
	if notified != 0 {
		t.Errorf("listeners were told %d times of refused values", notified)
	}
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Error("a refused value wrote the file")
	}

	meta := errtest.Route(t, s.Set(ctx, "no.such.setting", 1), "Store.Set.SettingUnknown")
	if meta["key"] != "no.such.setting" {
		t.Errorf("metadata = %v", meta)
	}
}

func TestSetOfTheSameValueDoesNothing(t *testing.T) {
	s := open(t, storagetest.Isolate(t))
	notified := 0
	s.Subscribe(func(string) { notified++ })
	// The default, set again: nothing to write, nobody to tell.
	if err := s.Set(ctx, "server.port", 2222); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Error("setting the default wrote the file")
	}
	if err := s.Set(ctx, "session.acceptEnv", []string{"LANG", "LC_*"}); err != nil {
		t.Fatal(err)
	}
	if notified != 0 {
		t.Errorf("told %d times", notified)
	}
	if err := s.Set(ctx, "server.port", 2022); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(ctx, "server.port", 2022); err != nil {
		t.Fatal(err)
	}
	if notified != 1 {
		t.Errorf("told %d times, want 1", notified)
	}
}

func TestListeners(t *testing.T) {
	s := open(t, storagetest.Isolate(t))
	var order []string
	cancelFirst := s.Subscribe(func(key string) {
		// The change is already in the store when a listener runs.
		order = append(order, "first:"+key+"="+s.Snapshot().String(UITheme))
	})
	s.Subscribe(func(key string) { order = append(order, "second:"+key) })
	if err := s.Set(ctx, "ui.theme", "dark"); err != nil {
		t.Fatal(err)
	}
	cancelFirst()
	cancelFirst() // cancelling twice is fine
	if err := s.Set(ctx, "ui.theme", "light"); err != nil {
		t.Fatal(err)
	}
	want := []string{"first:ui.theme=dark", "second:ui.theme", "second:ui.theme"}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestUnknownKeysAreKept(t *testing.T) {
	dirs := storagetest.Isolate(t)
	path := writeFile(t, dirs, `{"version":1,"settings":{"future.setting":{"nested":[1,2,3]},"server.port":2022,"another.future":"text"}}`)
	s := open(t, dirs)
	if s.Snapshot().Int(ServerPort) != 2022 {
		t.Errorf("server.port = %d", s.Snapshot().Int(ServerPort))
	}
	if _, ok := s.Snapshot().Value("future.setting"); ok {
		t.Error("an unknown key is a setting")
	}
	if err := s.Set(ctx, "ui.theme", "dark"); err != nil {
		t.Fatal(err)
	}
	var f struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	var nested struct {
		Nested []int `json:"nested"`
	}
	if err := json.Unmarshal(f.Settings["future.setting"], &nested); err != nil || !reflect.DeepEqual(nested.Nested, []int{1, 2, 3}) {
		t.Errorf("future.setting was not kept: %s", f.Settings["future.setting"])
	}
	if string(f.Settings["another.future"]) != `"text"` || string(f.Settings["ui.theme"]) != `"dark"` || string(f.Settings["server.port"]) != "2022" {
		t.Errorf("the rewritten file is wrong:\n%s", data)
	}
}

func TestOpenRefusesBrokenFiles(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantRoute string
		wantMeta  map[string]any
	}{
		{"not JSON", `{"version":1,"settings":{`, "Open.SettingsParseFailed", nil},
		{"not an object", `[1,2,3]`, "Open.SettingsParseFailed", nil},
		{"empty", ``, "Open.SettingsParseFailed", nil},
		{"settings of the wrong type", `{"version":1,"settings":[]}`, "Open.SettingsParseFailed", nil},
		{"a newer version", `{"version":2,"settings":{}}`, "Open.SettingsVersionUnsupported", map[string]any{"version": 2, "supported": 1}},
		{"no version", `{"settings":{"server.port":2022}}`, "Open.SettingsVersionUnsupported", map[string]any{"version": 0}},
		{"a port out of range", `{"version":1,"settings":{"server.port":70000}}`, "Open.SettingsFileValueInvalid", map[string]any{"key": "server.port"}},
		{"a port that is text", `{"version":1,"settings":{"server.port":"2222"}}`, "Open.SettingsFileValueInvalid", map[string]any{"key": "server.port"}},
		{"a port with a fraction", `{"version":1,"settings":{"server.port":22.5}}`, "Open.SettingsFileValueInvalid", map[string]any{"key": "server.port"}},
		{"a choice that does not exist", `{"version":1,"settings":{"attempts.clear":"yearly"}}`, "Open.SettingsFileValueInvalid", map[string]any{"key": "attempts.clear"}},
		{"a list with a number", `{"version":1,"settings":{"session.acceptEnv":["LANG",5]}}`, "Open.SettingsFileValueInvalid", map[string]any{"key": "session.acceptEnv"}},
		{"null for a switch", `{"version":1,"settings":{"sftp.enabled":null}}`, "Open.SettingsFileValueInvalid", map[string]any{"key": "sftp.enabled"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirs := storagetest.Isolate(t)
			path := writeFile(t, dirs, tt.content)
			_, err := Open(ctx, dirs)
			meta := errtest.Route(t, err, tt.wantRoute)
			if meta["path"] != path {
				t.Errorf("metadata = %v, want the path %s", meta, path)
			}
			for key, want := range tt.wantMeta {
				if meta[key] != want {
					t.Errorf("metadata[%q] = %v, want %v", key, meta[key], want)
				}
			}
			// The broken file is left exactly as it was.
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(data) != tt.content {
				t.Errorf("the file was changed to %q", data)
			}
		})
	}
}

func TestOpenAcceptsWhatAFileMayHold(t *testing.T) {
	dirs := storagetest.Isolate(t)
	writeFile(t, dirs, `{"version":1,"settings":{"server.port":2022.0,"session.acceptEnv":[],"server.address":"10.0.0.7"}}`)
	snap := open(t, dirs).Snapshot()
	if snap.Int(ServerPort) != 2022 || len(snap.Strings(SessionAcceptEnv)) != 0 || snap.String(ServerAddress) != "10.0.0.7" {
		t.Errorf("port %d, env %v, address %q", snap.Int(ServerPort), snap.Strings(SessionAcceptEnv), snap.String(ServerAddress))
	}

	// A file without settings is the defaults.
	dirs = storagetest.Isolate(t)
	writeFile(t, dirs, `{"version":1}`)
	if open(t, dirs).Snapshot().Int(ServerPort) != 2222 {
		t.Error("a file without settings does not give the defaults")
	}
}

func TestOpenPassesStorageErrorsUp(t *testing.T) {
	dirs := storagetest.Isolate(t)
	writeFile(t, dirs, strings.Repeat(" ", maxFileBytes+1))
	_, err := Open(ctx, dirs)
	errtest.Route(t, err, "Open->ReadFile.FileTooLarge")
}

func TestSetFailures(t *testing.T) {
	t.Run("the file cannot be written", func(t *testing.T) {
		dirs := storagetest.Isolate(t)
		s := open(t, dirs)
		notified := 0
		s.Subscribe(func(string) { notified++ })
		// The configuration folder is gone: the write fails, and nothing changes.
		if err := os.RemoveAll(dirs.Config); err != nil {
			t.Fatal(err)
		}
		err := s.Set(ctx, "server.port", 2022)
		errtest.Route(t, err, "Store.Set->Store.save->WriteFileAtomic->writeFileAtomic.StorageWriteCreateFailed")
		if s.Snapshot().Int(ServerPort) != 2222 {
			t.Error("the value changed although the file was not written")
		}
		if notified != 0 {
			t.Error("listeners were told of a change that did not happen")
		}
	})
	t.Run("the state cannot be encoded", func(t *testing.T) {
		s := open(t, storagetest.Isolate(t))
		// An unknown entry that is not JSON cannot come from a file; it is put there to reach the error.
		s.current.Store(&state{explicit: map[string]any{}, unknown: map[string]json.RawMessage{"future": json.RawMessage("{not json")}})
		err := s.Set(ctx, "server.port", 2022)
		errtest.Route(t, err, "Store.Set->Store.save.SettingsEncodeFailed")
	})
	t.Run("a cancelled context", func(t *testing.T) {
		s := open(t, storagetest.Isolate(t))
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		err := s.Set(cancelled, "server.port", 2022)
		errtest.Route(t, err, "Store.Set->Store.save->WriteFileAtomic->writeFileAtomic.StorageWriteCancelled")
		_, err = Open(cancelled, storagetest.Isolate(t))
		errtest.Route(t, err, "Open->ReadFile.StorageReadCancelled")
	})
}

func TestConcurrentSet(t *testing.T) {
	dirs := storagetest.Isolate(t)
	s := open(t, dirs)
	var notified sync.Map
	s.Subscribe(func(key string) { notified.Store(key, true) })
	keys := []struct {
		key    string
		values []any
	}{
		{"server.port", []any{2001, 2002, 2003, 2004}},
		{"ui.theme", []any{"dark", "light", "system", "dark"}},
		{"sftp.enabled", []any{false, true, false, true}},
		{"session.acceptEnv", []any{[]string{"A"}, []string{"B"}, []string{"C"}, []string{"D"}}},
	}
	var wg sync.WaitGroup
	for _, k := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := 0; round < 10; round++ {
				for _, v := range k.values {
					if err := s.Set(ctx, k.key, v); err != nil {
						t.Errorf("Set(%s): %v", k.key, err)
					}
					s.Snapshot().Int(ServerPort)
				}
			}
		}()
	}
	wg.Wait()
	// Every writer's last value is in the store and in the file.
	reopened := open(t, dirs).Snapshot()
	for _, k := range keys {
		last := k.values[len(k.values)-1]
		for name, snap := range map[string]Snapshot{"store": s.Snapshot(), "file": reopened} {
			if got, _ := snap.Value(k.key); !reflect.DeepEqual(got, last) {
				t.Errorf("%s: %s = %#v, want %#v", name, k.key, got, last)
			}
		}
		if _, ok := notified.Load(k.key); !ok {
			t.Errorf("no listener was told of %s", k.key)
		}
	}
}

func TestMatchEnvName(t *testing.T) {
	tests := []struct {
		patterns []string
		name     string
		want     bool
	}{
		{[]string{"LANG", "LC_*"}, "LANG", true},
		{[]string{"LANG", "LC_*"}, "LC_ALL", true},
		{[]string{"LANG", "LC_*"}, "LC_", true},
		{[]string{"LANG", "LC_*"}, "LC", false},
		{[]string{"LANG", "LC_*"}, "LANGUAGE", false},
		{[]string{"LANG", "LC_*"}, "XLANG", false},
		{[]string{"LANG", "LC_*"}, "lang", false},
		{[]string{"LANG", "LC_*"}, "", false},
		{[]string{"*"}, "ANYTHING", true},
		{[]string{"*"}, "", true},
		{[]string{"*_PROXY"}, "HTTP_PROXY", true},
		{[]string{"*_PROXY"}, "_PROXY", true},
		{[]string{"*_PROXY"}, "HTTP_PROXY_X", false},
		{[]string{"GIT_*_NAME"}, "GIT_AUTHOR_NAME", true},
		{[]string{"GIT_*_NAME"}, "GIT__NAME", true},
		{[]string{"GIT_*_NAME"}, "GIT_NAME", false},
		{[]string{"A*B*C"}, "AxxBxxC", true},
		{[]string{"A*B*C"}, "ABC", true},
		{[]string{"A*B*C"}, "AxxCxxB", false},
		{[]string{"A**B"}, "AB", true},
		{[]string{"*A*"}, "xxAxx", true},
		{[]string{"*A*"}, "xxxx", false},
		{[]string{"a*a*a*a*a*a*a*a*b"}, strings.Repeat("a", 2000), false}, // no blow-up
		{nil, "LANG", false},
		{[]string{}, "LANG", false},
	}
	for _, tt := range tests {
		if got := MatchEnvName(tt.patterns, tt.name); got != tt.want {
			t.Errorf("MatchEnvName(%v, %q) = %v, want %v", tt.patterns, tt.name, got, tt.want)
		}
	}
}
