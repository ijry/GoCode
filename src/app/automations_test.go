package app

// automations_test.go covers the Automations workbench (UI_GAPS P1-7): the
// empty-state onboarding, the create form round-trip, the detail pane's facts
// and run history, and the row menu's delete confirmation. The fixture drives
// the REAL backend stack so create/run/cancel/delete hit real rows.

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// automationsShellForTest boots the real backend and returns a Shell already on
// the Automations route with the list cache seeded (so the first frame renders
// synchronously). When create is true it also creates one automation first.
func automationsShellForTest(t *testing.T, create bool) (*Shell, string) {
	t.Helper()
	b := newBackendForTest(t)
	id := ""
	if create {
		draft := map[string]any{
			"name": "nightly review", "enabled": true, "trigger_kind": "schedule",
			"cron": "0 9 * * *", "timezone": "UTC", "agent_type": "claude_code",
			"isolation": "worktree_per_run", "is_remote_branch": false,
			"config": map[string]any{"display_text": "review the diff"},
		}
		raw, err := b.raw("automation_create", map[string]any{"draft": draft})
		if err != nil {
			t.Fatalf("create automation: %v", err)
		}
		var rec map[string]any
		_ = json.Unmarshal(raw, &rec)
		id = fmt.Sprintf("%v", rec["id"])
	}
	s := New(b)
	s.stream = newStreamForTest()
	s.workbench = "automations"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["agents"] = json.RawMessage(`[{"agent_type":"claude_code"}]`)
	listRaw, err := b.raw("automation_list", map[string]any{})
	if err != nil {
		t.Fatalf("automation_list: %v", err)
	}
	s.cache["automations"] = listRaw
	return s, id
}

// waitText pumps frames (yielding to the async backend goroutines) until want's
// presence matches, or the budget runs out.
func waitText(t *testing.T, tt *ui.Tester, want string, present bool) bool {
	t.Helper()
	for i := 0; i < 300; i++ {
		tt.Frame()
		if tt.HasText(want) == present {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// TestAutomationsEmptyThenCreate walks the onboarding empty state into the
// create form and asserts the saved automation lands in the list.
func TestAutomationsEmptyThenCreate(t *testing.T) {
	s, _ := automationsShellForTest(t, false)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("No automations yet") {
		t.Fatalf("empty state missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("New automation"); err != nil {
		t.Fatalf("click New automation: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Save") || !tt.HasText("Trigger") {
		t.Fatalf("editor not shown; texts = %v", tt.Texts())
	}
	// Fill the form (the fields bind straight to the editor struct).
	s.autoEditor.name = "nightly review"
	s.autoEditor.prompt = "review the diff"
	s.autoEditor.cron = "0 9 * * *"
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("click Save: %v", err)
	}
	if !waitText(t, tt, "nightly review", true) {
		t.Fatalf("created automation not listed; texts = %v", tt.Texts())
	}
	// The detail pane reflects the new row.
	if !tt.HasText("review the diff") {
		t.Errorf("detail prompt missing; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "automations-created")
}

// TestAutomationDetailAndRunHistory checks the detail pane's facts and drives a
// run through the history: Run now → Running → Cancel → Cancelled.
func TestAutomationDetailAndRunHistory(t *testing.T) {
	s, _ := automationsShellForTest(t, true)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{
		"nightly review", "review the diff", "Run now", "Edit", "Delete",
		"Trigger: every cron 0 9 * * *", "Agent: claude_code",
		"Next run:", "Last run:",
	} {
		if !tt.HasText(want) {
			t.Errorf("detail missing %q; texts = %v", want, tt.Texts())
		}
	}
	if !tt.HasText("No runs yet.") {
		t.Errorf("run history should start empty; texts = %v", tt.Texts())
	}

	if err := tt.Click("Run now"); err != nil {
		t.Fatalf("click Run now: %v", err)
	}
	if !waitText(t, tt, "Running", true) {
		t.Fatalf("run history missing a Running row; texts = %v", tt.Texts())
	}
	if !tt.HasText("Cancel") {
		t.Errorf("a running row should offer Cancel; texts = %v", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatalf("click Cancel: %v", err)
	}
	// The run row itself must leave "Running" (the title chip alone would pass a
	// weaker "Cancelled" check, since last_run_status flips independently).
	if !waitText(t, tt, "Running", false) {
		t.Fatalf("run row stayed Running after cancel; texts = %v", tt.Texts())
	}
	if !tt.HasText("Cancelled") {
		t.Errorf("run history missing Cancelled; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "automations-detail")
}

// TestAutomationRowMenuDelete right-clicks a row, checks the quick-action menu,
// and drives the delete confirmation through to the empty state.
func TestAutomationRowMenuDelete(t *testing.T) {
	s, _ := automationsShellForTest(t, true)
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.RightClick("nightly review"); err != nil {
		t.Fatalf("right-click row: %v", err)
	}
	if got, want := tt.Menu(), []string{"Run now", "Disable", "Edit", "-", "Delete"}; !slices.Equal(got, want) {
		t.Fatalf("menu %v, want %v", got, want)
	}
	if err := tt.ChooseMenuItem("Delete"); err != nil {
		t.Fatalf("choose Delete: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Delete this automation?") {
		t.Fatalf("delete confirmation not shown; texts = %v", tt.Texts())
	}
	if err := tt.Click("Delete"); err != nil {
		t.Fatalf("confirm Delete: %v", err)
	}
	if !waitText(t, tt, "No automations yet", true) {
		t.Fatalf("delete not reflected; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "automations-deleted")
}

// TestAutomationRowMenuDisable checks the enable/disable label flips with state.
func TestAutomationRowMenuDisable(t *testing.T) {
	s, id := automationsShellForTest(t, true)
	tt := ui.NewTester(s.View, 1240, 820)
	// Enabled → the menu offers Disable.
	if err := tt.RightClick("nightly review"); err != nil {
		t.Fatalf("right-click: %v", err)
	}
	if got := tt.Menu(); !slices.Contains(got, "Disable") {
		t.Fatalf("enabled row menu %v should offer Disable", got)
	}
	if err := tt.ChooseMenuItem("Disable"); err != nil {
		t.Fatalf("choose Disable: %v", err)
	}
	// The toggle flips the detail button to "Enable" and the row menu too.
	if !waitText(t, tt, "Enable", true) {
		t.Fatalf("disable not reflected; texts = %v", tt.Texts())
	}
	if err := tt.RightClick("nightly review"); err != nil {
		t.Fatalf("right-click after disable: %v", err)
	}
	if got := tt.Menu(); !slices.Contains(got, "Enable") {
		t.Fatalf("disabled row menu %v should offer Enable", got)
	}
	_ = id
}
