package app

// markdown.go renders agent message text the way upstream's Streamdown does —
// fenced code blocks, headings, bullet/numbered lists, blockquotes and rules —
// in mygo's widget language. Upstream pipes markdown through a full renderer;
// the native build parses the subset that carries real meaning in coding-agent
// output (above all: code blocks, which otherwise dump as raw text).
//
// Block-level parsing is pure (parseMarkdown) so it is unit-testable without a
// display; viewMarkdown only turns the parsed blocks into widgets.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"
)

// monoFont is the monospace family the toolkit recognizes for code.
const monoFont = "monospace"

// mdBlock is one parsed block-level markdown element.
type mdBlock struct {
	kind    string // paragraph | heading | code | list | quote | rule
	level   int    // heading level (1..6)
	ordered bool   // list: numbered vs bullet
	items   []string
	text    string
	lang    string // code fence info string (language)
}

var (
	mdHeadingRe = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	mdBulletRe  = regexp.MustCompile(`^\s*[-*+]\s+(.*)$`)
	mdOrderRe   = regexp.MustCompile(`^\s*\d+[.)]\s+(.*)$`)
	mdRuleRe    = regexp.MustCompile(`^\s*(?:-{3,}|\*{3,}|_{3,})\s*$`)
	mdInlineRe  = regexp.MustCompile("(`[^`]+`|\\*\\*[^*]+\\*\\*|__[^_]+__|\\*[^*]+\\*|_[^_]+_|\\[[^\\]]+\\]\\([^)]+\\))")
)

// parseMarkdown splits text into block-level elements. Consecutive list lines
// fold into one list block; blank lines and a block-kind change close a
// paragraph.
func parseMarkdown(text string) []mdBlock {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var blocks []mdBlock
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			i++
		case strings.HasPrefix(trimmed, "```"):
			lang := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			i++
			var body []string
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
				body = append(body, lines[i])
				i++
			}
			if i < len(lines) {
				i++ // consume the closing fence
			}
			for len(body) > 0 && body[len(body)-1] == "" {
				body = body[:len(body)-1] // drop trailing blank lines (incl. EOF newline)
			}
			blocks = append(blocks, mdBlock{kind: "code", lang: lang, text: strings.Join(body, "\n")})
		case mdHeadingRe.MatchString(trimmed):
			m := mdHeadingRe.FindStringSubmatch(trimmed)
			blocks = append(blocks, mdBlock{kind: "heading", level: len(m[1]), text: strings.TrimSpace(m[2])})
			i++
		case mdRuleRe.MatchString(trimmed):
			blocks = append(blocks, mdBlock{kind: "rule"})
			i++
		case mdBulletRe.MatchString(line) || mdOrderRe.MatchString(line):
			ordered := mdOrderRe.MatchString(line)
			var items []string
			for i < len(lines) {
				l := lines[i]
				t := strings.TrimSpace(l)
				switch {
				case mdBulletRe.MatchString(l) && !ordered:
					items = append(items, strings.TrimSpace(mdBulletRe.FindStringSubmatch(l)[1]))
				case mdOrderRe.MatchString(l) && ordered:
					items = append(items, strings.TrimSpace(mdOrderRe.FindStringSubmatch(l)[1]))
				case t == "":
					// blank line ends the list
					goto listDone
				case (strings.HasPrefix(l, "  ") || strings.HasPrefix(l, "\t")) && len(items) > 0:
					// a continuation line belongs to the previous item
					items[len(items)-1] += " " + t
				default:
					goto listDone
				}
				i++
			}
		listDone:
			blocks = append(blocks, mdBlock{kind: "list", ordered: ordered, items: items})
		case strings.HasPrefix(trimmed, ">"):
			var quote []string
			for i < len(lines) {
				t := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(t, ">") {
					break
				}
				quote = append(quote, strings.TrimSpace(strings.TrimPrefix(t, ">")))
				i++
			}
			blocks = append(blocks, mdBlock{kind: "quote", text: strings.Join(quote, " ")})
		default:
			var para []string
			for i < len(lines) {
				l := lines[i]
				t := strings.TrimSpace(l)
				if t == "" || strings.HasPrefix(t, "```") || mdHeadingRe.MatchString(t) ||
					mdBulletRe.MatchString(l) || mdOrderRe.MatchString(l) ||
					strings.HasPrefix(t, ">") || mdRuleRe.MatchString(t) {
					break
				}
				para = append(para, l)
				i++
			}
			blocks = append(blocks, mdBlock{kind: "paragraph", text: strings.Join(para, "\n")})
		}
	}
	return blocks
}

