package app

// tasks_test.go covers the To-dos workbench (UI_GAPS P1-7): the empty state and
// create round-trip, the four-column board, the status-dependent card menu, the
// detail view's activity timeline, and the show-archived toggle.

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// tasksShellForTest opens a folder and returns a Shell on the To-dos route. When
// seed is non-empty it is used verbatim as the cached work_task_list (so tests
// can place tasks in any status without a status setter on the wire); otherwise
// the real (empty) list is fetched.
func tasksShellForTest(t *testing.T, seed string) (*Shell, string) {
	t.Helper()
	b := newBackendForTest(t)
	fraw, err := b.raw("add_folder_to_history", map[string]any{"path": t.TempDir()})
	if err != nil {
		t.Fatalf("add_folder: %v", err)
	}
	var folder map[string]any
	_ = json.Unmarshal(fraw, &folder)
	fid := fmt.Sprintf("%v", folder["id"])

	s := New(b)
	s.stream = newStreamForTest()
	s.workbench = "tasks"
	s.selectedFolder = fid
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["agents"] = json.RawMessage(`[{"agent_type":"claude_code"}]`)
	if seed != "" {
		s.cache["tasks:"+fid] = json.RawMessage(seed)
	} else {
		raw, err := b.raw("work_task_list", map[string]any{"folderId": numOrString(fid)})
		if err != nil {
			t.Fatalf("work_task_list: %v", err)
		}
		s.cache["tasks:"+fid] = raw
	}
	return s, fid
}

// tasksSeed places one task in each board column.
const tasksSeed = `[
  {"id":1,"folder_id":1,"title":"Add retry","status":"todo","agent_type":"claude_code","config":{},"archived_at":null,"updated_at":"2026-10-15T10:00:00Z","files_changed":0},
  {"id":2,"folder_id":1,"title":"Ship the fix","status":"running","agent_type":"claude_code","config":{},"archived_at":null,"updated_at":"2026-10-15T10:05:00Z","files_changed":3,"additions":20,"deletions":5,"work_branch":"task/2"},
  {"id":3,"folder_id":1,"title":"Needs review","status":"review","agent_type":"claude_code","config":{},"archived_at":null,"updated_at":"2026-10-15T10:10:00Z","files_changed":2,"additions":8,"deletions":1},
  {"id":4,"folder_id":1,"title":"Old task","status":"done","agent_type":"claude_code","config":{},"archived_at":null,"updated_at":"2026-10-15T10:15:00Z","files_changed":1,"additions":1,"deletions":0}
]`

// TestTasksEmptyThenCreate walks the empty state into the create form and
// asserts the saved to-do lands on the board.
func TestTasksEmptyThenCreate(t *testing.T) {
	s, _ := tasksShellForTest(t, "")
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("No to-dos yet") {
		t.Fatalf("empty state missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("New to-do"); err != nil {
		t.Fatalf("click New to-do: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Save") || !tt.HasText("Prompt") {
		t.Fatalf("form not shown; texts = %v", tt.Texts())
	}
	s.taskEditor.title = "Fix the flaky test"
	s.taskEditor.prompt = "make the test deterministic"
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("click Save: %v", err)
	}
	if !waitText(t, tt, "Fix the flaky test", true) {
		t.Fatalf("created to-do not on the board; texts = %v", tt.Texts())
	}
	// It lands in the To do column.
	for _, col := range []string{"To do", "In progress", "Needs attention", "Done"} {
		if !tt.HasText(col) {
			t.Errorf("board missing column %q; texts = %v", col, tt.Texts())
		}
	}
	dumpFrame(t, tt, "tasks-created")
}

// TestTasksBoardAndDetail checks the four-column board and the detail view.
func TestTasksBoardAndDetail(t *testing.T) {
	s, fid := tasksShellForTest(t, tasksSeed)
	s.cache["task-events:1"] = json.RawMessage(`[{"id":1,"task_id":1,"kind":"created","actor":"user","created_at":"2026-10-15T10:00:00Z"}]`)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{"To do", "In progress", "Needs attention", "Done", "Add retry", "Ship the fix", "Needs review", "Old task"} {
		if !tt.HasText(want) {
			t.Fatalf("board missing %q; texts = %v", want, tt.Texts())
		}
	}
	// Click a card → detail with facts + activity.
	if err := tt.Click("Add retry"); err != nil {
		t.Fatalf("click card: %v", err)
	}
	tt.Frame()
	for _, want := range []string{"← Board", "Agent: claude_code", "ACTIVITY", "created · user"} {
		if !tt.HasText(want) {
			t.Errorf("detail missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "tasks-detail")
	// Back to the board.
	if err := tt.Click("← Board"); err != nil {
		t.Fatalf("click back: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Ship the fix") {
		t.Errorf("back to board failed; texts = %v", tt.Texts())
	}
	_ = fid
}

// TestTaskCardMenuByStatus pins the status-dependent action set on the card
// menu (upstream buildTaskActions).
func TestTaskCardMenuByStatus(t *testing.T) {
	s, _ := tasksShellForTest(t, tasksSeed)
	tt := ui.NewTester(s.View, 1240, 820)
	cases := []struct {
		title string
		want  []string
	}{
		{"Add retry", []string{"Start", "Edit", "-", "Delete"}},
		{"Ship the fix", []string{"Cancel", "-", "Delete"}},
		{"Needs review", []string{"Merge", "-", "Delete"}},
		{"Old task", []string{"Archive", "-", "Delete"}},
	}
	for _, tc := range cases {
		if err := tt.RightClick(tc.title); err != nil {
			t.Fatalf("right-click %q: %v", tc.title, err)
		}
		if got := tt.Menu(); !slices.Equal(got, tc.want) {
			t.Fatalf("%q menu %v, want %v", tc.title, got, tc.want)
		}
		tt.CloseMenu()
		tt.Frame()
	}
	dumpFrame(t, tt, "tasks-board")
}

// TestTaskShowArchivedToggle checks archived tasks hide until the toggle is on,
// and that an archived task offers only Unarchive.
func TestTaskShowArchivedToggle(t *testing.T) {
	seed := `[{"id":9,"folder_id":1,"title":"Archived job","status":"done","agent_type":"claude_code","config":{},"archived_at":"2026-10-15T09:00:00Z","updated_at":"2026-10-15T09:00:00Z","files_changed":0}]`
	s, _ := tasksShellForTest(t, seed)
	tt := ui.NewTester(s.View, 1240, 820)
	if tt.HasText("Archived job") {
		t.Fatalf("archived task shown with the toggle off; texts = %v", tt.Texts())
	}
	s.taskShowArchived = true
	tt.Frame()
	if !tt.HasText("Archived job") {
		t.Fatalf("archived task hidden with the toggle on; texts = %v", tt.Texts())
	}
	if err := tt.RightClick("Archived job"); err != nil {
		t.Fatalf("right-click archived: %v", err)
	}
	if got := tt.Menu(); !slices.Equal(got, []string{"Unarchive"}) {
		t.Fatalf("archived menu %v, want [Unarchive]", got)
	}
}
