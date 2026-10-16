package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestTranscriptVirtualizesLongConversations covers UI_GAPS P2 #13: the
// transcript is a virtual list, so a very long conversation builds only the
// rows in view rather than every row each frame. FollowEnd still shows the end.
func TestTranscriptVirtualizesLongConversations(t *testing.T) {
	s := conversationShellForTest(t)
	setSnapshotStatus(t, s, "connected")

	// 200 turns: far more than a short window can show at once.
	var sb strings.Builder
	sb.WriteString(`{"turns":[`)
	for i := 0; i < 200; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"role":"assistant","blocks":[{"type":"text","text":"reply %d"}]}`, i)
	}
	sb.WriteString(`]}`)
	s.cache["turns:1"] = json.RawMessage(sb.String())
	s.stream = newStreamForTest() // drop the seeded live events; only the 200 turns

	tt := ui.NewTester(s.View, 1240, 320)
	tt.Frame()

	first, last := s.transcriptList.Visible()
	// FollowEnd shows the end...
	if last != 199 {
		t.Errorf("FollowEnd should show the last row: last=%d, want 199", last)
	}
	// ...and only a window of it is built: the first row is not among them.
	if first == 0 {
		t.Errorf("built every row from 0 — not virtualized: first=%d last=%d", first, last)
	}
	if n := last - first + 1; n > 100 {
		t.Errorf("window of %d rows is not virtualized (first=%d last=%d)", n, first, last)
	}
}
