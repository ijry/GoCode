package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestParseMarkdownBlocks(t *testing.T) {
	src := "# Title\n\nSome **bold** text.\n\n```go\nfmt.Println(\"hi\")\n```\n\n" +
		"- one\n- two\n\n1. first\n2. second\n\n> quoted\n\n---\n"
	blocks := parseMarkdown(src)

	wantKinds := []string{"heading", "paragraph", "code", "list", "list", "quote", "rule"}
	if len(blocks) != len(wantKinds) {
		t.Fatalf("got %d blocks (%+v), want %d", len(blocks), blocks, len(wantKinds))
	}
	for i, k := range wantKinds {
		if blocks[i].kind != k {
			t.Fatalf("block %d kind = %q, want %q", i, blocks[i].kind, k)
		}
	}
	if blocks[0].level != 1 || blocks[0].text != "Title" {
		t.Fatalf("heading = %+v", blocks[0])
	}
	if blocks[2].lang != "go" || blocks[2].text != `fmt.Println("hi")` {
		t.Fatalf("code = %+v", blocks[2])
	}
	if blocks[3].ordered || len(blocks[3].items) != 2 || blocks[3].items[0] != "one" {
		t.Fatalf("bullet list = %+v", blocks[3])
	}
	if !blocks[4].ordered || blocks[4].items[1] != "second" {
		t.Fatalf("ordered list = %+v", blocks[4])
	}
	if blocks[5].text != "quoted" {
		t.Fatalf("quote = %+v", blocks[5])
	}
}

func TestParseMarkdownUnclosedCodeFence(t *testing.T) {
	blocks := parseMarkdown("```\nabc\n")
	if len(blocks) != 1 || blocks[0].kind != "code" || blocks[0].text != "abc" {
		t.Fatalf("blocks = %+v", blocks)
	}
}

func TestParseMarkdownContinuationLine(t *testing.T) {
	blocks := parseMarkdown("- first\n  continued\n- second\n")
	if len(blocks) != 1 || len(blocks[0].items) != 2 {
		t.Fatalf("blocks = %+v", blocks)
	}
	if blocks[0].items[0] != "first continued" {
		t.Fatalf("continuation not folded: %q", blocks[0].items[0])
	}
}

func TestMDInlineSpans(t *testing.T) {
	spans := mdInlineSpans("a `x` b **y** c *z* [l](http://u)", &ui.Theme{})

	find := func(text string) *ui.Span {
		for i := range spans {
			if spans[i].Text == text {
				return &spans[i]
			}
		}
		return nil
	}
	if s := find("x"); s == nil || s.Font != monoFont {
		t.Fatalf("inline code span missing/mis-styled: %+v", spans)
	}
	if s := find("y"); s == nil || s.Weight != 700 {
		t.Fatalf("bold span missing/mis-styled: %+v", spans)
	}
	if s := find("z"); s == nil || !s.Italic {
		t.Fatalf("italic span missing/mis-styled: %+v", spans)
	}
	if s := find("l"); s == nil || !s.Underline {
		t.Fatalf("link label missing/mis-styled: %+v", spans)
	}
	if s := find(" (http://u)"); s == nil {
		t.Fatalf("link url span missing: %+v", spans)
	}
}

func TestMDInlineSpansPlain(t *testing.T) {
	spans := mdInlineSpans("just text", &ui.Theme{})
	if len(spans) != 1 || spans[0].Text != "just text" {
		t.Fatalf("spans = %+v", spans)
	}
}
