package acp

// queue_state_test.go: the prompts waiting behind a running turn are visible in
// the snapshot and can be dropped one at a time.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestQueuedPromptsVisibleAndDroppable(t *testing.T) {
	client, agent, done := pipePair(t)
	defer done()

	var mu sync.Mutex
	started := []string{}
	release := make(chan struct{}, 8)
	blockingPromptAgent(agent, &started, &mu, release)

	state := NewSessionState("conn-q2")
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
	m.live["conn-q2"] = &liveSession{session: sess, state: state}

	if err := m.PromptLive("conn-q2", textBlocks("first")); err != nil {
		t.Fatalf("prompt first: %v", err)
	}
	waitStarted(t, &mu, &started, 1)
	if err := m.PromptLive("conn-q2", textBlocks("second")); err != nil {
		t.Fatalf("prompt second: %v", err)
	}
	if err := m.PromptLive("conn-q2", textBlocks("third")); err != nil {
		t.Fatalf("prompt third: %v", err)
	}

	queued := state.Snapshot().QueuedPrompts
	if len(queued) != 2 || queued[0] != "second" || queued[1] != "third" {
		t.Fatalf("queued = %v, want [second third]", queued)
	}

	// Dropping the first waiting send leaves the other.
	if !m.DropQueuedPrompt("conn-q2", 0) {
		t.Fatal("drop reported nothing to remove")
	}
	if queued := state.Snapshot().QueuedPrompts; len(queued) != 1 || queued[0] != "third" {
		t.Fatalf("after drop = %v, want [third]", queued)
	}
	if m.DropQueuedPrompt("conn-q2", 5) {
		t.Fatal("out-of-range drop reported success")
	}
	if m.DropQueuedPrompt("nope", 0) {
		t.Fatal("drop on an unknown connection reported success")
	}

	// Draining runs only the remaining prompt, then the queue empties.
	release <- struct{}{}
	release <- struct{}{}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(started)
		mu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			mu.Lock()
			t.Fatalf("started = %v, want [first third]", started)
			mu.Unlock()
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	got := append([]string(nil), started...)
	mu.Unlock()
	if got[1] != "third" {
		t.Fatalf("started = %v, want [first third]", got)
	}
	for {
		if len(state.Snapshot().QueuedPrompts) == 0 {
			break
		}
		if time.Now().After(deadline.Add(2 * time.Second)) {
			t.Fatalf("queue not empty: %v", state.Snapshot().QueuedPrompts)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The snapshot always carries an array (never null) for clients.
	var raw map[string]any
	b, _ := json.Marshal(state.Snapshot())
	_ = json.Unmarshal(b, &raw)
	if _, ok := raw["queued_prompts"].([]any); !ok {
		t.Fatalf("queued_prompts not an array: %v", raw["queued_prompts"])
	}
}
