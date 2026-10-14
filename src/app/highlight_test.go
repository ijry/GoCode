package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestHighlightLinePreservesText(t *testing.T) {
	th := &ui.Theme{}
	cases := []struct{ line, lang string }{
		{`func main() {`, "go"},
		{`x := "hi" // trailing note`, "go"},
		{`def f(x): return x  # doc`, "python"},
		{`{"a": 1, "b": true, "c": null}`, "json"},
		{`echo "hi" # greet`, "bash"},
	}
	for _, c := range cases {
		var b strings.Builder
		for _, s := range highlightLine(c.line, c.lang, th) {
			b.WriteString(s.Text)
		}
		if b.String() != c.line {
			t.Fatalf("%s: joined %q != %q", c.lang, b.String(), c.line)
		}
	}
}

func TestHighlightLineClassifies(t *testing.T) {
	spans := highlightLine(`x := "hi" // note`, "go", &ui.Theme{})
	find := func(text string) *ui.Span {
		for i := range spans {
			if spans[i].Text == text {
				return &spans[i]
			}
		}
		return nil
	}
	if find("x") == nil {
		t.Fatalf("identifier token missing: %+v", spans)
	}
	if find(`"hi"`) == nil {
		t.Fatalf("string token missing: %+v", spans)
	}
	if s := find("// note"); s == nil || !s.Italic {
		t.Fatalf("comment token missing/not italic: %+v", spans)
	}
	// Every span carries the monospace family.
	for _, s := range spans {
		if s.Font != monoFont {
			t.Fatalf("span %q not monospace", s.Text)
		}
	}
}

func TestHighlightLineUnknownLang(t *testing.T) {
	spans := highlightLine("plain text here", "brainfuck", &ui.Theme{})
	if len(spans) != 1 || spans[0].Text != "plain text here" {
		t.Fatalf("unknown lang should be one plain span: %+v", spans)
	}
}

func TestKeywordsFor(t *testing.T) {
	if !keywordsFor("go")["func"] {
		t.Fatalf("go should know `func`")
	}
	if !keywordsFor("python")["def"] {
		t.Fatalf("python should know `def`")
	}
	if keywordsFor("json") == nil {
		t.Fatalf("json should be highlighted (strings/numbers/literals)")
	}
	if keywordsFor("brainfuck") != nil {
		t.Fatalf("unknown language must return nil")
	}
}
