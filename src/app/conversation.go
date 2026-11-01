package app

// conversation.go is the upstream-style visual layer of the conversation detail
// surface: message bubbles, tool-call cards and the composer dock. It mirrors
// upstream components/message (Message + MessageContent), ai-elements/tool
// (Tool + ToolHeader + ToolContent) and chat/message-input (the composer
// chrome), translated into mygo's widget tree.
//
// The reference look, taken from those files:
//
//   - transcript column: p-4, gap-8 between rows;
//   - user message: right-aligned (ml-auto) capsule, w-fit max-w-[88%],
//     rounded-lg bg-secondary px-4 py-3 text-sm;
//   - assistant message: plain full-width prose (no capsule);
//   - tool call: full-width rounded-md card, border-border/60, header with a
//     tool glyph + title + rounded-full status badge + chevron; body p-4;
//   - composer: rounded-xl chrome with a hairline border-foreground/20, the
//     draft input, and a footer row with the actions on the right.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"gocode/locales"
)

// ─── transcript view model ─────────────────────────────────────────────────

// transcriptItem is one row of the rendered transcript. A live ACP turn arrives
// as a flat stream of content/thinking deltas and tool-call events; buildTranscript
// folds that into role-grouped rows so each renders as a upstream bubble or card
// instead of a raw envelope dump.
type transcriptItem struct {
	kind string // message | thinking | tool | note
	role string // user | assistant | system (message rows only)
	text string
	tool *toolCard
	note string
	atts []attachmentItem // user-message attachments (images + files)
}

// toolCard is one tool call's live rendered state, updated in place as
// tool_call_update events arrive.
type toolCard struct {
	id       string
	title    string
	kind     string
	status   string // pending | running | success | failed | rejected
	output   string
	rawInput string // raw tool arguments (JSON string) — for companion cards
}

// buildTranscript folds a connection's flat ACP event stream into render rows.
func buildTranscript(events []map[string]any, m locales.Messages) []transcriptItem {
	var items []transcriptItem
	byTool := map[string]int{}
	var open *transcriptItem
	flush := func() {
		if open != nil {
			items = append(items, *open)
			open = nil
		}
	}
	for _, ev := range events {
		typ := str(ev, "type")
		switch typ {
		case "content_delta", "thinking":
			text := str(ev, "text")
			if text == "" {
				continue
			}
			want := "message"
			if typ == "thinking" {
				want = "thinking"
			}
			if open == nil || open.kind != want {
				flush()
				open = &transcriptItem{kind: want, role: "assistant"}
			}
			open.text += text
		case "tool_call":
			flush()
			tc := &toolCard{
				id:       str(ev, "tool_call_id"),
				title:    firstNonEmpty(str(ev, "title"), str(ev, "kind"), m.ToolFallback()),
				kind:     str(ev, "kind"),
				status:   str(ev, "status"),
				rawInput: firstNonEmpty(str(ev, "raw_input"), str(ev, "input"), str(ev, "arguments")),
			}
			items = append(items, transcriptItem{kind: "tool", tool: tc})
			if tc.id != "" {
				byTool[tc.id] = len(items) - 1
			}
		case "tool_call_update":
			if i, ok := byTool[str(ev, "tool_call_id")]; ok {
				if v := str(ev, "title"); v != "" {
					items[i].tool.title = v
				}
				if v := str(ev, "status"); v != "" {
					items[i].tool.status = v
				}
				if v := firstNonEmpty(str(ev, "raw_input"), str(ev, "input"), str(ev, "arguments")); v != "" {
					items[i].tool.rawInput = v
				}
				if v := firstNonEmpty(str(ev, "output"), str(ev, "result"), str(ev, "content")); v != "" {
					items[i].tool.output = v
				}
			}
		case "turn_complete":
			flush()
		case "error":
			flush()
			items = append(items, transcriptItem{kind: "note", note: str(ev, "message")})
		case "user_message":
			// The local client echoes the user's own turn (text + attachments)
			// so it appears in the transcript the way the agent's reply does.
			flush()
			it := transcriptItem{kind: "message", role: "user", text: str(ev, "text")}
			if raw := asListVal(ev["attachments"]); len(raw) > 0 {
				it.atts = rawAttachments(raw)
			}
			items = append(items, it)
		}
	}
	flush()
	return items
}

// ─── transcript rendering ──────────────────────────────────────────────────

// viewTranscript renders the whole transcript column: parsed historical turns
// (when a parser is available) followed by the live rows, in upstream's bubble and
// card language. An empty transcript shows a centred hint rather than a blank
// scroll area. A streaming turn appends upstream's blinking block cursor and
// keeps the column pinned to its end.
// convTurnsPageSize is the history page the transcript asks for. The route
// pages backwards from beforeIndex, and its clampWindow() floors a missing
// limit at 1 — so omitting it shows exactly one turn, which is what this build
// used to do. 100 is a screenful-plus and keeps the window under the server's
// 500-turn cap.
const convTurnsPageSize = 100

// conversationTurns returns the accumulated history for a conversation,
// loading the newest page on first use. Switching conversations resets it.
func (s *Shell) conversationTurns(c *ui.Context, convID string) []any {
	if s.convTurnsConv != convID {
		s.convTurnsConv = convID
		s.convTurnsPage = nil
		s.convTurnsOffset = -1
		s.convTurnsTotal = 0
		s.convTurnsLoading = false
	}
	if s.convTurnsOffset < 0 {
		raw := s.ensure("turns:"+convID, "get_folder_conversation_turns", map[string]any{
			"conversationId": numOrString(convID),
			"limit":          convTurnsPageSize,
		})
		if raw == nil {
			return nil
		}
		s.convTurnsPage, s.convTurnsOffset, s.convTurnsTotal = parseTurnsPage(raw, nil)
	}
	return s.convTurnsPage
}

