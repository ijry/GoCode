package app

// settings_quick_messages_test.go covers the Settings → Quick Messages depth
// (UI_GAPS P1-8): the empty state + create round-trip, inline validation of a
// blank title / content, edit, and delete-through-confirmation.

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

// qmShellForTest boots the real backend, seeds count messages, and returns a
// Shell already on Settings → Quick Messages with the list cache pre-seeded.
func qmShellForTest(t *testing.T, count int) (*Shell, *Backend) {
	t.Helper()
	b := newBackendForTest(t)
	for i := 0; i < count; i++ {
		if _, err := b.raw("quick_messages_create", map[string]any{
			"title":   fmt.Sprintf("Msg %d", i+1),
			"content": fmt.Sprintf("content %d", i+1),
		}); err != nil {
			t.Fatalf("seed quick message: %v", err)
		}
	}
	s := New(b)
	s.stream = newStreamForTest()
	s.section = "settings"
	s.settingsPage = "quick-messages"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	raw, err := b.raw("quick_messages_list", map[string]any{})
	if err != nil {
		t.Fatalf("quick_messages_list: %v", err)
	}
	s.cache["quick-messages"] = raw
	return s, b
}

// TestQuickMessagesEmptyThenCreate walks the empty state into the form, hits
// both validation errors, then saves and sees the snippet listed.
func TestQuickMessagesEmptyThenCreate(t *testing.T) {
	s, _ := qmShellForTest(t, 0)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("No quick messages yet") {
		t.Fatalf("empty state missing; texts = %v", tt.Texts())
	}
	// The page explains what quick messages are.
	if !tt.HasText("Reusable snippets") {
		t.Errorf("help text missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("New message"); err != nil {
		t.Fatalf("click New message: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Save") || !tt.HasText("Content") {
		t.Fatalf("form not shown; texts = %v", tt.Texts())
	}

	// Blank title → the page's own validation fires (the backend has none).
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("click Save (blank): %v", err)
	}
	tt.Frame()
	if !tt.HasText("A title is required.") {
		t.Fatalf("title validation missing; texts = %v", tt.Texts())
	}

	// Title but no content → content error.
	s.qmEditor.title = "Review checklist"
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("click Save (no content): %v", err)
	}
	tt.Frame()
	if !tt.HasText("Content is required.") {
		t.Fatalf("content validation missing; texts = %v", tt.Texts())
	}

	// Valid → saved and listed.
	s.qmEditor.content = "Check the diff for edge cases."
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("click Save (valid): %v", err)
	}
	if !waitText(t, tt, "Review checklist", true) {
		t.Fatalf("created message not listed; texts = %v", tt.Texts())
	}
	if !tt.HasText("Saved.") {
		t.Errorf("save feedback missing; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "settings-quick-messages")
}

// TestQuickMessagesEditAndDelete edits a seeded message, then deletes it
// through the confirmation.
func TestQuickMessagesEditAndDelete(t *testing.T) {
	s, _ := qmShellForTest(t, 1)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Msg 1") {
		t.Fatalf("seeded message missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Edit"); err != nil {
		t.Fatalf("click Edit: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Edit quick message") {
		t.Fatalf("edit form not shown; texts = %v", tt.Texts())
	}
	s.qmEditor.title = "Renamed"
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("click Save: %v", err)
	}
	if !waitText(t, tt, "Renamed", true) {
		t.Fatalf("rename not reflected; texts = %v", tt.Texts())
	}

	// The delete control is an icon button labelled with the title.
	if err := tt.Click("Delete Renamed"); err != nil {
		t.Fatalf("click delete icon: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Delete this message?") {
		t.Fatalf("delete confirmation missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Delete"); err != nil {
		t.Fatalf("confirm Delete: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("No quick messages yet") }) {
		t.Fatalf("delete not reflected; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "settings-quick-messages-deleted")
}
