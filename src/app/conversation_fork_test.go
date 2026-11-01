package app

// conversation_fork_test.go covers the header's Fork action: it appears only
// with a live connection, calls acp_fork with the conversation's connection,
// and reports a failure through an error toast.

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestConversationForkAction(t *testing.T) {
	s := conversationShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Fork") {
		t.Fatalf("fork action missing; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "conversation-header-fork")

	// The seeded connection ("c1") is not a real live session, so the backend
	// refuses the fork — which exercises the whole call path and its toast.
	if err := tt.Click("Fork"); err != nil {
		t.Fatalf("click Fork: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("fork failed") }) {
		t.Fatalf("no fork-failure toast; texts = %v", tt.Texts())
	}
}

func TestConversationForkHiddenWithoutConnection(t *testing.T) {
	s := conversationShellForTest(t)
	s.connByConv = map[string]string{}
	tt := ui.NewTester(s.View, 1240, 820)
	if tt.HasText("Fork") {
		t.Fatalf("fork shown without a live connection; texts = %v", tt.Texts())
	}
}
