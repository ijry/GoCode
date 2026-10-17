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
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
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
func buildTranscript(events []map[string]any) []transcriptItem {
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
				title:    firstNonEmpty(str(ev, "title"), str(ev, "kind"), "Tool"),
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
func (s *Shell) viewTranscript(c *ui.Context, turns []any, live []transcriptItem, streaming bool) {
	t := c.Theme()
	if len(turns) == 0 && len(live) == 0 && !streaming {
		ui.Column(c.Key("tr-empty")).Fill().Center().Gap(6).Children(func() {
			ui.Text(c, "No messages yet").FontSize(15).Bold().TextColor(t.TextMuted)
			ui.Text(c, "Send a prompt below to start the agent.").FontSize(12).TextColor(t.TextMuted)
		})
		return
	}
	// A virtual list (mygo's ui.List): only the rows in view are built, so a
	// very long conversation scrolls as one of a few. FollowEnd is upstream's
	// stick-to-bottom guard — it keeps the end in view while the user is at it,
	// and stops the moment they scroll back into history (a user reading older
	// messages is never yanked forward).
	s.transcriptList.FollowEnd = true
	n := len(turns) + len(live)
	if streaming {
		n++
	}
	ui.List(c.Key("transcript"), &s.transcriptList, n, func(i int) {
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
	ui.Box(c).Width(2).Height(16).Background(col).Label("Streaming cursor")
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
		ui.Text(c, firstNonEmpty(note, "The agent reported an error.")).
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
	// yoyocode-mcp workbench companion tools get a purpose-built one-liner card
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
				ui.Text(c, toolStatusLabel(tc.status)).FontSize(11).
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

// viewCodegMcpCard renders a yoyocode-mcp workbench companion tool as a one-line
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
				ui.Text(c, toolStatusLabel(tc.status)).FontSize(11).TextColor(toolStatusColor(t, tc.status))
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
					ui.Text(c, "verdict: "+m.Verdict).FontSize(12).TextColor(t.TextMuted)
				}
				if m.Prompt != "" {
					ui.Text(c, "prompt: "+m.Prompt).FontSize(12).TextColor(t.TextMuted).Wrap()
				}
				if m.Reason != "" {
					ui.Text(c, "reason: "+m.Reason).FontSize(12).TextColor(t.TextMuted).Wrap()
				}
				if m.ResultText != "" {
					ui.Text(c, m.ResultText).FontSize(12).TextColor(t.Text).Wrap()
				} else if m.Verdict == "" && m.Prompt == "" && m.Reason == "" {
					ui.Text(c, "No output recorded.").FontSize(12).TextColor(t.TextMuted)
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
func toolStatusLabel(status string) string {
	switch status {
	case "success":
		return "Completed"
	case "failed":
		return "Failed"
	case "rejected":
		return "Rejected"
	case "running":
		return "Running"
	case "pending":
		return "Queued"
	default:
		return firstNonEmpty(status, "Pending")
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
				title:  firstNonEmpty(str(b, "tool_name"), "Tool"),
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

// viewComposerDock renders the composer: upstream's rounded-xl chrome behind a
// hairline border, the draft input, and a footer row with the actions on the
// right. When the connection is degraded a status strip is drawn above the
// chrome (composer-dock-strip). When the live session advertises modes or a
// "model" config option, a thin selector row sits between the input and the
// footer so the user can switch without opening a popover.
func (s *Shell) viewComposerDock(c *ui.Context, connID, status, statusMsg string) {
	t := c.Theme()
	if strip := composerStrip(status, statusMsg); strip != "" {
		ui.Row(c.Key("composer-strip")).FillWidth().Gap(8).AlignItems(ui.Center).
			Background(t.Warning.Alpha(0.10)).Radius(8).Padding(6, 12, 6, 12).Children(func() {
			ui.Icon(c, iconInfo).Width(13).Height(13).TextColor(t.Warning)
			ui.Text(c, strip).FontSize(12).TextColor(t.Warning).MaxLines(1)
		})
	}
	ui.Column(c.Key("composer")).FillWidth().Radius(12).
		Border(1, t.Text.Alpha(0.2)).Children(func() {
		ui.Column(c).FillWidth().Padding(12, 12, 4, 12).Children(func() {
			ui.TextInput(c.Key("composer-input"), &s.composer).
				FillWidth().Placeholder("Message the agent…").
				OnTextKey(func(k ui.TextKey) bool {
					// ⌘Enter sends without reaching for the button; plain Enter
					// stays in the field (this is a single-line draft).
					if k.Key == ui.KeyEnter && k.Mods&ui.Cmd != 0 {
						s.sendPrompt(s.selectedConv)
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
				Label("Attach file")
			paper.Children(func() {
				ui.Icon(c, iconPaperclip).Width(16).Height(16).TextColor(t.TextMuted)
			})
			if paper.Clicked() {
				s.pickAttachment()
			}
			if hint := composerHint(status); hint != "" {
				ui.Text(c, hint).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
			}
			ui.Spacer(c)
			if connID != "" {
				if ui.Button(c.Key("cancel"), "Stop").Clicked() {
					s.cancelTurn(connID)
				}
			}
			if s.composer != "" {
				ui.Text(c, modKeyLabel()+"↵").FontSize(11).TextColor(t.TextMuted)
			}
			if ui.PrimaryButton(c.Key("send"), cat(c).ActionSend()).
				Disabled(s.composer == "" && len(s.stagedAttachments()) == 0).Clicked() {
				s.sendPrompt(s.selectedConv)
			}
		})
	})
}

// composerHint is the muted left-hand line in the composer footer.
func composerHint(status string) string {
	switch status {
	case "prompting":
		return "Agent is working…"
	case "connecting":
		return "Connecting to the agent…"
	case "error":
		return "Last turn failed"
	case "disconnected":
		return "Disconnected"
	default:
		return ""
	}
}

// composerStrip is the warning line drawn above the composer when the
// connection is degraded, or "" when there is nothing to report.
func composerStrip(status, msg string) string {
	switch status {
	case "error":
		return firstNonEmpty(msg, "The last turn failed.")
	case "connecting":
		return "Connecting to the agent…"
	case "disconnected":
		return "The agent connection is closed. Send a prompt to reconnect."
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
		s.viewSelectorRow(c, "composer-mode", "Mode", modes.CurrentModeID, opts, func(modeID string) {
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
		s.viewSelectorRow(c, "composer-model", "Model", model.CurrentValue, opts, func(valueID string) {
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
// On failure it stores a short error message that the existing red banner
// renders. We deliberately do NOT delete the snapshot: removing it would make
// both selector rows vanish for a frame (and, in tests, drop the very button a
// later click targets).
func (s *Shell) dispatchConfig(connID, cmd string, body map[string]any, patch func(snap map[string]any)) {
	svc := s.svc
	go func() {
		_, err := s.B.raw(cmd, body)
		s.mu.Lock()
		if err != nil {
			s.errorMsg = cmd + ": " + err.Error()
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
