package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestCodeBlockRendersWithCopyButton checks mygo's ui/markdown renders a fenced
// code block with its language and Copy button. The turn is marked complete so
// the reply is shown with View (a still-streaming reply is paced and its code
// block has no Copy button until the fence closes).
func TestCodeBlockRendersWithCopyButton(t *testing.T) {
	s := conversationShellForTest(t)
	// A code sample exercising the highlight classes: comment (gray italic),
	// string (green), number (orange) and keywords (accent).
	const body = "// greet returns a greeting\nfunc greet(name string) string {\n\treturn \"hi, \" + name + 42\n}"
	s.stream.handle(&wsFrame{Type: "event", Envelope: map[string]any{
		"connection_id": "c1",
		"seq":           1,
		"type":          "content_delta",
		"text":          "Here you go:\n\n```go\n" + body + "\n```\n",
	}})
	// The turn is done: the reply is complete, not streaming.
	s.cache["snapshot:c1"] = json.RawMessage(`{"connection_id":"c1","status":"connected","active_tool_calls":[]}`)

	tt := ui.NewTester(s.View, 1240, 820)
	dumpFrame(t, tt, "codeblock")
	if !tt.HasText("Copy") {
		t.Fatalf("copy button missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("go") {
		t.Fatalf("language label missing; texts = %v", tt.Texts())
	}
	for _, want := range []string{"// greet", "func", "greet", "\"hi, \"", "42"} {
		if !tt.HasText(want) {
			t.Fatalf("code token %q missing; texts = %v", want, tt.Texts())
		}
	}

	// The assistant reply has its own message-level Copy button earlier in the
	// tree; the code block's accessible label targets the block's button.
	if err := tt.Click("Copy code"); err != nil {
		t.Fatalf("click Copy code: %v", err)
	}
	// The block's content is the lines between the fences, newline included.
	if got, want := tt.Clipboard(), body+"\n"; got != want {
		t.Fatalf("clipboard = %q, want the code block body %q", got, want)
	}
}
