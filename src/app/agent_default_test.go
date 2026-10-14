package app

import "testing"

func TestPickDefaultAgent(t *testing.T) {
	// An available binary agent wins (starts without a package runner).
	agents := []map[string]any{
		{"agent_type": "claude_code", "available": true, "distribution_type": "npx"},
		{"agent_type": "cursor", "available": false, "distribution_type": "binary"},
		{"agent_type": "open_code", "available": true, "distribution_type": "binary"},
	}
	if got := pickDefaultAgent(agents, []string{"claude_code", "cursor", "open_code"}); got != "open_code" {
		t.Fatalf("got %q, want open_code (available binary)", got)
	}

	// No available binary → first available.
	agents2 := []map[string]any{
		{"agent_type": "claude_code", "available": true, "distribution_type": "npx"},
		{"agent_type": "open_code", "available": false, "distribution_type": "binary"},
	}
	if got := pickDefaultAgent(agents2, []string{"claude_code", "open_code"}); got != "claude_code" {
		t.Fatalf("got %q, want claude_code (first available)", got)
	}

	// Nothing available → first option.
	agents3 := []map[string]any{{"agent_type": "a", "available": false, "distribution_type": "npx"}}
	if got := pickDefaultAgent(agents3, []string{"a"}); got != "a" {
		t.Fatalf("got %q, want a (fallback)", got)
	}
}
