package parsers

// gemini.go reads Gemini CLI chat recordings. Gemini writes one file per
// session under <GEMINI_CLI_HOME>/.gemini/tmp/<project>/chats/:
//
//   - `session-<ts>-<shortId>.json`  — one JSON object with a `messages` array
//   - `session-<ts>-<shortId>.jsonl` — append-only records: metadata objects,
//     whole-list `{"$set":{"messages":[…]}}` snapshots, and per-message upserts
//     keyed by `id` (mirrors upstream parsers/gemini.rs, minus `$rewindTo`
//     bookkeeping and subagent summaries).
//
// A message is `{id, timestamp, type: "user"|"gemini", content, thoughts?,
// toolCalls?}`; content is a string or an array of `{text}` parts.

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// geminiBaseDir resolves GEMINI_CLI_HOME (then ~) + "/.gemini".
func geminiBaseDir() string {
	if v := os.Getenv("GEMINI_CLI_HOME"); v != "" {
		return filepath.Join(v, ".gemini")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini")
}

// findGeminiChatFile scans <base>/tmp/*/chats for the chat whose metadata names
// sessionID. The file name carries only the id's first 8 characters, so the
// walk first tries those files and falls back to every chat file.
func findGeminiChatFile(sessionID string) string {
	if !isSafeSessionID(sessionID) {
		return ""
	}
	root := filepath.Join(geminiBaseDir(), "tmp")
	short := sessionID
	if len(short) > 8 {
		short = short[:8]
	}
	scan := func(hint string) string {
		var found string
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || found != "" {
				return nil
			}
			name := d.Name()
			if !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".jsonl") {
				return nil
			}
			if !strings.Contains(filepath.ToSlash(p), "/chats/") {
				return nil
			}
			if hint != "" && !strings.Contains(name, hint) {
				return nil
			}
			if geminiSessionIDOf(p) == sessionID {
				found = p
			}
			return nil
		})
		return found
	}
	if p := scan(short); p != "" {
		return p
	}
	return scan("")
}

// geminiSessionIDOf reads just the sessionId out of a chat file.
func geminiSessionIDOf(path string) string {
	msgs, meta := readGeminiChat(path)
	_ = msgs
	return strOf(meta["sessionId"])
}

// ReadGeminiTurns reads the Gemini transcript for sessionID (the conversation
// external_id) and returns its user/assistant turns in order.
func ReadGeminiTurns(sessionID string) []Turn {
	path := findGeminiChatFile(sessionID)
	if path == "" {
		return nil
	}
	msgs, _ := readGeminiChat(path)
	return geminiTurns(msgs)
}

// readGeminiChat folds a chat file into its message list plus metadata, handling
// both the single-object and the append-only shapes.
func readGeminiChat(path string) ([]map[string]any, map[string]any) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	meta := map[string]any{}
	// A .json chat is one object; a .jsonl chat is one record per line. Try the
	// single-object shape first — it is what `.json` files hold and what the
	// legacy fallback re-reads.
	var single map[string]any
	if json.Unmarshal(data, &single) == nil && single != nil {
		if list, ok := single["messages"].([]any); ok {
			return geminiMessages(list), single
		}
	}

	messages := []map[string]any{}
	index := map[string]int{}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil || rec == nil {
			continue
		}
		if set, ok := rec["$set"].(map[string]any); ok {
			if list, ok := set["messages"].([]any); ok {
				messages = geminiMessages(list)
				index = map[string]int{}
				for i, m := range messages {
					if id := strOf(m["id"]); id != "" {
						index[id] = i
					}
				}
			}
			for k, v := range set {
				if k != "messages" {
					meta[k] = v
				}
			}
			continue
		}
		if id := strOf(rec["id"]); id != "" && strOf(rec["type"]) != "" {
			if i, ok := index[id]; ok {
				messages[i] = rec
			} else {
				index[id] = len(messages)
				messages = append(messages, rec)
			}
			continue
		}
		// Partial metadata objects repeat sessionId/projectHash.
		if strOf(rec["sessionId"]) != "" {
			for k, v := range rec {
				if k != "messages" {
					meta[k] = v
				}
			}
			if list, ok := rec["messages"].([]any); ok {
				messages = geminiMessages(list)
			}
		}
	}
	return messages, meta
}

