package parsers

// kimi_test.go covers the Kimi Code wire.jsonl reader: locating the
// directory-per-session file, folding prompt/content/tool events into
// alternating turns, and skipping non-content records.

import (
	"os"
	"path/filepath"
	"testing"
)

const kimiWireFixture = `{"type":"config.update","modelAlias":"kimi-k2","time":1782276649000}
{"type":"turn.prompt","input":[{"type":"text","text":"执行一下 pnpm build"}],"origin":{"kind":"user"},"time":1782276649227}
{"type":"context.append_loop_event","event":{"type":"step.begin","uuid":"s1","turnId":"0","step":1},"time":1782276649900}
{"type":"context.append_loop_event","event":{"type":"content.part","part":{"type":"think","think":"先看构建是否通过"}},"time":1782276650000}
{"type":"context.append_loop_event","event":{"type":"content.part","part":{"type":"text","text":"好的，开始构建。"}},"time":1782276650100}
{"type":"context.append_loop_event","event":{"type":"tool.call","toolCallId":"Bash_0","name":"Bash","args":{"command":"pnpm build"},"description":"Running: pnpm build"},"time":1782276651425}
{"type":"context.append_loop_event","event":{"type":"tool.result","parentUuid":"Bash_0","toolCallId":"Bash_0","result":{"output":"ok"}},"time":1782276660973}
{"type":"context.append_loop_event","event":{"type":"step.end","uuid":"s1","turnId":"0","step":1},"time":1782276660973}
{"type":"usage.record","model":"codeg-managed","usage":{"output":37},"time":1782276660974}
{"type":"context.append_message","message":{"role":"user"},"time":1782276660975}
{"type":"turn.prompt","input":[{"type":"text","text":"再跑一次"}],"origin":{"kind":"user"},"time":1782276670000}
`

func TestReadKimiTurns(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "wd_demo_abc123", "sess-1", "agents", "main")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wire.jsonl"), []byte(kimiWireFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KIMI_CODE_HOME", home)

	turns := ReadKimiTurns("sess-1")
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
	// think (as text) + text + tool_use; tool.result / step.* / usage.record are
	// not renderable blocks.
	if len(ab) != 3 {
		t.Fatalf("assistant blocks = %d, want 3: %+v", len(ab), ab)
	}
	if ab[0]["type"] != "text" || ab[0]["text"] != "先看构建是否通过" {
		t.Fatalf("think block = %+v", ab[0])
	}
	if ab[1]["type"] != "text" || ab[1]["text"] != "好的，开始构建。" {
		t.Fatalf("text block = %+v", ab[1])
	}
	if ab[2]["type"] != "tool_use" || ab[2]["tool_name"] != "Bash" || ab[2]["tool_use_id"] != "Bash_0" {
		t.Fatalf("tool block = %+v", ab[2])
	}

	if turns[2].Role != "user" {
		t.Fatalf("second user turn = %+v", turns[2])
	}
}

func TestReadKimiTurnsUnknownSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	if got := ReadKimiTurns("missing"); got != nil {
		t.Fatalf("unknown session = %+v", got)
	}
	// A traversal-shaped id is refused without touching the filesystem.
	if got := ReadKimiTurns("../etc"); got != nil {
		t.Fatalf("unsafe id = %+v", got)
	}
}
