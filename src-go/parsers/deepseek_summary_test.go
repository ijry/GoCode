package parsers

// deepseek_summary_test.go covers the import picker's DeepSeek lister: the header
// line supplies id/cwd/createdAt, `session/title` wins over the first prompt, an
// injected `user/message` (source.kind != "user") is not a prompt, and the live
// generation is the one that gets read — across multiple zstd frames, which is how
// the harness appends.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// writeDeepseekFrames writes the given chunks as separate zstd frames.
func writeDeepseekFrames(t *testing.T, path string, frames ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, frame := range frames {
		zw, err := zstd.NewWriter(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := zw.Write([]byte(frame)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListDeepseekConversations(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "sessions")
	dir := filepath.Join(root, "--home-u-proj--", "session-abc")

	// The stale generation 0 log says the session is about something else; the
	// v3 log is the live one and must win.
	writeDeepseekFrames(t, filepath.Join(dir, "session.jsonl.zstd"),
		`{"type":"session","version":1,"id":"session-abc","createdAt":1000,"cwd":"/wrong"}`+"\n")
	writeDeepseekFrames(t, filepath.Join(dir, "session.v3.jsonl.zstd"),
		// Two frames: the header in one, the rest in the next — exactly how the
		// harness appends, and what a streaming decoder has to walk.
		`{"type":"session","version":3,"id":"session-abc","createdAt":1792245419112,"cwd":"/home/u/proj","delegationDepth":0}`+"\n",
		`{"type":"model/selection","time":1792245420000,"data":{"provider":"x","model":"deepseek-v4-flash"}}`+"\n"+
			`{"type":"user/message","time":1792245421000,"data":{"content":[{"type":"text","text":"你是什么模型"}],"source":{"kind":"user"}}}`+"\n"+
			`{"type":"user/message","time":1792245422000,"data":{"content":[{"type":"text","text":"<system-reminder>injected</system-reminder>"}]}}`+"\n"+
			`{"type":"session/title","time":1792245423000,"data":{"title":"你是什么模型"}}`+"\n"+
			`{"type":"session/title","time":1792245424000,"data":{"title":"模型与配置问答"}}`+"\n"+
			`{"type":"turn/end","time":1792245500000,"data":{"reason":"completed"}}`+"\n")

	// A session directory with no log at all is not a conversation.
	if err := os.MkdirAll(filepath.Join(root, "--home-u-proj--", "session-empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DSH_HOME", home)
	t.Setenv("DEEPSEEK_ACP_SESSIONS_ROOT", "")

	got := ListDeepseekConversations()
	if len(got) != 1 {
		t.Fatalf("summaries = %d, want 1: %+v", len(got), got)
	}
	s := got[0]
	if s.ID != "session-abc" || s.AgentType != "deepseek" {
		t.Errorf("session = %+v", s)
	}
	// cwd from the live log's header, not the stale generation's.
	if s.FolderPath != "/home/u/proj" || s.FolderName != "proj" {
		t.Errorf("folder = %q / %q", s.FolderPath, s.FolderName)
	}
	// The last session/title wins; the injected user record never becomes one.
	if s.Title != "模型与配置问答" {
		t.Errorf("title = %q", s.Title)
	}
	if s.Model != "deepseek-v4-flash" {
		t.Errorf("model = %q", s.Model)
	}
	// Only the real prompt counts.
	if s.MessageCount != 1 {
		t.Errorf("message count = %d, want 1", s.MessageCount)
	}
	if s.StartedAt != "2026-10-17T13:56:59Z" || s.EndedAt != "2026-10-17T13:58:20Z" {
		t.Errorf("times = %q / %q", s.StartedAt, s.EndedAt)
	}
}

// TestDeepseekSummaryFallsBackToFirstPrompt keeps a session with no title event
// importable under its first prompt.
func TestDeepseekSummaryFallsBackToFirstPrompt(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "sessions")
	dir := filepath.Join(root, "--home-u-p--", "session-p")
	writeDeepseekFrames(t, filepath.Join(dir, "session.v3.jsonl.zstd"),
		`{"type":"session","version":3,"id":"session-p","createdAt":1792000000000,"cwd":"/home/u/p"}`+"\n"+
			`{"type":"user/message","time":1792000001000,"data":{"content":[{"type":"text","text":"为什么构建失败\n第二行"}],"source":{"kind":"user"}}}`+"\n")
	t.Setenv("DSH_HOME", home)
	t.Setenv("DEEPSEEK_ACP_SESSIONS_ROOT", "")

	got := ListDeepseekConversations()
	if len(got) != 1 {
		t.Fatalf("summaries = %+v", got)
	}
	if got[0].Title != "为什么构建失败" {
		t.Errorf("title = %q", got[0].Title)
	}
}