// conversationMarkdown renders a conversation as Markdown: the loaded history
// plus the live turn's message and tool rows. Upstream offers file export
// (image/markdown/html); this build has no save dialog, so the header's copy
// action puts this on the clipboard instead.
func (s *Shell) conversationMarkdown(m locales.Messages, convID string) string {
	var b strings.Builder
	if title := str(s.convByID(convID), "title"); title != "" {
		b.WriteString("# " + title + "\n\n")
	}
	writeTurn := func(role, text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		heading := m.ExportRoleAssistant()
		if role == "user" {
			heading = m.ExportRoleUser()
		}
		b.WriteString("## " + heading + "\n\n" + text + "\n\n")
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
		var text strings.Builder
		if blocks, ok := t["blocks"].([]any); ok {
			for _, bl := range blocks {
				bm, _ := bl.(map[string]any)
				if bm == nil {
					continue
				}
				if txt := str(bm, "text"); txt != "" {
					if text.Len() > 0 {
						text.WriteString("\n\n")
					}
					text.WriteString(txt)
				}
			}
		} else if txt := str(t, "text"); txt != "" {
			text.WriteString(txt)
		}
		writeTurn(str(t, "role"), text.String())
	}
	if connID := s.connByConv[convID]; connID != "" && s.stream != nil {
		for _, it := range buildTranscript(s.stream.Events(connID), m) {
			switch it.kind {
			case "message":
				writeTurn(it.role, it.text)
			case "tool":
				if it.tool != nil {
					line := "- " + it.tool.title
					if it.tool.status != "" {
						line += " (" + it.tool.status + ")"
					}
					b.WriteString(line + "\n")
				}
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// parseTurnsPage folds one page into the accumulated turns. prev is nil for the
// first page; otherwise the page's turns are older and get prepended, with
// turns_offset becoming the new earliest index.
func parseTurnsPage(raw json.RawMessage, prev []any) (turns []any, offset, total int) {
	var page map[string]any
	if err := json.Unmarshal(raw, &page); err != nil {
		return prev, 0, 0
	}
	older, _ := page["turns"].([]any)
	offset = int(intVal(page, "turns_offset"))
	total = int(intVal(page, "turns_total"))
	if prev == nil {
		return older, offset, total
	}
	out := make([]any, 0, len(older)+len(prev))
	out = append(out, older...)
	return append(out, prev...), offset, total
}

// loadEarlierTurns fetches the page before the earliest loaded turn and
// prepends it (the "Load earlier messages" row at the transcript head).
func (s *Shell) loadEarlierTurns(c *ui.Context) {
	if s.convTurnsLoading || s.convTurnsOffset <= 0 {
		return
	}
	convID := s.selectedConv
	before := s.convTurnsOffset
	s.convTurnsLoading = true
	svc := s.svc
	loc := c.Locale()
	go func() {
		raw, err := s.B.raw("get_folder_conversation_turns", map[string]any{
			"conversationId": numOrString(convID),
			"beforeIndex":    before,
			"limit":          convTurnsPageSize,
		})
		s.mu.Lock()
		s.convTurnsLoading = false
		switch {
		case err != nil:
			s.notifyErr(locales.In(loc).ConvLoadEarlierFailed(err.Error()))
		case s.convTurnsConv == convID:
			s.convTurnsPage, s.convTurnsOffset, s.convTurnsTotal = parseTurnsPage(raw, s.convTurnsPage)
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

func (s *Shell) viewTranscript(c *ui.Context, turns []any, live []transcriptItem, streaming bool) {
	t := c.Theme()
	if len(turns) == 0 && len(live) == 0 && !streaming {
		ui.Column(c.Key("tr-empty")).Fill().Center().Gap(6).Children(func() {
			ui.Text(c, cat(c).NoMessagesTitle()).FontSize(15).Bold().TextColor(t.TextMuted)
			ui.Text(c, cat(c).NoMessagesHint()).FontSize(12).TextColor(t.TextMuted)
		})
		return
	}
	// A virtual list (mygo's ui.List): only the rows in view are built, so a
	// very long conversation scrolls as one of a few. FollowEnd is upstream's
	// stick-to-bottom guard — it keeps the end in view while the user is at it,
	// and stops the moment they scroll back into history (a user reading older
	// messages is never yanked forward).
	s.transcriptList.FollowEnd = true
	// A "Load earlier messages" row heads the list while the backend still
	// holds older turns (turns_offset > 0).
	hasEarlier := s.convTurnsOffset > 0
	n := len(turns) + len(live)
	if streaming {
		n++
	}
	if hasEarlier {
		n++
	}
	ui.List(c.Key("transcript"), &s.transcriptList, n, func(i int) {
		if hasEarlier {
			if i == 0 {
				label := cat(c).ConvLoadEarlier()
				if s.convTurnsLoading {
					label = cat(c).LoadingLabel()
				}
				ui.Row(c).Center().Children(func() {
					if ui.Button(c.Key("load-earlier"), label).Clicked() {
						s.loadEarlierTurns(c)
					}
				})
				return
			}
			i--
		}
		switch {
		case i < len(turns):
			m, _ := turns[i].(map[string]any)
			s.viewHistoricalTurn(c, itoa("turn", i), m)
		case i < len(turns)+len(live):
			j := i - len(turns)
			it := live[j]
			key := itoa("tr", j)
			switch it.kind {
			case "message":
				// Only the assistant's tail bubble is still being written; the
				// user's own echo is complete the moment it is sent.
				s.viewMessageBubble(c, key, it.role, it.text, it.atts,
					streaming && j == len(live)-1 && it.role != "user")
			case "thinking":
				s.viewThinkingRow(c, key, it.text)
			case "tool":
				s.viewToolCard(c, key, it.tool)
			case "note":
				s.viewNoteRow(c, key, it.note)
			}
		default:
			s.viewStreamingCaret(c)
		}
	}).Grow(1).GapY(18)
}

// viewStreamingCaret draws upstream's blinking block cursor at the tail of the
// transcript while a turn streams: a thin bar that pulses between full and half
// opacity on the wall clock and asks for another frame, so it blinks for as
// long as the turn runs and stops between them. Mirrors the `animate-pulse`
// caret upstream appends to a streaming message (ai-elements/terminal,
// content-parts-renderer). The bar carries an accessibility label so it is
// discoverable by name, not only by pixels.
func (s *Shell) viewStreamingCaret(c *ui.Context) {
	t := c.Theme()
	col := t.Text
	if (time.Now().UnixMilli()/400)%2 != 0 {
		col = t.Text.Alpha(0.45)
	}
	ui.Box(c).Width(2).Height(16).Background(col).Label(cat(c).StreamingCursor())
	// Keep the frame loop alive only while streaming, so the caret blinks.
	c.After(400 * time.Millisecond)
}

// viewMessageBubble renders one role-grouped message in upstream's language: the
// user's is a right-aligned gray capsule hugging its content (max 88% wide),
// the assistant's is plain full-width prose, and a system note is a muted
// bordered card. Mirrors upstream Message + MessageContent.
//
// Prose goes through mygo's ui/markdown (CommonMark + GFM: tables, task lists,
// strikethrough, autolinks, footnotes, math) — Stream while the reply is still
// being written so half-typed syntax settles in, View once it is complete.
func (s *Shell) viewMessageBubble(c *ui.Context, key, role, text string, atts []attachmentItem, streaming bool) {
	t := c.Theme()
	switch role {
	case "user":
		ui.Row(c.Key(key)).FillWidth().Justify(ui.End).Children(func() {
			ui.Column(c).MaxWidthPercent(88).Radius(8).Background(t.SurfaceHover).
				Padding(12, 16, 12, 16).Children(func() {
				s.viewMarkdown(c, text, streaming)
				s.viewAttachmentsInline(c, key+"-atts", atts)
			})
		})
	case "system":
		ui.Column(c.Key(key)).FillWidth().Radius(6).Border(1, t.Border).
			Background(t.Surface).Padding(12, 12, 12, 12).Children(func() {
			ui.Text(c, text).FontSize(13).TextColor(t.TextMuted).Wrap()
		})
	default:
		ui.Column(c.Key(key)).FillWidth().Children(func() {
			s.viewMarkdown(c, text, streaming)
			s.viewAttachmentsInline(c, key+"-atts", atts)
			// A finished assistant reply can be lifted out whole — the code
			// blocks have their own copy buttons, prose does not. Hugged to the
			// left rather than stretched across the bubble.
			if !streaming && strings.TrimSpace(text) != "" {
				ui.Row(c).Gap(6).Children(func() {
					btn := ui.Button(c.Key(key+"-copy"), cat(c).ActionCopy())
					if btn.Clicked() {
						c.WriteClipboard(text)
						s.notify(cat(c).ToastCopied())
					}
				})
			}
		})
	}
}

// viewMarkdown renders prose with mygo's ui/markdown. A message still being
// written streams (half-typed syntax shows as what it will become); a finished
// one is shown as it reads. The renderer keeps its parse between frames, so a
// token appended to a long reply is cheap.
func (s *Shell) viewMarkdown(c *ui.Context, text string, streaming bool) {
	if streaming {
		markdown.Stream(c, text, &s.md)
		return
	}
	markdown.View(c, text, &s.md)
}

// viewThinkingRow renders the agent's reasoning the way upstream does: italic,
// muted, and visibly subordinate to the reply that follows.
func (s *Shell) viewThinkingRow(c *ui.Context, key, text string) {
	t := c.Theme()
	ui.Column(c.Key(key)).FillWidth().Padding(0, 2, 0, 2).Children(func() {
		ui.RichText(c, ui.Span{Text: text, Italic: true, Color: t.TextMuted}).
			FontSize(13).LineHeight(1.5).Wrap()
	})
}

// viewNoteRow renders a non-message transcript row (an agent error) in upstream's
// alert tone.
func (s *Shell) viewNoteRow(c *ui.Context, key, note string) {
	t := c.Theme()
	ui.Row(c.Key(key)).FillWidth().Radius(6).Border(1, t.Danger.Alpha(0.3)).
		Background(t.Danger.Alpha(0.06)).Padding(8, 12, 8, 12).Gap(8).
		AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconAlert).Width(14).Height(14).TextColor(t.Danger)
		ui.Text(c, firstNonEmpty(note, cat(c).NoteError())).
			FontSize(13).TextColor(t.Danger).Wrap()
	})
}

// ─── tool-call cards ───────────────────────────────────────────────────────

// viewToolCard renders one tool call as upstream's Tool shell: a full-width card
// whose header carries the tool glyph, its title, a rounded-full status badge
// and a chevron that flips with the open state. A settled failure starts open
// so it is never hidden behind a click (upstream's AgentCapsule defaultOpen).
func (s *Shell) viewToolCard(c *ui.Context, key string, tc *toolCard) {
	t := c.Theme()
	// gocode-mcp workbench companion tools get a purpose-built one-liner card
	// (upstream CodegMcpToolCard) instead of the generic tool shell.
	if model, ok := codegMcpModelFor(tc); ok {
		s.viewCodegMcpCard(c, key, tc, model)
		return
	}
	failed := tc.status == "failed" || tc.status == "rejected"
	open := s.toolIsOpen(tc.id, failed)

	ui.Column(c.Key(key)).FillWidth().Radius(6).Border(1, t.Border.Alpha(0.7)).Children(func() {
		head := ui.Row(c).FillWidth().Padding(8, 12, 8, 12).Gap(8).AlignItems(ui.Center)
		head.Children(func() {
			// upstream's ToolHeader: the tool glyph, the title, then a rounded-full
			// status badge that carries the state icon and its wording, and a
			// chevron on the trailing edge.
			ui.Icon(c, iconWrench).Width(15).Height(15).TextColor(t.TextMuted)
			ui.Text(c, tc.title).FontSize(14).Bold().MaxLines(1).
				TextColor(condColor(failed, t.Danger, t.Text))
			ui.Spacer(c)
			ui.Row(c).Radius(999).Background(t.SurfaceHover).Padding(2, 8, 2, 8).
				Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, toolGlyph(tc.status)).Width(12).Height(12).
					TextColor(toolStatusColor(t, tc.status))
				ui.Text(c, toolStatusLabel(cat(c), tc.status)).FontSize(11).
					TextColor(toolStatusColor(t, tc.status))
			})
			ui.Icon(c, chevronGlyph(open)).Width(14).Height(14).TextColor(t.TextMuted)
		})
		if head.Clicked() {
			s.toggleTool(tc.id, failed)
		}
		if open {
			ui.Divider(c)
			ui.Column(c).FillWidth().Padding(12, 12, 12, 12).Gap(6).Children(func() {
				s.viewToolBody(c, tc, key, t)
			})
		}
	})
}

// viewCodegMcpCard renders a gocode-mcp workbench companion tool as a one-line
// card stating the single argument the call is about; the expanded body adds the
// peeled result text, plus the audited prompt (authoring) / verdict / reason.
func (s *Shell) viewCodegMcpCard(c *ui.Context, key string, tc *toolCard, m codegMcpModel) {
	t := c.Theme()
	failed := m.IsError
	open := s.toolIsOpen(tc.id, failed)
	// The collapsed line: "<tool> · <detail>" (just the tool when detail empty).
	line := m.Tool
	if m.Detail != "" {
		line = m.Tool + " · " + m.Detail
	}
	ui.Column(c.Key(key)).FillWidth().Radius(6).Border(1, t.Border.Alpha(0.7)).Children(func() {
		head := ui.Row(c).FillWidth().Padding(8, 12, 8, 12).Gap(8).AlignItems(ui.Center)
		head.Children(func() {
			ui.Icon(c, iconWrench).Width(15).Height(15).TextColor(t.TextMuted)
			ui.Text(c, line).FontSize(14).Bold().MaxLines(1).TextColor(condColor(failed, t.Danger, t.Text))
			ui.Spacer(c)
			ui.Row(c).Radius(999).Background(t.SurfaceHover).Padding(2, 8, 2, 8).
				Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, toolGlyph(tc.status)).Width(12).Height(12).TextColor(toolStatusColor(t, tc.status))
				ui.Text(c, toolStatusLabel(cat(c), tc.status)).FontSize(11).TextColor(toolStatusColor(t, tc.status))
			})
			ui.Icon(c, chevronGlyph(open)).Width(14).Height(14).TextColor(t.TextMuted)
		})
		if head.Clicked() {
			s.toggleTool(tc.id, failed)
		}
		if open {
			ui.Divider(c)
			ui.Column(c).FillWidth().Padding(12, 12, 12, 12).Gap(6).Children(func() {
				if m.Verdict != "" {
					ui.Text(c, cat(c).VerdictLabel()+": "+m.Verdict).FontSize(12).TextColor(t.TextMuted)
				}
				if m.Prompt != "" {
					ui.Text(c, cat(c).PromptLabel()+": "+m.Prompt).FontSize(12).TextColor(t.TextMuted).Wrap()
				}
				if m.Reason != "" {
					ui.Text(c, cat(c).ReasonLabel()+": "+m.Reason).FontSize(12).TextColor(t.TextMuted).Wrap()
				}
				if m.ResultText != "" {
					ui.Text(c, m.ResultText).FontSize(12).TextColor(t.Text).Wrap()
				} else if m.Verdict == "" && m.Prompt == "" && m.Reason == "" {
					ui.Text(c, cat(c).NoOutputRecorded()).FontSize(12).TextColor(t.TextMuted)
				}
			})
		}
	})
}

