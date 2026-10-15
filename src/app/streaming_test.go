package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

// setSnapshotStatus flips the seeded live snapshot's status, which is how the
// view decides a turn is in flight (streaming) or not.
func setSnapshotStatus(t *testing.T, s *Shell, status string) {
	t.Helper()
	s.cache["snapshot:c1"] = json.RawMessage(`{"connection_id":"c1","status":"` + status + `","active_tool_calls":[]}`)
}

// TestTranscriptStreamingCaret asserts the streaming caret appears while a turn
// is prompting and is gone once the turn settles (upstream's isStreaming caret).
func TestTranscriptStreamingCaret(t *testing.T) {
	s := conversationShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)

	setSnapshotStatus(t, s, "prompting")
	tt.Frame()
	if !tt.HasText("Streaming cursor") {
		t.Errorf("no caret while prompting; texts = %v", tt.Texts())
	}
	setSnapshotStatus(t, s, "connected")
	tt.Frame()
	if tt.HasText("Streaming cursor") {
		t.Errorf("caret persists after the turn settled; texts = %v", tt.Texts())
	}
}

// TestTranscriptPinsToEndWhenAtEnd covers the follow-the-end rule: a transcript
// already showing its tail stays pinned as new rows arrive — including on the
// very first frame, which opens a conversation at its most recent message the
// way upstream's chat does — while one the user scrolled back into history is
// never yanked forward (upstream's stick-to-bottom "at bottom" guard).
func TestTranscriptPinsToEndWhenAtEnd(t *testing.T) {
	s := conversationShellForTest(t)
	setSnapshotStatus(t, s, "connected")
	// Make the transcript tall enough to overflow a short viewport, so MaxY > 0.
	// A short window is the honest way to force a scroll here: the transcript is
	// a Grow(1) column that otherwise just expands.
	body := ""
	for i := 0; i < 60; i++ {
		body += "line of a long assistant reply that wraps several times over "
	}
	s.stream.handle(&wsFrame{Type: "event", Envelope: map[string]any{
		"connection_id": "c1", "seq": float64(5), "type": "content_delta", "text": body,
	}})

	tt := ui.NewTester(s.View, 1240, 320)
	tt.Frame()
	maxY := s.transcriptScroll.MaxY
	if maxY <= 0 {
		t.Skipf("transcript did not overflow this window (MaxY=%v)", maxY)
	}
	// Opens at the tail, not the top: a chat shows its most recent turn.
	if s.transcriptScroll.Y < maxY-1 {
		t.Errorf("conversation did not open at its tail: Y=%v MaxY=%v", s.transcriptScroll.Y, maxY)
	}

	// Scrolled back into history: left exactly where the user put it.
	s.transcriptScroll.Y = 10
	tt.Frame()
	if s.transcriptScroll.Y != 10 {
		t.Errorf("transcript yanked a scrolled-up viewer forward: Y=%v, want 10", s.transcriptScroll.Y)
	}

	// Reaching the end pins it again, so further streaming rows stay visible.
	s.transcriptScroll.Y = s.transcriptScroll.MaxY
	tt.Frame()
	if s.transcriptScroll.Y != s.transcriptScroll.MaxY {
		t.Errorf("at-end transcript is not pinned: Y=%v MaxY=%v", s.transcriptScroll.Y, s.transcriptScroll.MaxY)
	}
}

// TestTranscriptEmptyShowsHintWhileNotStreaming guards the empty state when no
// turn is in flight, and that a streaming-but-empty transcript still renders
// (the caret) rather than the "No messages yet" hint.
func TestTranscriptEmptyShowsHintWhileNotStreaming(t *testing.T) {
	s := conversationShellForTest(t)
	// Drop the seeded turn (both the historical rows and the live stream) so the
	// transcript is genuinely empty.
	s.cache["turns:1"] = json.RawMessage(`{"turns":[]}`)
	s.stream = newStreamForTest()
	setSnapshotStatus(t, s, "connected")
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("No messages yet") {
		t.Errorf("empty transcript lost its hint; texts = %v", tt.Texts())
	}
	if tt.HasText("Streaming cursor") {
		t.Errorf("caret shows with no turn running; texts = %v", tt.Texts())
	}

	setSnapshotStatus(t, s, "prompting")
	tt.Frame()
	if !tt.HasText("Streaming cursor") {
		t.Errorf("streaming caret missing on an empty transcript; texts = %v", tt.Texts())
	}
}
