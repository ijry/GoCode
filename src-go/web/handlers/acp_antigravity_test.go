package handlers_test

import (
	"strings"
	"testing"
)

// TestAcpAntigravityLoginRoutes pins the three routes' registration and their
// pre-spawn error paths: validation, and the "nothing is waiting" answers.
func TestAcpAntigravityLoginRoutes(t *testing.T) {
	srv := newTestServer(t)

	code, body := rawPostJSON(t, srv, "acp_antigravity_login_start", map[string]any{"methodId": "gemini-api-key"})
	if code == 200 {
		t.Fatalf("api-key method should be rejected: %s", body)
	}
	if !strings.Contains(body, "does not use a browser sign-in") {
		t.Fatalf("start error = %s", body)
	}

	code, body = rawPostJSON(t, srv, "acp_antigravity_login_start", map[string]any{"method_id": "not-a-method"})
	if code == 200 || !strings.Contains(body, "unknown Antigravity auth method") {
		t.Fatalf("unknown method status = %d: %s", code, body)
	}

	code, body = rawPostJSON(t, srv, "acp_antigravity_login_finish", map[string]any{
		"handle": "gone", "redirect": "http://127.0.0.1:1/?code=x",
	})
	if code == 200 || !strings.Contains(body, "no sign-in is waiting") {
		t.Fatalf("finish status = %d: %s", code, body)
	}

	code, body = rawPostJSON(t, srv, "acp_antigravity_login_cancel", map[string]any{"handle": "gone"})
	if code == 200 || !strings.Contains(body, "no sign-in is waiting") {
		t.Fatalf("cancel status = %d: %s", code, body)
	}
}
