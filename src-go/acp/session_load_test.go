package acp

// session_load_test.go covers resuming a conversation's agent session: the
// capability gate, the load call, and the replay suppression that keeps a
// resumed conversation from showing its history twice.

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// loadAgent advertises session/load and replays two updates during the call.
func loadAgent(agent *Peer, loads *int) {
	agent.HandleRequest("initialize", func(json.RawMessage) (any, error) {
		return map[string]any{
			"protocolVersion":   ACPProtocolVersion,
			"agentCapabilities": map[string]any{"loadSession": true},
		}, nil
	})
	agent.HandleRequest("session/new", func(json.RawMessage) (any, error) {
		return map[string]any{"sessionId": "sess-new"}, nil
	})
	agent.HandleRequest("session/load", func(params json.RawMessage) (any, error) {
		var p struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(params, &p)
		*loads++
		// Replay history, as a conforming agent does before answering.
		for _, text := range []string{"old one ", "old two"} {
			_ = agent.Notify("session/update", map[string]any{
				"sessionId": p.SessionID,
				"update": map[string]any{
					"sessionUpdate": "agent_message_chunk",
					"content":       map[string]any{"type": "text", "text": text},
				},
			})
		}
		return map[string]any{}, nil
	})
}

func TestLoadSessionSuppressesReplay(t *testing.T) {
	client, agent, done := pipePair(t)
	defer done()
	loads := 0
	loadAgent(agent, &loads)

	state := NewSessionState("conn-load")
	sess := NewAgentSession(client, state, "claude_code")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := sess.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if !sess.SupportsLoadSession() {
		t.Fatal("agent advertised loadSession but the session says otherwise")
	}

	sid, err := sess.LoadSession(ctx, "sess-old", "/tmp/x", nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if sid != "sess-old" || sess.SessionID() != "sess-old" {
		t.Fatalf("session id = %q / %q, want sess-old", sid, sess.SessionID())
	}
	if loads != 1 {
		t.Fatalf("session/load calls = %d, want 1", loads)
	}

	// The replayed chunks must not reach the event stream: the transcript is
	// read from disk, so replaying it would duplicate every message.
	time.Sleep(50 * time.Millisecond)
	for _, ev := range collectEvents(state) {
		if ev["type"] == "content_delta" {
			t.Fatalf("replayed update leaked into the stream: %v", ev)
		}
	}
	// A session_started marker does go out, flagged as a resume.
	found := false
	for _, ev := range collectEvents(state) {
		if ev["type"] == "session_started" && ev["resumed"] == true {
			found = true
		}
	}
	if !found {
		t.Fatal("no resumed session_started event")
	}
}

func TestConversationSessionID(t *testing.T) {
	database := forkDB(t)
	m := NewManager(database, nil)
	if got := m.conversationSessionID(1); got != "" {
		t.Fatalf("fresh conversation has external_id %q", got)
	}
	m.rememberSessionID(1, "sess-1")
	if got := m.conversationSessionID(1); got != "sess-1" {
		t.Fatalf("conversationSessionID = %q, want sess-1", got)
	}
	if got := m.conversationSessionID(999); got != "" {
		t.Fatalf("unknown conversation returned %q", got)
	}
}
