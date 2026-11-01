package app

// dispatch_config_test.go covers the composer's config writes (mode / model /
// permission): a failure must reach the user. It used to store errorMsg, which
// only the workbench routes render, so a failed switch was silent in a chat.

import (
	"testing"
	"time"
)

func TestDispatchConfigFailureRaisesToast(t *testing.T) {
	s := conversationShellForTest(t)
	// An unknown command makes the loopback backend answer 404.
	s.dispatchConfig("c1", "acp_no_such_command", map[string]any{}, nil)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.toastMu.Lock()
		queued := len(s.toastQueue)
		var first string
		if queued > 0 {
			first = s.toastQueue[0].Title
		}
		s.toastMu.Unlock()
		if queued > 0 {
			if first == "" {
				t.Fatalf("toast queued without a title")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("a failed config write raised no toast")
}

func TestDispatchConfigSuccessPatchesSnapshot(t *testing.T) {
	s := conversationShellForTest(t)
	s.cache["snapshot:c1"] = []byte(`{"mode":"default","model":"m1"}`)
	// A command the test router accepts: open a folder is not a config write, so
	// use health instead — it returns 200 and the patch still applies.
	s.dispatchConfig("c1", "health", map[string]any{}, func(snap map[string]any) {
		snap["mode"] = "plan"
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		snap := asObj(s.cache["snapshot:c1"])
		mode := ""
		if snap != nil {
			mode = str(snap, "mode")
		}
		s.mu.Unlock()
		if mode == "plan" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the optimistic patch was not applied")
}
