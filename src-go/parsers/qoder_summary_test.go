package parsers

// qoder_summary_test.go covers the Qoder lister: Qoder stores sessions in
// Claude's format, so the Claude walker serves it — with the agent type rewritten.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListQoderConversations(t *testing.T) {
	cfg := t.TempDir()
	proj := filepath.Join(cfg, "projects", "-home-u-proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"user","sessionId":"q1","cwd":"/home/u/proj","timestamp":"2026-01-01T10:00:00Z","message":{"role":"user","content":"改一下登录页"}}
{"type":"assistant","sessionId":"q1","timestamp":"2026-01-01T10:01:00Z","message":{"role":"assistant","model":"qwen-3","content":[{"type":"text","text":"好的"}]}}
`
	if err := os.WriteFile(filepath.Join(proj, "q1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QODER_CONFIG_DIR", cfg)

	got := ListQoderConversations()
	if len(got) != 1 {
		t.Fatalf("summaries = %+v", got)
	}
	s := got[0]
	// The Claude walker sets claude_code; the Qoder lister rewrites it.
	if s.AgentType != "qoder" {
		t.Errorf("agent type = %q", s.AgentType)
	}
	if s.ID != "q1" || s.FolderPath != "/home/u/proj" || s.Title != "改一下登录页" {
		t.Errorf("session = %+v", s)
	}
	if s.Model != "qwen-3" || s.MessageCount != 2 {
		t.Errorf("model/count = %q / %d", s.Model, s.MessageCount)
	}
}
