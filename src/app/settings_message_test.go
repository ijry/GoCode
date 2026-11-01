package app

// settings_message_test.go covers the settings shell's action banner: pages
// report their outcome through actionMsg (a save failure, a token probe's
// result), which had no render site before, so those messages were invisible.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSettingsShowsActionMessage(t *testing.T) {
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.section = "settings"
	s.settingsPage = "appearance"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.settingsMsgPage = "appearance" // as if the page had been shown already
	s.actionMsg = "save failed: access denied"

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if !tt.HasText("save failed: access denied") {
		t.Fatalf("settings page did not show its action message; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "settings-action-message")

	// Switching pages drops the previous page's message.
	s.settingsPage = "general"
	tt.Frame()
	if tt.HasText("save failed: access denied") {
		t.Fatalf("the message followed the user to another page")
	}
	if s.actionMsg != "" {
		t.Fatalf("actionMsg = %q after switching pages", s.actionMsg)
	}
}
