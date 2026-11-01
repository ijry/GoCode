package parsers

// codex.go reads Codex's rollout transcripts. Codex writes one JSONL per
// session under $CODEX_HOME/sessions/<YYYY>/<MM>/<DD>/rollout-<ts>-<session>.jsonl,
// or the same stream zstd-compressed as `….jsonl.zst` — current codex writes the
// compressed form almost exclusively. Each line is an envelope with a type and a
// payload. The renderable turns are
// `response_item` / `payload.type == "message"` records with role user or
// assistant — developer (system-prompt) messages and tool/reasoning records are
// skipped, matching the minimal reader the Claude parser does.

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// codexConfigDir resolves CODEX_HOME, else ~/.codex.
func codexConfigDir() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

// findCodexSessionFile scans <codex>/sessions/<YYYY>/<MM>/<DD>/ for a rollout
// whose name ends in "-<sessionID>.jsonl", or the zstd-compressed
// "-<sessionID>.jsonl.zst" that current codex writes (on a machine with both,
// the compressed form is the overwhelming majority). The tree is at most a few
// thousand files deep (three levels), so a walk is cheap.
func findCodexSessionFile(sessionID string) string {
	if !isSafeSessionID(sessionID) {
		return ""
	}
	base := filepath.Join(codexConfigDir(), "sessions")
	suffix := "-" + sessionID + ".jsonl"
	var found string
	_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		name := strings.TrimSuffix(d.Name(), ".zst")
		if strings.HasSuffix(name, suffix) {
			found = path
		}
		return nil
	})
	return found
}

// ReadCodexTurns reads the Codex transcript for sessionID (the conversation
// external_id) and returns its user/assistant turns in order. Returns nil when
// no rollout exists on disk.
func ReadCodexTurns(sessionID string) []Turn {
	path := findCodexSessionFile(sessionID)
	if path == "" {
		return nil
	}
	return parseCodexFile(path)
}

func parseCodexFile(path string) []Turn {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(path, ".zst") {
		// A compressed rollout is a zstd stream of the same JSONL lines.
		zr, err := zstd.NewReader(f)
		if err != nil {
			return nil
		}
		defer zr.Close()
		r = zr
	}
	turns := []Turn{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024) // rollout lines get long
	idx := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if strOf(rec["type"]) != "response_item" {
			continue
		}
		payload, _ := rec["payload"].(map[string]any)
		if payload == nil || strOf(payload["type"]) != "message" {
			continue
		}
		role := strOf(payload["role"])
		if role != "user" && role != "assistant" {
			continue // developer/system prompts are not conversation turns
		}
		blocks := codexContentBlocks(payload["content"])
		if len(blocks) == 0 {
			continue
		}
		if role == "user" {
			first, _ := blocks[0]["text"].(string)
			if codexInjectedContext(first) {
				continue
			}
		}
		ts := strOf(rec["timestamp"])
		raw := map[string]any{"id": codexTurnID(payload, idx), "role": role, "blocks": blocks}
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

// codexTurnID prefers the record's own id and falls back to a stable index.
func codexTurnID(payload map[string]any, idx int) string {
	if id := strOf(payload["id"]); id != "" {
		return id
	}
	return "codex-turn-" + itoa(idx)
}

// codexInjectedContext reports whether a user-role message is Codex's own
// injected context rather than something the user typed: the AGENTS.md
// instructions block, the environment context, and the user-instructions block.
// They arrive as user messages in the rollout and would otherwise show up as
// bogus turns at the head of the history.
func codexInjectedContext(text string) bool {
	t := strings.TrimSpace(text)
	for _, prefix := range []string{
		"# AGENTS.md instructions",
		"<environment_context",
		"<user_instructions>",
		"<permissions instructions>",
	} {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

// codexContentBlocks normalizes a Codex message.content array (input_text /
// output_text items) into the wire ContentBlock list, keeping text only.
func codexContentBlocks(content any) []map[string]any {
	out := []map[string]any{}
	switch v := content.(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			out = append(out, map[string]any{"type": "text", "text": v})
		}
	case []any:
		for _, raw := range v {
			b, _ := raw.(map[string]any)
			if b == nil {
				continue
			}
			switch strOf(b["type"]) {
			case "input_text", "output_text", "text":
				if txt := strOf(b["text"]); txt != "" {
					out = append(out, map[string]any{"type": "text", "text": txt})
				}
			}
		}
	}
	return out
}
