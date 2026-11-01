package app

// conversation_feedback_test.go covers the live-feedback strip above the
// composer: waiting / received states while a turn runs, the expired form once
// the turn ends, and the inline note composer opened from the composer entry.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestConversationFeedbackNotes(t *testing.T) {
	s := conversationShellForTest(t)
	s.cache["snapshot:c1"] = json.RawMessage(`{"connection_id":"c1","status":"prompting","active_tool_calls":[],"feedback_tool_available":true,"feedback":[
		{"id":"f1","text":"use tabs","created_at":"2026-10-25T10:00:00Z","status":"pending"},
		{"id":"f2","text":"skip the docs","created_at":"2026-10-25T10:01:00Z","status":"delivered"}]}`)
	s.cache["feedback-settings"] = json.RawMessage(`{"enabled":true}`)

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	for _, want := range []string{"use tabs", "skip the docs", "waiting", "received"} {
		if !tt.HasText(want) {
			t.Fatalf("notes missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "conversation-feedback")

	// The composer entry opens the inline note composer.
	if err := tt.Click("Feedback"); err != nil {
		t.Fatalf("click Feedback: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Live feedback") || !tt.HasText("Cancel") {
		t.Fatalf("note composer did not open; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "conversation-feedback-composer")
}

func TestConversationFeedbackExpired(t *testing.T) {
	s := conversationShellForTest(t)
	// The turn ended (status connected) with a note the agent never read.
	s.cache["snapshot:c1"] = json.RawMessage(`{"connection_id":"c1","status":"connected","active_tool_calls":[],"feedback":[
		{"id":"f1","text":"use tabs","created_at":"2026-10-25T10:00:00Z","status":"pending"}]}`)
	s.cache["feedback-settings"] = json.RawMessage(`{"enabled":true}`)

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	for _, want := range []string{"The turn ended before the agent read these", "use tabs", "Send as message", "Dismiss"} {
		if !tt.HasText(want) {
			t.Fatalf("expired form missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "conversation-feedback-expired")
}

func TestConversationNoFeedbackWhenDisabled(t *testing.T) {
	s := conversationShellForTest(t)
	s.cache["snapshot:c1"] = json.RawMessage(`{"connection_id":"c1","status":"prompting","active_tool_calls":[],"feedback_tool_available":true,"feedback":[
		{"id":"f1","text":"use tabs","created_at":"2026-10-25T10:00:00Z","status":"pending"}]}`)
	// Feature off: the entry is not offered (but the note list still shows — the
	// server only sends notes when the user sent one).
	s.cache["feedback-settings"] = json.RawMessage(`{"enabled":false}`)

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if err := tt.Click("Feedback"); err == nil {
		t.Fatal("feedback entry shown while disabled")
	}
}
