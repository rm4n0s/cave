package app

import (
	"strings"
	"testing"

	"github.com/rm4n0s/cave/internal/core/settings"
)

func TestEverySettingHasText(t *testing.T) {
	groups := []string{GroupServer, GroupAttempts, GroupSessions, GroupForwarding, GroupNotifications, GroupAppearance}
	known := map[string]bool{}
	for _, g := range groups {
		known[g] = true
	}
	texts := SettingTexts()
	byKey := map[string]SettingText{}
	for _, text := range texts {
		if _, dup := byKey[text.Key]; dup {
			t.Errorf("%s has two texts", text.Key)
		}
		byKey[text.Key] = text
		if !known[text.Group] || text.Label == "" || !strings.HasSuffix(text.Help, ".") {
			t.Errorf("%s: %+v", text.Key, text)
		}
	}
	schema := settings.Schema()
	for _, def := range schema {
		if _, ok := byKey[def.Key]; !ok {
			t.Errorf("%s has no text", def.Key)
		}
	}
	if len(texts) != len(schema) {
		t.Errorf("%d texts for %d settings", len(texts), len(schema))
	}
	// The same order as the schema, so that the groups are one block each, in the list's order.
	for i, def := range schema {
		if texts[i].Key != def.Key {
			t.Errorf("text %d is %s, the schema has %s", i, texts[i].Key, def.Key)
		}
	}
	seen := -1
	for _, text := range texts {
		index := 0
		for i, g := range groups {
			if g == text.Group {
				index = i
			}
		}
		if index < seen {
			t.Errorf("%s: group %q comes after a later group", text.Key, text.Group)
		}
		seen = index
	}
}

func TestDesktopOnlySettings(t *testing.T) {
	want := map[string]bool{"notify.accepted": true, "notify.rejected": true, "ui.theme": true, "ui.closeToTray": true}
	for _, text := range SettingTexts() {
		if text.DesktopOnly != want[text.Key] {
			t.Errorf("%s: DesktopOnly = %v", text.Key, text.DesktopOnly)
		}
	}
}

func TestAgentForwardingSaysWhoCanReachTheAgent(t *testing.T) {
	for _, text := range SettingTexts() {
		if text.Key != "forwarding.agent" {
			continue
		}
		want := "While a person is connected with ssh -A, the programs of this computer's user and of the other connected people can use that person's agent."
		if !strings.Contains(text.Help, want) {
			t.Errorf("Help = %q, want it to say %q", text.Help, want)
		}
		return
	}
	t.Error("no text for forwarding.agent")
}

func TestSettingTextsIsACopy(t *testing.T) {
	first := SettingTexts()
	first[0].Label = "changed"
	if SettingTexts()[0].Label == "changed" {
		t.Error("the caller changed the table")
	}
}
