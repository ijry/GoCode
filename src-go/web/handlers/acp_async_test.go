package handlers_test

import "testing"

func TestAcpStopAsyncTaskAndPlanApproval(t *testing.T) {
	srv := newTestServer(t)
	// stop_async_task → false (unknown connection, so nothing to stop), 200.
	code, body := rawPostJSON(t, srv, "acp_stop_async_task", map[string]any{"connectionId": "x", "asyncTaskId": "t"})
	if code != 200 || trimWS(body) != "false" {
		t.Fatalf("stop_async_task should be false, got %d %q", code, body)
	}
	// answer_plan_approval → null no-op, 200.
	code2, _ := call(t, srv, "acp_answer_plan_approval", map[string]any{
		"connectionId": "x", "approvalId": "a", "answer": map[string]any{"decision": "approve"},
	})
	if code2 != 200 {
		t.Fatalf("answer_plan_approval should be 200, got %d", code2)
	}
	// A missing asyncTaskId is a malformed call, not "nothing to stop".
	if code3, _ := call(t, srv, "acp_stop_async_task", map[string]any{"connectionId": "x"}); code3 != 400 {
		t.Fatalf("stop_async_task without asyncTaskId should be 400, got %d", code3)
	}
}