// viewMarkdown renders markdown text as a column of styled blocks.
func viewMarkdown(c *ui.Context, key, text string, t *ui.Theme) {
	blocks := parseMarkdown(text)
	if len(blocks) == 0 {
		return
	}
	ui.Column(c.Key(key)).FillWidth().Gap(8).Children(func() {
		for i, b := range blocks {
			viewMDBlock(c, key+"-"+itoa("b", i), b, t)
		}
	})
}

// viewMDBlock renders one block-level element.
func viewMDBlock(c *ui.Context, key string, b mdBlock, t *ui.Theme) {
	switch b.kind {
	case "heading":
		size := float32(20 - (b.level-1)*2)
		if size < 15 {
			size = 15
		}
		ui.Text(c.Key(key), b.text).FontSize(size).Bold().TextColor(t.Text).Wrap()
	case "code":
		langCode(c, key, b, t)
	case "list":
		ui.Column(c.Key(key)).FillWidth().Gap(4).Children(func() {
			for i, item := range b.items {
				marker := "\u2022" // bullet
				if b.ordered {
					marker = fmt.Sprintf("%d.", i+1)
				}
				ui.Row(c.Key(key+"-"+itoa("li", i))).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
					ui.Text(c, marker).FontSize(14).TextColor(t.TextMuted)
					ui.Column(c).Grow(1).Children(func() {
						ui.RichText(c, mdInlineSpans(item, t)...).FontSize(14).LineHeight(1.5).Wrap()
					})
				})
			}
		})
	case "quote":
		ui.Row(c.Key(key)).FillWidth().Gap(10).AlignItems(ui.Start).Children(func() {
			ui.Text(c, "\u2502").FontSize(14).TextColor(t.Border)
			ui.Column(c).Grow(1).Children(func() {
				ui.RichText(c, mdInlineSpans(b.text, t)...).FontSize(14).LineHeight(1.5).Wrap().
					TextColor(t.TextMuted)
			})
		})
	case "rule":
		ui.Divider(c.Key(key))
	default: // paragraph
		ui.RichText(c.Key(key), mdInlineSpans(b.text, t)...).FontSize(14).LineHeight(1.5).Wrap()
	}
}

// langCode renders a fenced code block: a bordered surface with an optional
// language label and the code lines in monospace. Lines are rendered one widget
// each so newlines survive.
func langCode(c *ui.Context, key string, b mdBlock, t *ui.Theme) {
	ui.Column(c.Key(key)).FillWidth().Radius(6).Border(1, t.Border.Alpha(0.6)).
		Background(t.Surface).Children(func() {
		if b.lang != "" {
			ui.Row(c).FillWidth().Padding(6, 10, 0, 10).Children(func() {
				ui.Text(c, b.lang).FontSize(11).TextColor(t.TextMuted)
			})
		}
		ui.Column(c).FillWidth().Padding(10, 10, 10, 10).Children(func() {
			for _, line := range strings.Split(b.text, "\n") {
				if line == "" {
					line = " " // keep the blank line's height
				}
				ui.Text(c, line).Font(monoFont).FontSize(12.5).TextColor(t.Text).Wrap()
			}
		})
	})
}

// mdInlineSpans parses inline markdown (code, bold, italic, links) into styled
// spans; everything else stays body text.
func mdInlineSpans(text string, t *ui.Theme) []ui.Span {
	var spans []ui.Span
	last := 0
	for _, loc := range mdInlineRe.FindAllStringIndex(text, -1) {
		if loc[0] > last {
			spans = append(spans, ui.Span{Text: text[last:loc[0]]})
		}
		tok := text[loc[0]:loc[1]]
		switch {
		case strings.HasPrefix(tok, "`"):
			spans = append(spans, ui.Span{Text: tok[1 : len(tok)-1], Font: monoFont, Background: t.SurfacePressed})
		case strings.HasPrefix(tok, "**") || strings.HasPrefix(tok, "__"):
			spans = append(spans, ui.Span{Text: tok[2 : len(tok)-2], Weight: 700})
		case strings.HasPrefix(tok, "["):
			close := strings.Index(tok, "](")
			label := tok[1:close]
			url := tok[close+2 : len(tok)-1]
			spans = append(spans, ui.Span{Text: label, Underline: true, Color: t.Accent})
			spans = append(spans, ui.Span{Text: " (" + url + ")", Color: t.TextMuted})
		default: // *italic* / _italic_
			spans = append(spans, ui.Span{Text: tok[1 : len(tok)-1], Italic: true})
		}
		last = loc[1]
	}
	if last < len(text) {
		spans = append(spans, ui.Span{Text: text[last:]})
	}
	if len(spans) == 0 {
		spans = append(spans, ui.Span{Text: text})
	}
	return spans
}
