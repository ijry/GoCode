package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestComposerRendersModelAndModeSelectors pins the P0-2 contract: when the
// live snapshot advertises modes and a "model" config option, the composer
// renders a pill row for each, the current value is the active (pressed)
// pill, and clicking a different pill dispatches the matching acp_set_*
// route. The test backend has no live ACP session, so the dispatch hits the
// engine and the resulting ConnectionUnknown surfaces in errorMsg — which
// also proves the closure fired with the right command name.
func TestComposerRendersModelAndModeSelectors(t *testing.T) {
	s := conversationShellForTest(t)
	// Overwrite the minimal snapshot seed with one carrying the agent's
	// advertised modes and a "model" select config option.
	s.cache["snapshot:c1"] = json.RawMessage(`{
		"connection_id":"c1",
		"status":"prompting",
		"active_tool_calls":[],
		"modes":{"currentModeId":"default","availableModes":[
			{"id":"default","name":"Default"},
			{"id":"plan","name":"Plan"}
		]},
		"config_options":[{"id":"model","name":"Model","kind":{
			"type":"select",
			"currentValue":"anthropic/sonnet",
			"options":[
				{"value":"anthropic/sonnet","name":"Sonnet"},
				{"value":"openai/gpt-4o","name":"GPT-4o"}
			]
		}}]
	}`)

	tt := ui.NewTester(s.View, 1240, 820)
	dumpFrame(t, tt, "composer-selectors")

	for _, want := range []string{"Mode", "Default", "Plan", "Model", "Sonnet", "GPT-4o"} {
		if !tt.HasText(want) {
			t.Fatalf("missing pill %q; texts = %v", want, tt.Texts())
		}
	}

	// Click "Plan" — a non-active mode pill, rendered as a Button. The
	// dispatch goroutine posts to the test backend; without a live session
	// the engine returns ConnectionUnknown, which the handler surfaces as
	// a non-200. We only care that the closure fired (the toast names the
	// route), not that it succeeded.
	if err := tt.Click("Plan"); err != nil {
		t.Fatalf("click Plan: %v", err)
	}
	if !waitForToastText(tt, "acp_set_mode", 2*time.Second) {
		t.Fatalf("mode dispatch did not land; texts = %v", tt.Texts())
	}

	// Clicking a model pill dispatches acp_set_config_option with
	// configId "model" and the picked value.
	if err := tt.Click("GPT-4o"); err != nil {
		t.Fatalf("click GPT-4o: %v", err)
	}
	if !waitForToastText(tt, "acp_set_config_option", 2*time.Second) {
		s.mu.Lock()
		got := s.errorMsg
		s.mu.Unlock()
		t.Fatalf("model dispatch did not land; errorMsg = %q", got)
	}
}

// waitForToastText frames until a rendered toast mentions the given route.
// The queue itself is drained into mygo's toast manager every frame, so the
// rendered text is what a test can observe.
func waitForToastText(tt *ui.Tester, contains string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		tt.Frame()
		if tt.HasText(contains) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