// toolGlyph picks the card's leading glyph, mirroring upstream's status icons.
func toolGlyph(status string) *ui.SVG {
	switch status {
	case "success":
		return iconCheck
	case "failed", "rejected":
		return iconCross
	case "running":
		return iconClock
	default:
		return iconWrench
	}
}

// chevronGlyph points right when collapsed and down when expanded.
func chevronGlyph(open bool) *ui.SVG {
	if open {
		return iconChevronD
	}
	return iconChevron
}

// toolStatusColor maps a tool status onto the theme's semantic colors, matching
// upstream's status icon tints (green check, red cross, amber clock).
func toolStatusColor(t *ui.Theme, status string) ui.Color {
	switch status {
	case "success":
		return t.Success
	case "failed", "rejected":
		return t.Danger
	case "running", "pending":
		return t.Warning
	default:
		return t.TextMuted
	}
}

// toolStatusLabel is upstream's status wording (Folder.chat.tool.status.*).
func toolStatusLabel(m locales.Messages, status string) string {
	switch status {
	case "success":
		return m.ToolStatusCompleted()
	case "failed":
		return m.ToolStatusFailed()
	case "rejected":
		return m.ToolStatusRejected()
	case "running":
		return m.ToolStatusRunning()
	case "pending":
		return m.ToolStatusQueued()
	default:
		// Unknown statuses pass through as-is (upstream does the same); only a
		// truly empty one falls back to the pending wording.
		return firstNonEmpty(status, m.ToolStatusPending())
	}
}

