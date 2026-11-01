package app

// conversation_liveconn_test.go covers adopting a live agent session the UI has
// no connection id for: a window reload or another client can leave one running,
// and the conversation must show its status instead of looking idle.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestConversationAdoptsLiveConnection(t *testing.T) {
	s := conversationShellForTest(t)
	// The UI forgot the connection id; the backend still knows the session.
	s.connByConv = map[string]string{}
	s.cache["liveconn:1"] = json.RawMessage(`{"connection_id":"c1","event_seq":7}`)

	tt := ui.NewTester(s.View, 1240, 820)
	if !waitUntil(t, tt, func() bool { return s.connByConv["1"] == "c1" }) {
		t.Fatalf("live connection not adopted; connByConv = %v", s.connByConv)
	}
	tt.Frame()
	// With the connection adopted, the composer offers Stop and the snapshot's
	// status drives the hint.
	if !tt.HasText("Stop") {
		t.Fatalf("composer did not adopt the live session; texts = %v", tt.Texts())
	}
}

func TestConversationNoLiveConnection(t *testing.T) {
	s := conversationShellForTest(t)
	s.connByConv = map[string]string{}
	// The backend answers "none" (a null result): the conversation stays idle.
	s.cache["liveconn:1"] = json.RawMessage(`null`)

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if got := s.connByConv["1"]; got != "" {
		t.Fatalf("adopted %q from a null lookup", got)
	}
	if tt.HasText("Stop") {
		t.Fatalf("idle conversation shows Stop; texts = %v", tt.Texts())
	}
}
