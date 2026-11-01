package app

// changes_test.go covers the git-changes panel: it lists the working-tree
// changes and shows the selected file's diff. Responses are pre-seeded so the
// view renders without a git repo on disk.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func changesShellForTest(t *testing.T) *Shell {
	t.Helper()
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.workbench = ""
	s.auxTab = "changes"
	s.selectedFolder = "1"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["folders"] = json.RawMessage(`[{"id":1,"name":"demo","path":"/tmp/demo"}]`)
	s.cache["gitstatus:/tmp/demo"] = json.RawMessage(
		`[{"status":"M","file":"main.go"},{"status":"??","file":"new.txt"}]`)
	s.cache["gitdiff:main.go"] = json.RawMessage(`"@@ -1 +1 @@\n-old\n+new"`)
	return s
}

func TestChangesPanelListsAndDiffs(t *testing.T) {
	s := changesShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{"Changes", "main.go", "new.txt", "??", "Commit all"} {
		if !tt.HasText(want) {
			t.Errorf("changes panel missing %q; texts = %v", want, tt.Texts())
		}
	}
	// Clicking a changed file shows its diff (git_diff).
	if err := tt.Click("main.go"); err != nil {
		t.Fatalf("click changed file: %v", err)
	}
	tt.Frame()
	if s.diffSel != "main.go" {
		t.Errorf("diffSel = %q, want main.go", s.diffSel)
	}
	// The diff view's own lines are painter-drawn (no text label), so the hunk
	// header it builds from the patch is the tester-visible proof that the git
	// diff went through mygo's editor.DiffView.
	if !tt.HasText("@@ -1,1 +1,1 @@") {
		t.Errorf("diff missing its hunk header; texts = %v", tt.Texts())
	}
	// The selected file's write actions are present.
	if !tt.HasText("Stage") || !tt.HasText("Discard") {
		t.Errorf("write actions missing; texts = %v", tt.Texts())
	}
}

// TestChangesDiscardConfirmStep locks the two-step discard: the first click only
// arms the confirmation, and Cancel backs out — nothing is discarded.
func TestChangesDiscardConfirmStep(t *testing.T) {
	s := changesShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("main.go"); err != nil {
		t.Fatalf("click file: %v", err)
	}
	tt.Frame()
	if err := tt.Click("Discard"); err != nil {
		t.Fatalf("click discard: %v", err)
	}
	tt.Frame()
	if s.discardConfirm != "main.go" {
		t.Errorf("discardConfirm = %q, want main.go", s.discardConfirm)
	}
	if !tt.HasText("Confirm discard") {
		t.Errorf("confirm step missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatalf("click cancel: %v", err)
	}
	tt.Frame()
	if s.discardConfirm != "" {
		t.Errorf("discardConfirm = %q, want empty", s.discardConfirm)
	}
}

func TestChangesPanelEmptyAndNoFolder(t *testing.T) {
	// No folder selected → a hint, no crash.
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.auxTab = "changes"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Select a folder") {
		t.Errorf("no-folder hint missing; texts = %v", tt.Texts())
	}
}

// TestChangesMultiSelectBulkActions locks the multi-select surface: a click
// chooses a row (mygo's ListState.Selection), more can be added, and the bulk
// buttons appear with the count — discard still taking a second click.
func TestChangesMultiSelectBulkActions(t *testing.T) {
	s := changesShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("main.go"); err != nil {
		t.Fatalf("click row: %v", err)
	}
	tt.Frame()
	if !s.changesSel.Has("main.go") || s.changesSel.Len() != 1 {
		t.Fatalf("click did not choose the row: len=%d", s.changesSel.Len())
	}

	// A second file, chosen the way Ctrl/Cmd-click would.
	s.changesSel.Add("new.txt")
	tt.Frame()
	if !tt.HasText("Stage 2") || !tt.HasText("Discard 2") {
		t.Fatalf("bulk actions missing; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "changes-multi-select")

	// Discard takes a second click, then the choice is spent.
	if err := tt.Click("Discard 2"); err != nil {
		t.Fatalf("click bulk discard: %v", err)
	}
	tt.Frame()
	if !s.changesDiscardArmed {
		t.Fatal("bulk discard not armed on the first click")
	}
	if !tt.HasText("Confirm discard 2?") {
		t.Fatalf("bulk confirm step missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Confirm discard 2?"); err != nil {
		t.Fatalf("click bulk confirm: %v", err)
	}
	tt.Frame()
	if s.changesDiscardArmed || s.changesSel.Len() != 0 {
		t.Fatalf("bulk discard did not settle: armed=%v len=%d", s.changesDiscardArmed, s.changesSel.Len())
	}
}

func TestSelectedChangeFilesSorted(t *testing.T) {
	s := New(newBackendForTest(t))
	s.changesSel.Add("b.txt")
	s.changesSel.Add("a.txt")
	got := s.selectedChangeFiles()
	if len(got) != 2 || got[0] != "a.txt" || got[1] != "b.txt" {
		t.Fatalf("selectedChangeFiles = %v, want [a.txt b.txt]", got)
	}
}
