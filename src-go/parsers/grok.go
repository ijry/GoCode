package parsers

// grok.go reads Grok (xAI) transcripts.
//
// Layout: `<GROK_HOME or ~/.grok>/sessions/<encoded-cwd>/<session-uuid>/`
// holding `updates.jsonl`, the conversation, plus `summary.json` / `plan.json` /
// `chat_history.jsonl` (not read here).
//
// `updates.jsonl` is a newline-delimited **ACP `session/update` stream** — the
// same shape this app consumes live:
//
//	{"method":"session/update","params":{"sessionId":…,"update":{…}},"timestamp":<unix secs>}
//
// `update.sessionUpdate` is `user_message_chunk`, `agent_message_chunk`,
// `agent_thought_chunk`, `tool_call`, `tool_call_update`, `turn_completed`, …
// The chunks are **deltas**, so consecutive chunks of one kind must be
// concatenated into a single block rather than rendered one block each.
//
// Two behaviours are load-bearing: a `user_message_chunk` whose
// `_meta.hideFromScrollback` is true is one of Grok's own reminders (its TUI
// never shows them), and a `user_message_chunk` continues the open user turn
// when no assistant content intervened — a "text + image" prompt is one bubble.
//
// Mirrors upstream parsers/grok.rs, minus usage, sub-agent stats, the
// `use_tool` envelope peel, plan/TODO state and tool_result pairing.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// grokSessionsDir resolves GROK_HOME (then ~) + "/sessions".
func grokSessionsDir() string {
	if v := strings.TrimSpace(os.Getenv("GROK_HOME")); v != "" {
		return filepath.Join(v, "sessions")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".grok", "sessions")
}

// findGrokUpdates locates `<sessions>/<encoded-cwd>/<sessionID>/updates.jsonl`.
func findGrokUpdates(sessionID string) string {
	if !isSafeSessionID(sessionID) {
		return ""
	}
	base := grokSessionsDir()
	groups, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	for _, g := range groups {
		if !g.IsDir() {
			continue
		}
		p := filepath.Join(base, g.Name(), sessionID, "updates.jsonl")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// ReadGrokTurns reads the Grok transcript for sessionID (the conversation's
// external_id).
func ReadGrokTurns(sessionID string) []Turn {
	path := findGrokUpdates(sessionID)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return grokTurns(data)
}

// grokTurns folds the update stream into alternating turns. Chunks of one kind
// that arrive back to back extend the block before them, so a streamed reply is
// one text block and a streamed thought is one thinking block.
func grokTurns(data []byte) []Turn {
	turns := []Turn{}
	var cur *Turn
	var blocks []map[string]any
	var kinds []string
	flush := func() {
		if cur != nil && len(blocks) > 0 {
			// Chunks are concatenated raw so a delta's own spacing survives;
			// trim once the turn is complete.
			for _, b := range blocks {
				if strOf(b["type"]) == "text" {
					b["text"] = strings.TrimSpace(strOf(b["text"]))
				}
			}
			cur.Raw["blocks"] = blocks
			turns = append(turns, *cur)
		}
		cur = nil
		blocks, kinds = nil, nil
	}
	idx := 0
	start := func(role string, tsMillis int64, hasTS bool) {
		flush()
		cur = &Turn{Role: role, TSMillis: tsMillis, HasTS: hasTS,
			Raw: map[string]any{"id": "grok-" + role + "-" + itoa(idx), "role": role}}
		idx++
	}
	// push appends text of the given kind, extending the previous block when it
	// is the same kind (the chunks are deltas of one message).
	push := func(kind, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		if n := len(blocks); n > 0 && kinds[n-1] == kind {
			blocks[n-1]["text"] = strOf(blocks[n-1]["text"]) + text
			return
		}
		blocks = append(blocks, map[string]any{"type": "text", "text": text})
		kinds = append(kinds, kind)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil || rec == nil {
			continue
		}
		params, _ := rec["params"].(map[string]any)
		update, _ := params["update"].(map[string]any)
		if update == nil {
			continue
		}
		kind := strOf(update["sessionUpdate"])
		if kind == "" {
			continue
		}
		// Grok's own reminders ride user chunks; its TUI hides them, and so do we.
		if kind == "user_message_chunk" {
			if meta, ok := update["_meta"].(map[string]any); ok {
				if hide, _ := meta["hideFromScrollback"].(bool); hide {
					continue
				}
			}
		}
		tsMillis, hasTS := grokMillis(rec["timestamp"])
		switch kind {
		case "user_message_chunk":
			text := grokContentText(update["content"])
			if text == "" {
				continue
			}
			// A chunk continues the open user turn unless assistant content has
			// intervened since.
			if cur == nil || cur.Role != "user" {
				start("user", tsMillis, hasTS)
			}
			push("text", text)
		case "agent_message_chunk":
			text := grokContentText(update["content"])
			if text == "" {
				continue
			}
			if cur == nil || cur.Role != "assistant" {
				start("assistant", tsMillis, hasTS)
			}
			push("text", text)
		case "agent_thought_chunk":
			text := grokContentText(update["content"])
			if text == "" {
				continue
			}
			if cur == nil || cur.Role != "assistant" {
				start("assistant", tsMillis, hasTS)
			}
			push("thinking", text)
		case "tool_call":
			blk := grokToolBlock(update)
			if blk == nil {
				continue
			}
			if cur == nil || cur.Role != "assistant" {
				start("assistant", tsMillis, hasTS)
			}
			blocks = append(blocks, blk)
			kinds = append(kinds, "tool")
		}
	}
	flush()
	return turns
}

// grokContentText reads an ACP content value as text: a `{type:"text",text}`
// object, a bare string, or an array of parts.
func grokContentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case map[string]any:
		if strOf(v["type"]) != "text" {
			return ""
		}
		return strOf(v["text"])
	case []any:
		parts := make([]string, 0, len(v))
		for _, raw := range v {
			if s := grokContentText(raw); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// grokToolBlock maps one `tool_call` update to a wire tool_use block. Grok names
// a tool in `_meta["x.ai/tool"].name`, falling back to `title` then `kind`.
func grokToolBlock(update map[string]any) map[string]any {
	name := ""
	if meta, ok := update["_meta"].(map[string]any); ok {
		if xt, ok := meta["x.ai/tool"].(map[string]any); ok {
			name = strOf(xt["name"])
		}
	}
	if name == "" {
		name = strOf(update["title"])
	}
	if name == "" {
		name = strOf(update["kind"])
	}
	if name == "" {
		name = "unknown"
	}
	blk := map[string]any{"type": "tool_use", "tool_name": name}
	if id := strOf(update["toolCallId"]); id != "" {
		blk["tool_use_id"] = id
	}
	if prev := inputPreview(update["rawInput"]); prev != "" {
		blk["input_preview"] = prev
	}
	return blk
}

// grokMillis reads an event's `timestamp`, which Grok writes in Unix **seconds**.
func grokMillis(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		if n > 0 {
			return int64(n * 1000), true
		}
	case int64:
		if n > 0 {
			return n * 1000, true
		}
	}
	return 0, false
}
