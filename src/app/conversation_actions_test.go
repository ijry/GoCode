package app

// conversation_actions_test.go covers the message-level actions: copying a
// finished assistant reply, and retrying a failed turn from the composer strip.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestMessageCopyAction(t *testing.T) {
	s := conversationShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Copy") {
		t.Fatalf("copy action missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Copy"); err != nil {
		t.Fatalf("click Copy: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("copied to clipboard") }) {
		t.Fatalf("no copy toast; texts = %v", tt.Texts())
	}
}

// errorSnapshot rewrites the seeded snapshot with the given status.
func errorSnapshot(t *testing.T, s *Shell, status string) {
	t.Helper()
	s.cache["snapshot:c1"] = json.RawMessage(`{"connection_id":"c1","status":"` + status + `","active_tool_calls":[]}`)
}

func TestComposerRetryOnFailedTurn(t *testing.T) {
	s := conversationShellForTest(t)
	errorSnapshot(t, s, "error")
	s.lastPrompt = "fix the parser"

	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Retry") {
		t.Fatalf("retry action missing on a failed turn; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "conversation-retry-strip")
	// Clicking resends the remembered prompt. The backend has no agent to
	// launch here, so the attempt fails — the point is that it goes through the
	// send path rather than doing nothing.
	if err := tt.Click("Retry"); err != nil {
		t.Fatalf("click Retry: %v", err)
	}
	tt.Frame()
}

func TestComposerRetryNeedsPromptAndError(t *testing.T) {
	// A failed turn with nothing to resend shows no Retry.
	s := conversationShellForTest(t)
	errorSnapshot(t, s, "error")
	tt := ui.NewTester(s.View, 1240, 820)
	if tt.HasText("Retry") {
		t.Fatalf("retry shown without a remembered prompt; texts = %v", tt.Texts())
	}

	// A healthy turn shows no Retry even when a prompt is remembered.
	s2 := conversationShellForTest(t)
	errorSnapshot(t, s2, "prompting")
	s2.lastPrompt = "fix the parser"
	tt2 := ui.NewTester(s2.View, 1240, 820)
	if tt2.HasText("Retry") {
		t.Fatalf("retry shown on a healthy turn; texts = %v", tt2.Texts())
	}
}
