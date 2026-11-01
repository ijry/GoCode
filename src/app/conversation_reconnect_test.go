package app

// conversation_reconnect_test.go covers the composer strip's Reconnect action
// when the conversation's live connection has dropped.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestConversationDisconnectedOffersReconnect(t *testing.T) {
	s := conversationShellForTest(t)
	s.cache["snapshot:c1"] = json.RawMessage(`{"connection_id":"c1","status":"disconnected","active_tool_calls":[]}`)

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if !tt.HasText("Disconnected") || !tt.HasText("Reconnect") {
		t.Fatalf("reconnect affordance missing; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "conversation-reconnect")

	// Clicking it kicks off a reconnect (against the test backend this surfaces
	// either the connecting status or a connect error banner — never a panic).
	if err := tt.Click("Reconnect"); err != nil {
		t.Fatalf("click Reconnect: %v", err)
	}
	tt.Frame()
}

func TestConversationConnectedHasNoReconnect(t *testing.T) {
	s := conversationShellForTest(t) // snapshot status prompting
	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if tt.HasText("Reconnect") {
		t.Fatalf("Reconnect shown while connected; texts = %v", tt.Texts())
	}
}
