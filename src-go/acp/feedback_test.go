package acp

// feedback_test.go covers the live-feedback data path: submitting a note parks
// it as pending on the snapshot, the companion's check delivers it (pending →
// delivered), and a new prompt clears the turn's notes.

import (
	"encoding/json"
	"testing"
)

func newFeedbackTestSession(t *testing.T) (*Manager, *SessionState, *AgentSession) {
	t.Helper()
	client, agent, done := pipePair(t)
	t.Cleanup(done)
	agent.HandleRequest("initialize", func(json.RawMessage) (any, error) {
		return map[string]any{"protocolVersion": ACPProtocolVersion}, nil
	})
	agent.HandleRequest("session/new", func(json.RawMessage) (any, error) {
		return map[string]any{"sessionId": "sess-f"}, nil
	})
	agent.HandleRequest("session/prompt", func(json.RawMessage) (any, error) {
		return map[string]any{"stopReason": "end_turn"}, nil
	})
	state := NewSessionState("conn-f")
	state.FeedbackToolAvailable = true // what ConnectLive sets when it offers the companion
	sess := NewAgentSession(client, state, "claude_code")
	if err := sess.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := sess.NewSession(t.Context(), "/tmp/x", nil); err != nil {
		t.Fatalf("new session: %v", err)
	}
	m := NewManager(nil, nil)
	m.live["conn-f"] = &liveSession{session: sess, state: state}
	return m, state, sess
}

func TestSubmitAndDeliverFeedback(t *testing.T) {
	m, state, _ := newFeedbackTestSession(t)

	item, err := m.SubmitFeedback("conn-f", "please use tabs")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if item.Status != "pending" || item.Text != "please use tabs" || item.ID == "" {
		t.Fatalf("item = %+v", item)
	}
	snap := state.Snapshot()
	var notes []FeedbackItem
	if err := json.Unmarshal(snap.Feedback, &notes); err != nil {
		t.Fatalf("snapshot feedback: %v", err)
	}
	if len(notes) != 1 || notes[0].Status != "pending" {
		t.Fatalf("snapshot notes = %+v", notes)
	}
	if !snap.FeedbackToolAvailable || snap.NativeSteeringAvailable {
		t.Fatalf("capability flags = tool:%v native:%v", snap.FeedbackToolAvailable, snap.NativeSteeringAvailable)
	}

	delivered := m.CheckUserFeedback("conn-f")
	if len(delivered) != 1 || delivered[0].ID != item.ID || delivered[0].Status != "delivered" || delivered[0].DeliveredAt == "" {
		t.Fatalf("delivered = %+v", delivered)
	}
	// A second check finds nothing pending (already read).
	if again := m.CheckUserFeedback("conn-f"); len(again) != 0 {
		t.Fatalf("second check delivered %+v", again)
	}
	// The snapshot now reports the note delivered.
	notes = nil
	_ = json.Unmarshal(state.Snapshot().Feedback, &notes)
	if len(notes) != 1 || notes[0].Status != "delivered" {
		t.Fatalf("snapshot after deliver = %+v", notes)
	}
}

func TestSubmitFeedbackNoLiveSession(t *testing.T) {
	m := NewManager(nil, nil)
	if _, err := m.SubmitFeedback("nope", "hi"); err == nil {
		t.Fatal("submit without a live session should fail")
	}
	if got := m.CheckUserFeedback("nope"); got != nil {
		t.Fatalf("check without a live session = %+v", got)
	}
}

func TestNewPromptClearsFeedback(t *testing.T) {
	m, state, sess := newFeedbackTestSession(t)
	if _, err := m.SubmitFeedback("conn-f", "note one"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := sess.Prompt(t.Context(), []any{map[string]any{"type": "text", "text": "next"}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if raw := state.Snapshot().Feedback; len(raw) != 0 {
		t.Fatalf("notes survived a new prompt: %s", raw)
	}
}
