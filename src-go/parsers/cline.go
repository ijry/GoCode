package parsers

// cline.go reads cline 3.x transcripts.
//
// One directory per session under `<CLINE_DIR or ~/.cline/data>/sessions/`:
//
//	sessions/<id>/<id>.json            the manifest
//	sessions/<id>/<id>.messages.json   the transcript
//
// The transcript is `{updated_at, messages:[…]}`; each message is
// `{role, content, ts, modelInfo, metrics, metadata}` where `content` is either
// a string or an array of Anthropic-shaped blocks (`text` / `thinking` /
// `tool_use` / `tool_result`). Tool results arrive as `role:"user"` messages,
// and cline wraps a real prompt in `<user_input>` / `<user_command>` — so a
// user message is only a prompt once the wrapper is unwrapped.
//
// Mirrors upstream parsers/cline.rs, minus usage/metrics, the manifest
// (title/cwd/model), the pre-3.x `state/taskHistory.json` fallback and
// teammate/compaction transcripts.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// clineDataDir resolves CLINE_DIR, else ~/.cline/data.
func clineDataDir() string {
	if v := strings.TrimSpace(os.Getenv("CLINE_DIR")); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cline", "data")
}

// clineMessagesPath is <data>/sessions/<id>/<id>.messages.json.
func clineMessagesPath(sessionID string) string {
	if !isSafeSessionID(sessionID) {
		return ""
	}
	return filepath.Join(clineDataDir(), "sessions", sessionID, sessionID+".messages.json")
}

// ReadClineTurns reads the cline transcript for sessionID (the conversation's
// external_id).
func ReadClineTurns(sessionID string) []Turn {
	path := clineMessagesPath(sessionID)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Messages []map[string]any `json:"messages"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	return clineTurns(doc.Messages)
}

// clineTurns folds the transcript into alternating turns. A wrapped user
// message opens a user turn; assistant blocks accumulate into one assistant
// turn. Tool results (role:"user") are dropped — the paired `tool_use` block
// already names the tool.
func clineTurns(msgs []map[string]any) []Turn {
	turns := []Turn{}
	var cur *Turn
	var blocks []map[string]any
	flush := func() {
		if cur != nil && len(blocks) > 0 {
			cur.Raw["blocks"] = blocks
			turns = append(turns, *cur)
		}
		cur = nil
		blocks = nil
	}
	idx := 0
	startAssistant := func(tsMillis int64, hasTS bool) {
		if cur != nil && cur.Role == "assistant" {
			return
		}
		flush()
		cur = &Turn{Role: "assistant", TSMillis: tsMillis, HasTS: hasTS,
			Raw: map[string]any{"id": "cline-asst-" + itoa(idx), "role": "assistant"}}
		idx++
	}
	for _, m := range msgs {
		if meta, ok := m["metadata"].(map[string]any); ok {
			// A failed run's display-only error is not part of the conversation.
			if displayOnly, _ := meta["displayOnly"].(bool); displayOnly {
				continue
			}
		}
		tsMillis, hasTS := jsonMillis(m["ts"])
		switch strOf(m["role"]) {
		case "user":
			var text strings.Builder
			for _, b := range clineBlocks(m["content"]) {
				if strOf(b["type"]) == "text" {
					text.WriteString(strOf(b["text"]))
				}
			}
			txt := clineCleanUserText(text.String())
			if strings.TrimSpace(txt) == "" {
				continue
			}
			flush()
			cur = &Turn{Role: "user", TSMillis: tsMillis, HasTS: hasTS,
				Raw: map[string]any{"id": "cline-user-" + itoa(idx), "role": "user"}}
			idx++
			blocks = []map[string]any{{"type": "text", "text": txt}}
		case "assistant":
			next := clineBlocks(m["content"])
			if len(next) == 0 {
				continue
			}
			startAssistant(tsMillis, hasTS)
			blocks = append(blocks, next...)
		}
	}
	flush()
	return turns
}

// clineBlocks maps a message's `content` to wire blocks (text / tool_use).
func clineBlocks(content any) []map[string]any {
	switch v := content.(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			return []map[string]any{{"type": "text", "text": v}}
		}
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, raw := range v {
			item, _ := raw.(map[string]any)
			if item == nil {
				continue
			}
			switch strOf(item["type"]) {
			case "text":
				if txt := strOf(item["text"]); strings.TrimSpace(txt) != "" {
					out = append(out, map[string]any{"type": "text", "text": txt})
				}
			case "thinking":
				if txt := strOf(item["thinking"]); strings.TrimSpace(txt) != "" {
					out = append(out, map[string]any{"type": "text", "text": txt})
				}
			case "tool_use":
				name := strOf(item["name"])
				if name == "" {
					name = "unknown"
				}
				blk := map[string]any{"type": "tool_use", "tool_name": name}
				if id := strOf(item["id"]); id != "" {
					blk["tool_use_id"] = id
				}
				if prev := inputPreview(item["input"]); prev != "" {
					blk["input_preview"] = prev
				}
				out = append(out, blk)
			}
		}
		return out
	}
	return nil
}

// clineCleanUserText unwraps the `<user_input>` / `<user_command>` wrappers
// (keeping their inner text) and drops `<mode_notice>` blocks whole.
func clineCleanUserText(s string) string {
	s = clineRewriteWrapper(s, "user_input", true)
	s = clineRewriteWrapper(s, "user_command", true)
	s = clineRewriteWrapper(s, "mode_notice", false)
	return strings.TrimSpace(s)
}

// clineRewriteWrapper replaces every `<tag …>inner</tag>` with `inner` (or drops
// the block when keepInner is false). The closing tag is searched from AFTER the
// opening tag's `>`, so the string strictly shrinks and the loop always ends —
// upstream shipped an exponential hang here once.
func clineRewriteWrapper(text, tag string, keepInner bool) string {
	open := "<" + tag
	closeTag := "</" + tag + ">"
	out := text
	from := 0
	for {
		rel := strings.Index(out[from:], open)
		if rel < 0 {
			return out
		}
		start := from + rel
		after := start + len(open)
		if after >= len(out) {
			return out
		}
		// `<user_input` must be the whole tag name, not a prefix of
		// `<user_inputs>`: the next character decides.
		if c := out[after]; c != '>' && c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			from = after
			continue
		}
		gt := strings.IndexByte(out[after:], '>')
		if gt < 0 {
			return out
		}
		innerStart := after + gt + 1
		closeRel := strings.Index(out[innerStart:], closeTag)
		if closeRel < 0 {
			return out
		}
		inner := out[innerStart : innerStart+closeRel]
		end := innerStart + closeRel + len(closeTag)
		if keepInner {
			out = out[:start] + inner + out[end:]
		} else {
			out = out[:start] + out[end:]
		}
		from = start
	}
}
