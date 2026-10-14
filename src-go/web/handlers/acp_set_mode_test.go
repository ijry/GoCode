package handlers_test

import "testing"

// TestAcpSetModeValidation guards the input checks on acp_set_mode: the
// handler used to be an unconditional {ok:true} stub, so it accepted any
// payload silently. After wiring it to the real engine it must reject the
// empty connectionId and the empty modeId, and route an unknown id through
// the engine so a missing live connection becomes a real error.
func TestAcpSetModeValidation(t *testing.T) {
	srv, _ := newTestServerState(t)

	if c, _ := call(t, srv, "acp_set_mode", map[string]any{"modeId": "plan"}); c == 200 {
		t.Fatalf("missing connectionId should be refused, got 200")
	}
	if c, _ := call(t, srv, "acp_set_mode", map[string]any{"connectionId": "c-missing"}); c == 200 {
		t.Fatalf("missing modeId should be refused, got 200")
	}
	// Unknown connectionId: the engine has no live session, so the handler
	// (now routing through it) returns a non-200 — the stub would have
	// returned 200 with {ok:true}.
	if c, _ := call(t, srv, "acp_set_mode", map[string]any{"connectionId": "c-missing", "modeId": "plan"}); c == 200 {
		t.Fatalf("unknown connectionId should reach the engine and be refused, got 200")
	}
}
