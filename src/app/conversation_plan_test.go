package app

// conversation_plan_test.go covers the plan card above the composer: the
// completed/total count, the step rows, and the collapse toggle.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestConversationPlanCard(t *testing.T) {
	s := conversationShellForTest(t)
	s.cache["snapshot:c1"] = json.RawMessage(`{
		"connection_id":"c1","status":"prompting","active_tool_calls":[],
		"plan":[
			{"content":"read the parser","status":"completed","priority":"high"},
			{"content":"fix the parser","status":"in_progress"},
			{"content":"add a test","status":"pending"}]}`)

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	for _, want := range []string{"Plan", "1/3", "read the parser", "fix the parser", "add a test"} {
		if !tt.HasText(want) {
			t.Fatalf("plan card missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "conversation-plan")

	// The header collapses the steps.
	if err := tt.Click("Plan"); err != nil {
		t.Fatalf("click plan header: %v", err)
	}
	tt.Frame()
	if tt.HasText("fix the parser") {
		t.Fatalf("steps still shown after collapse; texts = %v", tt.Texts())
	}
	if !tt.HasText("1/3") {
		t.Fatalf("header lost after collapse; texts = %v", tt.Texts())
	}
}

func TestConversationPlanCardFromLiveEvent(t *testing.T) {
	s := conversationShellForTest(t) // snapshot has no plan
	s.stream.handle(&wsFrame{Type: "event", Envelope: map[string]any{
		"connection_id": "c1", "type": "plan_update",
		"entries": []any{
			map[string]any{"content": "live step one", "status": "completed"},
			map[string]any{"content": "live step two", "status": "in_progress"},
		},
	}})

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if !tt.HasText("live step two") || !tt.HasText("1/2") {
		t.Fatalf("live plan not shown; texts = %v", tt.Texts())
	}
}
