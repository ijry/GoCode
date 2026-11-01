package handlers_test

// ask_user_route_test.go drives the companion bridge end to end: the park route
// blocks, the snapshot shows the pending question, and acp_answer_question
// returns the answer to the waiting call.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func postJSON(t *testing.T, url string, payload map[string]any) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, map[string]any{"error": err.Error()}
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestCompanionAskUserRoundTrip(t *testing.T) {
	srv, st := newTestServerState(t)
	st.ACP.SetSpawner(fakeAgentSpawner())

	_, conn := call(t, srv, "acp_connect", map[string]any{"agentType": "claude_code", "folderPath": t.TempDir()})
	connID, _ := conn["id"].(string)
	if connID == "" {
		t.Fatalf("no connection: %v", conn)
	}

	// The park call blocks until the user answers, so run it off the test thread.
	type parked struct {
		code int
		out  map[string]any
	}
	done := make(chan parked, 1)
	go func() {
		code, out := postJSON(t, srv.URL+"/api/companion_ask_user_question", map[string]any{
			"sessionId": connID,
			"question":  "which branch?",
			"options":   []any{"main", "dev"},
		})
		done <- parked{code, out}
	}()

	// The question reaches the UI through the snapshot.
	var requestID string
	deadline := time.Now().Add(3 * time.Second)
	for requestID == "" {
		_, snap := call(t, srv, "acp_get_session_snapshot", map[string]any{"connectionId": connID})
		if pq, ok := snap["pending_question"].(map[string]any); ok {
			requestID, _ = pq["request_id"].(string)
			qs, _ := pq["questions"].([]any)
			if len(qs) != 1 {
				t.Fatalf("questions = %v", pq["questions"])
			}
			q, _ := qs[0].(map[string]any)
			if q["question"] != "which branch?" {
				t.Fatalf("question text = %v", q["question"])
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("question never became pending")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Answering unparks the tool call.
	code, ans := call(t, srv, "acp_answer_question", map[string]any{
		"connectionId": connID, "requestId": requestID, "answer": "main",
	})
	if code != 200 || ans["ok"] != true {
		t.Fatalf("acp_answer_question = %d %v", code, ans)
	}

	select {
	case p := <-done:
		if p.code != 200 || p.out["answer"] != "main" {
			t.Fatalf("parked call = %d %v, want 200 answer=main", p.code, p.out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("parked call did not return")
	}

	// The snapshot no longer carries the question.
	_, snap := call(t, srv, "acp_get_session_snapshot", map[string]any{"connectionId": connID})
	if _, ok := snap["pending_question"]; ok {
		t.Fatalf("pending_question still set: %v", snap)
	}
}

func TestCompanionAskUserValidation(t *testing.T) {
	srv, _ := newTestServerState(t)
	if code, out := call(t, srv, "companion_ask_user_question", map[string]any{"question": "x"}); code != 400 {
		t.Fatalf("missing sessionId = %d %v", code, out)
	}
	if code, out := call(t, srv, "companion_ask_user_question", map[string]any{"sessionId": "nope"}); code != 400 {
		t.Fatalf("missing question = %d %v", code, out)
	}
	if code, out := call(t, srv, "companion_ask_user_question", map[string]any{"sessionId": "nope", "question": "x"}); code != 404 {
		t.Fatalf("unknown session = %d %v", code, out)
	}
}
