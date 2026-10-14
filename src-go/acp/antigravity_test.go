package acp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// geminiEnv builds a runtime env pointing GEMINI_HOME at an absolute temp dir,
// so the tests never touch the process environment or the real ~/.gemini.
func geminiEnv(t *testing.T, extra map[string]string) (map[string]string, string) {
	t.Helper()
	dir := t.TempDir()
	env := map[string]string{"GEMINI_HOME": dir}
	for k, v := range extra {
		env[k] = v
	}
	return env, filepath.Join(dir, "antigravity-acp")
}

func readSettings(t *testing.T, acpDir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(acpDir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

func TestSyncCreatesDefaultWhenNothingRecorded(t *testing.T) {
	env, acpDir := geminiEnv(t, nil)
	rep := SyncAntigravitySettingsForEnv(env)
	if rep.Status != AntigravitySyncWritten {
		t.Fatalf("status = %q, want written (reason %v)", rep.Status, rep.Reason)
	}
	m := readSettings(t, acpDir)
	auth, _ := m["auth"].(map[string]any)
	if auth["type"] != "oauth-personal" {
		t.Fatalf("auth.type = %v, want oauth-personal", auth["type"])
	}
}

func TestSyncLeavesExistingTypeAlone(t *testing.T) {
	env, acpDir := geminiEnv(t, nil)
	if err := os.MkdirAll(acpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"auth":{"type":"gemini-api-key"}}`
	path := filepath.Join(acpDir, "settings.json")
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := SyncAntigravitySettingsForEnv(env)
	if rep.Status != AntigravitySyncAlreadyCurrent {
		t.Fatalf("status = %q, want already_current", rep.Status)
	}
	// Nothing recorded + an existing type → never overridden, never rewritten.
	after, _ := os.ReadFile(path)
	if string(after) != original {
		t.Fatalf("file was rewritten: %q", after)
	}
}

func TestSyncUsesRecordedMethod(t *testing.T) {
	env, acpDir := geminiEnv(t, map[string]string{"AGY_AUTH_METHOD": "gemini-api-key"})
	rep := SyncAntigravitySettingsForEnv(env)
	if rep.Status != AntigravitySyncWritten {
		t.Fatalf("status = %q, want written", rep.Status)
	}
	m := readSettings(t, acpDir)
	if m["auth"].(map[string]any)["type"] != "gemini-api-key" {
		t.Fatalf("auth.type = %v", m["auth"])
	}
}

func TestSyncSkipsNonJSONFile(t *testing.T) {
	env, acpDir := geminiEnv(t, map[string]string{"AGY_AUTH_METHOD": "oauth-personal"})
	if err := os.MkdirAll(acpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hjson := "{\n  // a comment\n  \"auth\": { \"type\": \"oauth-personal\" },\n}"
	path := filepath.Join(acpDir, "settings.json")
	if err := os.WriteFile(path, []byte(hjson), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := SyncAntigravitySettingsForEnv(env)
	if rep.Status != AntigravitySyncSkipped || rep.Reason == nil {
		t.Fatalf("status = %q reason = %v, want skipped", rep.Status, rep.Reason)
	}
	// Left untouched (fail closed — the server can read it, codeg cannot).
	after, _ := os.ReadFile(path)
	if string(after) != hjson {
		t.Fatalf("file was modified: %q", after)
	}
}

func TestSyncSkipsNonObjectAuth(t *testing.T) {
	env, acpDir := geminiEnv(t, map[string]string{"AGY_AUTH_METHOD": "oauth-personal"})
	if err := os.MkdirAll(acpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(acpDir, "settings.json"), []byte(`{"auth":"nope"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := SyncAntigravitySettingsForEnv(env)
	if rep.Status != AntigravitySyncSkipped || rep.Reason == nil || *rep.Reason != "`auth` is not an object" {
		t.Fatalf("status = %q reason = %v", rep.Status, rep.Reason)
	}
}

func TestSyncWritesAndClearsGcp(t *testing.T) {
	// agent-platform with a stored project+location → both written.
	env, acpDir := geminiEnv(t, map[string]string{
		"AGY_AUTH_METHOD":       "agent-platform",
		"GOOGLE_CLOUD_PROJECT":  "proj-1",
		"GOOGLE_CLOUD_LOCATION": "us-central1",
	})
	if rep := SyncAntigravitySettingsForEnv(env); rep.Status != AntigravitySyncWritten {
		t.Fatalf("status = %q", rep.Status)
	}
	m := readSettings(t, acpDir)
	gcp, _ := m["gcp"].(map[string]any)
	if gcp["project"] != "proj-1" || gcp["location"] != "us-central1" {
		t.Fatalf("gcp = %v", gcp)
	}

	// oauth-business with no stored values → clears the existing block.
	env2, acpDir2 := geminiEnv(t, map[string]string{"AGY_AUTH_METHOD": "oauth-business"})
	if err := os.MkdirAll(acpDir2, 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `{"auth":{"type":"oauth-business"},"gcp":{"project":"OLD","location":"OLDL"}}`
	if err := os.WriteFile(filepath.Join(acpDir2, "settings.json"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if rep := SyncAntigravitySettingsForEnv(env2); rep.Status != AntigravitySyncWritten {
		t.Fatalf("clear status = %q", rep.Status)
	}
	m2 := readSettings(t, acpDir2)
	if _, has := m2["gcp"]; has {
		t.Fatalf("gcp block should be dropped when cleared: %v", m2["gcp"])
	}
}

func TestEffectiveAuthType(t *testing.T) {
	// Absent: no file.
	env, _ := geminiEnv(t, nil)
	if got := AntigravityEffectiveAuthType(env); got.Kind != "absent" {
		t.Fatalf("kind = %q, want absent", got.Kind)
	}

	// Declared (with legacy spelling canonicalized).
	env2, acpDir2 := geminiEnv(t, nil)
	if err := os.MkdirAll(acpDir2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(acpDir2, "settings.json"),
		[]byte(`{"auth":{"type":"vertex-ai"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := AntigravityEffectiveAuthType(env2); got.Kind != "declared" || got.Declared != "agent-platform" {
		t.Fatalf("got %+v, want declared agent-platform", got)
	}

	// Unreadable: a file codeg cannot parse.
	env3, acpDir3 := geminiEnv(t, nil)
	if err := os.MkdirAll(acpDir3, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(acpDir3, "settings.json"), []byte(`{ // comment`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := AntigravityEffectiveAuthType(env3); got.Kind != "unreadable" {
		t.Fatalf("kind = %q, want unreadable", got.Kind)
	}
}

func TestExpandHomePrefix(t *testing.T) {
	home := filepath.FromSlash("/home/u")
	if got := expandHomePrefix("~", home); got != home {
		t.Fatalf("~ → %q", got)
	}
	if got := expandHomePrefix("~/x", home); got != filepath.Join(home, "x") {
		t.Fatalf("~/x → %q", got)
	}
	if got := expandHomePrefix("/abs", home); got != "/abs" {
		t.Fatalf("absolute should pass through: %q", got)
	}
}
