package app

// conversation_export_test.go covers the two export arms: "Copy as Markdown"
// (the header action and the Markdown it builds from loaded history plus the
// live turn) and "Export as HTML" (a standalone document).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"gocode/locales"
)

func TestConversationMarkdownFromHistory(t *testing.T) {
	s := conversationShellForTest(t)
	s.convTurnsConv = "1"
	s.convTurnsPage = []any{
		map[string]any{"role": "user", "blocks": []any{
			map[string]any{"type": "text", "text": "Fix the parser."},
		}},
		map[string]any{"role": "assistant", "blocks": []any{
			map[string]any{"type": "text", "text": "Done, patched main.go."},
		}},
	}

	md := s.conversationMarkdown(locales.In("en"), "1")
	for _, want := range []string{"# Fix the parser", "## User", "Fix the parser.", "## Assistant", "Done, patched main.go."} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}

func TestConversationCopyMarkdownButton(t *testing.T) {
	s := conversationShellForTest(t)
	s.convTurnsConv = "1"
	s.convTurnsPage = []any{
		map[string]any{"role": "user", "blocks": []any{
			map[string]any{"type": "text", "text": "hello there"},
		}},
	}
	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	dumpFrame(t, tt, "conversation-export")

	// The header's export action is labelled; clicking it copies and toasts.
	if err := tt.Click("Copy as Markdown"); err != nil {
		t.Fatalf("copy-markdown action missing: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("copied to clipboard") }) {
		t.Fatalf("no copied toast; texts = %v", tt.Texts())
	}
}

// TestConversationHTMLExport covers the HTML arm: a self-contained document
// (inline CSS, no scripts) with escaped prose and tool cards, which is what
// "Export as HTML" writes to the file the save dialog returns.
func TestConversationHTMLExport(t *testing.T) {
	s := conversationShellForTest(t)
	s.convTurnsConv = "1"
	s.convTurnsPage = []any{
		map[string]any{"role": "user", "blocks": []any{
			map[string]any{"type": "text", "text": `fix <script>alert(1)</script> & "quotes"`},
		}},
		map[string]any{"role": "assistant", "blocks": []any{
			map[string]any{"type": "text", "text": "先看看。"},
			map[string]any{"type": "tool_use", "tool_name": "Bash", "tool_use_id": "call_1",
				"input_preview": `{"command":"pnpm build"}`},
		}},
	}

	doc := s.conversationHTML(locales.In("en"), "1")
	for _, want := range []string{
		"<!DOCTYPE html>",
		"<title>Fix the parser</title>",
		"Fix the parser", // the heading
		`class="turn user"`,
		`class="turn assistant"`,
		"先看看。",
		`class="tool-name">Bash`,
		"pnpm build",
		"<style>", // inline CSS, so the file opens offline
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("html missing %q: %s", want, doc)
		}
	}
	// Prose is escaped, and the document carries no scripts of its own.
	if !strings.Contains(doc, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("prose was not escaped: %s", doc)
	}
	if strings.Contains(doc, "<script") {
		t.Fatalf("exported document contains a script tag: %s", doc)
	}
	// Drop the real file next to the screenshots so it can be opened as-is.
	if dir := os.Getenv("MYGO_UI_PNG"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "conversation-export.html"), []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConversationExportHTMLButton(t *testing.T) {
	s := conversationShellForTest(t)
	s.convTurnsConv = "1"
	s.convTurnsPage = []any{
		map[string]any{"role": "user", "blocks": []any{
			map[string]any{"type": "text", "text": "hello there"},
		}},
	}
	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if !tt.HasText("Export as HTML") {
		t.Fatalf("export-html action missing; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "conversation-export-html")
}
