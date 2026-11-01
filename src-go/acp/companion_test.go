package acp

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFakeCompanion drops a ( content-less ) file the resolver treats as the
// gocode-mcp binary and points GOCODE_MCP_PATH at it.
func writeFakeCompanion(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "gocode-mcp-*")
	if err != nil {
		t.Fatalf("temp: %v", err)
	}
	f.Close()
	if err := os.Chmod(f.Name(), 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Setenv("GOCODE_MCP_PATH", f.Name())
	return f.Name()
}

func TestResolveCompanionBinary_Override(t *testing.T) {
	path := writeFakeCompanion(t)
	got, ok := ResolveCompanionBinary()
	if !ok {
		t.Fatalf("expected binary resolved via GOCODE_MCP_PATH")
	}
	if got != path {
		t.Fatalf("resolved %q, want %q", got, path)
	}
}

func TestResolveCompanionBinary_Missing(t *testing.T) {
	// Point the override at a file that does not exist; with no sibling/exe
	// match and nothing in PATH, resolution must report "not found" rather than
	// a phantom path.
	t.Setenv("GOCODE_MCP_PATH", filepath.Join(t.TempDir(), "nope-gocode-mcp"))
	if _, ok := ResolveCompanionBinary(); ok {
		t.Fatalf("expected resolution to fail for a missing binary")
	}
}

func TestCompanionMCPServer_Shape(t *testing.T) {
	path := writeFakeCompanion(t)
	spec, ok := CompanionMCPServer("http://127.0.0.1:9", "tok", "folder-7", "opencode", "sess-3")
	if !ok {
		t.Fatalf("expected spec built when binary present")
	}
	if spec["name"] != "gocode-mcp" || spec["type"] != "stdio" {
		t.Fatalf("unexpected name/type: %v", spec)
	}
	if spec["command"] != path {
		t.Fatalf("command = %v, want %q", spec["command"], path)
	}
	args, _ := spec["args"].([]any)
	if len(args) != 0 {
		t.Fatalf("args = %v, want empty", args)
	}
	env, _ := spec["env"].(map[string]any)
	if env == nil {
		t.Fatalf("env missing")
	}
	wantEnv := map[string]string{
		"GOCODE_API":         "http://127.0.0.1:9",
		"GOCODE_TOKEN":       "tok",
		"GOCODE_FOLDER_ID":   "folder-7",
		"GOCODE_AGENT_TYPE":  "opencode",
		"GOCODE_SESSION_ID":  "sess-3",
	}
	for k, v := range wantEnv {
		if env[k] != v {
			t.Fatalf("env[%s] = %v, want %q", k, env[k], v)
		}
	}
}

func TestCompanionMCPServer_MissingBinary(t *testing.T) {
	t.Setenv("GOCODE_MCP_PATH", filepath.Join(t.TempDir(), "nope-gocode-mcp"))
	if _, ok := CompanionMCPServer("http://x", "t", "", "opencode", "s"); ok {
		t.Fatalf("expected no spec when binary missing")
	}
}

func TestManagerCompanionSpec(t *testing.T) {
	// No backend configured → no injection.
	m := NewManager(nil, nil)
	if spec := m.companionSpec("opencode", "s1"); spec != nil {
		t.Fatalf("expected nil spec without backend config, got %v", spec)
	}

	// Backend configured + binary present → injects.
	path := writeFakeCompanion(t)
	m.SetCompanionBackend("http://127.0.0.1:9", "tok")
	spec := m.companionSpec("opencode", "s1")
	if spec == nil {
		t.Fatalf("expected spec injected when backend + binary present")
	}
	if spec["command"] != path {
		t.Fatalf("command = %v, want %q", spec["command"], path)
	}
	env := spec["env"].(map[string]any)
	if env["GOCODE_SESSION_ID"] != "s1" || env["GOCODE_AGENT_TYPE"] != "opencode" {
		t.Fatalf("session context not injected: %v", env)
	}

	// Binary missing → skip injection even with backend configured.
	t.Setenv("GOCODE_MCP_PATH", filepath.Join(t.TempDir(), "nope-gocode-mcp"))
	if spec := m.companionSpec("opencode", "s2"); spec != nil {
		t.Fatalf("expected nil spec when binary missing, got %v", spec)
	}
}
