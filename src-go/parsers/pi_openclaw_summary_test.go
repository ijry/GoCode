package parsers

// pi_openclaw_summary_test.go covers the two agents that share the
// `type:"session"` header: pi (id inside the file, dashed-cwd bucket dirs) and
// OpenClaw (id in the name too, plus a sessions.json index that is not a session).

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListPiConversations(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "--home-u-proj--")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"session","version":3,"id":"pi-1","timestamp":"2026-06-27T10:00:00Z","cwd":"/home/u/proj"}
{"type":"message","id":"m1","message":{"role":"user","content":"重构导出流程\n第二行"}}
`
	if err := os.WriteFile(filepath.Join(dir, "2026-06-27T10-00-00_pi1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", base)

	got := ListPiConversations()
	if len(got) != 1 {
		t.Fatalf("summaries = %+v", got)
	}
	s := got[0]
	if s.ID != "pi-1" || s.AgentType != "pi" {
		t.Errorf("session = %+v", s)
	}
	if s.FolderPath != "/home/u/proj" || s.FolderName != "proj" {
		t.Errorf("folder = %q / %q", s.FolderPath, s.FolderName)
	}
	if s.Title != "重构导出流程" || s.StartedAt != "2026-06-27T10:00:00Z" {
		t.Errorf("title/time = %q / %q", s.Title, s.StartedAt)
	}
}

func TestListOpenClawConversations(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".openclaw", "agents", "main", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"session","version":3,"id":"claw-1","timestamp":"2026-03-17T04:46:14Z","cwd":"/tmp/demo"}
{"type":"message","id":"u1","message":{"role":"user","content":[{"type":"text","text":"[Tue 2026-03-17 12:56 GMT+8] 修一下构建"}]}}
`
	if err := os.WriteFile(filepath.Join(dir, "claw-1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// The index file is not a session and must not be listed.
	if err := os.WriteFile(filepath.Join(dir, "sessions.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	got := ListOpenClawConversations()
	if len(got) != 1 {
		t.Fatalf("summaries = %+v (sessions.json must be skipped)", got)
	}
	s := got[0]
	if s.ID != "claw-1" || s.AgentType != "open_claw" {
		t.Errorf("session = %+v", s)
	}
	if s.FolderName != "demo" || s.Title != "修一下构建" {
		t.Errorf("folder/title = %q / %q", s.FolderName, s.Title)
	}
}
