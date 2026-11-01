package acp

// api_retry_test.go covers the in-flight API-retry reading: parsing the
// `_claude/sdkMessage` notification, folding it into the snapshot, and clearing
// it once the turn makes progress.

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseApiRetry(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		wantOK bool
	}{
		{"valid", `{"message":{"type":"system","subtype":"api_retry","attempt":3,"max_retries":10,"error":"rate limited","error_status":429,"retry_delay_ms":3000}}`, true},
		{"numeric strings", `{"message":{"type":"system","subtype":"api_retry","attempt":"2","max_retries":"5"}}`, true},
		{"other subtype", `{"message":{"type":"system","subtype":"init"}}`, false},
		{"other type", `{"message":{"type":"assistant","subtype":"api_retry"}}`, false},
		{"no message", `{}`, false},
		{"garbage", `not json`, false},
	}
	for _, c := range cases {
		st, ok := parseApiRetry(json.RawMessage(c.raw))
		if ok != c.wantOK {
			t.Fatalf("%s: ok = %v, want %v", c.name, ok, c.wantOK)
		}
		if !ok {
			continue
		}
		if c.name == "valid" {
			if st.Attempt == nil || *st.Attempt != 3 || st.MaxRetries == nil || *st.MaxRetries != 10 ||
				st.Error != "rate limited" || st.ErrorStatus == nil || *st.ErrorStatus != 429 ||
				st.RetryDelayMs == nil || *st.RetryDelayMs != 3000 {
				t.Fatalf("state = %+v", st)
			}
		}
		if c.name == "numeric strings" && (st.Attempt == nil || *st.Attempt != 2 || st.MaxRetries == nil || *st.MaxRetries != 5) {
			t.Fatalf("numeric-string state = %+v", st)
		}
	}
}

func TestApiRetryInSnapshotClearsOnProgress(t *testing.T) {
	client, agent, done := pipePair(t)
	defer done()
	agent.HandleRequest("initialize", func(json.RawMessage) (any, error) {
		return map[string]any{"protocolVersion": ACPProtocolVersion}, nil
	})
	agent.HandleRequest("session/new", func(json.RawMessage) (any, error) {
		return map[string]any{"sessionId": "sess-r"}, nil
	})

	state := NewSessionState("conn-r")
	sess := NewAgentSession(client, state, "claude_code")
	if err := sess.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := sess.NewSession(t.Context(), "/tmp/x", nil); err != nil {
		t.Fatalf("new session: %v", err)
	}

	_ = agent.Notify("_claude/sdkMessage", map[string]any{
		"sessionId": "sess-r",
		"message": map[string]any{
			"type": "system", "subtype": "api_retry",
			"attempt": 2, "max_retries": 5, "error": "overloaded", "error_status": 529, "retry_delay_ms": 2000,
		},
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		r := state.Snapshot().ApiRetry
		if r != nil && r.Attempt != nil && *r.Attempt == 2 {
			if r.Error != "overloaded" || r.ErrorStatus == nil || *r.ErrorStatus != 529 {
				t.Fatalf("retry = %+v", r)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("retry not recorded: %+v", state.Snapshot().ApiRetry)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Real progress (a content chunk) resolves the retry.
	_ = agent.Notify("session/update", map[string]any{
		"sessionId": "sess-r",
		"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "ok"}},
	})
	deadline = time.Now().Add(2 * time.Second)
	for {
		if state.Snapshot().ApiRetry == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("retry not cleared after progress")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
