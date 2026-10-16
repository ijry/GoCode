package app

// markdown_render_test.go checks the transcript renders Markdown through mygo's
// ui/markdown: headings, GFM tables, task lists, strikethrough and inline code
// (the hand-rolled renderer this replaced supported none of the first three).

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestTranscriptRendersGFMMarkdown(t *testing.T) {
	s := conversationShellForTest(t)
	// A completed turn (not streaming) so the reply is shown with View.
	s.cache["snapshot:c1"] = json.RawMessage(`{"connection_id":"c1","status":"connected","active_tool_calls":[]}`)
	s.stream.handle(&wsFrame{Type: "event", Envelope: map[string]any{
		"connection_id": "c1",
		"seq":           float64(1),
		"type":          "content_delta",
		"text": "## Results\n\n" +
			"| File | Status |\n| --- | --- |\n| main.go | ok |\n\n" +
			"- [x] parser\n- [ ] lexer\n\n" +
			"~~old~~ and **bold** and `code`.\n",
	}})

	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{
		"Results", // heading
		"main.go", // table cell
		"parser",  // checked task item
		"lexer",   // unchecked task item
		"bold",    // strong
		"old",     // strikethrough text
		"code",    // inline code
	} {
		if !tt.HasText(want) {
			t.Errorf("markdown missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "markdown-gfm")
}
