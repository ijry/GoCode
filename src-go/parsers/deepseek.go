package parsers

// deepseek.go reads DeepSeek Harness (deepseek-acp) transcripts.
//
// Each session is an append-only event log, generation-addressed:
//
//	$DEEPSEEK_ACP_SESSIONS_ROOT | $DSH_HOME/sessions | ~/.dsh/sessions
//	└── <munged cwd>/
//	    └── <session uuid>/
//	        ├── session.jsonl.zstd      generation 0 (deepseek-acp <= 0.8.0)
//	        └── session.v3.jsonl.zstd   generation 3 (deepseek-acp >= 0.9.0)
//
// The numerically highest generation is live; a migration leaves its
// predecessor beside it, so picking the wrong one would freeze history at the
// migration point. A `compression: "none"` deployment writes the same names
// without `.zstd`, and a zstd set always outranks a raw one.
//
// The file is a sequence of complete Zstandard frames (one per write batch), so
// a streaming decoder walks them all. Each line is `{type, seq, time, data}`
// with `time` in epoch ms. Content comes from:
//
//   - `user/message` — `data.source.kind` must be "user"; "plugin" records are
//     injected runtime context (and compaction summaries), not prompts.
//   - `assistant/message` — `data.message.content[]` of `text` / `reasoning` /
//     `tool-call {id, name, arguments}`.
//
// Raw `assistant/chunk` / `*-chunks` rows duplicate the assembled message and
// are skipped (0.9.0 stopped writing them; older logs still hold them).
// Mirrors upstream parsers/deepseek.rs, minus usage, attachments, delegation
// and turn/end bookkeeping.

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// dshHomeDir resolves DSH_HOME (a whitespace-only value counts as unset, a
// leading `~` expands), else ~/.dsh.
func dshHomeDir() string {
	home, _ := os.UserHomeDir()
	if v := strings.TrimSpace(os.Getenv("DSH_HOME")); v != "" {
		if strings.HasPrefix(v, "~") {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(v, "~"), "/"))
		}
		return v
	}
	return filepath.Join(home, ".dsh")
}

// deepseekSessionsRoot resolves DEEPSEEK_ACP_SESSIONS_ROOT, else $DSH_HOME/sessions.
func deepseekSessionsRoot() string {
	if v := strings.TrimSpace(os.Getenv("DEEPSEEK_ACP_SESSIONS_ROOT")); v != "" {
		return v
	}
	return filepath.Join(dshHomeDir(), "sessions")
}

// deepseekLogGeneration parses `session[.vN].jsonl[.zstd]` into its generation
// and encoding. `[1-9][0-9]*` only: no `.v0`, no leading zeros.
func deepseekLogGeneration(name string) (gen int, compressed bool, ok bool) {
	stem := name
	if strings.HasSuffix(stem, ".zstd") {
		compressed = true
		stem = strings.TrimSuffix(stem, ".zstd")
	}
	if !strings.HasSuffix(stem, ".jsonl") {
		return 0, false, false
	}
	stem = strings.TrimSuffix(stem, ".jsonl")
	if stem == "session" {
		return 0, compressed, true
	}
	digits, found := strings.CutPrefix(stem, "session.v")
	if !found || digits == "" || digits[0] < '1' || digits[0] > '9' {
		return 0, false, false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, false, false
		}
	}
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0, false, false
	}
	return n, compressed, true
}

// findDeepseekLog locates the live log for sessionID. Encoding first (a zstd
// set outranks a raw one), generation second (the highest wins).
func findDeepseekLog(sessionID string) string {
	if !isSafeSessionID(sessionID) {
		return ""
	}
	root := deepseekSessionsRoot()
	buckets, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, b := range buckets {
		if !b.IsDir() {
			continue
		}
		if best := deepseekBestLogIn(filepath.Join(root, b.Name(), sessionID)); best != "" {
			return best
		}
	}
	return ""
}

