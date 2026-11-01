package app

// settings_shortcuts_test.go covers Settings → Shortcuts: the page renders the
// defaults, recording captures a chord and persists it to shortcuts.json,
// conflicts keep the recorder armed, Escape cancels, reset restores defaults,
// and the installed chords drive the window's chrome and tab strip.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func shortcutsShellForTest(t *testing.T, dir string) *Shell {
	t.Helper()
	t.Setenv("GOCODE_DATA_DIR", dir)
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.section = "settings"
	s.settingsPage = "shortcuts"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	return s
}

func readSavedShortcuts(t *testing.T, dir string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "shortcuts.json"))
	if err != nil {
		t.Fatalf("read shortcuts.json: %v", err)
	}
	var out map[string]string
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode shortcuts.json: %v", err)
	}
	return out
}

func TestSettingsShortcutsRecordPersistAndReset(t *testing.T) {
	dir := t.TempDir()
	s := shortcutsShellForTest(t, dir)
	tt := ui.NewTester(s.View, 1240, 820)

	for _, want := range []string{
		"Shortcuts", "Reset defaults", "Open Search", "Toggle Left Sidebar",
		"Toggle Terminal", "Toggle Right Panel", "Ctrl+K", "Ctrl+B", "Ctrl+Shift+Tab",
	} {
		if !tt.HasText(want) {
			t.Errorf("shortcuts page missing %q; texts = %v", want, tt.Texts())
		}
	}
	if !s.shortcutsAreDefault() {
		t.Fatalf("fresh profile should start on defaults: %v", s.shortcuts)
	}
	dumpFrame(t, tt, "settings-shortcuts")

	// Arm the sidebar binding, then record Cmd/Ctrl+Shift+B.
	if err := tt.Click("Ctrl+B"); err != nil {
		t.Fatalf("click sidebar binding: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Press shortcut…") {
		t.Fatalf("recorder not armed; texts = %v", tt.Texts())
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyB)
	tt.Frame()
	if got := s.shortcuts["toggle_sidebar"]; got != "mod+shift+b" {
		t.Fatalf("recorded chord = %q, want mod+shift+b", got)
	}
	if !tt.HasText("Ctrl+Shift+B") || tt.HasText("Press shortcut…") {
		t.Errorf("binding label did not update; texts = %v", tt.Texts())
	}
	if saved := readSavedShortcuts(t, dir)["toggle_sidebar"]; saved != "mod+shift+b" {
		t.Fatalf("persisted chord = %q", saved)
	}

	// A chord another action holds is rejected and recording stays armed.
	if err := tt.Click("Ctrl+Shift+B"); err != nil {
		t.Fatalf("click rebound button: %v", err)
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyK) // Open Search
	tt.Frame()
	if !tt.HasText(`already used by "Open Search"`) {
		t.Fatalf("conflict toast missing; texts = %v", tt.Texts())
	}
	if s.shortcutRecording != "toggle_sidebar" {
		t.Fatalf("recorder should stay armed, got %q", s.shortcutRecording)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.shortcutRecording != "" {
		t.Fatalf("Escape did not cancel recording: %q", s.shortcutRecording)
	}

	// Reset restores every default and rewrites the file.
	if err := tt.Click("Reset defaults"); err != nil {
		t.Fatalf("click Reset defaults: %v", err)
	}
	if !s.shortcutsAreDefault() {
		t.Fatalf("reset did not restore defaults: %v", s.shortcuts)
	}
	if saved := readSavedShortcuts(t, dir); saved["toggle_sidebar"] != "mod+b" {
		t.Fatalf("persisted reset chord = %q", saved["toggle_sidebar"])
	}

	// A new shell on the same profile reads the custom bindings back.
	s.shortcuts["toggle_sidebar"] = "mod+shift+b"
	if err := saveShortcuts(s.shortcuts); err != nil {
		t.Fatalf("saveShortcuts: %v", err)
	}
	s2 := New(newBackendForTest(t))
	if got := s2.shortcuts["toggle_sidebar"]; got != "mod+shift+b" {
		t.Fatalf("reload chord = %q, want mod+shift+b", got)
	}
}

func TestShortcutActionsDriveChrome(t *testing.T) {
	s, _, _, _ := tabsShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)

	tt.Key(ui.Cmd, ui.KeyB)
	tt.Frame()
	if !s.sidebarHidden {
		t.Fatal("Ctrl+B did not hide the sidebar")
	}
	tt.Key(ui.Cmd, ui.KeyB)
	tt.Frame()
	if s.sidebarHidden {
		t.Fatal("Ctrl+B did not restore the sidebar")
	}

	tt.Key(ui.Cmd, ui.KeyE)
	tt.Frame()
	if !s.auxHidden {
		t.Fatal("Ctrl+E did not hide the aux panel")
	}
	tt.Key(ui.Cmd, ui.KeyJ)
	tt.Frame()
	if !s.termOpen {
		t.Fatal("Ctrl+J did not open the terminal")
	}
	tt.Key(ui.Cmd, ui.KeyComma)
	tt.Frame()
	if s.section != "settings" {
		t.Fatalf("Ctrl+, went to %q, want settings", s.section)
	}
}

func TestShortcutTabNavigation(t *testing.T) {
	s, _, id1, id2 := tabsShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	if !waitUntil(t, tt, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.openTabs) == 2
	}) {
		t.Fatalf("tabs did not hydrate; texts = %v", tt.Texts())
	}
	s.selectedConv = id1
	tt.Frame()

	tt.Key(ui.Cmd, ui.KeyTab)
	tt.Frame()
	if s.selectedConv != id2 {
		t.Fatalf("Ctrl+Tab selected %q, want %q", s.selectedConv, id2)
	}
	tt.Key(ui.Cmd, ui.Key1)
	tt.Frame()
	if s.selectedConv != id1 {
		t.Fatalf("Ctrl+1 selected %q, want %q", s.selectedConv, id1)
	}
}

