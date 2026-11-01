package parsers

// pi_test.go covers the pi reader: locating the session by its header id,
// the per-cwd bucket layout, Claude-ish content blocks (note `thinking` and
// object `arguments`), and the sessions-dir precedence.

import (
	"os"
	"path/filepath"
	"testing"
)

const piFixture = `{"type":"session","version":3,"id":"0f3c1d2e-1111-2222-3333-444455556666","timestamp":"2026-06-27T10:00:00.000Z","cwd":"/Users/demo/my-app"}
{"type":"session_info","id":"i1","parentId":null,"timestamp":"2026-06-27T10:00:00.100Z","name":"Build the app"}
{"type":"message","id":"m1","parentId":"i1","timestamp":"2026-06-27T10:00:01.000Z","message":{"role":"user","content":"run pnpm build"}}
{"type":"model_change","id":"mc1","parentId":"m1","timestamp":"2026-06-27T10:00:01.500Z","provider":"anthropic","modelId":"x"}
{"type":"message","id":"m2","parentId":"mc1","timestamp":"2026-06-27T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"check the build first","thinkingSignature":"sig"},{"type":"text","text":"Running the build now."},{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"pnpm build"}}]}}
{"type":"message","id":"m3","parentId":"m2","timestamp":"2026-06-27T10:00:09.000Z","message":{"role":"toolResult","toolCallId":"call_1","toolName":"bash","content":[{"type":"text","text":"Compiled successfully"}],"isError":false}}
{"type":"message","id":"m4","parentId":"m3","timestamp":"2026-06-27T10:00:10.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Build succeeded."}]}}
`

func TestReadPiTurns(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "--Users-demo-my-app--")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-06-27T10-00-00_0f3c1d2e.jsonl"), []byte(piFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", base)

	turns := ReadPiTurns("0f3c1d2e-1111-2222-3333-444455556666")
	if len(turns) != 2 {
		t.Fatalf("turns = %d, want 2: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || !turns[0].HasTS || turns[0].TSMillis != 1782554401000 {
		t.Fatalf("user turn = %+v", turns[0])
	}
	ub, _ := turns[0].Raw["blocks"].([]map[string]any)
	if len(ub) != 1 || ub[0]["text"] != "run pnpm build" {
		t.Fatalf("user blocks = %+v", ub)
	}

	ab, _ := turns[1].Raw["blocks"].([]map[string]any)
	// thinking + text + toolCall, then the later assistant message folds into the
	// same turn; the toolResult row is dropped.
	if len(ab) != 4 {
		t.Fatalf("assistant blocks = %d, want 4: %+v", len(ab), ab)
	}
	if ab[0]["text"] != "check the build first" || ab[1]["text"] != "Running the build now." {
		t.Fatalf("text blocks = %+v", ab)
	}
	if ab[3]["text"] != "Build succeeded." {
		t.Fatalf("trailing assistant text = %+v", ab[3])
	}
	if ab[2]["type"] != "tool_use" || ab[2]["tool_name"] != "bash" || ab[2]["tool_use_id"] != "call_1" {
		t.Fatalf("tool block = %+v", ab[2])
	}
	if prev, _ := ab[2]["input_preview"].(string); prev != `{"command":"pnpm build"}` {
		t.Fatalf("input preview = %q", prev)
	}
}

func TestPiSessionsDirPrecedence(t *testing.T) {
	agentDir := t.TempDir()
	// settings.json relocates the store — but only when the value is absolute.
	if err := os.WriteFile(filepath.Join(agentDir, "settings.json"),
		[]byte(`{"sessionDir":"`+filepath.ToSlash(t.TempDir())+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", "")
	got := piSessionsDir()
	if got == filepath.Join(agentDir, "sessions") {
		t.Fatalf("absolute sessionDir in settings.json was ignored: %s", got)
	}

	// A relative one is not ours to resolve: fall back to <agent dir>/sessions.
	if err := os.WriteFile(filepath.Join(agentDir, "settings.json"),
		[]byte(`{"sessionDir":"relative/sessions"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := piSessionsDir(); got != filepath.Join(agentDir, "sessions") {
		t.Fatalf("relative sessionDir = %s, want <agent dir>/sessions", got)
	}

	// The env var wins over both.
	envDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", envDir)
	if got := piSessionsDir(); got != envDir {
		t.Fatalf("env dir = %s, want %s", got, envDir)
	}
}

func TestReadPiTurnsUnknownSession(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	if got := ReadPiTurns("missing"); got != nil {
		t.Fatalf("unknown session = %+v", got)
	}
}
