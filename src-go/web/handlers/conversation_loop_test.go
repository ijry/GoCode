package handlers_test

// conversation_loop_test.go drives the conversation core loop against a real
// server with an in-memory agent: connect records the agent session on the
// conversation, a send that lands mid-turn queues instead of interleaving, and
// cancel ends the turn as "cancelled" rather than an error.

import (
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"gocode/src-go/acp"
)

// blockingAgentSpawner answers the handshake and blocks each prompt until the
// test releases it, recording how many prompts started.
func blockingAgentSpawner(started *[]string, mu *sync.Mutex, release <-chan struct{}) func(acp.SpawnConfig) (*acp.AgentProcess, error) {
	return func(cfg acp.SpawnConfig) (*acp.AgentProcess, error) {
		cr, aw := io.Pipe()
		ar, cw := io.Pipe()
		clientPeer := acp.NewPeer(cr, cw)
		agentPeer := acp.NewPeer(ar, aw)

		agentPeer.HandleRequest("initialize", func(json.RawMessage) (any, error) {
			return map[string]any{"protocolVersion": acp.ACPProtocolVersion}, nil
		})
		agentPeer.HandleRequest("session/new", func(json.RawMessage) (any, error) {
			return map[string]any{"sessionId": "sess-loop"}, nil
		})
		agentPeer.HandleRequest("session/prompt", func(params json.RawMessage) (any, error) {
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
			select {
			case <-release:
			case <-time.After(5 * time.Second):
			}
			return map[string]any{"stopReason": "end_turn"}, nil
		})

		go clientPeer.Run()
		go agentPeer.Run()
		return &acp.AgentProcess{Peer: clientPeer, CloseFn: func() { cw.Close(); aw.Close(); cr.Close(); ar.Close() }}, nil
	}
}

func TestConversationCoreLoop(t *testing.T) {
	srv, st := newTestServerState(t)
	var mu sync.Mutex
	started := []string{}
	release := make(chan struct{}, 8)
	st.ACP.SetSpawner(blockingAgentSpawner(&started, &mu, release))

	dir := t.TempDir()
	_, folder := call(t, srv, "open_folder", map[string]any{"path": dir})
	fid := folder["id"]
	code, rawID := callRaw(t, srv, "create_conversation", map[string]any{
		"folderId": fid, "agentType": "claude_code", "title": "Loop",
	})
	if code != 200 {
		t.Fatalf("create_conversation %d: %v", code, rawID)
	}
	convID, _ := rawID.(float64)

	// Connect with the conversation id: the agent session id is recorded on the
	// row so the history endpoint can find its transcript later.
	code, conn := call(t, srv, "acp_connect", map[string]any{
		"agentType": "claude_code", "folderPath": dir, "conversationId": convID,
	})
	if code != 200 {
		t.Fatalf("acp_connect %d: %v", code, conn)
	}
	connID, _ := conn["id"].(string)

	deadline := time.Now().Add(2 * time.Second)
	for {
		var ext any
		err := st.DB.Conn.QueryRow(`SELECT external_id FROM conversation WHERE id = ?`, int64(convID)).Scan(&ext)
		if err == nil && ext != nil && ext != "" {
			if got, _ := ext.(string); got != "sess-loop" {
				t.Fatalf("external_id = %q, want sess-loop", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("agent session id was not recorded on the conversation")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// First send starts a turn; the second lands mid-turn and must queue.
	if code, _ := call(t, srv, "acp_prompt", map[string]any{"connectionId": connID, "prompt": "one"}); code != 200 {
		t.Fatalf("prompt one %d", code)
	}
	waitStarted(t, &mu, &started, 1)
	if code, _ := call(t, srv, "acp_prompt", map[string]any{"connectionId": connID, "prompt": "two"}); code != 200 {
		t.Fatalf("prompt two %d", code)
	}
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	if len(started) != 1 {
		mu.Unlock()
		t.Fatalf("second send started a concurrent turn: %v", started)
	}
	mu.Unlock()

	release <- struct{}{}
	waitStarted(t, &mu, &started, 2)

	// Cancel the queued-turned-running session: the turn ends as cancelled, not
	// as an error.
	call(t, srv, "acp_cancel", map[string]any{"connectionId": connID})
	state := st.Session.GetState(connID)
	if state == nil {
		t.Fatal("no session state")
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		cancelled := false
		envs, _ := state.RecentEventsAfter(0)
		for _, e := range envs {
			var m map[string]any
			_ = json.Unmarshal(e.Payload, &m)
			if m["type"] == "turn_complete" && m["stop_reason"] == "cancelled" {
				cancelled = true
			}
			if m["type"] == "error" {
				t.Fatalf("cancel surfaced an error event: %v", m)
			}
		}
		if cancelled {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("cancel did not produce a cancelled turn_complete")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitStarted blocks until at least want prompts have started.
func waitStarted(t *testing.T, mu *sync.Mutex, started *[]string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(*started)
		mu.Unlock()
		if n >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("prompts started = %v, want %d", *started, want)
}