// TestSettingsShortcutsNewActionsAndUnassign covers the action set added for
// terminal tabs, reopen and the composer: the shared default chords, the
// unassigned state, and its persistence across a reload.
func TestSettingsShortcutsNewActionsAndUnassign(t *testing.T) {
	dir := t.TempDir()
	s := shortcutsShellForTest(t, dir)
	tt := ui.NewTester(s.View, 1240, 900)

	for _, want := range []string{
		"New Terminal", "Close Current Terminal", "Reopen Closed Tab",
		"Send Message", "Newline in Message", "Ctrl+Shift+T", "Enter", "Shift+Enter",
	} {
		if !tt.HasText(want) {
			t.Errorf("shortcuts page missing %q; texts = %v", want, tt.Texts())
		}
	}
	// The conversation and terminal versions of new-tab and close-tab share
	// their defaults, as upstream's SHARED_SHORTCUT_PAIRS do.
	if s.shortcuts["new_terminal_tab"] != "mod+t" || s.shortcuts["new_conversation"] != "mod+t" {
		t.Fatalf("new-tab defaults = %q / %q, want mod+t",
			s.shortcuts["new_terminal_tab"], s.shortcuts["new_conversation"])
	}
	if s.shortcuts["close_current_terminal_tab"] != "mod+w" || s.shortcuts["close_current_tab"] != "mod+w" {
		t.Fatalf("close-tab defaults = %q / %q, want mod+w",
			s.shortcuts["close_current_terminal_tab"], s.shortcuts["close_current_tab"])
	}

	if err := tt.Click("Unassign Toggle Left Sidebar"); err != nil {
		t.Fatalf("click unassign: %v", err)
	}
	tt.Frame()
	if got := s.shortcuts["toggle_sidebar"]; got != "" {
		t.Fatalf("unassigned chord = %q, want empty", got)
	}
	if !tt.HasText("Unassigned") {
		t.Fatalf("unassigned label missing; texts = %v", tt.Texts())
	}
	if saved := readSavedShortcuts(t, dir)["toggle_sidebar"]; saved != "" {
		t.Fatalf("persisted unassigned chord = %q", saved)
	}

	// An unassigned action does not fall back to its default.
	tt.Key(ui.Cmd, ui.KeyB)
	tt.Frame()
	if s.sidebarHidden {
		t.Fatal("unassigned shortcut still fired")
	}

	s2 := New(newBackendForTest(t))
	if got := s2.shortcuts["toggle_sidebar"]; got != "" {
		t.Fatalf("reloaded unassigned chord = %q, want empty", got)
	}
}

// TestShortcutSharedChords checks that recording the chord a paired action
// holds is accepted instead of reported as a conflict.
func TestShortcutSharedChords(t *testing.T) {
	s := shortcutsShellForTest(t, t.TempDir())
	tt := ui.NewTester(s.View, 1240, 900)
	s.shortcuts["new_conversation"] = "mod+alt+t"
	tt.Frame()

	if err := tt.Click("Ctrl+Alt+T"); err != nil {
		t.Fatalf("click new-conversation binding: %v", err)
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyT)
	tt.Frame()
	if got := s.shortcuts["new_conversation"]; got != "mod+t" {
		t.Fatalf("shared chord = %q, want mod+t", got)
	}
	if s.shortcutRecording != "" {
		t.Fatalf("recorder stayed armed after a shared chord: %q", s.shortcutRecording)
	}
	if tt.HasText(`already used by`) {
		t.Fatalf("shared chord reported a conflict; texts = %v", tt.Texts())
	}
}

