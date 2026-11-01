package parsers

// codebuddy.go reads CodeBuddy (Tencent Cloud) transcripts.
//
// Layout borrows Claude Code's: one JSONL per session under
// `<CODEBUDDY_CONFIG_DIR or ~/.codebuddy>/projects/<encoded-cwd>/<sessionId>.jsonl`.
// The per-line schema is the OpenAI Agents SDK "items" shape, NOT Claude's:
//
//   - `message` with `role` "user" | "assistant"; text lives in `content[]`
//     items typed `input_text` / `output_text`.
//   - `reasoning` — reasoning text in `rawContent[].text` (some records mirror
//     it under `content[]`).
//   - `function_call` — `callId` / `name` / `arguments` (a JSON string).
//   - `timestamp` — millisecond epoch.
//
// Mirrors upstream parsers/codebuddy.rs, minus usage, title resolution,
// sub-agent/background-task bookkeeping and `function_call_result` pairing.

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// codebuddyProjectsDir resolves CODEBUDDY_CONFIG_DIR (then ~) + "/projects".
func codebuddyProjectsDir() string {
	if v := os.Getenv("CODEBUDDY_CONFIG_DIR"); v != "" {
		return filepath.Join(v, "projects")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codebuddy", "projects")
}

// findCodeBuddyFile scans <projects>/*/<sessionID>.jsonl.
func findCodeBuddyFile(sessionID string) string {
	if !isSafeSessionID(sessionID) {
		return ""
	}
	base := codebuddyProjectsDir()
	ents, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		cand := filepath.Join(base, e.Name(), sessionID+".jsonl")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return ""
}

// ReadCodeBuddyTurns reads the CodeBuddy transcript for sessionID (the
// conversation's external_id).
func ReadCodeBuddyTurns(sessionID string) []Turn {
	path := findCodeBuddyFile(sessionID)
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	return codebuddyTurns(f)
}

// codebuddyTurns folds the JSONL into alternating turns: a user message opens a
// user turn, and the assistant message/reasoning/function_call records that
// follow accumulate into one assistant turn until the next user message.
func codebuddyTurns(r io.Reader) []Turn {
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
		tsMillis, hasTS := jsonMillis(rec["timestamp"])
		switch strOf(rec["type"]) {
		case "message":
			role := strOf(rec["role"])
			switch role {
			case "user":
				text := codebuddyText(rec, "input_text")
				if strings.TrimSpace(text) == "" {
					continue
				}
				flush()
				cur = &Turn{Role: "user", TSMillis: tsMillis, HasTS: hasTS,
					Raw: map[string]any{"id": "cb-user-" + itoa(idx), "role": "user"}}
				idx++
				blocks = []map[string]any{{"type": "text", "text": text}}
			case "assistant":
				text := codebuddyText(rec, "output_text")
				if strings.TrimSpace(text) == "" {
					continue
				}
				if cur == nil || cur.Role != "assistant" {
					flush()
					cur = &Turn{Role: "assistant", TSMillis: tsMillis, HasTS: hasTS,
						Raw: map[string]any{"id": "cb-asst-" + itoa(idx), "role": "assistant"}}
					idx++
				}
				blocks = append(blocks, map[string]any{"type": "text", "text": text})
			}
		case "reasoning":
			text := codebuddyReasoning(rec)
			if strings.TrimSpace(text) == "" {
				continue
			}
			if cur == nil || cur.Role != "assistant" {
				flush()
				cur = &Turn{Role: "assistant", TSMillis: tsMillis, HasTS: hasTS,
					Raw: map[string]any{"id": "cb-asst-" + itoa(idx), "role": "assistant"}}
				idx++
			}
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		case "function_call":
			name := strOf(rec["name"])
			if name == "" {
				name = "unknown"
			}
			blk := map[string]any{"type": "tool_use", "tool_name": name}
			if id := strOf(rec["callId"]); id != "" {
				blk["tool_use_id"] = id
			}
			if prev := jsonStringArgsPreview(rec["arguments"]); prev != "" {
				blk["input_preview"] = prev
			}
			if cur == nil || cur.Role != "assistant" {
				flush()
				cur = &Turn{Role: "assistant", TSMillis: tsMillis, HasTS: hasTS,
					Raw: map[string]any{"id": "cb-asst-" + itoa(idx), "role": "assistant"}}
				idx++
			}
			blocks = append(blocks, blk)
		}
	}
	flush()
	return turns
}

// codebuddyText concatenates the `content[]` items of the given type.
func codebuddyText(rec map[string]any, itemType string) string {
	items, _ := rec["content"].([]any)
	var b strings.Builder
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil || strOf(item["type"]) != itemType {
			continue
		}
		b.WriteString(strOf(item["text"]))
	}
	return b.String()
}

// codebuddyReasoning reads reasoning text from `rawContent[]`, falling back to
// `content[]` (some records mirror it there).
func codebuddyReasoning(rec map[string]any) string {
	for _, key := range []string{"rawContent", "content"} {
		items, _ := rec[key].([]any)
		var b strings.Builder
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if item == nil {
				continue
			}
			b.WriteString(strOf(item["text"]))
		}
		if strings.TrimSpace(b.String()) != "" {
			return b.String()
		}
	}
	return ""
}

// jsonStringArgsPreview renders an `arguments` value that is a JSON string as the
// same compact preview a native object would produce.
func jsonStringArgsPreview(v any) string {
	if s, ok := v.(string); ok {
		var parsed any
		if json.Unmarshal([]byte(s), &parsed) == nil {
			return inputPreview(parsed)
		}
		return inputPreview(s)
	}
	return inputPreview(v)
}
