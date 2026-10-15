package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestToolCardEditRendersDiff pins P0-3: an Edit tool whose raw arguments carry
// old_string/new_string renders a colorized unified diff in the card body (even
// when the tool result is just "Edited N lines"), with the file path shown and
// the changed lines present. Collapsed, the diff is hidden; expanding reveals it.
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

	// Collapsed: the edit's diff lines are not in the frame yet.
	if tt.HasText("return 42") {
		t.Fatalf("collapsed edit card should hide its diff; texts = %v", tt.Texts())
	}
	// Expand by clicking the header (the tool title).
	if err := tt.Click("Edit main.go"); err != nil {
		t.Fatalf("click edit header: %v", err)
	}
	tt.Frame()
	for _, want := range []string{"return 42", "return 0"} {
		if !tt.HasText(want) {
			t.Fatalf("expanded diff missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "toolcard-edit-diff")
}

// TestUnifiedDiff checks the LCS line diff produces a valid unified diff with
// the changed lines prefixed and a hunk header carrying the line counts.
func TestUnifiedDiff(t *testing.T) {
	got := unifiedDiff("a\nb\nc", "a\nB\nc", "x.go")
	for _, want := range []string{"--- x.go", "+++ x.go", "@@ -1,3 +1,3 @@", "-b", "+B"} {
		if !strings.Contains(got, want) {
			t.Fatalf("unifiedDiff missing %q; got =\n%q", want, got)
		}
	}
}
