package parsers

// hermes_summary_test.go covers the import picker's Hermes lister: one query over
// `sessions`, the cwd falling back to `model_config` JSON, archived/empty/child
// sessions skipped, and epoch-second timestamps.

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestListHermesConversations(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, model TEXT, model_config TEXT, cwd TEXT,
			title TEXT, started_at REAL, ended_at REAL, parent_session_id TEXT, archived INTEGER)`,
		`CREATE TABLE messages (id INTEGER PRIMARY KEY, session_id TEXT, role TEXT, active INTEGER)`,
		// cwd column set: the simple case.
		`INSERT INTO sessions VALUES ('s1','hermes-4','{}','/home/u/proj','修复导出',1783000000.5,1783000600.5,NULL,0)`,
		// cwd NULL: it lives in model_config JSON instead.
		`INSERT INTO sessions VALUES ('s2','hermes-4','{"cwd":"/home/u/other"}',NULL,'',1782000000,1782000100,NULL,0)`,
		// Archived: hidden.
		`INSERT INTO sessions VALUES ('s3','hermes-4','{}','/home/u/proj','old',1781000000,1781000100,NULL,1)`,
		// Child session: belongs to its parent.
		`INSERT INTO sessions VALUES ('s4','hermes-4','{}','/home/u/proj','child',1780000000,1780000100,'s1',0)`,
		// No real messages: skipped.
		`INSERT INTO sessions VALUES ('s5','hermes-4','{}','/home/u/proj','empty',1779000000,1779000100,NULL,0)`,
		`INSERT INTO messages (session_id, role, active) VALUES ('s1','user',1), ('s1','system',1), ('s1','assistant',1)`,
		`INSERT INTO messages (session_id, role, active) VALUES ('s2','user',1)`,
		`INSERT INTO messages (session_id, role, active) VALUES ('s3','user',1)`,
		`INSERT INTO messages (session_id, role, active) VALUES ('s4','user',1)`,
		`INSERT INTO messages (session_id, role, active) VALUES ('s5','system',1)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HERMES_HOME", home)

	got := ListHermesConversations()
	if len(got) != 2 {
		t.Fatalf("summaries = %d, want 2 (archived/child/empty skipped): %+v", len(got), got)
	}
	// Newest first.
	if got[0].ID != "s1" || got[1].ID != "s2" {
		t.Fatalf("order = %q, %q", got[0].ID, got[1].ID)
	}
	s1 := got[0]
	if s1.FolderPath != "/home/u/proj" || s1.Title != "修复导出" || s1.Model != "hermes-4" {
		t.Errorf("session = %+v", s1)
	}
	// The system row does not count.
	if s1.MessageCount != 2 {
		t.Errorf("message count = %d, want 2", s1.MessageCount)
	}
	if s1.StartedAt != "2026-07-02T13:46:40Z" {
		t.Errorf("started = %q", s1.StartedAt)
	}
	// s2's cwd came out of model_config.
	if got[1].FolderPath != "/home/u/other" || got[1].FolderName != "other" {
		t.Errorf("model_config cwd = %q / %q", got[1].FolderPath, got[1].FolderName)
	}
}

func TestListHermesConversationsMissingDB(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HERMES_HOME", home)
	if got := ListHermesConversations(); len(got) != 0 {
		t.Fatalf("missing db = %+v", got)
	}
}
