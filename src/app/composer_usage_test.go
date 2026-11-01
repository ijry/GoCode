package app

// composer_usage_test.go covers the context-window meter: the token formatting,
// the snapshot reading, and the live stream overriding it mid-turn.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFormatTokens(t *testing.T) {
	cases := map[uint64]string{
		0:        "0",
		999:      "999",
		1000:     "1k",
		1234:     "1.2k",
		200000:   "200k",
		1000000:  "1M",
		1100000:  "1.1M",
	}
	for in, want := range cases {
		if got := formatTokens(in); got != want {
			t.Fatalf("formatTokens(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestComposerContextUsageFromSnapshot(t *testing.T) {
	s := conversationShellForTest(t)
	s.cache["snapshot:c1"] = json.RawMessage(
		`{"connection_id":"c1","status":"prompting","active_tool_calls":[],"context_usage":{"used":12345,"size":200000}}`)

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if !tt.HasText("12.3k / 200k · 6%") {
		t.Fatalf("context meter missing; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "composer-context-usage")
}

func TestComposerContextUsageLiveWins(t *testing.T) {
	s := conversationShellForTest(t)
	s.cache["snapshot:c1"] = json.RawMessage(
		`{"connection_id":"c1","status":"prompting","active_tool_calls":[],"context_usage":{"used":1000,"size":200000}}`)
	// A usage_update event from the running turn is fresher than the snapshot.
	s.stream.handle(&wsFrame{Type: "event", Envelope: map[string]any{
		"connection_id": "c1", "type": "usage_update", "used": float64(180000), "size": float64(200000),
	}})

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if !tt.HasText("180k / 200k · 90%") {
		t.Fatalf("live usage did not win; texts = %v", tt.Texts())
	}
}

func TestComposerContextUsageHiddenWithoutData(t *testing.T) {
	s := conversationShellForTest(t) // seeded snapshot has no context_usage
	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if tt.HasText("%") {
		t.Fatalf("meter shown without a reading; texts = %v", tt.Texts())
	}
}