// geminiMessages normalizes a raw message array into objects.
func geminiMessages(list []any) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, raw := range list {
		if m, ok := raw.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// geminiTurns converts messages into the neutral turn list, skipping the
// session-context preamble Gemini injects as a user message.
func geminiTurns(msgs []map[string]any) []Turn {
	turns := []Turn{}
	idx := 0
	for _, m := range msgs {
		role := ""
		switch strOf(m["type"]) {
		case "user":
			role = "user"
		case "gemini", "model", "assistant":
			role = "assistant"
		default:
			continue
		}
		blocks := geminiBlocks(m, role)
		if len(blocks) == 0 {
			continue
		}
		if role == "user" {
			if first, _ := blocks[0]["text"].(string); strings.HasPrefix(strings.TrimSpace(first), "<session_context>") {
				continue
			}
		}
		id := strOf(m["id"])
		if id == "" {
			id = "gemini-turn-" + itoa(idx)
		}
		ts := strOf(m["timestamp"])
		raw := map[string]any{"id": id, "role": role, "blocks": blocks}
		if ts != "" {
			raw["timestamp"] = ts
		}
		t := Turn{Role: role, Raw: raw}
		if ts != "" {
			if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
				t.TSMillis = parsed.UnixMilli()
				t.HasTS = true
			}
		}
		turns = append(turns, t)
		idx++
	}
	return turns
}

// geminiBlocks renders a message's content, plus the assistant's thoughts and
// tool calls, as wire content blocks.
func geminiBlocks(m map[string]any, role string) []map[string]any {
	out := []map[string]any{}
	if role == "assistant" {
		if thoughts, ok := m["thoughts"].([]any); ok {
			for _, raw := range thoughts {
				th, _ := raw.(map[string]any)
				if th == nil {
					continue
				}
				subject := strings.TrimSpace(strOf(th["subject"]))
				desc := strings.TrimSpace(strOf(th["description"]))
				text := subject
				switch {
				case subject != "" && desc != "":
					text = subject + ": " + desc
				case text == "":
					text = desc
				}
				if text != "" {
					out = append(out, map[string]any{"type": "text", "text": text})
				}
			}
		}
	}
	out = append(out, geminiTextBlocks(m["content"])...)
	if role == "assistant" {
		if calls, ok := m["toolCalls"].([]any); ok {
			for _, raw := range calls {
				call, _ := raw.(map[string]any)
				if call == nil {
					continue
				}
				name := strOf(call["displayName"])
				if name == "" {
					name = strOf(call["name"])
				}
				if name == "" {
					name = "unknown"
				}
				blk := map[string]any{"type": "tool_use", "tool_name": name}
				if id := strOf(call["id"]); id != "" {
					blk["tool_use_id"] = id
				}
				if prev := inputPreview(call["args"]); prev != "" {
					blk["input_preview"] = prev
				}
				out = append(out, blk)
			}
		}
	}
	return out
}

// geminiTextBlocks reads content as a string or as an array of text parts.
func geminiTextBlocks(content any) []map[string]any {
	out := []map[string]any{}
	switch v := content.(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			out = append(out, map[string]any{"type": "text", "text": v})
		}
	case []any:
		for _, raw := range v {
			part, _ := raw.(map[string]any)
			if part == nil {
				continue
			}
			if txt := strOf(part["text"]); txt != "" {
				out = append(out, map[string]any{"type": "text", "text": txt})
			}
		}
	case map[string]any:
		if txt := strOf(v["text"]); txt != "" {
			out = append(out, map[string]any{"type": "text", "text": txt})
		}
	}
	return out
}
