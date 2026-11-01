package acp

// ask_user_test.go covers the companion bridge: a parked question lands in the
// snapshot and on the event stream, and the answer unparks it.

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAskUserParksUntilAnswered(t *testing.T) {
	state := NewSessionState("conn-q")
	m := NewManager(nil, nil)
	// The bridge only needs a live entry; no agent process is involved.
	m.live["conn-q"] = &liveSession{state: state}

	questions := json.RawMessage(`[{"id":"q1","question":"which branch?"}]`)
	type result struct {
		answer string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		answer, err := m.AskUser("conn-q", questions)
		done <- result{answer, err}
	}()

	// The question becomes visible to the UI through the snapshot.
	var requestID string
	deadline := time.Now().Add(2 * time.Second)
	for requestID == "" {
		if q := state.Snapshot().PendingQuestion; q != nil {
			requestID = q.RequestID
			if string(q.Questions) != string(questions) {
				t.Fatalf("questions = %s, want %s", q.Questions, questions)
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("question never became pending")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !m.AnswerQuestion("conn-q", requestID, "main") {
		t.Fatal("AnswerQuestion reported no waiting question")
	}
	select {
	case r := <-done:
		if r.err != nil || r.answer != "main" {
			t.Fatalf("AskUser = (%q,%v), want (main,nil)", r.answer, r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AskUser did not return after the answer")
	}

	// The pending question clears and both events were emitted.
	if q := state.Snapshot().PendingQuestion; q != nil {
		t.Fatalf("pending question not cleared: %+v", q)
	}
	envs, _ := state.RecentEventsAfter(0)
	var types []string
	for _, e := range envs {
		var m map[string]any
		_ = json.Unmarshal(e.Payload, &m)
		if ty, ok := m["type"].(string); ok {
			types = append(types, ty)
		}
	}
	found := map[string]bool{}
	for _, ty := range types {
		found[ty] = true
	}
	if !found["question_requested"] || !found["question_resolved"] {
		t.Fatalf("events = %v, want question_requested + question_resolved", types)
	}

	// A second answer for the same id is refused (already delivered).
	if m.AnswerQuestion("conn-q", requestID, "again") {
		t.Fatal("answered twice")
	}
}

func TestAskUserNeedsLiveSession(t *testing.T) {
	m := NewManager(nil, nil)
	if _, err := m.AskUser("nope", json.RawMessage(`[]`)); err == nil {
		t.Fatal("expected an error for an unknown connection")
	}
}

func TestDisconnectDropsParkedQuestion(t *testing.T) {
	state := NewSessionState("conn-d")
	m := NewManager(nil, nil)
	m.live["conn-d"] = &liveSession{state: state}

	done := make(chan string, 1)
	go func() {
		answer, _ := m.AskUser("conn-d", json.RawMessage(`[{"id":"q1","question":"?"}]`))
		done <- answer
	}()
	deadline := time.Now().Add(2 * time.Second)
	for state.Snapshot().PendingQuestion == nil {
		if time.Now().After(deadline) {
			t.Fatal("question never parked")
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.dropQuestions("conn-d")
	select {
	case answer := <-done:
		if answer != "" {
			t.Fatalf("dropped question answered with %q", answer)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dropping the session did not unpark the question")
	}
}
