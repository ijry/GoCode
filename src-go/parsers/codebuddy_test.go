package parsers

// codebuddy_test.go covers the CodeBuddy JSONL reader: locating the per-session
// file, folding user/assistant/reasoning/function_call records into alternating
// turns, and previewing a JSON-string `arguments` payload.

import (
	"os"
	"path/filepath"
	"testing"
)

const codebuddyFixture = `{"type":"ai-title","title":"build it","timestamp":1782276649000}
{"type":"message","role":"user","content":[{"type":"input_text","text":"执行一下 pnpm build"}],"timestamp":1782276649227}
{"type":"reasoning","rawContent":[{"type":"reasoning_text","text":"先看构建"}],"timestamp":1782276650000}
{"type":"message","role":"assistant","content":[{"type":"output_text","text":"好的，开始构建。"}],"timestamp":1782276650100}
{"type":"function_call","name":"Bash","callId":"call_1","arguments":"{\"command\": \"pnpm build\"}","timestamp":1782276651425}
{"type":"function_call_result","callId":"call_1","output":"ok","timestamp":1782276660973}
{"type":"message","role":"user","content":[{"type":"input_text","text":"再跑一次"}],"timestamp":1782276670000}
`

func TestReadCodeBuddyTurns(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "projects", "-Users-demo-my-app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess-cb.jsonl"), []byte(codebuddyFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEBUDDY_CONFIG_DIR", home)

	turns := ReadCodeBuddyTurns("sess-cb")
	if len(turns) != 3 {
		t.Fatalf("turns = %d, want 3: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || !turns[0].HasTS || turns[0].TSMillis != 1782276649227 {
		t.Fatalf("user turn = %+v", turns[0])
	}
	ub, _ := turns[0].Raw["blocks"].([]map[string]any)
	if len(ub) != 1 || ub[0]["text"] != "执行一下 pnpm build" {
		t.Fatalf("user blocks = %+v", ub)
	}

	if turns[1].Role != "assistant" {
		t.Fatalf("assistant turn = %+v", turns[1])
	}
	ab, _ := turns[1].Raw["blocks"].([]map[string]any)
	// reasoning (as text) + assistant text + tool_use; function_call_result is
	// not a renderable block.
	if len(ab) != 3 {
		t.Fatalf("assistant blocks = %d, want 3: %+v", len(ab), ab)
	}
	if ab[0]["text"] != "先看构建" || ab[1]["text"] != "好的，开始构建。" {
		t.Fatalf("text blocks = %+v", ab[:2])
	}
	if ab[2]["type"] != "tool_use" || ab[2]["tool_name"] != "Bash" || ab[2]["tool_use_id"] != "call_1" {
		t.Fatalf("tool block = %+v", ab[2])
	}
	// `arguments` is a JSON string; the preview must be the parsed object.
	if prev, _ := ab[2]["input_preview"].(string); prev != `{"command":"pnpm build"}` {
		t.Fatalf("input preview = %q", prev)
	}

	if turns[2].Role != "user" {
		t.Fatalf("second user turn = %+v", turns[2])
	}
}

func TestReadCodeBuddyTurnsUnknownSession(t *testing.T) {
	t.Setenv("CODEBUDDY_CONFIG_DIR", t.TempDir())
	if got := ReadCodeBuddyTurns("missing"); got != nil {
		t.Fatalf("unknown session = %+v", got)
	}
}
