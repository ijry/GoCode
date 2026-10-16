package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestToolCardEditRendersDiff pins P0-3: an Edit tool whose raw arguments carry
// old_string/new_string renders a colorized unified diff in the card body (even
// when the tool result is just "Edited N lines"), with the file path shown.
// Collapsed, the diff is hidden; expanding reveals it.
//
// The diff's own lines are painted by mygo's editor.DiffView (like the markdown
// code block's), so they carry no text label for the tester to see — the
// structural labels plus the dumped frame (toolcard-edit-diff.png) are the
// proof; toolcard-patch-diff.png shows the same viewer with a real patch.
func TestToolCardEditRendersDiff(t *testing.T) {
	s := conversationShellForTest(t)
	push := func(ev map[string]any) {
		ev["connection_id"] = "c1"
		s.stream.handle(&wsFrame{Type: "event", Envelope: ev})
	}
	push(map[string]any{
		"seq":          float64(10),
		"type":         "tool_call",
		"tool_call_id": "t2",
		"title":        "Edit main.go",
		"kind":         "edit",
		"status":       "running",
		"raw_input":    `{"file_path":"/tmp/demo/main.go","old_string":"func hi() {\n\treturn 0\n}","new_string":"func hi() {\n\treturn 42\n}"}`,
	})
	push(map[string]any{
		"seq":          float64(11),
		"type":         "tool_call_update",
		"tool_call_id": "t2",
		"status":       "success",
		"output":       "Edited 1 section in /tmp/demo/main.go",
	})

	tt := ui.NewTester(s.View, 1240, 820)

	// Collapsed: the edit's file header is not in the frame yet.
	if tt.HasText("/tmp/demo/main.go") {
		t.Fatalf("collapsed edit card should hide its diff; texts = %v", tt.Texts())
	}
	// Expand by clicking the header (the tool title).
	if err := tt.Click("Edit main.go"); err != nil {
		t.Fatalf("click edit header: %v", err)
	}
	tt.Frame()
	for _, want := range []string{"/tmp/demo/main.go", "New text"} {
		if !tt.HasText(want) {
			t.Fatalf("expanded diff missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "toolcard-edit-diff")
}

// TestToolCardPatchRendersDiff drives a real unified diff in a tool's result
// through mygo's editor.DiffView (ParsePatch): the file name and the hunk
// header the viewer builds from the patch are present once expanded.
func TestToolCardPatchRendersDiff(t *testing.T) {
	s := conversationShellForTest(t)
	patch := "diff --git a/demo/main.go b/demo/main.go\n" +
		"--- a/demo/main.go\n+++ b/demo/main.go\n" +
		"@@ -1,3 +1,3 @@\n line one\n-line two\n+line two, fixed\n line three"
	s.stream.handle(&wsFrame{Type: "event", Envelope: map[string]any{
		"connection_id": "c1",
		"seq":           float64(12),
		"type":          "tool_call",
		"tool_call_id":  "t3",
		"title":         "Apply patch",
		"kind":          "edit",
		"status":        "running",
		"raw_input":     `{"file_path":"demo/main.go"}`,
	}})
	s.stream.handle(&wsFrame{Type: "event", Envelope: map[string]any{
		"connection_id": "c1",
		"seq":           float64(13),
		"type":          "tool_call_update",
		"tool_call_id":  "t3",
		"status":        "success",
		"output":        patch,
	}})

	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Apply patch"); err != nil {
		t.Fatalf("click patch header: %v", err)
	}
	tt.Frame()
	for _, want := range []string{"demo/main.go", "@@ -1,3 +1,3 @@", "New text"} {
		if !tt.HasText(want) {
			t.Errorf("patch view missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "toolcard-patch-diff")
}
