package parsers

// pi.go reads `pi` (pi-acp) transcripts.
//
// Sessions live under the sessions directory as
//
//	<sessions>/--<dashed-cwd>--/<timestamp>_<uuid>.jsonl
//
// (a custom `sessionDir` drops the per-cwd bucket and writes flat files). The
// first line is a header `{"type":"session","version":3,"id":…,"cwd":…}` whose
// `id` IS the conversation's external id.
//
// Records are `{type, id, parentId, timestamp}`; `type:"message"` carries
// `message.role` of user / assistant / toolResult:
//   - user `content` is a string or an array of `{type:"text",text}`;
//   - assistant `content` is an array of `{type:"text",text}`,
//     `{type:"thinking",thinking}` and `{type:"toolCall",id,name,arguments}`
//     where `arguments` is an object.
//
// The sessions directory resolves, highest precedence first, from
// `PI_CODING_AGENT_SESSION_DIR`, then an ABSOLUTE `sessionDir` in the agent
// dir's `settings.json`, then `<agent dir>/sessions`; the agent dir is
// `PI_CODING_AGENT_DIR` (tilde-expanded) or `~/.pi/agent`.
//
// Mirrors upstream parsers/pi.rs, minus usage, titles, model changes and
// toolResult pairing.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// expandPiTilde applies pi's own rule: a leading `~` means the user's home.
func expandPiTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, _ := os.UserHomeDir()
	rest := strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/")
	return filepath.Join(home, rest)
}

// piAgentDir resolves PI_CODING_AGENT_DIR, else ~/.pi/agent.
func piAgentDir() string {
	if v := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR")); v != "" {
		return expandPiTilde(v)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "agent")
}

// piSessionsDir resolves the sessions directory (see the file comment).
func piSessionsDir() string {
	if v := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_SESSION_DIR")); v != "" {
		return expandPiTilde(v)
	}
	agentDir := piAgentDir()
	// settings.json may relocate the store, but only when it is absolute — pi
	// resolves a relative one against its own cwd, which is not ours.
	if data, err := os.ReadFile(filepath.Join(agentDir, "settings.json")); err == nil {
		var settings struct {
			SessionDir string `json:"sessionDir"`
		}
		if json.Unmarshal(data, &settings) == nil && settings.SessionDir != "" {
			if dir := expandPiTilde(settings.SessionDir); filepath.IsAbs(dir) {
				return dir
			}
		}
	}
	return filepath.Join(agentDir, "sessions")
}

// findPiSessionFile walks the sessions directory for the file whose header id
// matches sessionID. The file name ends with the id's first eight characters,
// so that hint is tried first.
func findPiSessionFile(sessionID string) string {
	if !isSafeSessionID(sessionID) {
		return ""
	}
	base := piSessionsDir()
	short := sessionID
	if len(short) > 8 {
		short = short[:8]
	}
	scan := func(hint string) string {
		found := ""
		_ = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || found != "" {
				return nil
			}
			name := d.Name()
			if !strings.HasSuffix(name, ".jsonl") {
				return nil
			}
			if hint != "" && !strings.Contains(name, hint) {
				return nil
			}
			if piSessionIDOf(p) == sessionID {
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

// piSessionIDOf reads just the header's `id`.
func piSessionIDOf(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 64*1024)
	n, _ := f.Read(buf)
	line := string(buf[:n])
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	var rec map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(line)), &rec) != nil {
		return ""
	}
	if strOf(rec["type"]) != "session" {
		return ""
	}
	return strOf(rec["id"])
}

// ReadPiTurns reads the pi transcript for sessionID (the conversation's
// external_id).
func ReadPiTurns(sessionID string) []Turn {
	path := findPiSessionFile(sessionID)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return piTurns(data)
}

// piTurns folds the JSONL into alternating turns: a user message opens a user
// turn, and the assistant messages that follow accumulate into one assistant
// turn. `toolResult` rows are dropped.
func piTurns(data []byte) []Turn {
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
	start := func(role string, tsMillis int64, hasTS bool) {
		flush()
		cur = &Turn{Role: role, TSMillis: tsMillis, HasTS: hasTS,
			Raw: map[string]any{"id": "pi-" + role + "-" + itoa(idx), "role": role}}
		idx++
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
		if strOf(rec["type"]) != "message" {
			continue // session header, session_info, model_change, …
		}
		msg, _ := rec["message"].(map[string]any)
		if msg == nil {
			continue
		}
		tsMillis, hasTS := int64(0), false
		if parsed, err := time.Parse(time.RFC3339, strOf(rec["timestamp"])); err == nil {
			tsMillis, hasTS = parsed.UnixMilli(), true
		}
		switch strOf(msg["role"]) {
		case "user":
			text := piUserText(msg["content"])
			if text == "" {
				continue
			}
			start("user", tsMillis, hasTS)
			blocks = []map[string]any{{"type": "text", "text": text}}
		case "assistant":
			next := piAssistantBlocks(msg["content"])
			if len(next) == 0 {
				continue
			}
			if cur == nil || cur.Role != "assistant" {
				start("assistant", tsMillis, hasTS)
			}
			blocks = append(blocks, next...)
		}
	}
	flush()
	return turns
}

// piUserText reads a user `content` as text: a bare string or an array of text
// parts.
func piUserText(content any) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, raw := range v {
			part, _ := raw.(map[string]any)
			if part == nil || strOf(part["type"]) != "text" {
				continue
			}
			if s := strings.TrimSpace(strOf(part["text"])); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// piAssistantBlocks maps an assistant `content` array to wire blocks.
func piAssistantBlocks(content any) []map[string]any {
	items, _ := content.([]any)
	out := make([]map[string]any, 0, len(items))
	for _, raw := range items {
		blk, _ := raw.(map[string]any)
		if blk == nil {
			continue
		}
		switch strOf(blk["type"]) {
		case "text":
			if s := strings.TrimSpace(strOf(blk["text"])); s != "" {
				out = append(out, map[string]any{"type": "text", "text": s})
			}
		case "thinking":
			// The payload key is `thinking`, not `text`.
			if s := strings.TrimSpace(strOf(blk["thinking"])); s != "" {
				out = append(out, map[string]any{"type": "text", "text": s})
			}
		case "toolCall":
			name := strOf(blk["name"])
			if name == "" {
				name = "unknown"
			}
			b := map[string]any{"type": "tool_use", "tool_name": name}
			if id := strOf(blk["id"]); id != "" {
				b["tool_use_id"] = id
			}
			if prev := inputPreview(blk["arguments"]); prev != "" {
				b["input_preview"] = prev
			}
			out = append(out, b)
		}
	}
	return out
}
