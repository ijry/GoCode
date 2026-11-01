package app

// export_html.go renders a conversation as a self-contained HTML document:
// inline CSS, no scripts and no external assets, so the file opens offline and
// can be shared as-is. The Markdown export (conversationMarkdown) stays for the
// clipboard; this one is the "save a readable copy" path upstream's
// exportConversation offers as html.

import (
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"gocode/locales"
)

// conversationHTML builds the document. convID selects which conversation's
// loaded turns and live rows go in.
func (s *Shell) conversationHTML(m locales.Messages, convID string) string {
	title := str(s.convByID(convID), "title")
	if title == "" {
		title = m.ExportUntitled()
	}
	var body strings.Builder
	body.WriteString(`<h1>` + html.EscapeString(title) + `</h1>`)
	meta := []string{}
	if agent := str(s.convByID(convID), "agent_type"); agent != "" {
		meta = append(meta, html.EscapeString(agent))
	}
	if model := str(s.convByID(convID), "model"); model != "" {
		meta = append(meta, html.EscapeString(model))
	}
	meta = append(meta, html.EscapeString(time.Now().Format("2006-01-02 15:04")))
	body.WriteString(`<p class="meta">` + strings.Join(meta, " · ") + `</p>`)

	writeTurn := func(role string, blocks []map[string]any) {
		if len(blocks) == 0 {
			return
		}
		class := "assistant"
		label := m.ExportRoleAssistant()
		if role == "user" {
			class, label = "user", m.ExportRoleUser()
		}
		body.WriteString(`<section class="turn ` + class + `">`)
		body.WriteString(`<div class="role">` + html.EscapeString(label) + `</div>`)
		for _, b := range blocks {
			switch str(b, "type") {
			case "tool_use":
				name := firstNonEmpty(str(b, "tool_name"), m.ToolFallback())
				body.WriteString(`<div class="tool"><span class="tool-name">` +
					html.EscapeString(name) + `</span>`)
				// A live row knows the tool's status but not its input, so the
				// status rides the name line instead of the preview block.
				if status := str(b, "tool_status"); status != "" {
					body.WriteString(`<span class="tool-status">` + html.EscapeString(status) + `</span>`)
				}
				if prev := str(b, "input_preview"); prev != "" {
					body.WriteString(`<pre>` + html.EscapeString(prev) + `</pre>`)
				}
				body.WriteString(`</div>`)
			default:
				if text := str(b, "text"); strings.TrimSpace(text) != "" {
					body.WriteString(htmlParagraphs(text))
				}
			}
		}
		body.WriteString(`</section>`)
	}

	var turns []any
	if s.convTurnsConv == convID {
		turns = s.convTurnsPage
	}
	for _, raw := range turns {
		t, _ := raw.(map[string]any)
		if t == nil {
			continue
		}
		blocks, _ := t["blocks"].([]any)
		writeTurn(str(t, "role"), asBlockMaps(blocks, t))
	}
	// The live rows: what the running turn has produced so far.
	if connID := s.connByConv[convID]; connID != "" && s.stream != nil {
		for _, it := range buildTranscript(s.stream.Events(connID), m) {
			switch it.kind {
			case "message":
				writeTurn(it.role, []map[string]any{{"type": "text", "text": it.text}})
			case "tool":
				if it.tool != nil {
					writeTurn("assistant", []map[string]any{{
						"type":        "tool_use",
						"tool_name":   it.tool.title,
						"tool_status": it.tool.status,
					}})
				}
			}
		}
	}

	return `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(title) + `</title>
<style>
:root { color-scheme: light dark; }
body {
  margin: 0 auto; padding: 40px 24px; max-width: 820px;
  font: 15px/1.7 -apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif;
  color: #1f2328; background: #ffffff;
}
h1 { font-size: 22px; line-height: 1.4; margin: 0 0 6px; }
.meta { margin: 0 0 28px; font-size: 12px; color: #6b7280; }
.turn { margin: 0 0 22px; padding-left: 14px; border-left: 3px solid #e5e7eb; }
.turn.user { border-left-color: #3b82f6; }
.role { font-size: 11px; letter-spacing: .04em; text-transform: uppercase; color: #6b7280; margin-bottom: 6px; }
.prose { white-space: pre-wrap; word-break: break-word; margin: 0 0 10px; }
.tool {
  margin: 8px 0; padding: 10px 12px; border-radius: 8px;
  background: #f6f8fa; border: 1px solid #e5e7eb;
}
.tool-name { font: 600 12px/1.4 ui-monospace, SFMono-Regular, Menlo, monospace; color: #57606a; }
.tool-status { margin-left: 8px; font-size: 11px; color: #1a7f37; }
.tool pre { margin: 6px 0 0; white-space: pre-wrap; word-break: break-all; font: 12px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; color: #57606a; }
@media (prefers-color-scheme: dark) {
  body { color: #e6edf3; background: #0d1117; }
  .meta, .role { color: #8b949e; }
  .turn { border-left-color: #30363d; }
  .turn.user { border-left-color: #58a6ff; }
  .tool { background: #161b22; border-color: #30363d; }
  .tool-name, .tool pre { color: #8b949e; }
  .tool-status { color: #3fb950; }
}
</style>
</head>
<body>
` + body.String() + `
</body>
</html>
`
}

// asBlockMaps normalizes a turn's `blocks` (decoded JSON) into maps. A turn with
// no blocks but a `text` field still renders, matching the Markdown export.
func asBlockMaps(blocks []any, turn map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(blocks))
	for _, raw := range blocks {
		if b, _ := raw.(map[string]any); b != nil {
			out = append(out, b)
		}
	}
	if len(out) == 0 {
		if text := str(turn, "text"); strings.TrimSpace(text) != "" {
			out = append(out, map[string]any{"type": "text", "text": text})
		}
	}
	return out
}

// htmlParagraphs escapes text and keeps its line breaks, which is what a
// transcript's prose needs.
func htmlParagraphs(text string) string {
	return `<div class="prose">` + html.EscapeString(strings.TrimSpace(text)) + `</div>`
}

// exportHTML writes the conversation to a file the user picks. The dialog runs
// off the UI thread, like openFolder's.
func (s *Shell) exportHTML(loc, convID string) {
	doc := s.conversationHTML(locales.In(loc), convID)
	if strings.TrimSpace(doc) == "" {
		return
	}
	name := strings.TrimSpace(str(s.convByID(convID), "title"))
	if name == "" {
		name = "conversation"
	}
	svc := s.svc
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Title:       locales.In(loc).DialogExportHTML(),
			DefaultPath: name + ".html",
			Filters:     []mygo.FileFilter{{Name: "HTML", Extensions: []string{"html"}}},
		})
		if err != nil || path == "" {
			return
		}
		// A toast, not actionMsg: only the workbench routes and the Changes panel
		// render actionMsg, so a conversation-level message written there would
		// never be seen. enqueueToast has its own lock and is safe off-thread.
		if werr := os.WriteFile(path, []byte(doc), 0o644); werr != nil {
			s.notifyErr(locales.In(loc).ToastExportFailed(werr.Error()))
		} else {
			s.notify(locales.In(loc).ToastExported(filepath.Base(path)))
		}
		svc.Invalidate()
	}()
}
