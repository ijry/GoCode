package handlers_test

// feedback_route_test.go drives the live-feedback bridge end to end: a submit
// parks a pending note on the snapshot, the companion's check delivers it
// (pending → delivered), and a second check finds nothing left.

import (
	"testing"
)

func TestSubmitAndCheckFeedbackRoundTrip(t *testing.T) {
	srv, st := newTestServerState(t)
	st.ACP.SetSpawner(fakeAgentSpawner())

	_, conn := call(t, srv, "acp_connect", map[string]any{"agentType": "claude_code", "folderPath": t.TempDir()})
	connID, _ := conn["id"].(string)
	if connID == "" {
		t.Fatalf("no connection: %v", conn)
	}

	// Submit → 200 with a pending item.
	code, item := call(t, srv, "submit_session_feedback", map[string]any{
		"connectionId": connID, "text": "use the staging branch",
	})
	if code != 200 {
		t.Fatalf("submit = %d %v", code, item)
	}
	if item["status"] != "pending" || item["text"] != "use the staging branch" || item["id"] == "" {
		t.Fatalf("item = %v", item)
	}

	// The note shows on the snapshot, and the tool capability is advertised.
	_, snap := call(t, srv, "acp_get_session_snapshot", map[string]any{"connectionId": connID})
	notes, _ := snap["feedback"].([]any)
	if len(notes) != 1 {
		t.Fatalf("snapshot feedback = %v", snap["feedback"])
	}
	if n, _ := notes[0].(map[string]any); n["status"] != "pending" {
		t.Fatalf("snapshot note = %v", n)
	}

	// The companion's check delivers the note and marks it read.
	code, out := call(t, srv, "companion_check_user_feedback", map[string]any{"sessionId": connID})
	if code != 200 {
		t.Fatalf("check = %d %v", code, out)
	}
	delivered, _ := out["notes"].([]any)
	if len(delivered) != 1 {
		t.Fatalf("delivered notes = %v", out["notes"])
	}
	if d, _ := delivered[0].(map[string]any); d["status"] != "delivered" || d["delivered_at"] == nil {
		t.Fatalf("delivered note = %v", d)
	}

	// A second check has nothing left to deliver.
	_, out2 := call(t, srv, "companion_check_user_feedback", map[string]any{"sessionId": connID})
	if notes2, _ := out2["notes"].([]any); len(notes2) != 0 {
		t.Fatalf("second check delivered %v", out2["notes"])
	}
}

func TestCompanionCheckFeedbackValidation(t *testing.T) {
	srv, _ := newTestServerState(t)
	if code, out := call(t, srv, "companion_check_user_feedback", map[string]any{}); code != 400 {
		t.Fatalf("missing sessionId = %d %v", code, out)
	}
	// Unknown session → 200 with no notes (nothing parked to deliver).
	if code, out := call(t, srv, "companion_check_user_feedback", map[string]any{"sessionId": "nope"}); code != 200 {
		t.Fatalf("unknown session = %d %v", code, out)
	}
}
