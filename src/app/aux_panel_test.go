package app

// aux_panel_test.go covers the workspace's right-hand aux panel (upstream's
// AuxPanel): its tabs, and the Commits tab's git history (git_log) whose
// selected commit opens its changed files in the detail pane.

import (
	"encoding/json"
	"testing"
	"time"

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

// TestAuxPanelCommitsKeyboardNav locks the List behaviour the panel gained:
// the arrow keys walk the log and the detail pane follows, so reviewing commits
// does not need the pointer.
func TestAuxPanelCommitsKeyboardNav(t *testing.T) {
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.selectedFolder = "1"
	s.auxTab = "commits"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["folders"] = json.RawMessage(`[{"id":1,"name":"demo","path":"/tmp/demo"}]`)
	s.cache["gitlog:/tmp/demo"] = json.RawMessage(
		`{"entries":[
			{"hash":"abc1234","author":"Ada","date":"2026-10-16T10:00:00Z","message":"first commit","files":[]},
			{"hash":"def5678","author":"Ada","date":"2026-10-15T10:00:00Z","message":"second commit","files":[]}],
		  "has_upstream":false}`)
	s.cache["commitfiles:abc1234"] = json.RawMessage(`[{"path":"main.go","status":"M","additions":3,"deletions":1}]`)
	s.cache["commitfiles:def5678"] = json.RawMessage(`[{"path":"util.go","status":"A","additions":9,"deletions":0}]`)

	tt := ui.NewTester(s.View, 1400, 860)
	tt.Frame()
	if err := tt.Click("first commit"); err != nil {
		t.Fatalf("click first commit: %v", err)
	}
	tt.Frame()
	if s.commitSel != "abc1234" {
		t.Fatalf("after click commitSel = %q", s.commitSel)
	}
	if !tt.HasText("main.go") {
		t.Fatalf("detail missing after click; texts = %v", tt.Texts())
	}

	// The list has the focus now: Down moves the choice and the detail follows.
	tt.KeyDown(ui.KeyDown)
	tt.Frame()
	if s.commitsIdx != 1 || s.commitSel != "def5678" {
		t.Fatalf("after Down: idx=%d sel=%q", s.commitsIdx, s.commitSel)
	}
	if !tt.HasText("util.go") {
		t.Fatalf("detail did not follow the key; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "aux-commits-list")
}

// TestCommitCardsRelativeTime shows a commit card's age as a compact relative
// label (upstream's commit cards) with the absolute date on hover.
func TestCommitCardsRelativeTime(t *testing.T) {
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.selectedFolder = "1"
	s.auxTab = "commits"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["folders"] = json.RawMessage(`[{"id":1,"name":"demo","path":"/tmp/demo"}]`)
	old := time.Now().AddDate(0, 0, -3).UTC().Format(time.RFC3339)
	s.cache["gitlog:/tmp/demo"] = json.RawMessage(
		`{"entries":[{"hash":"abc1234","author":"Ada","date":"` + old + `","message":"aged commit","files":[]}]} `)
	s.cache["commitfiles:abc1234"] = json.RawMessage(`[{"path":"main.go","status":"M","additions":1,"deletions":0}]`)

	tt := ui.NewTester(s.View, 1400, 860)
	tt.Frame()
	// relTime floors at days: a three-day-old commit reads "3d", not a date.
	if !tt.HasText("3d") {
		t.Fatalf("relative age missing; texts = %v", tt.Texts())
	}
	if tt.HasText("2026-") {
		t.Errorf("absolute date leaked into the card; texts = %v", tt.Texts())
	}
}
