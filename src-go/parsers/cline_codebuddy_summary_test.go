package parsers

// cline_codebuddy_summary_test.go covers the last two file-based listers:
// cline (a manifest per session directory, no transcript read) and CodeBuddy
// (Claude's directory layout with OpenAI-Agents record types).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListClineConversations(t *testing.T) {
	data := t.TempDir()
	dir := filepath.Join(data, "sessions", "s1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The manifest mirrors cline's own sessions table.
	manifest := `{
		"session_id": "s1",
		"started_at": "2026-04-02T09:00:00Z",
		"ended_at": "2026-04-02T09:30:00Z",
		"model": "claude-sonnet-4",
		"cwd": "/home/u/proj",
		"prompt": "重构导出流程\n第二行不该进标题",
		"metadata": {"title": "", "git": {"branch": "feat/export"}}
	}`
	if err := os.WriteFile(filepath.Join(dir, "s1.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	// No manifest: a spawned sub-agent. Not listed.
	if err := os.MkdirAll(filepath.Join(data, "sessions", "s2"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A manifest with an explicit title wins over the prompt.
	dir3 := filepath.Join(data, "sessions", "s3")
	if err := os.MkdirAll(dir3, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest3 := `{"session_id":"s3","started_at":"2026-04-01T09:00:00Z",
		"workspace_root":"/home/u/other","prompt":"x","metadata":{"title":"登录页改造"}}`
	if err := os.WriteFile(filepath.Join(dir3, "s3.json"), []byte(manifest3), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLINE_DIR", data)

	got := ListClineConversations()
	if len(got) != 2 {
		t.Fatalf("summaries = %d, want 2 (the manifest-less child is skipped): %+v", len(got), got)
	}
	// Newest first.
	if got[0].ID != "s1" || got[1].ID != "s3" {
		t.Fatalf("order = %q, %q", got[0].ID, got[1].ID)
	}
	s1 := got[0]
	if s1.AgentType != "cline" || s1.FolderPath != "/home/u/proj" || s1.FolderName != "proj" {
		t.Errorf("session = %+v", s1)
	}
	if s1.Title != "重构导出流程" {
		t.Errorf("title = %q (the prompt's first line is the fallback)", s1.Title)
	}
	if s1.Model != "claude-sonnet-4" || s1.GitBranch != "feat/export" {
		t.Errorf("model/branch = %q / %q", s1.Model, s1.GitBranch)
	}
	if s1.StartedAt != "2026-04-02T09:00:00Z" || s1.EndedAt != "2026-04-02T09:30:00Z" {
		t.Errorf("times = %q / %q", s1.StartedAt, s1.EndedAt)
	}
	// workspace_root stands in when cwd is absent.
	if got[1].FolderPath != "/home/u/other" || got[1].Title != "登录页改造" {
		t.Errorf("workspace_root fallback = %+v", got[1])
	}
}

func TestListClineConversationsMissingDir(t *testing.T) {
	t.Setenv("CLINE_DIR", t.TempDir())
	if got := ListClineConversations(); len(got) != 0 {
		t.Fatalf("missing dir = %+v", got)
	}
}

func TestListCodeBuddyConversations(t *testing.T) {
	cfg := t.TempDir()
	proj := filepath.Join(cfg, "projects", "-home-u-proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	// Records are OpenAI-Agents items; timestamps are epoch millis. The injected
	// user record (isMeta) must not become the title.
	body := `{"type":"message","role":"user","sessionId":"cb1","cwd":"/home/u/proj","timestamp":1792245419112,"content":[{"type":"input_text","text":"<task-notification>done</task-notification>"}],"providerData":{"isMeta":true}}
{"type":"message","role":"user","sessionId":"cb1","timestamp":1792245420000,"content":[{"type":"input_text","text":"修一下导出按钮\n第二行"}],"providerData":{"requestModelName":"GLM-5.1","model":"glm-5.1"}}
{"type":"ai-title","timestamp":1792245421000,"aiTitle":"导出按钮修复"}
{"type":"message","role":"assistant","timestamp":1792245430000,"content":[{"type":"output_text","text":"好的"}]}
{"type":"custom-title","timestamp":1792245431000,"customTitle":"导出按钮"}
`
	if err := os.WriteFile(filepath.Join(proj, "cb1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// A metadata-only file is not a conversation.
	if err := os.WriteFile(filepath.Join(proj, "cb2.jsonl"),
		[]byte(`{"type":"ai-title","timestamp":1792245000000,"aiTitle":"empty"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEBUDDY_CONFIG_DIR", cfg)

	got := ListCodeBuddyConversations()
	if len(got) != 1 {
		t.Fatalf("summaries = %d, want 1 (metadata-only file skipped): %+v", len(got), got)
	}
	s := got[0]
	if s.ID != "cb1" || s.AgentType != "code_buddy" {
		t.Errorf("session = %+v", s)
	}
	if s.FolderPath != "/home/u/proj" || s.FolderName != "proj" {
		t.Errorf("folder = %q / %q", s.FolderPath, s.FolderName)
	}
	// custom-title beats ai-title; the injected record never wins.
	if s.Title != "导出按钮" {
		t.Errorf("title = %q", s.Title)
	}
	if s.Model != "GLM-5.1" {
		t.Errorf("model = %q", s.Model)
	}
	// The injected record is not counted as a message.
	if s.MessageCount != 2 {
		t.Errorf("message count = %d, want 2", s.MessageCount)
	}
	if s.StartedAt != "2026-10-17T13:56:59Z" || s.EndedAt != "2026-10-17T13:57:10Z" {
		t.Errorf("times = %q / %q", s.StartedAt, s.EndedAt)
	}
}

// TestCodeBuddyTitleAfterHeadPass keeps a mid-session rename: title records are
// appended anywhere, and the tail pass (which counts by bytes) must still decode
// them — otherwise a /rename past the head window would be lost.
func TestCodeBuddyTitleAfterHeadPass(t *testing.T) {
	cfg := t.TempDir()
	proj := filepath.Join(cfg, "projects", "-home-u-proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString(`{"type":"message","role":"user","sessionId":"cb9","cwd":"/home/u/proj","timestamp":1792245419000,"content":[{"type":"input_text","text":"第一句提示"}]}` + "\n")
	// Push the rename past the head window with filler records.
	for i := 0; i < codebuddyHeadLines+10; i++ {
		b.WriteString(`{"type":"message","role":"assistant","timestamp":1792245420000,"content":[{"type":"output_text","text":"ok"}]}` + "\n")
	}
	b.WriteString(`{"type":"custom-title","timestamp":1792245430000,"customTitle":"后来改的名字"}` + "\n")
	if err := os.WriteFile(filepath.Join(proj, "cb9.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEBUDDY_CONFIG_DIR", cfg)

	got := ListCodeBuddyConversations()
	if len(got) != 1 {
		t.Fatalf("summaries = %+v", got)
	}
	if got[0].Title != "后来改的名字" {
		t.Errorf("title = %q (a rename past the head window must still win)", got[0].Title)
	}
	if got[0].MessageCount != codebuddyHeadLines+11 {
		t.Errorf("message count = %d, want %d", got[0].MessageCount, codebuddyHeadLines+11)
	}
}