// deepseekBestLogIn picks the live log inside one session directory: encoding
// first (a zstd set outranks a raw one), generation second (the highest wins).
func deepseekBestLogIn(dir string) string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best := ""
	bestGen := -1
	bestCompressed := false
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		gen, compressed, ok := deepseekLogGeneration(e.Name())
		if !ok {
			continue
		}
		better := best == "" ||
			(compressed && !bestCompressed) ||
			(compressed == bestCompressed && gen > bestGen)
		if better {
			best = filepath.Join(dir, e.Name())
			bestGen = gen
			bestCompressed = compressed
		}
	}
	return best
}

// ReadDeepseekTurns reads the DeepSeek transcript for sessionID (the
// conversation's external_id).
func ReadDeepseekTurns(sessionID string) []Turn {
	path := findDeepseekLog(sessionID)
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(path, ".zstd") {
		zr, err := zstd.NewReader(f)
		if err != nil {
			return nil
		}
		defer zr.Close()
		r = zr
	}
	return deepseekTurns(r)
}

// deepseekTurns folds the event log into alternating turns: a real user prompt
// opens a user turn, and the assistant messages that follow accumulate into one
// assistant turn.
func deepseekTurns(r io.Reader) []Turn {
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
		data, _ := rec["data"].(map[string]any)
		switch strOf(rec["type"]) {
		case "user/message":
			if data == nil {
				continue
			}
			src, _ := data["source"].(map[string]any)
			if strOf(src["kind"]) != "user" {
				continue
			}
			text := deepseekTextParts(data["content"])
			if strings.TrimSpace(text) == "" {
				continue
			}
			flush()
			cur = &Turn{Role: "user", TSMillis: tsMillis, HasTS: hasTS,
				Raw: map[string]any{"id": "ds-user-" + itoa(idx), "role": "user"}}
			idx++
			blocks = []map[string]any{{"type": "text", "text": text}}
		case "assistant/message":
			if data == nil {
				continue
			}
			msg, _ := data["message"].(map[string]any)
			if msg == nil {
				continue
			}
			next := deepseekAssistantBlocks(msg["content"])
			if len(next) == 0 {
				continue
			}
			if cur == nil || cur.Role != "assistant" {
				flush()
				cur = &Turn{Role: "assistant", TSMillis: tsMillis, HasTS: hasTS,
					Raw: map[string]any{"id": "ds-asst-" + itoa(idx), "role": "assistant"}}
				idx++
			}
			blocks = append(blocks, next...)
		}
	}
	flush()
	return turns
}

// deepseekTextParts joins the `text` parts of a message `content[]`.
func deepseekTextParts(content any) string {
	items, _ := content.([]any)
	parts := make([]string, 0, len(items))
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil || strOf(item["type"]) != "text" {
			continue
		}
		if txt := strOf(item["text"]); strings.TrimSpace(txt) != "" {
			parts = append(parts, txt)
		}
	}
	return strings.Join(parts, "\n")
}

// deepseekAssistantBlocks maps an assistant message's `content[]` to wire
// blocks (text / reasoning-as-text / tool_use).
func deepseekAssistantBlocks(content any) []map[string]any {
	items, _ := content.([]any)
	out := make([]map[string]any, 0, len(items))
	for _, raw := range items {
		blk, _ := raw.(map[string]any)
		if blk == nil {
			continue
		}
		switch strOf(blk["type"]) {
		case "text", "reasoning":
			if txt := strings.TrimSpace(strOf(blk["text"])); txt != "" {
				out = append(out, map[string]any{"type": "text", "text": txt})
			}
		case "tool-call":
			name := strOf(blk["name"])
			if name == "" {
				name = "unknown"
			}
			b := map[string]any{"type": "tool_use", "tool_name": name}
			if id := strOf(blk["id"]); id != "" {
				b["tool_use_id"] = id
			}
			if prev := jsonStringArgsPreview(blk["arguments"]); prev != "" {
				b["input_preview"] = prev
			}
			out = append(out, b)
		}
	}
	return out
}