// condColor returns a when cond, else b.
func condColor(cond bool, a, b ui.Color) ui.Color {
	if cond {
		return a
	}
	return b
}

// toolIsOpen reports whether a tool card is expanded. Cards with no recorded
// choice fall back to their default (open on failure).
func (s *Shell) toolIsOpen(id string, fallback bool) bool {
	if id == "" {
		return fallback
	}
	if v, ok := s.toolOpen[id]; ok {
		return v
	}
	return fallback
}

// toggleTool flips a tool card's open state.
func (s *Shell) toggleTool(id string, fallback bool) {
	if id == "" {
		return
	}
	s.toolOpen[id] = !s.toolIsOpen(id, fallback)
}

// ─── historical turns ──────────────────────────────────────────────────────

// viewHistoricalTurn renders one parsed transcript turn. Its blocks are either
// text (prose) or tool_use (a tool card); consecutive text blocks join into one
// bubble, matching the split the live stream produces.
func (s *Shell) viewHistoricalTurn(c *ui.Context, key string, m map[string]any) {
	role := str(m, "role")
	blocks, _ := m["blocks"].([]any)
	if len(blocks) == 0 {
		s.viewMessageBubble(c, key, role, turnText(m), nil, false)
		return
	}
	var prose strings.Builder
	part := 0
	flush := func() {
		if prose.Len() == 0 {
			return
		}
		s.viewMessageBubble(c, key+"-p"+itoa("", part), role, prose.String(), nil, false)
		prose.Reset()
		part++
	}
	for _, raw := range blocks {
		b, _ := raw.(map[string]any)
		if str(b, "type") == "tool_use" {
			flush()
			s.viewToolCard(c, key+"-t"+itoa("", part), &toolCard{
				title:  firstNonEmpty(str(b, "tool_name"), cat(c).ToolFallback()),
				kind:   str(b, "tool_name"),
				status: "success",
				output: str(b, "input_preview"),
			})
			part++
			continue
		}
		prose.WriteString(str(b, "text"))
	}
	flush()
}

