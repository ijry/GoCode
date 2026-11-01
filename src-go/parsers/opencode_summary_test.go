package parsers

// opencode_summary_test.go covers the import picker's OpenCode lister: one query
// over the session table, sub-agent children skipped, directory and times carried
// through.

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestListOpenCodeConversations(t *testing.T) {
	data := t.TempDir()
	dir := filepath.Join(data, "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY, parent_id TEXT, directory TEXT, title TEXT,
			time_created INTEGER, time_updated INTEGER)`,
		`INSERT INTO session VALUES ('ses_a', NULL, 'D:/repos/demo', '修复登录页', 1792245419112, 1792245419112)`,
		// A sub-agent child must not become its own importable conversation.
		`INSERT INTO session VALUES ('ses_child', 'ses_a', 'D:/repos/demo', 'child', 1792245419200, 1792245419200)`,
		`INSERT INTO session VALUES ('ses_b', '', 'D:/repos/other', 'New session - 2026-10-17T13:50:00.000Z', 1792245000000, 1792245100000)`,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT, time_created INTEGER, data TEXT)`,
		`CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT, time_created INTEGER, data TEXT)`,
		`INSERT INTO message VALUES ('m1','ses_b',1792245000100,'{"role":"user"}')`,
		// The injected prose is skipped; the user's own words become the title.
		`INSERT INTO part VALUES ('p0','m1',1792245000101,'{"type":"text","text":"[reminder] plan mode","synthetic":true}')`,
		`INSERT INTO part VALUES ('p1','m1',1792245000102,'{"type":"text","text":"重构导入流程"}')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	t.Setenv("XDG_DATA_HOME", data)

	got := ListOpenCodeConversations()
	if len(got) != 2 {
		t.Fatalf("summaries = %d, want 2 (the child is skipped): %+v", len(got), got)
	}
	// Newest first.
	if got[0].ID != "ses_a" || got[1].ID != "ses_b" {
		t.Fatalf("order = %q, %q", got[0].ID, got[1].ID)
	}
	a := got[0]
	if a.AgentType != "open_code" || a.Title != "修复登录页" {
		t.Errorf("session = %+v", a)
	}
	if a.FolderPath != "D:/repos/demo" || a.FolderName != "demo" {
		t.Errorf("folder = %q / %q", a.FolderPath, a.FolderName)
	}
	if a.StartedAt != "2026-10-17T13:56:59Z" {
		t.Errorf("started = %q", a.StartedAt)
	}
	if got[1].EndedAt != "2026-10-17T13:51:40Z" {
		t.Errorf("ended = %q", got[1].EndedAt)
	}
	// A default "New session - …" title is replaced by the first real prompt.
	if got[1].Title != "重构导入流程" {
		t.Errorf("derived title = %q", got[1].Title)
	}
}

func TestListOpenCodeConversationsMissingDB(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if got := ListOpenCodeConversations(); len(got) != 0 {
		t.Fatalf("missing db = %+v", got)
	}
}
