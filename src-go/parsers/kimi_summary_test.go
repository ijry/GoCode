package parsers

// kimi_summary_test.go covers the import picker's Kimi lister: the index supplies
// the id and working directory, state.json the title and times, and a session the
// index does not name is not listed.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListKimiConversations(t *testing.T) {
	home := t.TempDir()
	sess := filepath.Join(home, "sessions", "wd_proj_ab12", "k1")
	if err := os.MkdirAll(sess, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sess, "state.json"),
		[]byte(`{"title":"修复导出","createdAt":"2026-05-01T10:00:00Z","updatedAt":"2026-05-01T11:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	index := `{"sessionId":"k1","sessionDir":"sessions/wd_proj_ab12/k1","workDir":"/home/u/proj"}
{"sessionId":"k2","sessionDir":"sessions/wd_proj_ab12/k2","workDir":"/home/u/proj"}
`
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KIMI_CODE_HOME", home)

	got := ListKimiConversations()
	// k2 is indexed but its directory/state.json is missing: still listed (the
	// history is what matters), just without a title.
	if len(got) != 2 {
		t.Fatalf("summaries = %+v", got)
	}
	byID := map[string]ConversationSummary{}
	for _, s := range got {
		byID[s.ID] = s
	}
	k1 := byID["k1"]
	if k1.AgentType != "kimi_code" || k1.FolderPath != "/home/u/proj" || k1.FolderName != "proj" {
		t.Errorf("session = %+v", k1)
	}
	if k1.Title != "修复导出" || k1.StartedAt != "2026-05-01T10:00:00Z" || k1.EndedAt != "2026-05-01T11:00:00Z" {
		t.Errorf("state = %+v", k1)
	}
	if k2 := byID["k2"]; k2.Title != "" || k2.FolderName != "proj" {
		t.Errorf("state-less session = %+v", k2)
	}
}

func TestListKimiConversationsMissingIndex(t *testing.T) {
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	if got := ListKimiConversations(); len(got) != 0 {
		t.Fatalf("missing index = %+v", got)
	}
}
