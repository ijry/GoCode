package app

// model_providers_test.go covers Settings → Model providers: the empty state,
// create, edit, and delete, all against the real list/create/update/delete
// backend routes.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

// mpShellForTest opens Settings → Model providers with the list seeded from the
// backend, so later mutations' cache-delete triggers a clean refetch.
func mpShellForTest(t *testing.T) (*Shell, *Backend) {
	t.Helper()
	b := newBackendForTest(t)
	s := New(b)
	s.stream = newStreamForTest()
	s.section = "settings"
	s.settingsPage = "model-providers"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	raw, err := b.raw("list_model_providers", map[string]any{})
	if err != nil {
		t.Fatalf("list_model_providers: %v", err)
	}
	s.cache[mpCacheKey] = raw
	return s, b
}

// TestModelProvidersEmpty shows the empty state and the create form.
func TestModelProvidersEmpty(t *testing.T) {
	s, _ := mpShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{
		"Model providers",
		"Add provider",
		"Create",
		"No model providers configured yet.",
	} {
		if !tt.HasText(want) {
			t.Errorf("providers empty frame missing %q; texts = %v", want, tt.Texts())
		}
	}
}

// TestModelProvidersCreate adds a provider and waits for its row.
func TestModelProvidersCreate(t *testing.T) {
	s, _ := mpShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	s.mpNewName = "Acme"
	s.mpNewURL = "https://api.acme.test"
	s.mpNewKey = "sk-1234567890abcdef"
	s.mpNewAgent = "claude_code"
	tt.Frame()
	if err := tt.Click("Create"); err != nil {
		t.Fatalf("click Create: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("Acme") }) {
		t.Fatalf("created provider never appeared; texts = %v", tt.Texts())
	}
	if !tt.HasText("https://api.acme.test") {
		t.Errorf("provider URL missing; texts = %v", tt.Texts())
	}
}

// TestModelProvidersEdit renames a provider through the inline edit form.
func TestModelProvidersEdit(t *testing.T) {
	s, b := mpShellForTest(t)
	if _, err := b.raw("create_model_provider", map[string]any{
		"name": "Old", "apiUrl": "https://old.test", "agentType": "claude_code",
	}); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	raw, _ := b.raw("list_model_providers", map[string]any{})
	s.cache[mpCacheKey] = raw
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Edit"); err != nil {
		t.Fatalf("click Edit: %v", err)
	}
	tt.Frame()
	s.mpEditName = "Renamed"
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("click Save: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("Renamed") }) {
		t.Fatalf("renamed provider never appeared; texts = %v", tt.Texts())
	}
}

// TestModelProvidersDelete removes a provider through the two-step confirm.
func TestModelProvidersDelete(t *testing.T) {
	s, b := mpShellForTest(t)
	if _, err := b.raw("create_model_provider", map[string]any{
		"name": "Doomed", "apiUrl": "https://doomed.test", "agentType": "claude_code",
	}); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	raw, _ := b.raw("list_model_providers", map[string]any{})
	s.cache[mpCacheKey] = raw
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Doomed") {
		t.Fatalf("seeded provider missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Delete"); err != nil {
		t.Fatalf("click Delete: %v", err)
	}
	tt.Frame()
	if err := tt.Click("Confirm delete"); err != nil {
		t.Fatalf("click Confirm delete: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return !tt.HasText("Doomed") }) {
		t.Fatalf("deleted provider row never left; texts = %v", tt.Texts())
	}
}

// TestAgentTypeOptions builds the picker's list from the registry, appending a
// stored value that is not in the list exactly once.
func TestAgentTypeOptions(t *testing.T) {
	s, _ := mpShellForTest(t)
	s.cache["agents"] = json.RawMessage(`[
		{"agent_type":"claude_code","name":"Claude Code","available":true},
		{"agent_type":"codex","name":"Codex","available":false}
	]`)
	got := s.agentTypeOptions("")
	if len(got) != 2 || got[0] != "claude_code" || got[1] != "codex" {
		t.Fatalf("options = %v, want the registry's two", got)
	}
	got = s.agentTypeOptions("legacy-agent")
	if len(got) != 3 || got[2] != "legacy-agent" {
		t.Fatalf("options = %v, want the registry plus legacy-agent", got)
	}
	got = s.agentTypeOptions("codex")
	if len(got) != 2 {
		t.Fatalf("existing option duplicated: %v", got)
	}
}
