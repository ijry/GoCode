package acp

// prompt_queue_test.go covers the per-session prompt queue: a send that lands
// mid-turn waits for the running turn instead of interleaving on the same ACP
// session, and Stop drops whatever is queued behind it.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// blockingPromptAgent answers the handshake and blocks each prompt until the
// test releases it, recording the order prompts started in.
func blockingPromptAgent(agent *Peer, started *[]string, mu *sync.Mutex, release <-chan struct{}) {
	agent.HandleRequest("initialize", func(json.RawMessage) (any, error) {
		return map[string]any{"protocolVersion": ACPProtocolVersion, "agentCapabilities": map[string]any{}}, nil
	})
	agent.HandleRequest("session/new", func(json.RawMessage) (any, error) {
		return map[string]any{"sessionId": "sess-q"}, nil
	})
	agent.HandleRequest("session/prompt", func(params json.RawMessage) (any, error) {
		var p struct {
			Prompt []map[string]any `json:"prompt"`
		}
		_ = json.Unmarshal(params, &p)
		text := ""
		if len(p.Prompt) > 0 {
			text, _ = p.Prompt[0]["text"].(string)
		}
		mu.Lock()
		*started = append(*started, text)
		mu.Unlock()
		<-release
		return map[string]any{"stopReason": "end_turn"}, nil
	})
}

func waitStarted(t *testing.T, mu *sync.Mutex, started *[]string, want int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := append([]string(nil), *started...)
		mu.Unlock()
		if len(got) >= want {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("prompts started = %v, want %d", *started, want)
	return nil
}

func textBlocks(text string) []any {
	return []any{map[string]any{"type": "text", "text": text}}
}

func TestPromptLiveQueuesWhileTurnRuns(t *testing.T) {
	client, agent, done := pipePair(t)
	defer done()

	var mu sync.Mutex
	started := []string{}
	release := make(chan struct{}, 8)
	blockingPromptAgent(agent, &started, &mu, release)

	state := NewSessionState("conn-q")
	sess := NewAgentSession(client, state, "claude_code")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sess.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := sess.NewSession(ctx, "/tmp/x", nil); err != nil {
		t.Fatalf("new session: %v", err)
	}

	m := NewManager(nil, nil)
	m.live["c1"] = &liveSession{session: sess, state: state}

	if err := m.PromptLive("c1", textBlocks("one")); err != nil {
		t.Fatalf("prompt one: %v", err)
	}
	waitStarted(t, &mu, &started, 1)

	// Two more sends while the first turn blocks: both queue.
	if err := m.PromptLive("c1", textBlocks("two")); err != nil {
		t.Fatalf("prompt two: %v", err)
	}
	if err := m.PromptLive("c1", textBlocks("three")); err != nil {
		t.Fatalf("prompt three: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if len(started) != 1 {
		mu.Unlock()
		t.Fatalf("queued sends started early: %v", started)
	}
	mu.Unlock()

	// Let each turn finish; the queue drains in order.
	release <- struct{}{}
	got := waitStarted(t, &mu, &started, 2)
	if got[0] != "one" || got[1] != "two" {
		t.Fatalf("order = %v, want [one two ...]", got)
	}
	release <- struct{}{}
	release <- struct{}{}
	got = waitStarted(t, &mu, &started, 3)
	if got[2] != "three" {
		t.Fatalf("third prompt = %q, want three", got[2])
	}
}
