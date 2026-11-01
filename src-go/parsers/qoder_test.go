package parsers

// qoder_test.go covers the Qoder reader: Qoder stores Claude-shaped JSONL, so
// the test only has to prove the directory resolution and the reuse of the
// Claude line parser.

import (
	"os"
	"path/filepath"
	"testing"
)

const qoderFixture = `{"type":"user","uuid":"u1","timestamp":"2026-10-27T03:00:00Z","message":{"content":"读一下 /tmp/target.txt"}}
{"type":"assistant","uuid":"a1","timestamp":"2026-10-27T03:00:01Z","message":{"content":[{"type":"thinking","thinking":"先读文件。"},{"type":"tool_use","id":"call_1","name":"read","input":{"file_path":"/tmp/target.txt"}}]}}
{"type":"assistant","uuid":"a2","timestamp":"2026-10-27T03:00:02Z","message":{"content":[{"type":"text","text":"文件内容是 hello。"}]}}
`

func TestReadQoderTurns(t *testing.T) {
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "projects", "-Users-demo-app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess-q.jsonl"), []byte(qoderFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QODER_CONFIG_DIR", cfg)

	turns := ReadQoderTurns("sess-q")
	if len(turns) != 3 {
		t.Fatalf("turns = %d, want 3: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || !turns[0].HasTS {
		t.Fatalf("user turn = %+v", turns[0])
	}
	ab, _ := turns[1].Raw["blocks"].([]map[string]any)
	if len(ab) != 2 || ab[0]["text"] != "先读文件。" || ab[1]["tool_name"] != "read" {
		t.Fatalf("assistant blocks = %+v", ab)
	}
}

func TestReadQoderTurnsFallsBackToGeminiHome(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".qoder", "projects", "enc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess-g.jsonl"), []byte(qoderFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QODER_CONFIG_DIR", "")
	t.Setenv("QODER_CLI_HOME", "")
	t.Setenv("GEMINI_CLI_HOME", home)

	if turns := ReadQoderTurns("sess-g"); len(turns) != 3 {
		t.Fatalf("gemini-home fallback turns = %d, want 3", len(turns))
	}
}

func TestReadQoderTurnsUnknownSession(t *testing.T) {
	t.Setenv("QODER_CONFIG_DIR", t.TempDir())
	if got := ReadQoderTurns("missing"); got != nil {
		t.Fatalf("unknown session = %+v", got)
	}
}
