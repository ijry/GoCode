package acp

// plan_state_test.go: an ACP `plan` update lands in the session snapshot, so a
// cold attach can render the plan without waiting for the next update.

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPlanUpdateFoldsIntoSnapshot(t *testing.T) {
	client, agent, done := pipePair(t)
	defer done()
	agent.HandleRequest("initialize", func(json.RawMessage) (any, error) {
		return map[string]any{"protocolVersion": ACPProtocolVersion}, nil
	})
	agent.HandleRequest("session/new", func(json.RawMessage) (any, error) {
		return map[string]any{"sessionId": "sess-p"}, nil
	})

	state := NewSessionState("conn-p")
	sess := NewAgentSession(client, state, "claude_code")
	if err := sess.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := sess.NewSession(t.Context(), "/tmp/x", nil); err != nil {
		t.Fatalf("new session: %v", err)
	}

	_ = agent.Notify("session/update", map[string]any{
		"sessionId": "sess-p",
		"update": map[string]any{
			"sessionUpdate": "plan",
			"entries": []any{
				map[string]any{"content": "read the parser", "status": "completed", "priority": "high"},
				map[string]any{"content": "fix it", "status": "in_progress"},
			},
		},
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		snap := state.Snapshot()
		if len(snap.Plan) > 0 {
			var entries []map[string]any
			if err := json.Unmarshal(snap.Plan, &entries); err != nil {
				t.Fatalf("plan not an array: %v", err)
			}
			if len(entries) != 2 || entries[0]["content"] != "read the parser" || entries[1]["status"] != "in_progress" {
				t.Fatalf("plan entries = %v", entries)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("plan not folded into the snapshot")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
