package app

// aux_panel_test.go covers the workspace's right-hand aux panel (upstream's
// AuxPanel): its tabs, and the Commits tab's git history (git_log) whose
// selected commit opens its changed files in the detail pane.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAuxPanelCommitsTab(t *testing.T) {
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.selectedFolder = "1"
	s.auxTab = "commits"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["folders"] = json.RawMessage(`[{"id":1,"name":"demo","path":"/tmp/demo"}]`)
	s.cache["gitlog:/tmp/demo"] = json.RawMessage(
		`{"entries":[{"hash":"abc1234","full_hash":"abc1234def","author":"Ada","date":"2026-10-16T10:00:00Z","message":"first commit","files":[],"pushed":false}],"has_upstream":false}`)
	s.cache["commitfiles:abc1234"] = json.RawMessage(`[{"path":"main.go","status":"M","additions":3,"deletions":1}]`)

	tt := ui.NewTester(s.View, 1400, 860)
	// The four aux tabs are present and the Commits tab lists the history.
	for _, want := range []string{"Session", "Files", "Changes", "Commits", "abc1234", "first commit"} {
		if !tt.HasText(want) {
			t.Fatalf("aux panel missing %q; texts = %v", want, tt.Texts())
		}
	}
	// Selecting a commit opens its changed files in the detail pane.
	if err := tt.Click("first commit"); err != nil {
		t.Fatalf("click commit: %v", err)
	}
	tt.Frame()
	if s.commitSel != "abc1234" {
		t.Errorf("commitSel = %q, want abc1234", s.commitSel)
	}
	if !tt.HasText("main.go") {
		t.Errorf("commit detail missing; texts = %v", tt.Texts())
	}
}
