package parsers

// cursor_summary_test.go covers the import picker's Cursor lister. The store is
// built blob by blob like cursor_test.go: meta key "0" carries the JSON head, the
// sidecar carries the ACP cwd, and the DAG root carries the workspace refs, the
// turn count and the clocks.

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// newCursorStore builds `<dir>/store.db` with the given meta JSON and root blob.
func newCursorStore(t *testing.T, dir string, meta map[string]any, root []byte, blobs map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "store.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE blobs (id TEXT PRIMARY KEY, data BLOB)`,
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	// meta value is hex-encoded UTF-8 JSON.
	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES ('0', ?)`, hex.EncodeToString(raw)); err != nil {
		t.Fatal(err)
	}
	if root != nil {
		id := hex.EncodeToString([]byte{0xf0})
		if _, err := db.Exec(`INSERT INTO blobs (id, data) VALUES (?, ?)`, id, root); err != nil {
			t.Fatal(err)
		}
	}
	for id, data := range blobs {
		if _, err := db.Exec(`INSERT INTO blobs (id, data) VALUES (?, ?)`, id, data); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListCursorConversations(t *testing.T) {
	home := t.TempDir()

	// ACP session: cwd comes from the sidecar, title too.
	acp := filepath.Join(home, "acp-sessions", "sess-1")
	idUser := []byte{0xb1}
	idTurn := []byte{0xb2}
	root := pbConcat(
		pbString(8, string(idTurn)),
		pbInt(26, 1792245419112),
		pbMessage(14, pbConcat(pbInt(1, 60000), pbInt(2, 1792245419112))),
		pbMessage(21, pbConcat(pbString(1, "/home/u/fromdag"), pbString(2, "main"))),
	)
	newCursorStore(t, acp, map[string]any{
		"latestRootBlobId": hex.EncodeToString([]byte{0xf0}),
		"name":             "export work",
		"createdAt":        1792245000000,
		"lastUsedModel":    "claude-4.5-sonnet",
	}, root, map[string][]byte{
		hex.EncodeToString(idUser): pbString(1, "改一下导出"),
		hex.EncodeToString(idTurn): pbMessage(1, pbString(1, string(idUser))),
	})
	sidecar := `{"schemaVersion":1,"cwd":"/home/u/proj","title":"导出按钮改造"}`
	if err := os.WriteFile(filepath.Join(acp, "meta.json"), []byte(sidecar), 0o644); err != nil {
		t.Fatal(err)
	}

	// Chats layout: no sidecar, so the DAG's workspace refs must supply the cwd.
	chatDir := filepath.Join(home, "chats", "3f2a", "sess-2")
	newCursorStore(t, chatDir, map[string]any{
		"latestRootBlobId": hex.EncodeToString([]byte{0xf0}),
		"createdAt":        1792000000000,
	}, pbConcat(
		pbString(9, "file:///home/u/other"),
	), nil)

	// A spawned sub-agent is not a conversation.
	newCursorStore(t, filepath.Join(home, "chats", "3f2a", "sess-3"), map[string]any{
		"latestRootBlobId": hex.EncodeToString([]byte{0xf0}),
		"isSubagent":       true,
	}, pbConcat(pbString(9, "/home/u/proj")), nil)

	// No root blob: nothing to import.
	newCursorStore(t, filepath.Join(home, "chats", "3f2a", "sess-4"), map[string]any{
		"createdAt": 1791000000000,
	}, nil, nil)

	t.Setenv("CURSOR_CONFIG_DIR", home)

	got := ListCursorConversations()
	if len(got) != 2 {
		t.Fatalf("summaries = %d, want 2 (subagent and root-less skipped): %+v", len(got), got)
	}
	byID := map[string]ConversationSummary{}
	for _, s := range got {
		byID[s.ID] = s
	}
	a, ok := byID["sess-1"]
	if !ok {
		t.Fatalf("sess-1 missing: %+v", got)
	}
	if a.AgentType != "cursor" {
		t.Errorf("agent type = %q", a.AgentType)
	}
	// The sidecar's cwd wins over the DAG's repo path.
	if a.FolderPath != "/home/u/proj" || a.FolderName != "proj" {
		t.Errorf("folder = %q / %q", a.FolderPath, a.FolderName)
	}
	if a.Title != "导出按钮改造" || a.Model != "claude-4.5-sonnet" {
		t.Errorf("title/model = %q / %q", a.Title, a.Model)
	}
	if a.GitBranch != "main" {
		t.Errorf("branch = %q", a.GitBranch)
	}
	// createdAt is the session's start; the last turn timing closes it.
	if a.StartedAt != "2026-10-17T13:50:00Z" || a.EndedAt != "2026-10-17T13:57:59Z" {
		t.Errorf("times = %q / %q", a.StartedAt, a.EndedAt)
	}
	if a.MessageCount != 2 {
		t.Errorf("message count = %d, want 2 (one turn)", a.MessageCount)
	}

	b, ok := byID["sess-2"]
	if !ok {
		t.Fatalf("sess-2 missing: %+v", got)
	}
	// The chats bucket is an md5, so only the DAG can name the folder.
	if b.FolderPath != "/home/u/other" || b.FolderName != "other" {
		t.Errorf("dag folder = %q / %q", b.FolderPath, b.FolderName)
	}
}

// TestCursorSummaryFallsBackToFirstPrompt keeps a session with no title record
// importable under a readable name.
func TestCursorSummaryFallsBackToFirstPrompt(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "chats", "aa", "sess-p")
	idUser := []byte{0xc1}
	idTurn := []byte{0xc2}
	newCursorStore(t, dir, map[string]any{
		"latestRootBlobId": hex.EncodeToString([]byte{0xf0}),
		"createdAt":        1792245000000,
	}, pbConcat(
		pbString(8, string(idTurn)),
		pbString(9, "/home/u/proj"),
	), map[string][]byte{
		hex.EncodeToString(idUser): pbString(1, "为什么构建失败\n第二行"),
		hex.EncodeToString(idTurn): pbMessage(1, pbString(1, string(idUser))),
	})
	t.Setenv("CURSOR_CONFIG_DIR", home)

	got := ListCursorConversations()
	if len(got) != 1 {
		t.Fatalf("summaries = %+v", got)
	}
	if got[0].Title != "为什么构建失败" {
		t.Errorf("title = %q (first prompt, first line only)", got[0].Title)
	}
}
