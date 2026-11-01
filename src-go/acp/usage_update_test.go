package acp

// usage_update_test.go covers folding ACP `usage_update` notifications into the
// session snapshot's context_usage, including the null fields some agents send.

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUsageUpdateFoldsIntoSnapshot(t *testing.T) {
	client, agent, done := pipePair(t)
	defer done()
	agent.HandleRequest("initialize", func(json.RawMessage) (any, error) {
		return map[string]any{"protocolVersion": ACPProtocolVersion}, nil
	})
	agent.HandleRequest("session/new", func(json.RawMessage) (any, error) {
		return map[string]any{"sessionId": "sess-u"}, nil
	})

	state := NewSessionState("conn-u")
	sess := NewAgentSession(client, state, "claude_code")
	if err := sess.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := sess.NewSession(t.Context(), "/tmp/x", nil); err != nil {
		t.Fatalf("new session: %v", err)
	}

	// A normal reading, then one with null fields (some adapters send those).
	_ = agent.Notify("session/update", map[string]any{
		"sessionId": "sess-u",
		"update":    map[string]any{"sessionUpdate": "usage_update", "used": 12345, "size": 200000},
	})
	_ = agent.Notify("session/update", map[string]any{
		"sessionId": "sess-u",
		"update":    map[string]any{"sessionUpdate": "usage_update", "used": nil, "size": nil},
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		snap := state.Snapshot()
		if snap.ContextUsage != nil && snap.ContextUsage.Used == 0 && snap.ContextUsage.Size == 0 {
			// The null reading landed; the first must have landed before it.
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("usage not folded: %+v", snap.ContextUsage)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The event stream carries both readings, in order, for live clients.
	envs, _ := state.RecentEventsAfter(0)
	var used []float64
	for _, e := range envs {
		var m map[string]any
		_ = json.Unmarshal(e.Payload, &m)
		if m["type"] == "usage_update" {
			v, _ := m["used"].(float64)
			used = append(used, v)
		}
	}
	if len(used) != 2 || used[0] != 12345 || used[1] != 0 {
		t.Fatalf("usage events = %v, want [12345 0]", used)
	}
}