// ─── composer dock ─────────────────────────────────────────────────────────

// mentionQueryOf reports the @-mention being typed at the end of the draft:
// the text after the last "@" that starts a word, when it holds no space yet.
func mentionQueryOf(text string) (query string, at int, ok bool) {
	at = strings.LastIndex(text, "@")
	if at < 0 {
		return "", 0, false
	}
	if at > 0 {
		switch text[at-1] {
		case ' ', '\n', '\t', '(', '[', '"':
		default:
			return "", 0, false
		}
	}
	query = text[at+1:]
	if strings.ContainsAny(query, " \n\t") {
		return "", 0, false
	}
	return query, at, true
}

// viewMentionMenu lists the folder's files matching the @-query and writes the
// chosen path into the draft. Only the draft's tail is considered (the composer
// is a plain text area without caret access), which covers typing a mention.
func (s *Shell) viewMentionMenu(c *ui.Context, root string) {
	query, at, ok := mentionQueryOf(s.composer)
	if !ok || root == "" {
		return
	}
	t := c.Theme()
	m := cat(c)
	entries := asList(s.ensure("mention:"+root, "get_file_tree", map[string]any{"path": root, "maxDepth": 1}))
	if entries == nil {
		return
	}
	lower := strings.ToLower(query)
	matches := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		name := str(e, "name")
		if lower != "" && !strings.Contains(strings.ToLower(name), lower) {
			continue
		}
		matches = append(matches, e)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		di, dj := str(matches[i], "kind") == "dir", str(matches[j], "kind") == "dir"
		if di != dj {
			return di
		}
		return strings.ToLower(str(matches[i], "name")) < strings.ToLower(str(matches[j], "name"))
	})
	if len(matches) > 8 {
		matches = matches[:8]
	}

	ui.Column(c.Key("mention-menu")).FillWidth().Radius(8).Border(1, t.Border).
		Background(t.Surface).Padding(4).Gap(1).Children(func() {
		if len(matches) == 0 {
			ui.Text(c, m.ComposerMentionNoMatch()).FontSize(12).TextColor(t.TextMuted).Padding(4, 6, 4, 6)
			return
		}
		for i, e := range matches {
			rel := str(e, "path")
			name := firstNonEmpty(str(e, "name"), rel)
			isDir := str(e, "kind") == "dir"
			row := ui.Row(c.Key("mention-"+itoa("", i))).FillWidth().Padding(4, 6, 4, 6).
				Radius(5).Gap(6).AlignItems(ui.Center).Label(rel)
			if row.Hovered() {
				row.Background(t.SurfaceHover)
			}
			row.Children(func() {
				icon := iconFile
				if isDir {
					icon = iconFolder
				}
				ui.Icon(c, icon).Width(13).Height(13).TextColor(t.TextMuted)
				ui.Text(c, name).FontSize(12).TextColor(t.Text).MaxLines(1)
				if isDir {
					ui.Text(c, "/").FontSize(12).TextColor(t.TextMuted)
				}
			})
			if row.Clicked() {
				s.composer = s.composer[:at] + "@" + rel + " "
			}
		}
	})
}

