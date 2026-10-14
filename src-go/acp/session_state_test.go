package acp

import "testing"

// TestSessionStateSnapshotExposesModesAndConfigOptions pins the contract the
// UI relies on to render mode / model pickers: the live snapshot (delivered
// over `acp_get_session_snapshot` and the initial WS attach frame) must
// include the agent's advertised Modes and ConfigOptions with their current
// values, not just the active-tool / status fields.
func TestSessionStateSnapshotExposesModesAndConfigOptions(t *testing.T) {
	s := NewSessionState("c1")
	// A fresh state has no modes and an empty (non-nil) options list, so
	// the JSON wire never carries a confusing "null" for either.
	if snap := s.Snapshot(); snap.Modes != nil {
		t.Fatalf("fresh snapshot should have no Modes, got %+v", snap.Modes)
	}
	if snap := s.Snapshot(); snap.ConfigOptions == nil {
		t.Fatalf("fresh snapshot ConfigOptions must be a non-nil empty slice, got nil")
	}

	modes := &SessionModeStateInfo{
		CurrentModeID: "default",
		AvailableModes: []SessionModeInfo{
			{ID: "default", Name: "Default"},
			{ID: "plan", Name: "Plan"},
		},
	}
	opts := []SessionConfigOptionInfo{
		{
			ID:   "model",
			Name: "Model",
			Kind: SessionConfigKindInfo{
				Type:   "select",
				Select: &SessionConfigSelectInfo{CurrentValue: "anthropic/sonnet"},
			},
		},
	}
	s.SetModes(modes)
	s.SetConfigOptions(opts)

	snap := s.Snapshot()
	if snap.Modes == nil || snap.Modes.CurrentModeID != "default" ||
		len(snap.Modes.AvailableModes) != 2 {
		t.Fatalf("snapshot.Modes = %+v, want default + 2 available", snap.Modes)
	}
	if len(snap.ConfigOptions) != 1 || snap.ConfigOptions[0].ID != "model" {
		t.Fatalf("snapshot.ConfigOptions = %+v, want 1 model entry", snap.ConfigOptions)
	}
}
