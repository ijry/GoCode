package parsers

// antigravity_summary_test.go covers the import picker's Antigravity lister: cwd
// from the `.meta` sidecar, title from the first user prompt, clock and model from
// the step metadata, and a pre-created schema-less DB read as an empty session.

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// antigravityStepPayload builds a `gemini_coder.Step` with a user prompt.
func antigravityUserStep(t *testing.T, secs int64, text string) []byte {
	t.Helper()
	meta := pbMessage(1, pbInt(1, secs)) // created_at
	return pbConcat(
		pbMessage(5, meta),
		pbMessage(19, pbString(1, text)),
	)
}

func TestListAntigravityConversations(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "antigravity-acp", "conversations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDB := func(id string, steps ...[]byte) {
		t.Helper()
		db, err := sql.Open("sqlite", filepath.Join(dir, id+".db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if _, err := db.Exec(`CREATE TABLE steps (idx INTEGER PRIMARY KEY, step_payload BLOB)`); err != nil {
			t.Fatal(err)
		}
		for i, payload := range steps {
			if _, err := db.Exec(`INSERT INTO steps (idx, step_payload) VALUES (?, ?)`, i, payload); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Step 0 has no prompt (a planner step); step 1 carries the first prompt.
	writeDB("agy1",
		pbMessage(20, pbString(1, "先看看仓库")),
		antigravityUserStep(t, 1792245419, "修一下导出按钮\n第二行"),
		pbMessage(5, pbConcat(
			pbMessage(1, pbInt(1, 1792245500)),
			pbMessage(24, pbConcat(pbString(8, "gemini-3.7-flash-high"), pbString(20, "Gemini 3.7 Flash"))),
		)),
	)
	if err := os.WriteFile(filepath.Join(dir, "agy1.meta"),
		[]byte(`{"cwd":"/home/u/proj","mode_id":"default"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// No sidecar: cwd stays unknown rather than defaulting to the Gemini home.
	writeDB("agy2", antigravityUserStep(t, 1792000000, "另一个会话"))
	// A pre-created DB with no schema yet is not a conversation.
	empty, err := sql.Open("sqlite", filepath.Join(dir, "agy3.db"))
	if err != nil {
		t.Fatal(err)
	}
	empty.Close()

	t.Setenv("GEMINI_HOME", home)

	got := ListAntigravityConversations()
	if len(got) != 2 {
		t.Fatalf("summaries = %d, want 2 (schema-less db skipped): %+v", len(got), got)
	}
	if got[0].ID != "agy1" || got[1].ID != "agy2" {
		t.Fatalf("order = %q, %q", got[0].ID, got[1].ID)
	}
	a := got[0]
	if a.AgentType != "antigravity" || a.FolderPath != "/home/u/proj" || a.FolderName != "proj" {
		t.Errorf("session = %+v", a)
	}
	if a.Title != "修一下导出按钮" {
		t.Errorf("title = %q (first prompt, first line)", a.Title)
	}
	if a.Model != "Gemini 3.7 Flash" {
		t.Errorf("model = %q (display_name wins)", a.Model)
	}
	if a.StartedAt != "2026-10-17T13:56:59Z" || a.EndedAt != "2026-10-17T13:58:20Z" {
		t.Errorf("times = %q / %q", a.StartedAt, a.EndedAt)
	}
	if a.MessageCount != 3 {
		t.Errorf("message count = %d, want 3 steps", a.MessageCount)
	}
	if got[1].FolderPath != "" {
		t.Errorf("cwd-less session got a folder: %q", got[1].FolderPath)
	}
}