// viewComposerDock renders the composer: upstream's rounded-xl chrome behind a
// hairline border, the draft input, and a footer row with the actions on the
// right. When the connection is degraded a status strip is drawn above the
// chrome (composer-dock-strip). When the live session advertises modes or a
// "model" config option, a thin selector row sits between the input and the
// footer so the user can switch without opening a popover.
func (s *Shell) viewComposerDock(c *ui.Context, connID, status, statusMsg string, usage *contextUsage, feedbackTool bool, retryLine string) {
	t := c.Theme()
	if strip := composerStrip(cat(c), status, statusMsg); strip != "" {
		ui.Row(c.Key("composer-strip")).FillWidth().Gap(8).AlignItems(ui.Center).
			Background(t.Warning.Alpha(0.10)).Radius(8).Padding(6, 12, 6, 12).Children(func() {
			ui.Icon(c, iconInfo).Width(13).Height(13).TextColor(t.Warning)
			ui.Text(c, strip).FontSize(12).TextColor(t.Warning).MaxLines(1).Grow(1)
			// A failed turn can be sent again without retyping: the last prompt
			// is still in memory.
			if status == "error" && s.lastPrompt != "" {
				if ui.Button(c.Key("composer-retry"), cat(c).ActionRetry()).Clicked() {
					s.retryLastPrompt(c.Locale(), s.selectedConv)
				}
			}
			// A dropped connection is re-established in place (session/load keeps
			// history) instead of requiring the user to send a prompt to wake it.
			if status == "disconnected" {
				if ui.Button(c.Key("composer-reconnect"), cat(c).ActionReconnect()).Clicked() {
					s.reconnectLive(c.Locale(), s.selectedConv)
				}
			}
		})
	}
	// The agent is retrying an API call on its own (rate limit / 5xx): an amber
	// line with the cause, status, attempt counter and the next-attempt delay.
	// It updates in place and clears itself when the turn progresses.
	if retryLine != "" {
		ui.Row(c.Key("composer-retry-line")).FillWidth().Gap(8).AlignItems(ui.Center).
			Background(t.Warning.Alpha(0.10)).Radius(8).Padding(6, 12, 6, 12).Children(func() {
			ui.Icon(c, iconRefresh).Width(13).Height(13).TextColor(t.Warning)
			ui.Text(c, retryLine).FontSize(12).TextColor(t.Warning).MaxLines(1).Grow(1)
		})
	}
	// Files dragged in from Explorer/Finder drop on the composer and stage as
	// attachments (upstream's pasteDropFiles). mygo 0.6 hands the paths to the
	// element that asks for them.
	composer := ui.Column(c.Key("composer")).FillWidth().Radius(12).
		Border(1, t.Text.Alpha(0.2))
	composer.Children(func() {
		// Live-feedback note composer (upstream's feedback dialog), drawn above
		// the prompt so a note can steer the running turn without leaving the
		// conversation.
		if s.feedbackOpen {
			s.viewFeedbackComposer(c, connID)
		}
		ui.Column(c).FillWidth().Padding(12, 12, 4, 12).Children(func() {
			// Typing `@` at the end of the draft opens a file picker that writes
			// the path into the prompt (upstream's file-mention menu).
			s.viewMentionMenu(c, s.folderPathByID(s.selectedFolder))
			ui.TextArea(c.Key("composer-input"), &s.composer).
				FillWidth().Lines(1, 8).Placeholder(cat(c).ComposerPlaceholder()).
				Label(cat(c).ComposerPlaceholder()).
				OnTextKey(func(k ui.TextKey) bool {
					// The user's send / newline bindings (Settings → Shortcuts):
					// the send chord wins when both match, as upstream's
					// decideComposerKey does. A plain newline has no native event
					// to defer to; the text area inserts it.
					if textKeyMatches(k, s.shortcuts["send_message"]) {
						if text := k.Text(); text != "" || len(s.stagedAttachments()) > 0 {
							s.composer = text
							s.sendPrompt(c.Locale(), s.selectedConv)
							k.SetText("")
						}
						return true
					}
					if textKeyMatches(k, s.shortcuts["newline_in_message"]) {
						if k.Key == ui.KeyEnter {
							return false
						}
						// TextKey cannot splice at the caret; append instead.
						k.SetText(k.Text() + "\n")
						return true
					}
					return false
				})
			// The branch picker (upstream's BranchDropdown chip): the folder's
			// checked-out branch, one click from the branch workspace. Hidden in
			// a folderless chat — there is no git branch to show.
			s.viewBranchChip(c, s.folderPathByID(s.selectedFolder))
			s.viewAttachmentTray(c)
		})
		if connID != "" {
			s.viewComposerSelectors(c, connID)
		}
		ui.Row(c).FillWidth().Padding(0, 8, 8, 8).Gap(8).AlignItems(ui.Center).Children(func() {
			// Paperclip opens the host file dialog (upstream's attachment
			// picker); staged files preview in viewAttachmentTray above.
			paper := ui.Row(c.Key("attach-btn")).Width(30).Height(30).Radius(6).
				AlignItems(ui.Center).Justify(ui.Center).Background(t.SurfaceHover).
				Label(cat(c).ActionAttach())
			paper.Children(func() {
				ui.Icon(c, iconPaperclip).Width(16).Height(16).TextColor(t.TextMuted)
			})
			if paper.Clicked() {
				s.pickAttachment()
			}
			// Live-feedback entry (upstream's composer "+" menu): only while a
			// turn is running and the feature is enabled. Opens the inline note
			// composer above the prompt.
			if connID != "" && status == "prompting" && feedbackTool && s.feedbackEnabled() {
				fb := ui.Row(c.Key("feedback-btn")).Width(30).Height(30).Radius(6).
					AlignItems(ui.Center).Justify(ui.Center).Background(t.SurfaceHover).
					Label(cat(c).FeedbackEntry())
				fb.Children(func() {
					ui.Icon(c, iconChat).Width(16).Height(16).TextColor(t.TextMuted)
				})
				if fb.Clicked() {
					s.feedbackOpen = !s.feedbackOpen
				}
			}
			// The strip above already says a turn failed; don't repeat it under
			// the input.
			if hint := composerHint(cat(c), status); hint != "" && status != "error" {
				ui.Text(c, hint).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
			}
			// Context-window usage: the agent reports used/size through
			// `usage_update`; the meter turns warning-coloured near the limit.
			if usage != nil && usage.Size > 0 {
				pct := int(float64(usage.Used)/float64(usage.Size)*100 + 0.5)
				col := t.TextMuted
				if pct >= 80 {
					col = t.Warning
				}
				ui.Textf(c, "%s / %s · %d%%", formatTokens(usage.Used), formatTokens(usage.Size), pct).
					FontSize(11).TextColor(col)
			}
			ui.Spacer(c)
			if connID != "" {
				if ui.Button(c.Key("cancel"), cat(c).ActionStop()).Clicked() {
					s.cancelTurn(c.Locale(), connID)
				}
			}
			if s.composer != "" {
				if send := s.shortcuts["send_message"]; send != "" {
					ui.Text(c, shortcutLabel(send)).FontSize(11).TextColor(t.TextMuted)
				}
			}
			if ui.PrimaryButton(c.Key("send"), cat(c).ActionSend()).
				Disabled(s.composer == "" && len(s.stagedAttachments()) == 0).Clicked() {
				s.sendPrompt(c.Locale(), s.selectedConv)
			}
		})
	})
	if paths := composer.DroppedFiles(); len(paths) > 0 {
		for _, p := range paths {
			s.addAttachment(p)
		}
		s.svc.Invalidate()
	}
}

