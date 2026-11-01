package parsers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// writeCodexRollout lays out a Codex sessions tree under a temp CODEX_HOME.
func writeCodexRollout(t *testing.T, sessionID, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dir := filepath.Join(home, "sessions", "2026", "10", "20")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-10-20T10-00-00-"+sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadCodexTurns(t *testing.T) {
	const sid = "019d6adf-6880-76a0-aeaf-63d2f9b7f358"
	writeCodexRollout(t, sid, `{"timestamp":"2026-10-20T10:00:00Z","type":"session_meta","payload":{"session_id":"`+sid+`"}}
{"timestamp":"2026-10-20T10:00:01Z","type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"<permissions instructions>"}]}}
{"timestamp":"2026-10-20T10:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions for /repo\n\n<INSTRUCTIONS>be nice"}]}}
{"timestamp":"2026-10-20T10:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"fix the build"}]}}
{"timestamp":"2026-10-20T10:00:03Z","type":"response_item","payload":{"type":"reasoning","summary":[]}}
{"timestamp":"2026-10-20T10:00:04Z","type":"response_item","payload":{"type":"function_call","name":"shell_command","arguments":"{}"}}
{"timestamp":"2026-10-20T10:00:05Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Looking at the compiler error."}]}}
{"timestamp":"2026-10-20T10:00:06Z","type":"event_msg","payload":{"type":"token_count"}}
{"timestamp":"2026-10-20T10:00:07Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"thanks"}]}}
`)

	turns := ReadCodexTurns(sid)
	if len(turns) != 3 {
		t.Fatalf("turns = %d, want 3 (user, assistant, user)", len(turns))
	}
	wantRoles := []string{"user", "assistant", "user"}
	wantText := []string{"fix the build", "Looking at the compiler error.", "thanks"}
	for i, turn := range turns {
		if turn.Role != wantRoles[i] {
			t.Fatalf("turn %d role = %q, want %q", i, turn.Role, wantRoles[i])
		}
		blocks, _ := turn.Raw["blocks"].([]map[string]any)
		if len(blocks) != 1 || blocks[0]["text"] != wantText[i] {
			t.Fatalf("turn %d blocks = %v, want text %q", i, turn.Raw["blocks"], wantText[i])
		}
		if !turn.HasTS {
			t.Fatalf("turn %d lost its timestamp", i)
		}
	}

	// Unknown session ids and unsafe ids are empty, not errors.
	if got := ReadCodexTurns("does-not-exist"); len(got) != 0 {
		t.Fatalf("unknown session returned %d turns", len(got))
	}
	if got := ReadCodexTurns("../escape"); len(got) != 0 {
		t.Fatalf("unsafe id returned %d turns", len(got))
	}
}

// TestReadCodexCompressedRollout locks the `.jsonl.zst` path: current codex
// writes rollouts zstd-compressed, and on a real machine they are the
// overwhelming majority (1273 vs 23 here), so a reader that only matched
// `.jsonl` showed almost no history.
func TestReadCodexCompressedRollout(t *testing.T) {
	const sid = "019d6adf-6880-76a0-aeaf-63d2f9b7f358"
	body := `{"timestamp":"2026-10-20T10:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"fix the build"}]}}
{"timestamp":"2026-10-20T10:00:05Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Looking at it."}]}}
`
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dir := filepath.Join(home, "sessions", "2026", "10", "20")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-10-20T10-00-00-"+sid+".jsonl.zst")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	turns := ReadCodexTurns(sid)
	if len(turns) != 2 {
		t.Fatalf("turns = %d, want 2 from a .jsonl.zst rollout: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || turns[1].Role != "assistant" {
		t.Fatalf("roles = %s/%s", turns[0].Role, turns[1].Role)
	}
}
