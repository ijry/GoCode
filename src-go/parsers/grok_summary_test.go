package parsers

// grok_summary_test.go covers the import picker's Grok lister: everything comes
// from the `summary.json` sidecar (no transcript read), a spawned sub-agent is
// skipped, and a session whose sidecar is missing is still listed.

import (
	"os"
	"path/filepath"
	"testing"
)

func writeGrokSession(t *testing.T, base, group, id, summary string, withUpdates bool) {
	t.Helper()
	dir := filepath.Join(base, "sessions", group, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if withUpdates {
		if err := os.WriteFile(filepath.Join(dir, "updates.jsonl"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if summary != "" {
		if err := os.WriteFile(filepath.Join(dir, "summary.json"), []byte(summary), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListGrokConversations(t *testing.T) {
	home := t.TempDir()
	writeGrokSession(t, home, "%2Fhome%2Fu%2Fproj", "g1", `{
		"info": {"cwd": "/home/u/proj"},
		"generated_title": "修复导出",
		"session_summary": "a one-liner",
		"current_model_id": "grok-4.5",
		"head_branch": "main",
		"created_at": "2026-07-08T10:00:00+08:00",
		"updated_at": "2026-07-08T10:30:00+08:00",
		"session_kind": "primary"
	}`, true)
	// A spawned child is not a conversation.
	writeGrokSession(t, home, "%2Fhome%2Fu%2Fproj", "g2",
		`{"info":{"cwd":"/home/u/proj"},"session_kind":"subagent"}`, true)
	// No sidecar: still importable, just without metadata.
	writeGrokSession(t, home, "%2Fhome%2Fu%2Fproj", "g3", "", true)
	// No transcript: nothing to import.
	writeGrokSession(t, home, "%2Fhome%2Fu%2Fproj", "g4", `{"info":{"cwd":"/x"}}`, false)
	t.Setenv("GROK_HOME", home)

	got := ListGrokConversations()
	if len(got) != 2 {
		t.Fatalf("summaries = %d, want 2 (subagent and empty dir skipped): %+v", len(got), got)
	}
	byID := map[string]ConversationSummary{}
	for _, s := range got {
		byID[s.ID] = s
	}
	g1, ok := byID["g1"]
	if !ok {
		t.Fatalf("g1 missing: %+v", got)
	}
	if g1.FolderPath != "/home/u/proj" || g1.FolderName != "proj" {
		t.Errorf("folder = %q / %q", g1.FolderPath, g1.FolderName)
	}
	// The generated title wins over the summary line.
	if g1.Title != "修复导出" || g1.Model != "grok-4.5" || g1.GitBranch != "main" {
		t.Errorf("metadata = %+v", g1)
	}
	// Timestamps normalize to UTC.
	if g1.StartedAt != "2026-07-08T02:00:00Z" || g1.EndedAt != "2026-07-08T02:30:00Z" {
		t.Errorf("times = %q / %q", g1.StartedAt, g1.EndedAt)
	}
	if g3, ok := byID["g3"]; !ok || g3.Title != "" {
		t.Errorf("sidecar-less session = %+v", g3)
	}
}