// viewFeedbackComposer renders the inline note composer (upstream's feedback
// dialog): a short textarea plus Cancel / Send, drawn above the prompt. Enter
// sends via the Send button; the note is add-only once sent.
func (s *Shell) viewFeedbackComposer(c *ui.Context, connID string) {
	t := c.Theme()
	m := cat(c)
	ui.Column(c.Key("feedback-composer")).FillWidth().Gap(6).Padding(12, 12, 0, 12).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconChat).Width(13).Height(13).TextColor(t.TextMuted)
			ui.Text(c, m.FeedbackTitle()).FontSize(12).TextColor(t.Text).Grow(1)
			if ui.Button(c.Key("feedback-cancel"), m.FeedbackCancel()).Clicked() {
				s.feedbackOpen = false
			}
			if ui.PrimaryButton(c.Key("feedback-send"), m.FeedbackSend()).
				Disabled(strings.TrimSpace(s.feedbackDraft) == "").Clicked() {
				s.submitFeedback(c.Locale(), connID)
			}
		})
		ui.TextArea(c.Key("feedback-input"), &s.feedbackDraft).
			FillWidth().Lines(1, 4).Placeholder(m.FeedbackPlaceholder()).
			Label(m.FeedbackTitle())
	})
}

// feedbackEnabled reports whether live feedback is on (the `feedback.enabled`
// setting, default off). Fetched lazily through ensure so the composer entry
// appears once the setting is known; the settings page's broadcast invalidates
// the cache (see ensureStream).
func (s *Shell) feedbackEnabled() bool {
	raw := s.ensure("feedback-settings", "get_feedback_settings", nil)
	if len(raw) == 0 {
		return false
	}
	return asBool(asObj(raw)["enabled"])
}

// apiRetryLine renders the agent's in-flight API retry as one line:
// "<cause> · HTTP 429 · attempt 2/5 · next in 3.0s". Empty when idle.
func apiRetryLine(m locales.Messages, r map[string]any) string {
	if r == nil {
		return ""
	}
	parts := []string{firstNonEmpty(str(r, "error"), m.ApiRetryFallbackError())}
	if st := intVal(r, "error_status"); st > 0 {
		parts = append(parts, m.ApiRetryHttp(int(st)))
	}
	attempt, max := intVal(r, "attempt"), intVal(r, "max_retries")
	if attempt > 0 && max > 0 {
		parts = append(parts, m.ApiRetryAttempt(int(attempt), int(max)))
	} else if attempt > 0 {
		parts = append(parts, m.ApiRetryAttemptOnly(int(attempt)))
	}
	if ms := intVal(r, "retry_delay_ms"); ms > 0 {
		parts = append(parts, m.ApiRetryNextIn(fmt.Sprintf("%.1f", float64(ms)/1000)))
	}
	return strings.Join(parts, " · ")
}

