package handlers_test

import (
	"testing"
)

// TestAcpPreflight returns one structured row per built-in agent, with the
// binary agents flagged installable and unavailable when their runner/binary is
// absent (empty PATH makes npx/uvx undetectable).
func TestAcpPreflight(t *testing.T) {
	srv := newTestServer(t)
	t.Setenv("PATH", t.TempDir())
	status, out := call(t, srv, "acp_preflight", map[string]any{})
	if status != 200 {
		t.Fatalf("preflight status = %d", status)
	}
	agents, ok := out["agents"].([]any)
	if !ok {
		t.Fatalf("no agents array in %v", out)
	}
	if len(agents) != 15 {
		t.Fatalf("expected 15 agents, got %d", len(agents))
	}
	var openCode map[string]any
	for _, a := range agents {
		m := a.(map[string]any)
		if m["agent_type"] == "open_code" {
			openCode = m
		}
	}
	if openCode == nil {
		t.Fatal("open_code missing from preflight")
	}
	if openCode["installable"] != true {
		t.Errorf("open_code should be installable: %v", openCode)
	}
	if openCode["available"] != false {
		t.Errorf("open_code should be unavailable offline: %v", openCode)
	}
}

// TestAcpInstallNonBinaryAgentRejected confirms a non-binary agent (npx) cannot
// be installed through this route — it fetches via its own package manager.
func TestAcpInstallNonBinaryAgentRejected(t *testing.T) {
	srv := newTestServer(t)
	status, _ := call(t, srv, "acp_install_agent_binary", map[string]any{"agentType": "claude_code"})
	if status < 400 {
		t.Fatalf("expected a rejection for a non-binary agent, got %d", status)
	}
}

// TestAcpInstallMissingAgentType is a clear client-error contract.
func TestAcpInstallMissingAgentType(t *testing.T) {
	srv := newTestServer(t)
	status, _ := call(t, srv, "acp_install_agent_binary", map[string]any{})
	if status < 400 {
		t.Fatalf("expected a rejection without agentType, got %d", status)
	}
}