// TestTerminalTabShortcuts drives the terminal's tab actions through the
// window: they apply while the pointer rests over the panel, and the same
// chords go back to the conversation strip once it leaves.
func TestTerminalTabShortcuts(t *testing.T) {
	s, _, _, _ := tabsShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	if !waitUntil(t, tt, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.openTabs) == 2
	}) {
		t.Fatalf("tabs did not hydrate; texts = %v", tt.Texts())
	}
	s.termOpen = true
	tt.Frame()

	// Rest the pointer inside the open bottom panel.
	start, ok := tt.Find("Start terminal")
	if !ok {
		t.Fatalf("terminal start prompt missing; texts = %v", tt.Texts())
	}
	tt.Move(start.X+start.W/2, start.Y+start.H/2)
	tt.Frame()
	if !s.termHovered {
		t.Fatalf("pointer inside the panel did not register as hovering it")
	}
	tt.Key(ui.Cmd, ui.KeyT)
	tt.Frame()
	if !waitUntil(t, tt, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.termTabs) == 1
	}) {
		t.Fatalf("Ctrl+T did not open a terminal tab; texts = %v", tt.Texts())
	}
	if tabs := s.openTabCount(); tabs != 2 {
		t.Fatalf("terminal shortcut changed the conversation strip: %d tabs", tabs)
	}

	tt.Key(ui.Cmd, ui.KeyW)
	tt.Frame()
	if !waitUntil(t, tt, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.termTabs) == 0
	}) {
		t.Fatalf("Ctrl+W did not close the terminal tab; texts = %v", tt.Texts())
	}
	if tabs := s.openTabCount(); tabs != 2 {
		t.Fatalf("closing the terminal tab closed a conversation instead: %d tabs", tabs)
	}

	// The pointer leaves the panel: the chord is the strip's again.
	tt.Move(620, 100)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyT)
	tt.Frame()
	if n := s.termTabCount(); n != 0 {
		t.Fatalf("Ctrl+T opened a terminal tab outside the panel: %d tabs", n)
	}
}

// TestShortcutReopenClosedTab closes the active tab and brings it back at the
// slot it left from.
func TestShortcutReopenClosedTab(t *testing.T) {
	s, _, id1, id2 := tabsShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	if !waitUntil(t, tt, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.openTabs) == 2
	}) {
		t.Fatalf("tabs did not hydrate; texts = %v", tt.Texts())
	}
	s.selectedConv = id2
	tt.Frame()

	tt.Key(ui.Cmd, ui.KeyW)
	tt.Frame()
	if n := s.openTabCount(); n != 1 {
		t.Fatalf("Ctrl+W left %d conversation tabs, want 1", n)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyT)
	tt.Frame()

	s.mu.Lock()
	ids := make([]string, len(s.openTabs))
	for i := range s.openTabs {
		ids[i] = s.openTabs[i].convID
	}
	selected := s.selectedConv
	s.mu.Unlock()
	if len(ids) != 2 || ids[0] != id1 || ids[1] != id2 {
		t.Fatalf("reopened strip = %v, want [%s %s]", ids, id1, id2)
	}
	if selected != id2 {
		t.Fatalf("reopened tab selected %q, want %q", selected, id2)
	}
}

// TestComposerSendAndNewlineShortcuts exercises the composer's configurable
// send / newline bindings, including a remapped send chord.
func TestComposerSendAndNewlineShortcuts(t *testing.T) {
	s, _, id1, _ := tabsShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	if !waitUntil(t, tt, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.openTabs) == 2
	}) {
		t.Fatalf("tabs did not hydrate; texts = %v", tt.Texts())
	}
	s.selectedConv = id1
	tt.Frame()

	if err := tt.Click("Message the agent…"); err != nil {
		t.Fatalf("focus composer: %v", err)
	}
	tt.Frame()
	tt.Type("draft one")
	tt.Frame()
	if s.composer != "draft one" {
		t.Fatalf("composer = %q after typing", s.composer)
	}

	// Shift+Enter breaks the line (newline_in_message).
	tt.Key(ui.Shift, ui.KeyEnter)
	tt.Frame()
	if !strings.Contains(s.composer, "\n") {
		t.Fatalf("Shift+Enter did not insert a line break: %q", s.composer)
	}
	// Enter sends (send_message) and clears the draft.
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if s.composer != "" {
		t.Fatalf("Enter did not send and clear the composer: %q", s.composer)
	}

	// A remapped send chord is honored; plain Enter then breaks the line.
	s.shortcuts["send_message"] = "mod+enter"
	tt.Frame()
	tt.Type("second")
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Frame()
	if s.composer != "" {
		t.Fatalf("remapped send chord did not send: %q", s.composer)
	}
	tt.Type("third")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !strings.Contains(s.composer, "\n") {
		t.Fatalf("plain Enter sent after the send chord moved: %q", s.composer)
	}
}

// openTabCount and termTabCount read the shell's tab state under its lock.
func (s *Shell) openTabCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.openTabs)
}

func (s *Shell) termTabCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.termTabs)
}
