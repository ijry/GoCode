package app

// settings_collaboration_test.go covers Settings → Collaboration: the
// delegation form (native switch + step slider + cache field), the agent
// defaults tab, and the in-conversation tool matrix. Saves round-trip through
// the real backend, so the assertions read the applied values back.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func collabShellForTest(t *testing.T) (*Shell, *Backend) {
	t.Helper()
	b := newBackendForTest(t)
	s := New(b)
	s.stream = newStreamForTest()
	s.section = "settings"
	s.settingsPage = "collaboration"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["delegation"] = json.RawMessage(`{"enabled":true,"depth_limit":5,"completed_cache_max_mb":256,"agent_defaults":{"claude_code":{"mode":"auto","config_values":{"model":"opus"}}}}`)
	s.cache["tools-feedback"] = json.RawMessage(`{"enabled":true}`)
	s.cache["tools-question"] = json.RawMessage(`{"enabled":false}`)
	s.cache["tools-session-info"] = json.RawMessage(`{"enabled":true}`)
	s.cache["tools-browser"] = json.RawMessage(`{"enabled":true,"eval":true}`)
	s.cache["tools-computer"] = json.RawMessage(`{"enabled":false}`)
	s.cache["tools-chat-authoring"] = json.RawMessage(`{"automations_enabled":true,"work_tasks_enabled":false}`)
	return s, b
}

func TestSettingsCollaborationPage(t *testing.T) {
	s, _ := collabShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)

	for _, want := range []string{
		"Collaboration", "Multi-Agent Collaboration",
		"Enable delegation", "Maximum delegation depth", "Completed-result cache (MB)",
		"General", "Agent defaults",
		"In-conversation tools", "Live Feedback", "Ask user question", "Get session info",
		"Read and drive the built-in browser", "Run code in the built-in browser",
		"See and use your desktop's windows", "Create automations", "Create to-do tasks",
	} {
		if !tt.HasText(want) {
			t.Errorf("collaboration page missing %q; texts = %v", want, tt.Texts())
		}
	}
	// Native Switch renders its state; the StepSlider shows the seeded depth.
	if !tt.HasText("on") || !tt.HasText("off") {
		t.Errorf("switch states missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("5") {
		t.Errorf("depth value missing; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "settings-collaboration")

	// Agent defaults tab lists the per-agent override and its config count.
	if err := tt.Click("Agent defaults"); err != nil {
		t.Fatalf("click Agent defaults: %v", err)
	}
	tt.Frame()
	for _, want := range []string{"claude_code", "Mode: auto · 1 config", "Remove override"} {
		if !tt.HasText(want) {
			t.Errorf("agent defaults tab missing %q; texts = %v", want, tt.Texts())
		}
	}
	if err := tt.Click("Remove override"); err != nil {
		t.Fatalf("click Remove override: %v", err)
	}
	tt.Frame()
	if !tt.HasText("No per-agent delegation overrides.") || !tt.HasText("Unsaved changes") {
		t.Errorf("override removal not reflected; texts = %v", tt.Texts())
	}

	// Change the depth and save: the backend clamps and applies it.
	s.collabDepth = 7
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("click Save: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		raw, err := backendRaw(t, s, "get_delegation_settings")
		if err != nil {
			return false
		}
		d := asObj(raw)
		return intVal(d, "depth_limit") == 7 && truthy(d["enabled"]) && d["agent_defaults"] == nil
	}) {
		t.Fatalf("delegation save did not apply; texts = %v", tt.Texts())
	}

	// The tool matrix saves only the groups that moved.
	s.tools.feedback = false
	s.tools.browser = false // forces eval off too
	tt.Frame()
	s.saveAgentTools("en")
	if !waitUntil(t, tt, func() bool {
		raw, err := backendRaw(t, s, "get_feedback_settings")
		if err != nil {
			return false
		}
		if truthy(asObj(raw)["enabled"]) {
			return false
		}
		braw, err := backendRaw(t, s, "get_browser_tools_settings")
		if err != nil {
			return false
		}
		bv := asObj(braw)
		return !truthy(bv["enabled"]) && !truthy(bv["eval"])
	}) {
		t.Fatalf("tool save did not apply; texts = %v", tt.Texts())
	}
	if !waitUntil(t, tt, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.toolsLoaded && !s.tools.feedback && !s.tools.browser
	}) {
		t.Fatalf("tool switches did not reseed; texts = %v", tt.Texts())
	}
}

// TestSettingsCollaborationLocalize renders the page in Chinese: the section
// headers, switch rows and tab labels follow the window language.
func TestSettingsCollaborationLocalize(t *testing.T) {
	s, _ := collabShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	for _, want := range []string{
		"多 Agent 协作", "启用委派", "最大委派深度", "对话内工具", "实时反馈", "创建待办任务",
	} {
		if !tt.HasText(want) {
			t.Errorf("zh collaboration page missing %q; texts = %v", want, tt.Texts())
		}
	}
}

// backendRaw runs one backend command directly, bypassing the view's cache, to
// observe what a save actually persisted.
func backendRaw(t *testing.T, s *Shell, cmd string) (json.RawMessage, error) {
	t.Helper()
	return s.B.raw(cmd, nil)
}
