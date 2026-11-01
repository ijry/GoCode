package app

// shell_test.go covers the root shell's information architecture: there is no
// outer navigation rail — the leftmost column IS the workspace sidebar — and
// Settings / Pet are reached from the bottom chrome (the single-window
// stand-in for the separate routes upstream opens).

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func shellForTest(t *testing.T) *Shell {
	t.Helper()
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["folders"] = json.RawMessage(`[]`)
	return s
}

func TestShellBootsIntoTheWorkspaceSidebar(t *testing.T) {
	s := shellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	if s.section != "workspace" {
		t.Fatalf("boot section = %q", s.section)
	}
	// The workspace sidebar is the leftmost column: its nav rows render.
	for _, want := range []string{"New chat", "Automations", "To-dos", "Repositories", "Canvas"} {
		if !tt.HasText(want) {
			t.Errorf("workspace sidebar missing %q; texts = %v", want, tt.Texts())
		}
	}
	// The settings page is not mounted until the section is switched.
	if tt.HasText("Appearance") {
		t.Errorf("settings page should not be mounted on boot; texts = %v", tt.Texts())
	}
	// The section switcher lives on the bottom chrome.
	for _, want := range []string{"Workspace", "Settings", "Pet"} {
		if !tt.HasText(want) {
			t.Errorf("status bar missing the %q switcher; texts = %v", want, tt.Texts())
		}
	}
}

func TestStatusBarSwitchesSections(t *testing.T) {
	s := shellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)

	if err := tt.Click("Settings"); err != nil {
		t.Fatalf("click Settings: %v", err)
	}
	if s.section != "settings" {
		t.Fatalf("section = %q", s.section)
	}
	tt = ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Appearance") {
		t.Errorf("settings page not rendered; texts = %v", tt.Texts())
	}

	if err := tt.Click("Pet"); err != nil {
		t.Fatalf("click Pet: %v", err)
	}
	if s.section != "pet" {
		t.Fatalf("section = %q", s.section)
	}

	if err := tt.Click("Workspace"); err != nil {
		t.Fatalf("click Workspace: %v", err)
	}
	if s.section != "workspace" {
		t.Fatalf("section = %q", s.section)
	}
	tt = ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("New chat") {
		t.Errorf("workspace not restored; texts = %v", tt.Texts())
	}
}

func TestSidebarSegmentedSwitcher(t *testing.T) {
	s := shellForTest(t)
	s.selectedFolder = "1"
	s.cache["convs:1"] = json.RawMessage(`[{"id":10,"title":"Recent Chat","agent_type":"claude_code","status":"idle"},{"id":11,"title":"Pinned Chat","agent_type":"claude_code","status":"idle","pinned":true}]`)
	s.cache["folders"] = json.RawMessage(`[{"id":1,"name":"proj","path":"/tmp/proj"}]`)
	s.cache["agents"] = json.RawMessage(`[]`)

	tt := ui.NewTester(s.View, 1240, 820)
	// All three segments render; the recent tab lists only the recent chat.
	for _, want := range []string{"Recent", "Pinned", "Folders"} {
		if !tt.HasText(want) {
			t.Fatalf("segmented switcher missing %q; texts = %v", want, tt.Texts())
		}
	}
	if !tt.HasText("Recent Chat") {
		t.Fatalf("recent tab missing the recent chat; texts = %v", tt.Texts())
	}
	if tt.HasText("Pinned Chat") {
		t.Errorf("recent tab must hide the pinned chat (no vertical stacking); texts = %v", tt.Texts())
	}

	// Switch to Pinned → only the pinned chat.
	if err := tt.Click("Pinned"); err != nil {
		t.Fatalf("click Pinned: %v", err)
	}
	tt = ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Pinned Chat") || tt.HasText("Recent Chat") {
		t.Errorf("pinned tab must show only the pinned chat; texts = %v", tt.Texts())
	}

	// Switch to Folders → the folder list replaces the conversations.
	if err := tt.Click("Folders"); err != nil {
		t.Fatalf("click Folders: %v", err)
	}
	if s.sidebarTab != "folders" {
		t.Fatalf("sidebarTab = %q, want folders", s.sidebarTab)
	}
	tt = ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("proj") {
		t.Errorf("folders tab missing the folder; texts = %v", tt.Texts())
	}
	if tt.HasText("Recent Chat") || tt.HasText("Pinned Chat") {
		t.Errorf("folders tab must hide conversations; texts = %v", tt.Texts())
	}
}

func TestStatusBarIndicators(t *testing.T) {
	s := shellForTest(t)
	s.cache["status-mcp"] = json.RawMessage(`{"state":"stopped"}`)
	s.cache["status-appupdate"] = json.RawMessage(`{"currentVersion":"0.34.0","update":null}`)
	s.cache["status-worktasks"] = json.RawMessage(`[{"id":1,"title":"a","status":"todo"},{"id":2,"title":"b","status":"in_progress"},{"id":3,"title":"c","status":"done"}]`)

	tt := ui.NewTester(s.View, 1240, 820)
	// MCP health always shows (stopped here); the liveness dot + version stay.
	if !tt.HasText("MCP") {
		t.Errorf("MCP health indicator missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("connecting") && !tt.HasText("connected") {
		t.Errorf("liveness indicator must remain; texts = %v", tt.Texts())
	}
	if !tt.HasText("GoCode v0.1.0") {
		t.Errorf("version must remain bottom-right; texts = %v", tt.Texts())
	}
	// To-dos badge shows the 2 OPEN tasks (done excluded), update badge hidden.
	if !tt.HasText("\u2611 2") {
		t.Errorf("to-dos badge should show 2 open; texts = %v", tt.Texts())
	}
	if tt.HasText("Update") {
		t.Errorf("update badge should be hidden when no update is available; texts = %v", tt.Texts())
	}
	// Clicking the to-dos badge jumps to the To-dos workbench.
	if err := tt.Click("\u2611 2"); err != nil {
		t.Fatalf("click to-dos badge: %v", err)
	}
	if s.workbench != "tasks" {
		t.Fatalf("workbench = %q, want tasks", s.workbench)
	}
}
