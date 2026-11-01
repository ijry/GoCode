package parsers

// openclaw_test.go covers the OpenClaw reader: the id/parentId tree (a fork is
// resolved to its longest chain), Claude-shaped content blocks, and the user
// text prefixes OpenClaw wraps prompts in.

import (
	"os"
	"path/filepath"
	"testing"
)

const openclawFixture = `{"type":"session","cwd":"/tmp/demo"}
{"type":"message","id":"u1","parentId":null,"timestamp":"2026-03-17T04:56:22.819Z","message":{"role":"user","content":[{"type":"text","text":"[Tue 2026-03-17 12:56 GMT+8] Hello"}],"timestamp":1773723382812}}
{"type":"message","id":"a1","parentId":"u1","timestamp":"2026-03-17T04:56:30.466Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"想一想。"},{"type":"text","text":"Hi there!"}],"timestamp":1773723390466}}
{"type":"message","id":"a2","parentId":"a1","timestamp":"2026-03-17T04:57:00.000Z","message":{"role":"user","content":[{"type":"text","text":"[Working directory: /tmp/demo] 再跑一次"}],"timestamp":1773723420000}}
{"type":"message","id":"a4","parentId":"a2","timestamp":"2026-03-17T04:57:05.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"read","input":{"file_path":"/tmp/target.txt"}},{"type":"text","text":"好的。"}],"timestamp":1773723425000}}
{"type":"message","id":"f1","parentId":"a1","timestamp":"2026-03-17T04:56:40.000Z","message":{"role":"assistant","content":[{"type":"text","text":"forked aside"}],"timestamp":1773723400000}}
`

func TestReadOpenClawTurns(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".openclaw", "agents", "main", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess-oc.jsonl"), []byte(openclawFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows: os.UserHomeDir reads USERPROFILE

	turns := ReadOpenClawTurns("sess-oc")
	// The longest chain is u1 → a1 → a2 → a4; the fork f1 is left out.
	if len(turns) != 4 {
		t.Fatalf("turns = %d, want 4: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || !turns[0].HasTS || turns[0].TSMillis != 1773723382812 {
		t.Fatalf("user turn = %+v", turns[0])
	}
	ub, _ := turns[0].Raw["blocks"].([]map[string]any)
	if len(ub) != 1 || ub[0]["text"] != "Hello" {
		t.Fatalf("user blocks = %+v (the timestamp prefix must be stripped)", ub)
	}

	ab, _ := turns[1].Raw["blocks"].([]map[string]any)
	if len(ab) != 2 || ab[0]["text"] != "想一想。" || ab[1]["text"] != "Hi there!" {
		t.Fatalf("assistant blocks = %+v", ab)
	}

	if turns[2].Role != "user" {
		t.Fatalf("second user turn = %+v", turns[2])
	}
	ub2, _ := turns[2].Raw["blocks"].([]map[string]any)
	if len(ub2) != 1 || ub2[0]["text"] != "再跑一次" {
		t.Fatalf("second user blocks = %+v (the working-directory notice must be stripped)", ub2)
	}

	ab2, _ := turns[3].Raw["blocks"].([]map[string]any)
	if len(ab2) != 2 || ab2[0]["type"] != "tool_use" || ab2[0]["tool_name"] != "read" || ab2[1]["text"] != "好的。" {
		t.Fatalf("second assistant blocks = %+v", ab2)
	}
	for _, tk := range turns {
		if b, _ := tk.Raw["blocks"].([]map[string]any); len(b) == 1 && b[0]["text"] == "forked aside" {
			t.Fatalf("the shorter fork leaked into the chain: %+v", turns)
		}
	}
}

func TestOpenClawStripsSenderBlock(t *testing.T) {
	in := "Sender (untrusted metadata):\n```json\n{\"id\":\"x\"}\n```\n[Tue 2026-03-17 12:56 GMT+8] real prompt"
	if got := openclawUserText([]map[string]any{{"type": "text", "text": in}}); got != "real prompt" {
		t.Fatalf("stripped = %q", got)
	}
}

func TestReadOpenClawTurnsUnknownSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	if got := ReadOpenClawTurns("missing"); got != nil {
		t.Fatalf("unknown session = %+v", got)
	}
}
