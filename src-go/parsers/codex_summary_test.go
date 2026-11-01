package parsers

// codex_summary_test.go covers the import picker's Codex lister: the id, cwd and
// timestamp come from the session_meta head, the title from the first real user
// prompt (injected context does not count), and a compressed rollout reads the
// same as a plain one.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

const codexSummaryBody = `{"timestamp":"2026-05-28T02:11:21.853Z","type":"session_meta","payload":{"id":"019e6c59-aa9c-72e3-a5e3-178c2842773c","timestamp":"2026-05-28T02:11:21.272Z","cwd":"D:/Repos/demo/app","originator":"Codex Desktop"}}
{"timestamp":"2026-05-28T02:11:22Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions for /repo\n\n<INSTRUCTIONS>x"}]}}
{"timestamp":"2026-05-28T02:11:23Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"修一下登录页的样式\n第二行不该进标题"}]}}
`

func TestListCodexConversations(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "05", "28")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "rollout-2026-05-28T10-11-21-019e6c59-aa9c-72e3-a5e3-178c2842773c.jsonl")
	if err := os.WriteFile(plain, []byte(codexSummaryBody), 0o644); err != nil {
		t.Fatal(err)
	}
	// The compressed form must summarize identically.
	zpath := filepath.Join(dir, "rollout-2026-05-29T10-11-21-019e6c59-1111-2222-3333-444455556666.jsonl.zst")
	f, err := os.Create(zpath)
	if err != nil {
		t.Fatal(err)
	}
	zw, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write([]byte(codexSummaryBody)); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	f.Close()
	t.Setenv("CODEX_HOME", home)

	got := ListCodexConversations()
	if len(got) != 2 {
		t.Fatalf("summaries = %d, want 2: %+v", len(got), got)
	}
	// Newest first: the .zst file's name carries the later timestamp only for
	// sorting; both bodies share session_meta, so order comes from StartedAt.
	for _, s := range got {
		if s.AgentType != "codex" {
			t.Errorf("agent type = %q", s.AgentType)
		}
		if s.ID != "019e6c59-aa9c-72e3-a5e3-178c2842773c" {
			t.Errorf("id = %q", s.ID)
		}
		if s.FolderPath != `D:/Repos/demo/app` || s.FolderName != "app" {
			t.Errorf("folder = %q / %q", s.FolderPath, s.FolderName)
		}
		if s.Title != "修一下登录页的样式" {
			t.Errorf("title = %q (the AGENTS.md block and later lines must not leak in)", s.Title)
		}
		if s.StartedAt != "2026-05-28T02:11:21.272Z" {
			t.Errorf("started = %q", s.StartedAt)
		}
	}
}

// TestBaseNameOfPath covers both separators: agents write whichever their OS uses.
func TestBaseNameOfPath(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`D://Repos//demo//app`, "app"},
		{"/home/me/app", "app"},
		{"/home/me/app/", "app"},
		{"app", "app"},
	} {
		if got := baseNameOfPath(c.in); got != c.want {
			t.Errorf("baseNameOfPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestCodexSummaryFallsBackToFileName keeps a rollout whose header is unreadable
// in the picker: the id rides the file name.
func TestCodexSummaryFallsBackToFileName(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "05", "28")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "rollout-2026-05-28T10-11-21-019e6c59-aaaa-bbbb-cccc-ddddeeeeffff.jsonl"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", home)

	got := ListCodexConversations()
	if len(got) != 1 || got[0].ID != "019e6c59-aaaa-bbbb-cccc-ddddeeeeffff" {
		t.Fatalf("summaries = %+v", got)
	}
}
