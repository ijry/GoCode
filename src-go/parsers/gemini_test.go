package parsers

import (
	"os"
	"path/filepath"
	"testing"
)

// writeGeminiChat lays out a Gemini chats tree under a temp GEMINI_CLI_HOME.
func writeGeminiChat(t *testing.T, project, name, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GEMINI_CLI_HOME", home)
	dir := filepath.Join(home, ".gemini", "tmp", project, "chats")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadGeminiTurnsJSONL(t *testing.T) {
	const sid = "37d8ba66-b276-4acb-9a4f-53aba840210f"
	writeGeminiChat(t, "demo", "session-2026-10-20T10-00-37d8ba66.jsonl", `{"sessionId":"`+sid+`","projectHash":"abc","kind":"main"}
{"$set":{"messages":[{"id":"u1","timestamp":"2026-10-20T10:00:00Z","type":"user","content":[{"text":"<session_context>\nThis is the Gemini CLI."}]}]}}
{"$set":{"messages":[{"id":"u1","timestamp":"2026-10-20T10:00:00Z","type":"user","content":[{"text":"<session_context>\nThis is the Gemini CLI."}]},{"id":"u2","timestamp":"2026-10-20T10:00:01Z","type":"user","content":[{"text":"list the files"}]}]}}
{"id":"a1","timestamp":"2026-10-20T10:00:02Z","type":"gemini","content":"Here they are","thoughts":[{"subject":"Plan","description":"list dir"}],"toolCalls":[{"id":"t1","displayName":"list_directory","args":{"path":"."}}]}
`)

	turns := ReadGeminiTurns(sid)
	if len(turns) != 2 {
		t.Fatalf("turns = %d, want 2 (session_context skipped)", len(turns))
	}
	if turns[0].Role != "user" {
		t.Fatalf("turn 0 role = %q", turns[0].Role)
	}
	blocks, _ := turns[0].Raw["blocks"].([]map[string]any)
	if len(blocks) != 1 || blocks[0]["text"] != "list the files" {
		t.Fatalf("user blocks = %v", turns[0].Raw["blocks"])
	}
	if turns[1].Role != "assistant" {
		t.Fatalf("turn 1 role = %q", turns[1].Role)
	}
	// Thinking, text, then the tool call.
	blocks, _ = turns[1].Raw["blocks"].([]map[string]any)
	if len(blocks) != 3 {
		t.Fatalf("assistant blocks = %v, want 3", turns[1].Raw["blocks"])
	}
	if blocks[0]["text"] != "Plan: list dir" || blocks[1]["text"] != "Here they are" {
		t.Fatalf("assistant text blocks = %v", blocks)
	}
	if blocks[2]["type"] != "tool_use" || blocks[2]["tool_name"] != "list_directory" {
		t.Fatalf("tool block = %v", blocks[2])
	}
}

func TestReadGeminiTurnsSingleObject(t *testing.T) {
	const sid = "aaaaaaaa-1111-2222-3333-444444444444"
	writeGeminiChat(t, "demo2", "session-2026-10-20T11-00-aaaaaaaa.json", `{"sessionId":"`+sid+`","projectHash":"abc","messages":[
{"id":"u1","timestamp":"2026-10-20T11:00:00Z","type":"user","content":[{"text":"hi"}]},
{"id":"a1","timestamp":"2026-10-20T11:00:01Z","type":"gemini","content":"hello"}]}`)

	turns := ReadGeminiTurns(sid)
	if len(turns) != 2 || turns[0].Role != "user" || turns[1].Role != "assistant" {
		t.Fatalf("turns = %v", turns)
	}
	if !turns[1].HasTS {
		t.Fatal("timestamp lost")
	}

	if got := ReadGeminiTurns("no-such-session"); len(got) != 0 {
		t.Fatalf("unknown session returned %d turns", len(got))
	}
	if got := ReadGeminiTurns("../escape"); len(got) != 0 {
		t.Fatalf("unsafe id returned %d turns", len(got))
	}
}
