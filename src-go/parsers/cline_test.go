package parsers

// cline_test.go covers the cline 3.x transcript reader: unwrapping
// <user_input>, folding assistant text/thinking/tool_use into one turn,
// dropping tool-result-only user messages and display-only error records.

import (
	"os"
	"path/filepath"
	"testing"
)

const clineFixture = `{
  "updated_at": "2026-10-27T00:00:00Z",
  "messages": [
    {"role":"user","content":[{"type":"text","text":"<user_input>读一下 /tmp/target.txt</user_input>"}],"ts":1782276649227},
    {"role":"assistant","content":[{"type":"thinking","thinking":"先读文件。"},{"type":"tool_use","id":"call_1","name":"read","input":{"file_path":"/tmp/target.txt"}}],"ts":1782276650000},
    {"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"text","text":"hello"}]}],"ts":1782276651000},
    {"role":"assistant","content":[{"type":"text","text":"文件内容是 hello。"}],"ts":1782276652000},
    {"role":"assistant","content":"API key expired.","metadata":{"displayOnly":true,"displayRole":"error"},"ts":1782276653000},
    {"role":"user","content":"<user_input>再读一次</user_input>","ts":1782276660000}
  ]
}`

func TestReadClineTurns(t *testing.T) {
	data := t.TempDir()
	dir := filepath.Join(data, "sessions", "sess-cl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess-cl.messages.json"), []byte(clineFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLINE_DIR", data)

	turns := ReadClineTurns("sess-cl")
	if len(turns) != 3 {
		t.Fatalf("turns = %d, want 3: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || !turns[0].HasTS || turns[0].TSMillis != 1782276649227 {
		t.Fatalf("user turn = %+v", turns[0])
	}
	ub, _ := turns[0].Raw["blocks"].([]map[string]any)
	if len(ub) != 1 || ub[0]["text"] != "读一下 /tmp/target.txt" {
		t.Fatalf("user blocks = %+v (wrapper should be unwrapped)", ub)
	}

	if turns[1].Role != "assistant" {
		t.Fatalf("assistant turn = %+v", turns[1])
	}
	ab, _ := turns[1].Raw["blocks"].([]map[string]any)
	// thinking (as text) + tool_use + later assistant text; the tool-result user
	// message and the display-only error contribute nothing.
	if len(ab) != 3 {
		t.Fatalf("assistant blocks = %d, want 3: %+v", len(ab), ab)
	}
	if ab[0]["text"] != "先读文件。" || ab[2]["text"] != "文件内容是 hello。" {
		t.Fatalf("text blocks = %+v", ab)
	}
	if ab[1]["type"] != "tool_use" || ab[1]["tool_name"] != "read" || ab[1]["tool_use_id"] != "call_1" {
		t.Fatalf("tool block = %+v", ab[1])
	}

	if turns[2].Role != "user" {
		t.Fatalf("second user turn = %+v", turns[2])
	}
}

func TestClineRewriteWrapper(t *testing.T) {
	cases := []struct{ in, want string }{
		{"<user_input>hi</user_input>", "hi"},
		// A longer tag name is not this tag.
		{"<user_inputs>keep</user_inputs>", "<user_inputs>keep</user_inputs>"},
		// Unclosed: left alone.
		{"<user_input>dangling", "<user_input>dangling"},
	}
	for _, c := range cases {
		if got := clineRewriteWrapper(c.in, "user_input", true); got != c.want {
			t.Fatalf("rewrite(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// The full clean-up pass handles both prompt wrappers and drops mode notices.
	clean := []struct{ in, want string }{
		{"a <user_command>run</user_command> b", "a run b"},
		{"hi <mode_notice>plan</mode_notice>", "hi"},
		{"<mode_notice>plan</mode_notice>", ""},
	}
	for _, c := range clean {
		if got := clineCleanUserText(c.in); got != c.want {
			t.Fatalf("clean(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestReadClineTurnsUnknownSession(t *testing.T) {
	t.Setenv("CLINE_DIR", t.TempDir())
	if got := ReadClineTurns("missing"); got != nil {
		t.Fatalf("unknown session = %+v", got)
	}
}
