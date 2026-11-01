package handlers

// Internal coverage for the ported turn-window algorithm (unexported), with a
// synthetic turn list that loadConversationTurns cannot yet produce over HTTP.

import (
	"os"
	"path/filepath"
	"testing"

	"gocode/src-go/db"
)

func tt(role string, ms int64) convTurn {
	return convTurn{role: role, tsMillis: ms, hasTS: true, raw: map[string]any{"role": role}}
}

func TestPrefixFingerprintAndBounds(t *testing.T) {
	// empty prefix = FNV offset basis.
	if got := prefixFingerprint(nil); got != "cbf29ce484222325" {
		t.Fatalf("empty fingerprint = %s", got)
	}
	// deterministic + order-sensitive.
	a := []convTurn{tt("user", 1000), tt("assistant", 2000)}
	b := []convTurn{tt("assistant", 2000), tt("user", 1000)}
	if prefixFingerprint(a) == prefixFingerprint(b) {
		t.Fatalf("fingerprint not order-sensitive")
	}
	if prefixFingerprint(a) != prefixFingerprint([]convTurn{tt("user", 1000), tt("assistant", 2000)}) {
		t.Fatalf("fingerprint not deterministic")
	}

	// round-align walks a window start back to a user boundary.
	turns := []convTurn{tt("user", 1), tt("assistant", 2), tt("assistant", 3), tt("user", 4), tt("assistant", 5)}
	if got := roundAlignBackward(turns, 2); got != 0 {
		t.Fatalf("roundAlignBackward(2) = %d, want 0", got)
	}
	if got := roundAlignBackward(turns, 4); got != 3 {
		t.Fatalf("roundAlignBackward(4) = %d, want 3 (already user)", got)
	}

	// page bounds: end clamps to total, start aligns back to a user turn.
	start, end := resolvePageBounds(turns, 100, 2)
	if end != 5 {
		t.Fatalf("end = %d, want 5 (clamped to total)", end)
	}
	if turns[start].role != "user" && start != 0 {
		t.Fatalf("page start %d is not a user boundary", start)
	}

	// window meta: assistant_before counts assistants in the prefix.");
	meta := computeWindowMeta(turns, 4)
	if meta.total != 5 || meta.offset != 4 {
		t.Fatalf("meta offset/total = %d/%d", meta.offset, meta.total)
	}
	if meta.assistantBefore != 2 {
		t.Fatalf("assistantBefore = %d, want 2", meta.assistantBefore)
	}
	if meta.uncoveredMaxTS == nil {
		t.Fatalf("uncoveredMaxTS should be set for a non-empty prefix")
	}
}

// TestTranscriptReadersRegistered locks the agent→reader table: every agent the
// app offers a transcript for is wired, and a reader handed an unknown id
// returns nothing instead of panicking or reading outside its store.
func TestTranscriptReadersRegistered(t *testing.T) {
	for _, agent := range []string{
		"claude_code", "claude", "codex", "gemini", "kimi_code", "code_buddy",
		"cline", "deepseek", "hermes", "qoder", "open_claw", "pi", "grok", "open_code", "antigravity", "cursor",
	} {
		if transcriptReaders[agent] == nil {
			t.Errorf("no transcript reader for %q", agent)
		}
	}
	for agent, read := range transcriptReaders {
		if got := read("__no_such_session__"); len(got) != 0 {
			t.Errorf("%s: unknown id returned %d turns", agent, len(got))
		}
	}
}

// TestLoadConversationTurnsReadsRealTranscript locks the whole chain the UI
// depends on: a conversation row's agent_type + external_id → that agent's
// reader → convTurn.raw["blocks"] in the shape viewHistoricalTurn renders
// (`type:"text"` with `text`, `type:"tool_use"` with `tool_name`).
func TestLoadConversationTurnsReadsRealTranscript(t *testing.T) {
	// A Claude-shaped transcript in a temp config dir.
	cfg := t.TempDir()
	proj := filepath.Join(cfg, "projects", "-tmp-demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	const sid = "sess-e2e"
	body := `{"type":"user","uuid":"u1","timestamp":"2026-10-20T10:00:01Z","message":{"content":"跑一下构建"}}
{"type":"assistant","uuid":"a1","timestamp":"2026-10-20T10:00:02Z","message":{"content":[{"type":"thinking","thinking":"先看看。"},{"type":"text","text":"好的。"},{"type":"tool_use","id":"call_1","name":"Bash","input":{"command":"pnpm build"}}]}}
`
	if err := os.WriteFile(filepath.Join(proj, sid+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)

	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Conn.Close()
	now := "2026-10-20T10:00:00Z"
	res, err := d.Conn.Exec(
		`INSERT INTO conversation (folder_id, title, agent_type, status, external_id, message_count, created_at, updated_at)
		 VALUES (1,'T','claude_code','idle',?,0,?,?)`, sid, now, now)
	if err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	convID, _ := res.LastInsertId()

	turns := loadConversationTurns(d, convID)
	if len(turns) != 2 {
		t.Fatalf("turns = %d, want 2: %+v", len(turns), turns)
	}
	if turns[0].role != "user" {
		t.Fatalf("first turn role = %q", turns[0].role)
	}
	userBlocks, _ := turns[0].raw["blocks"].([]map[string]any)
	if len(userBlocks) != 1 || userBlocks[0]["text"] != "跑一下构建" {
		t.Fatalf("user blocks = %+v", userBlocks)
	}
	asstBlocks, _ := turns[1].raw["blocks"].([]map[string]any)
	if len(asstBlocks) != 3 {
		t.Fatalf("assistant blocks = %+v", asstBlocks)
	}
	// The shapes viewHistoricalTurn reads: prose via `text`, cards via tool_name.
	if asstBlocks[0]["text"] != "先看看。" || asstBlocks[1]["text"] != "好的。" {
		t.Fatalf("prose blocks = %+v", asstBlocks)
	}
	if asstBlocks[2]["type"] != "tool_use" || asstBlocks[2]["tool_name"] != "Bash" {
		t.Fatalf("tool block = %+v", asstBlocks[2])
	}
	if prev, _ := asstBlocks[2]["input_preview"].(string); prev == "" {
		t.Fatalf("tool block lost its input preview: %+v", asstBlocks[2])
	}
}

// TestLoadConversationTurnsUnknownAgentKeepsEmptyHistory keeps the fallback:
// an agent with no reader yields no turns rather than an error.
func TestLoadConversationTurnsUnknownAgentKeepsEmptyHistory(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Conn.Close()
	now := "2026-10-20T10:00:00Z"
	res, err := d.Conn.Exec(
		`INSERT INTO conversation (folder_id, title, agent_type, status, external_id, message_count, created_at, updated_at)
		 VALUES (1,'T','some_new_agent','idle','x',0,?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	convID, _ := res.LastInsertId()
	if got := loadConversationTurns(d, convID); len(got) != 0 {
		t.Fatalf("unknown agent turns = %+v", got)
	}
}
