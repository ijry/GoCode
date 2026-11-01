package handlers_test

import (
	"os"
	"path/filepath"
	"testing"
)

// pinAgentHomes points every agent's session store at home, so a scan test sees
// only the fixtures it seeds — never the developer's own sessions. Without this
// the scan reads real data as soon as one of these variables is set on the
// machine, and the "one folder, one session" assertions break. CLAUDE_CONFIG_DIR
// stays the caller's choice: each test names its own directory.
func pinAgentHomes(t *testing.T, home string) {
	t.Helper()
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("GEMINI_CLI_HOME", home)
	t.Setenv("GEMINI_HOME", filepath.Join(home, ".gemini"))
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(home, ".pi-sessions"))
	t.Setenv("GROK_HOME", filepath.Join(home, ".grok"))
	t.Setenv("HERMES_HOME", filepath.Join(home, ".hermes"))
	t.Setenv("QODER_CONFIG_DIR", filepath.Join(home, ".qoder"))
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, ".kimi-code"))
	t.Setenv("CLINE_DIR", filepath.Join(home, ".cline", "data"))
	t.Setenv("CODEBUDDY_CONFIG_DIR", filepath.Join(home, ".codebuddy"))
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(home, ".cursor"))
	t.Setenv("DSH_HOME", filepath.Join(home, ".dsh"))
	t.Setenv("DEEPSEEK_ACP_SESSIONS_ROOT", "")
}

func TestScanImportableSessions(t *testing.T) {
	home := setHome(t)
	claudeDir := filepath.Join(home, ".claude-test")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	pinAgentHomes(t, home)
	srv := newTestServer(t)

	// seed a claude transcript under <claude>/projects/<slug>/<sess>.jsonl
	projDir := filepath.Join(claudeDir, "projects", "-home-u-proj")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := `{"type":"user","sessionId":"sess-abc","cwd":"/home/u/proj","gitBranch":"main","timestamp":"2026-01-01T10:00:00Z","message":{"role":"user","content":"Hello world task"}}
{"type":"assistant","sessionId":"sess-abc","timestamp":"2026-01-01T10:01:00Z","message":{"role":"assistant","model":"claude-sonnet-4","content":[{"type":"text","text":"hi"}]}}
`
	if err := os.WriteFile(filepath.Join(projDir, "sess-abc.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := call(t, srv, "scan_importable_sessions", map[string]any{})
	if code != 200 {
		t.Fatalf("scan status = %d (%v)", code, out)
	}
	if out["total_sessions"].(float64) != 1 || out["importable_count"].(float64) != 1 || out["no_folder_count"].(float64) != 0 {
		t.Fatalf("scan counts wrong: %v", out)
	}
	folders, _ := out["folders"].([]any)
	if len(folders) != 1 {
		t.Fatalf("expected 1 folder group, got %d", len(folders))
	}
	f0 := folders[0].(map[string]any)
	if f0["path"] != "/home/u/proj" || f0["name"] != "proj" || f0["exists_in_codeg"] != false {
		t.Fatalf("folder group wrong: %v", f0)
	}
	sessions, _ := f0["sessions"].([]any)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	s0 := sessions[0].(map[string]any)
	if s0["external_id"] != "sess-abc" || s0["agent_type"] != "claude_code" || s0["status"] != "new" {
		t.Fatalf("session wrong: %v", s0)
	}
	if s0["model"] != "claude-sonnet-4" || s0["git_branch"] != "main" || s0["message_count"].(float64) != 2 {
		t.Fatalf("session fields wrong: %v", s0)
	}
	if s0["title"] != "Hello world task" {
		t.Fatalf("title wrong: %v", s0["title"])
	}
}

// TestScanImportableCodexSessions locks the Codex lister into the scan: a rollout
// under the same cwd joins the Claude session's folder group, so the picker shows
// both agents' sessions for one project.
func TestScanImportableCodexSessions(t *testing.T) {
	home := setHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude-test"))
	pinAgentHomes(t, home)
	srv := newTestServer(t)

	dir := filepath.Join(home, ".codex", "sessions", "2026", "05", "28")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const sid = "019e6c59-aa9c-72e3-a5e3-178c2842773c"
	body := `{"timestamp":"2026-05-28T02:11:21.853Z","type":"session_meta","payload":{"id":"` + sid + `","timestamp":"2026-05-28T02:11:21.272Z","cwd":"/home/u/proj"}}
{"timestamp":"2026-05-28T02:11:23Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"修登录页"}]}}
`
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-05-28T10-11-21-"+sid+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := call(t, srv, "scan_importable_sessions", map[string]any{})
	if code != 200 {
		t.Fatalf("scan status = %d (%v)", code, out)
	}
	if out["total_sessions"].(float64) != 1 {
		t.Fatalf("scan counts wrong: %v", out)
	}
	folders, _ := out["folders"].([]any)
	if len(folders) != 1 {
		t.Fatalf("expected 1 folder group, got %d", len(folders))
	}
	sessions, _ := folders[0].(map[string]any)["sessions"].([]any)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	s0 := sessions[0].(map[string]any)
	if s0["external_id"] != sid || s0["agent_type"] != "codex" || s0["title"] != "修登录页" {
		t.Fatalf("codex session wrong: %v", s0)
	}
}
