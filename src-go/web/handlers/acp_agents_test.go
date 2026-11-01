package handlers_test

import (
	"testing"
)

func TestAcpListAgents(t *testing.T) {
	srv := newTestServer(t)
	// The list stamps `available` from whether the agent's launcher resolves
	// on PATH (npx/uvx runner, or the binary itself). Point PATH at an empty
	// directory so the assertion is about the catalog, not this machine —
	// with a real npx on PATH the first row would read available:true.
	t.Setenv("PATH", t.TempDir())
	_, arr := callArr(t, srv, "acp_list_agents", map[string]any{})
	if len(arr) != 15 {
		t.Fatalf("expected 15 agents, got %d", len(arr))
	}
	first := arr[0].(map[string]any)
	if first["agent_type"] != "claude_code" || first["registry_id"] != "claude-acp" {
		t.Fatalf("first agent wrong: %v", first)
	}
	if first["available"] != false {
		t.Fatalf("agent should be unavailable until preflight: %v", first)
	}
}
