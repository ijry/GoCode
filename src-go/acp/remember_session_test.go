package acp

// remember_session_test.go covers the conversation ↔ agent-session link: the
// history endpoint reads the agent's transcript by conversation.external_id, so
// a live connect must record it — and must not re-point an existing one.

import "testing"

func TestRememberSessionID(t *testing.T) {
	database := forkDB(t) // conversation id 1, no external_id
	m := NewManager(database, nil)

	m.rememberSessionID(1, "sess-1")

	var got string
	if err := database.Conn.QueryRow(
		`SELECT COALESCE(external_id,'') FROM conversation WHERE id = 1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "sess-1" {
		t.Fatalf("external_id = %q, want sess-1", got)
	}

	// A later reconnect starts a new agent session; the original id stays so the
	// conversation's transcript remains stable.
	m.rememberSessionID(1, "sess-2")
	if err := database.Conn.QueryRow(
		`SELECT COALESCE(external_id,'') FROM conversation WHERE id = 1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "sess-1" {
		t.Fatalf("external_id re-pointed to %q, want sess-1", got)
	}

	// Unknown rows and empty ids are no-ops, not panics.
	m.rememberSessionID(999, "sess-x")
	m.rememberSessionID(1, "")
}
