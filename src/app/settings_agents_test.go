package app

// settings_agents_test.go covers the Settings → Agents depth (UI_GAPS P1-8):
// the catalog with check feedback, the on-demand version probe, and the
// custom-agent form (client-side validation mirrors the backend) plus delete
// through the inline confirmation.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// agentsShellForTest boots the real backend and returns a Shell already on
// Settings → Agents with both list caches pre-seeded.
func agentsShellForTest(t *testing.T) (*Shell, *Backend) {
	t.Helper()
	b := newBackendForTest(t)
	s := New(b)
	s.stream = newStreamForTest()
	s.section = "settings"
	s.settingsPage = "agents"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	agents, err := b.raw("acp_list_agents", map[string]any{})
	if err != nil {
		t.Fatalf("acp_list_agents: %v", err)
	}
	s.cache["agents"] = agents
	custom, err := b.raw("acp_list_custom_agents", map[string]any{})
	if err != nil {
		t.Fatalf("acp_list_custom_agents: %v", err)
	}
	s.cache["custom-agents"] = custom
	return s, b
}

// TestSettingsAgentsRendersAndChecks asserts the page explains the catalog and
// renders every check outcome: installed version, newer release, failure, and
// the no-local-install note.
func TestSettingsAgentsRendersAndChecks(t *testing.T) {
	s, _ := agentsShellForTest(t)
	s.agentVer["claude_code"] = "0.86.0"
	s.agentLatest["claude_code"] = "0.90.0"
	s.agentCheckNote["open_code"] = "No local installation found."
	s.agentCheckErr["gemini"] = "Version check failed: npm failed"
	tt := ui.NewTester(s.View, 1240, 820)

	if !tt.HasText("Agents are launched over ACP") {
		t.Errorf("help text missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("15 agents in the built-in catalog") {
		t.Errorf("catalog count missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("Claude Code") {
		t.Errorf("built-in agent missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("update available: v0.90.0") {
		t.Errorf("latest-release hint missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("Version check failed: npm failed") {
		t.Errorf("probe error missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("No local installation found.") {
		t.Errorf("not-installed note missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("Custom agents") || !tt.HasText("No custom agents yet.") {
		t.Errorf("custom section missing; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "settings-agents")
}

// TestSettingsAgentsCheckVersionFeedback clicks Check on a fake agent row, so
// the probe is local-only, and waits for the note + toast.
func TestSettingsAgentsCheckVersionFeedback(t *testing.T) {
	s, _ := agentsShellForTest(t)
	s.cache["agents"] = json.RawMessage(`[{"agent_type":"not_an_agent","name":"Fake Agent","description":"probe only","available":false}]`)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Fake Agent") {
		t.Fatalf("fake row missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Check"); err != nil {
		t.Fatalf("click Check: %v", err)
	}
	if !waitText(t, tt, "No local installation found.", true) {
		t.Fatalf("check note missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("Fake Agent checked") {
		t.Errorf("check toast missing; texts = %v", tt.Texts())
	}
}

// TestSettingsAgentsCustomCreateAndDelete walks the form through each
// validation error, saves a valid agent, and deletes it via the confirmation.
func TestSettingsAgentsCustomCreateAndDelete(t *testing.T) {
	s, _ := agentsShellForTest(t)
	// One catalog row keeps the custom section above the fold, so the delete
	// icon is clickable without scrolling.
	s.cache["agents"] = json.RawMessage(`[{"agent_type":"claude_code","name":"Claude Code","description":"","available":true}]`)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("No custom agents yet.") {
		t.Fatalf("empty custom section missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Add custom agent"); err != nil {
		t.Fatalf("click Add custom agent: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Registry id") || !tt.HasText("Save agent") {
		t.Fatalf("form not shown; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "settings-agents-form")

	if err := tt.Click("Save agent"); err != nil {
		t.Fatalf("click Save agent (blank): %v", err)
	}
	tt.Frame()
	if !tt.HasText("A registry id is required.") {
		t.Fatalf("id-required validation missing; texts = %v", tt.Texts())
	}

	s.caEditor.registryID = "bad id"
	tt.Frame()
	if err := tt.Click("Save agent"); err != nil {
		t.Fatalf("click Save agent (bad id): %v", err)
	}
	tt.Frame()
	if !tt.HasText("Use only letters, digits, dot, dash, or underscore") {
		t.Fatalf("id-invalid validation missing; texts = %v", tt.Texts())
	}

	s.caEditor.registryID = "team_agent"
	tt.Frame()
	if err := tt.Click("Save agent"); err != nil {
		t.Fatalf("click Save agent (no name): %v", err)
	}
	tt.Frame()
	if !tt.HasText("A name is required.") {
		t.Fatalf("name-required validation missing; texts = %v", tt.Texts())
	}

	s.caEditor.name = "Team Agent"
	tt.Frame()
	if err := tt.Click("Save agent"); err != nil {
		t.Fatalf("click Save agent (no package): %v", err)
	}
	tt.Frame()
	if !tt.HasText("A package is required") {
		t.Fatalf("package-required validation missing; texts = %v", tt.Texts())
	}

	s.caEditor.pkg = "@acme/agent"
	s.caEditor.version = "bad version"
	tt.Frame()
	if err := tt.Click("Save agent"); err != nil {
		t.Fatalf("click Save agent (bad version): %v", err)
	}
	tt.Frame()
	if !tt.HasText("max 128") {
		t.Fatalf("version-invalid validation missing; texts = %v", tt.Texts())
	}

	s.caEditor.version = "1.0.0"
	s.caEditor.skillsDir = "relative/skills"
	tt.Frame()
	if err := tt.Click("Save agent"); err != nil {
		t.Fatalf("click Save agent (relative skills): %v", err)
	}
	tt.Frame()
	if !tt.HasText("The skills directory must be absolute") {
		t.Fatalf("skills-abs validation missing; texts = %v", tt.Texts())
	}

	s.caEditor.skillsDir = "~/agents/team/skills"
	tt.Frame()
	if err := tt.Click("Save agent"); err != nil {
		t.Fatalf("click Save agent (valid): %v", err)
	}
	if !waitText(t, tt, "Team Agent", true) {
		t.Fatalf("saved custom agent not listed; texts = %v", tt.Texts())
	}
	// The saved row carries the launcher details the backend resolved.
	if !tt.HasText("custom: team_agent") || !tt.HasText("npx") {
		t.Errorf("custom row details missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("Custom agent Team Agent saved") {
		t.Errorf("save toast missing; texts = %v", tt.Texts())
	}
	time.Sleep(1200 * time.Millisecond)
	tt.Frame()
	dumpFrame(t, tt, "settings-agents-custom")

	if err := tt.Click("Delete Team Agent"); err != nil {
		t.Fatalf("click delete icon: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Remove custom agent Team Agent?") {
		t.Fatalf("delete confirmation missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Delete"); err != nil {
		t.Fatalf("confirm Delete: %v", err)
	}
	if !waitText(t, tt, "No custom agents yet.", true) {
		t.Fatalf("delete not reflected; texts = %v", tt.Texts())
	}
}

// TestCustomAgentRules pins the client-side mirrors of the backend rules.
func TestCustomAgentRules(t *testing.T) {
	ids := map[string]bool{
		"team_agent":   true,
		"Team.Agent-2": true,
		"":             false,
		".hidden":      false,
		"bad id":       false,
		"team/agent":   false,
		"claude_code":  true, // built-in collisions are the backend's diagnosis
	}
	for id, want := range ids {
		if got := validCustomAgentID(id); got != want {
			t.Errorf("validCustomAgentID(%q) = %v, want %v", id, got, want)
		}
	}
	long := make([]byte, 65)
	for i := range long {
		long[i] = 'a'
	}
	if validCustomAgentID(string(long)) {
		t.Errorf("validCustomAgentID(65 chars) = true, want false")
	}
	versions := map[string]bool{
		"1.0.0": true, "1.0.0-beta.1": true, "custom": true,
		"": false, ".1": false, "1 0": false, "v1/2": false,
	}
	for v, want := range versions {
		if got := validCustomVersion(v); got != want {
			t.Errorf("validCustomVersion(%q) = %v, want %v", v, got, want)
		}
	}
}
