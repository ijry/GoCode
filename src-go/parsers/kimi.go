package parsers

// kimi.go reads Kimi Code (Moonshot) transcripts.
//
// Kimi Code uses a directory-per-session layout:
//
//	$KIMI_CODE_HOME/                      (default ~/.kimi-code)
//	├── session_index.jsonl               {sessionId, sessionDir, workDir}
//	└── sessions/
//	    └── <workDirKey>/                 bucketed by working directory
//	        └── <sessionId>/
//	            ├── state.json
//	            └── agents/main/wire.jsonl   the primary event stream
//
// wire.jsonl is an append-only event log, not an ACP session/update stream. The
// records that carry conversation content are:
//
//   - `turn.prompt` — a user prompt (`input[]` of `{type:"text", text}` parts).
//   - `context.append_loop_event` — the assistant's work, where `event.type` is
//     `content.part` (`part.type` "text" | "think"), `tool.call`
//     (`toolCallId` / `name` / `args`), or `tool.result` (skipped here).
//   - `usage.record` / `step.begin` / `step.end` — token accounting, skipped.
//
// `context.append_message` records only echo the prompt into the model context,
// so they are skipped to avoid duplicate messages. Mirrors upstream
// parsers/kimi_code.rs, minus usage/sub-agent statistics and the model-name
// recovery from logs/.

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// kimiSessionsDir resolves KIMI_CODE_HOME (then ~) + "/sessions".
func kimiSessionsDir() string {
	if v := os.Getenv("KIMI_CODE_HOME"); v != "" {
		return filepath.Join(v, "sessions")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kimi-code", "sessions")
}

// findKimiWireFile locates <sessions>/<workDirKey>/<sessionID>/agents/main/
// wire.jsonl. The bucket name is opaque, so it is discovered by walking the
// session root's immediate children.
func findKimiWireFile(sessionID string) string {
	if !isSafeSessionID(sessionID) {
		return ""
	}
	base := kimiSessionsDir()
	buckets, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	for _, b := range buckets {
		if !b.IsDir() {
			continue
		}
		p := filepath.Join(base, b.Name(), sessionID, "agents", "main", "wire.jsonl")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// ReadKimiTurns reads the Kimi transcript for sessionID (the conversation's
// external_id) and returns its user/assistant turns in order.
func ReadKimiTurns(sessionID string) []Turn {
	path := findKimiWireFile(sessionID)
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	return kimiTurns(f)
}

// kimiTurns folds the wire log into alternating turns: a `turn.prompt` opens a
// user turn, and the content/tool events that follow accumulate into a single
// assistant turn until the next prompt.
func kimiTurns(r io.Reader) []Turn {
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
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	idx := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil || rec == nil {
			continue
		}
		tsMillis, hasTS := jsonMillis(rec["time"])
		switch strOf(rec["type"]) {
		case "turn.prompt":
			flush()
			text := kimiPromptText(rec)
			if strings.TrimSpace(text) == "" {
				continue
			}
			cur = &Turn{Role: "user", TSMillis: tsMillis, HasTS: hasTS,
				Raw: map[string]any{"id": "kimi-user-" + itoa(idx), "role": "user"}}
			idx++
			blocks = []map[string]any{{"type": "text", "text": text}}
		case "context.append_loop_event":
			ev, _ := rec["event"].(map[string]any)
			if ev == nil {
				continue
			}
			next := kimiEventBlocks(ev)
			if len(next) == 0 {
				continue
			}
			if cur == nil || cur.Role != "assistant" {
				flush()
				cur = &Turn{Role: "assistant", TSMillis: tsMillis, HasTS: hasTS,
					Raw: map[string]any{"id": "kimi-asst-" + itoa(idx), "role": "assistant"}}
				idx++
			}
			blocks = append(blocks, next...)
		}
	}
	flush()
	return turns
}

// kimiPromptText joins the text parts of a `turn.prompt`'s `input[]`.
func kimiPromptText(rec map[string]any) string {
	items, _ := rec["input"].([]any)
	parts := make([]string, 0, len(items))
	for _, raw := range items {
		m, _ := raw.(map[string]any)
		if m == nil || strOf(m["type"]) != "text" {
			continue
		}
		if txt := strOf(m["text"]); strings.TrimSpace(txt) != "" {
			parts = append(parts, txt)
		}
	}
	return strings.Join(parts, "\n")
}

// kimiEventBlocks maps one loop event to wire content blocks (nil when the
// event carries no renderable content).
func kimiEventBlocks(ev map[string]any) []map[string]any {
	switch strOf(ev["type"]) {
	case "content.part":
		part, _ := ev["part"].(map[string]any)
		if part == nil {
			return nil
		}
		switch strOf(part["type"]) {
		case "text":
			if txt := strings.TrimSpace(strOf(part["text"])); txt != "" {
				return []map[string]any{{"type": "text", "text": txt}}
			}
		case "think":
			if txt := strings.TrimSpace(strOf(part["think"])); txt != "" {
				return []map[string]any{{"type": "text", "text": txt}}
			}
		}
	case "tool.call":
		name := strOf(ev["name"])
		if name == "" {
			name = "unknown"
		}
		blk := map[string]any{"type": "tool_use", "tool_name": name}
		if id := strOf(ev["toolCallId"]); id != "" {
			blk["tool_use_id"] = id
		}
		if prev := inputPreview(ev["args"]); prev != "" {
			blk["input_preview"] = prev
		}
		return []map[string]any{blk}
	}
	return nil
}

// jsonMillis reads a record's millisecond timestamp (number or numeric string).
func jsonMillis(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), n > 0
	case int64:
		return n, n > 0
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil && f > 0 {
			return int64(f), true
		}
	}
	return 0, false
}
