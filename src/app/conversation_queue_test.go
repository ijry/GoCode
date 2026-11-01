package app

// conversation_queue_test.go covers the queued-send rows above the composer:
// the text of each waiting prompt, and removing one.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestConversationQueuedPrompts(t *testing.T) {
	s := conversationShellForTest(t)
	s.cache["snapshot:c1"] = json.RawMessage(`{"connection_id":"c1","status":"prompting",
		"active_tool_calls":[],"queued_prompts":["run the tests","then commit"]}`)

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	for _, want := range []string{"Queued", "run the tests", "then commit"} {
		if !tt.HasText(want) {
			t.Fatalf("queue rows missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "conversation-queue")

	if err := tt.Click("Remove"); err != nil {
		t.Fatalf("click remove: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("queued message removed") }) {
		t.Fatalf("no removal toast; texts = %v", tt.Texts())
	}
}

func TestConversationNoQueueWhenIdle(t *testing.T) {
	s := conversationShellForTest(t) // snapshot has no queued_prompts
	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if tt.HasText("Queued") {
		t.Fatalf("queue rows shown without a queue; texts = %v", tt.Texts())
	}
}
