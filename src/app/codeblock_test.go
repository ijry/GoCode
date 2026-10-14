package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestCodeBlockRendersWithCopyButton(t *testing.T) {
	s := conversationShellForTest(t)
	// A code sample exercising every highlight class: comment (gray italic),
	// string (green), number (orange) and keywords (accent). The string/number
	// contrast is what the screenshot proves — the upstream theme keeps Accent
	// close to Text by design, so keywords alone would look uniform.
	const body = "// greet returns a greeting\nfunc greet(name string) string {\n\treturn \"hi, \" + name + 42\n}"
	s.stream.handle(&wsFrame{Type: "event", Envelope: map[string]any{
		"connection_id": "c1",
		"seq":           1,
		"type":          "content_delta",
		"text":          "Here you go:\n\n```go\n" + body + "\n```\n",
	}})

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

	if err := tt.Click("Copy"); err != nil {
		t.Fatalf("click Copy: %v", err)
	}
	if got := tt.Clipboard(); got != body {
		t.Fatalf("clipboard = %q, want the code block body %q", got, body)
	}
}