// formatTokens renders a token count compactly: 999, 1.2k, 200k, 1.1M.
func formatTokens(n uint64) string {
	switch {
	case n < 1000:
		return itoa("", int(n))
	case n < 1000000:
		if n%1000 == 0 {
			return itoa("", int(n/1000)) + "k"
		}
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		if n%1000000 == 0 {
			return itoa("", int(n/1000000)) + "M"
		}
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
}

// composerHint is the muted left-hand line in the composer footer.
func composerHint(m locales.Messages, status string) string {
	switch status {
	case "prompting":
		return m.ComposerAgentWorking()
	case "connecting":
		return m.ComposerConnecting()
	case "error":
		return m.ComposerLastFailed()
	case "disconnected":
		return m.ComposerDisconnected()
	default:
		return ""
	}
}

// composerStrip is the warning line drawn above the composer when the
// connection is degraded, or "" when there is nothing to report.
func composerStrip(m locales.Messages, status, msg string) string {
	switch status {
	case "error":
		return firstNonEmpty(msg, m.ComposerStripFailed())
	case "connecting":
		return m.ComposerConnecting()
	case "disconnected":
		return m.ComposerStripDisconnected()
	default:
		return ""
	}
}

// viewComposerSelectors renders the mode and model pickers (when the live
// snapshot advertises them) as thin rows between the input and the footer.
// Both rows share the same pill component and the same dispatch path: a
// successful set applies an optimistic patch to the cached snapshot so the
// active pill moves immediately, and the next snapshot event from the agent
// overwrites it with the authoritative state.
func (s *Shell) viewComposerSelectors(c *ui.Context, connID string) {
	snap := asObj(s.cache["snapshot:"+connID])
	if snap == nil {
		return
	}
	if modes := snapshotModes(snap); modes != nil && len(modes.AvailableModes) > 1 {
		opts := make([]selectorOption, 0, len(modes.AvailableModes))
		for _, m := range modes.AvailableModes {
			id := str(m, "id")
			if id == "" {
				continue
			}
			opts = append(opts, selectorOption{value: id, label: firstNonEmpty(str(m, "name"), id)})
		}
		s.viewSelectorRow(c, "composer-mode", cat(c).SelectorMode(), modes.CurrentModeID, opts, func(modeID string) {
			s.dispatchConfig(connID, "acp_set_mode", map[string]any{
				"connectionId": connID,
				"modeId":       modeID,
			}, func(snap map[string]any) {
				if m, ok := snap["modes"].(map[string]any); ok {
					m["currentModeId"] = modeID
				}
			})
		})
	}
	if model := snapshotModelOption(snap); model != nil && len(model.Options) > 1 {
		opts := make([]selectorOption, 0, len(model.Options))
		for _, o := range model.Options {
			val := str(o, "value")
			if val == "" {
				continue
			}
			opts = append(opts, selectorOption{value: val, label: firstNonEmpty(str(o, "name"), val)})
		}
		s.viewSelectorRow(c, "composer-model", cat(c).SelectorModel(), model.CurrentValue, opts, func(valueID string) {
			s.dispatchConfig(connID, "acp_set_config_option", map[string]any{
				"connectionId": connID,
				"configId":     "model",
				"valueId":      valueID,
			}, func(snap map[string]any) {
				for _, raw := range asListVal(snap["config_options"]) {
					if str(raw, "id") != "model" {
						continue
					}
					if kind, ok := raw["kind"].(map[string]any); ok {
						kind["currentValue"] = valueID
					}
				}
			})
		})
	}
}

// snapshotSelectorInfo describes a "model" config option (and any future
// select option we choose to surface) as a flat list the row renderer can
// consume without re-walking the snapshot's nested kind union.
type snapshotSelectorInfo struct {
	CurrentValue string
	Options      []map[string]any
}

// snapshotModelOption is the conventional "model" config option from the
// snapshot, when the agent advertised one as a select kind. Returns nil if
// the snapshot has no such option or it's a non-select kind.
func snapshotModelOption(snap map[string]any) *snapshotSelectorInfo {
	for _, raw := range asListVal(snap["config_options"]) {
		if str(raw, "id") != "model" {
			continue
		}
		kind, _ := raw["kind"].(map[string]any)
		if str(kind, "type") != "select" {
			return nil
		}
		opts := asListVal(kind["options"])
		if len(opts) == 0 {
			return nil
		}
		return &snapshotSelectorInfo{
			CurrentValue: str(kind, "currentValue"),
			Options:      opts,
		}
	}
	return nil
}

// snapshotModes is the snapshot's Modes block as a flat struct the row
// renderer can consume, or nil when the agent didn't advertise any.
type snapshotModesInfo struct {
	CurrentModeID  string
	AvailableModes []map[string]any
}

func snapshotModes(snap map[string]any) *snapshotModesInfo {
	raw, _ := snap["modes"].(map[string]any)
	if raw == nil {
		return nil
	}
	current, _ := raw["currentModeId"].(string)
	avail := asListVal(raw["availableModes"])
	if len(avail) == 0 {
		return nil
	}
	return &snapshotModesInfo{CurrentModeID: current, AvailableModes: avail}
}

// dispatchConfig sends a write to the loopback backend (a mode switch or a
// model/config-option change) and, on success, applies an optimistic patch to
// the cached snapshot so the active pill moves immediately; the next real
// snapshot event from the agent overwrites it with the authoritative state.
// On failure it raises a toast: the red banner (viewActionOrError) belongs to
// the workbench routes, and the conversation view renders neither it nor
// errorMsg, so a message stored there would never be seen. We deliberately do
// NOT delete the snapshot: removing it would make both selector rows vanish for
// a frame (and, in tests, drop the very button a later click targets).
func (s *Shell) dispatchConfig(connID, cmd string, body map[string]any, patch func(snap map[string]any)) {
	svc := s.svc
	go func() {
		_, err := s.B.raw(cmd, body)
		s.mu.Lock()
		if err != nil {
			// Safe under s.mu: enqueueToast takes its own toastMu.
			s.notifyErr(cmd + ": " + err.Error())
		} else if patch != nil {
			if raw := s.cache["snapshot:"+connID]; len(raw) > 0 {
				if snap := asObj(raw); snap != nil {
					patch(snap)
					if b, e := json.Marshal(snap); e == nil {
						s.cache["snapshot:"+connID] = b
					}
				}
			}
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// viewPlanCard renders the agent's plan (ACP `plan` update) above the composer:
// a header with the completed/total count and a collapse toggle, then one row
// per step with a status glyph. Mirrors upstream's agent-plan-overlay.
func (s *Shell) viewPlanCard(c *ui.Context, entries []any) {
	if len(entries) == 0 {
		return
	}
	t := c.Theme()
	m := cat(c)
	done := 0
	for _, raw := range entries {
		if e, _ := raw.(map[string]any); str(e, "status") == "completed" {
			done++
		}
	}
	ui.Column(c.Key("plan-card")).FillWidth().Radius(8).Border(1, t.Border).
		Background(t.Surface).Padding(10, 12, 10, 12).Gap(6).Children(func() {
		head := ui.Row(c.Key("plan-head")).FillWidth().Gap(8).AlignItems(ui.Center).
			Label(m.PlanTitle())
		head.Children(func() {
			ui.Icon(c, iconTasks).Width(14).Height(14).TextColor(t.TextMuted)
			ui.Text(c, m.PlanTitle()).FontSize(12).Bold()
			ui.Textf(c, "%d/%d", done, len(entries)).FontSize(11).TextColor(t.TextMuted)
			ui.Spacer(c)
			glyph := iconChevronD
			if s.planCollapsed {
				glyph = iconChevron
			}
			ui.Icon(c, glyph).Width(14).Height(14).TextColor(t.TextMuted)
		})
		if head.Clicked() {
			s.planCollapsed = !s.planCollapsed
		}
		if s.planCollapsed {
			return
		}
		for i, raw := range entries {
			e, _ := raw.(map[string]any)
			status := str(e, "status")
			text := str(e, "content")
			if text == "" {
				continue
			}
			ui.Row(c.Key("plan-row-" + itoa("", i))).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
				icon, col := iconCircle, t.TextMuted
				switch status {
				case "completed":
					icon, col = iconCheck, t.Success
				case "in_progress":
					icon, col = iconRefresh, t.Accent
				}
				ui.Icon(c, icon).Width(13).Height(13).TextColor(col)
				txt := ui.Text(c, text).FontSize(12).TextColor(t.Text).Wrap()
				if status == "completed" {
					txt.TextColor(t.TextMuted)
				}
				if str(e, "priority") == "high" {
					ui.Text(c, "!").FontSize(11).Bold().TextColor(t.Warning)
				}
			})
		}
	})
}
