package parsers

// gemini_summary_test.go covers the import picker's Gemini lister: the cwd comes
// from the project's `.project_root` sidecar (the chat header only has a hash),
// the title from the first real user message, and sub-agent chats are skipped.

import (
	"os"
	"path/filepath"
	"testing"
)

func writeGeminiChatAt(t *testing.T, root, project, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "tmp", project, "chats")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListGeminiConversations(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".gemini")
	if err := os.MkdirAll(filepath.Join(root, "tmp", "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The sidecar is the only place the working directory survives.
	if err := os.WriteFile(filepath.Join(root, "tmp", "proj", ".project_root"),
		[]byte("/home/u/proj\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeGeminiChatAt(t, root, "proj", "session-2026-01-01T10-00-abcd1234.jsonl",
		// One record per line: the reader is a line scanner, so a pretty-printed
		// `$set` object would be split and dropped.
		`{"sessionId":"abcd1234-0000-1111-2222-333333333333","startTime":"2026-01-01T10:00:00Z","lastUpdated":"2026-01-01T10:05:00Z","kind":"main"}
{"$set":{"messages":[{"type":"user","content":[{"text":"<session_context>\nThis is the Gemini CLI"}]},{"type":"user","content":[{"text":"帮我改登录页\n第二行"}]}]}}`)
	// A sub-agent run is not a top-level conversation.
	writeGeminiChatAt(t, root, "proj", "session-2026-01-01T11-00-dead5678.jsonl",
		`{"sessionId":"dead5678-0000-1111-2222-333333333333","startTime":"2026-01-01T11:00:00Z","kind":"subagent"}
{"$set":{"messages":[{"type":"user","content":[{"text":"nested"}]}]}}`)
	t.Setenv("GEMINI_CLI_HOME", home)

	got := ListGeminiConversations()
	if len(got) != 1 {
		t.Fatalf("summaries = %d, want 1 (the sub-agent is skipped): %+v", len(got), got)
	}
	s := got[0]
	if s.ID != "abcd1234-0000-1111-2222-333333333333" || s.AgentType != "gemini" {
		t.Errorf("session = %+v", s)
	}
	if s.FolderPath != "/home/u/proj" || s.FolderName != "proj" {
		t.Errorf("folder = %q / %q", s.FolderPath, s.FolderName)
	}
	if s.Title != "帮我改登录页" {
		t.Errorf("title = %q (the session_context preamble must not win)", s.Title)
	}
	if s.StartedAt != "2026-01-01T10:00:00Z" || s.EndedAt != "2026-01-01T10:05:00Z" {
		t.Errorf("times = %q / %q", s.StartedAt, s.EndedAt)
	}
}

func TestListGeminiConversationsMissingRoot(t *testing.T) {
	t.Setenv("GEMINI_CLI_HOME", t.TempDir())
	if got := ListGeminiConversations(); len(got) != 0 {
		t.Fatalf("missing root = %+v", got)
	}
}
