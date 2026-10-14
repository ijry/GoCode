package app

// highlight.go gives fenced code blocks a light syntax highlight — comments,
// strings, numbers and keywords — using mygo's span styling. Upstream pipes code
// through Shiki; a full grammar is out of scope here, but a token-level pass
// over the common languages covers the bulk of the visual benefit and keeps the
// output readable. Unknown languages fall back to plain monospace.

import (
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"
)

// reCodeTok matches, left to right, one comment / string / number / identifier.
// Comments come first so `// "x"` is all comment; a quote before a `#` still
// wins for the string because the leftmost match is taken.
var reCodeTok = regexp.MustCompile(
	`(//[^\n]*|#[^\n]*)` +
		`|("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|` + "`[^`]*`" + `)` +
		`|(\b\d[\d._xXa-fA-F]*\b)` +
		`|([A-Za-z_][A-Za-z0-9_]*)`)

// highlightLine returns styled spans for one line of code in lang. A nil/unknown
// language yields a single plain monospace span.
func highlightLine(line, lang string, t *ui.Theme) []ui.Span {
	plain := []ui.Span{{Text: orSpace(line), Font: monoFont, Color: t.Text}}
	kw := keywordsFor(lang)
	if kw == nil {
		return plain
	}
	line = orSpace(line)

	var spans []ui.Span
	last := 0
	for _, loc := range reCodeTok.FindAllStringIndex(line, -1) {
		if loc[0] > last {
			spans = append(spans, ui.Span{Text: line[last:loc[0]], Font: monoFont, Color: t.Text})
		}
		tok := line[loc[0]:loc[1]]
		switch {
		case strings.HasPrefix(tok, "//") || strings.HasPrefix(tok, "#"):
			spans = append(spans, ui.Span{Text: tok, Font: monoFont, Color: t.TextMuted, Italic: true})
		case tok[0] == '"' || tok[0] == '\'' || tok[0] == '`':
			spans = append(spans, ui.Span{Text: tok, Font: monoFont, Color: t.Success})
		case tok[0] >= '0' && tok[0] <= '9':
			spans = append(spans, ui.Span{Text: tok, Font: monoFont, Color: t.Warning})
		case kw[tok]:
			spans = append(spans, ui.Span{Text: tok, Font: monoFont, Color: t.Accent})
		default:
			spans = append(spans, ui.Span{Text: tok, Font: monoFont, Color: t.Text})
		}
		last = loc[1]
	}
	if last < len(line) {
		spans = append(spans, ui.Span{Text: line[last:], Font: monoFont, Color: t.Text})
	}
	if len(spans) == 0 {
		return plain
	}
	return spans
}

func orSpace(s string) string {
	if s == "" {
		return " " // keep a blank code line's height
	}
	return s
}

// keywordsFor returns the keyword set for a language, or nil when the language
// is unknown (caller falls back to plain). Languages are grouped into families;
// `json` has no keywords but is still highlighted (strings/numbers/true/false).
func keywordsFor(lang string) map[string]bool {
	lang = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(lang, ".")))
	switch lang {
	case "json", "jsonc":
		return jsonKeywords
	case "python", "py":
		return pyKeywords
	case "bash", "sh", "shell", "zsh", "console":
		return shKeywords
	case "go", "golang", "javascript", "js", "jsx", "typescript", "ts", "tsx",
		"java", "c", "cpp", "c++", "h", "hpp", "rust", "rs", "csharp", "cs",
		"php", "swift", "kt", "kotlin", "scala", "dart", "yaml", "yml", "toml":
		return clikeKeywords
	default:
		return nil
	}
}

var (
	clikeKeywords = toSet(
		"if else for while do switch case default break continue return goto "+
			"func function fn def class struct interface type enum impl trait "+
			"var let const new this self super import export package from as use "+
			"public private protected static final abstract override async await "+
			"try catch finally throw throws raise except with lambda yield "+
			"nil null true false none and or not in is "+
			"void int int8 int16 int32 int64 uint float32 float64 string bool byte rune "+
			"chan map range defer go select make len cap append copy delete",
	)
	pyKeywords = toSet(
		"def class if elif else for while return import from as try except finally "+
			"with lambda yield True False None and or not in is pass raise global "+
			"nonlocal assert del async await self",
	)
	shKeywords = toSet(
		"if then else elif fi for while until do done case esac function return "+
			"export local readonly echo cd set unset source alias",
	)
	jsonKeywords = toSet("true false null")
)

func toSet(words string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(words) {
		m[w] = true
	}
	return m
}
