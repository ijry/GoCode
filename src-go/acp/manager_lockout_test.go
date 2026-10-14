package acp

import (
	"encoding/json"
	"testing"
	"time"
)

// nopSink is an EventSink that drops every event (Manager needs one non-nil).
type nopSink struct{}

func (nopSink) Send(string, any)                {}
func (nopSink) SendRaw(string, json.RawMessage) {}

func TestLockOutNewConnectionsGate(t *testing.T) {
	m := NewManager(nil, nopSink{})
	unlock := m.LockOutNewConnections()

	acquired := make(chan struct{})
	go func() {
		m.connGate.RLock()
		m.connGate.RUnlock()
		close(acquired)
	}()
	select {
	case <-acquired:
		t.Fatalf("shared gate acquired while locked out")
	case <-time.After(50 * time.Millisecond):
	}

	unlock()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatalf("shared gate still blocked after unlock")
	}
}

func TestDisconnectByAgentTypeCounts(t *testing.T) {
	m := NewManager(nil, nopSink{})
	m.Connect("antigravity", "/x", 0)
	m.Connect("antigravity", "/y", 0)
	m.Connect("codex", "/z", 0)

	if n := m.DisconnectByAgentType("antigravity"); n != 2 {
		t.Fatalf("disconnected = %d, want 2", n)
	}
	// Idempotent: the rows are gone, so a second sweep finds nothing.
	if n := m.DisconnectByAgentType("antigravity"); n != 0 {
		t.Fatalf("second sweep = %d, want 0", n)
	}
	// The codex connection is untouched.
	remaining := m.ListConnections()
	if len(remaining) != 1 || remaining[0].AgentType != "codex" {
		t.Fatalf("remaining = %+v, want one codex connection", remaining)
	}
}
