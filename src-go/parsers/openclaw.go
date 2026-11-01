package parsers

// openclaw.go reads OpenClaw transcripts.
//
// Layout: `<~/.openclaw/agents>/<agent>/sessions/<sessionId>.jsonl`, plus
// rotated archives named `<sessionId>.jsonl.reset.<timestamp>`.
//
// The file is a **tree**, not a stream: every record carries `id` and
// `parentId`, and a fork writes a second child under the same parent. A
// `type:"session"` header (skipped here) carries `cwd`. Conversation records are
// `type:"message"` with `message.role` of user / assistant / toolResult and
// Claude-shaped `message.content[]` blocks (text / thinking / tool_use) — the
// same shape parseClaudeFile reads, so contentBlocks is reused.
//
// A session file holds several branches; upstream lists one conversation per
// branch. This single-id reader follows the **longest root-to-leaf chain**, the
// branch a reader most likely means.
//
// User text is prefixed with plumbing that is stripped: a
// `Sender (untrusted metadata): ```…``` ` block, a `[Tue 2026-03-17 12:56 …]`
// timestamp, and a `[Working directory: …]` notice.
//
// Mirrors upstream parsers/openclaw.rs, minus usage, per-branch summaries and
// toolResult pairing.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	openclawSenderBlock = regexp.MustCompile("(?s)^Sender \\(untrusted metadata\\):\\s*```[^`]*```\\s*")
	openclawStampPrefix = regexp.MustCompile(`^\[.*?\]\s*`)
	openclawWorkDir     = regexp.MustCompile(`^\[Working directory:[^\]]*\]\s*`)
)

// openclawAgentsDir is ~/.openclaw/agents.
func openclawAgentsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".openclaw", "agents")
}

// findOpenClawSessionFile looks for `<id>.jsonl` (then the newest
// `<id>.jsonl.reset.*`) under every agent's sessions directory.
func findOpenClawSessionFile(sessionID string) string {
	if !isSafeSessionID(sessionID) {
		return ""
	}
	base := openclawAgentsDir()
	agents, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	fallback := ""
	for _, a := range agents {
		if !a.IsDir() {
			continue
		}
		sessions := filepath.Join(base, a.Name(), "sessions")
		if st, err := os.Stat(filepath.Join(sessions, sessionID+".jsonl")); err == nil && !st.IsDir() {
			return filepath.Join(sessions, sessionID+".jsonl")
		}
		ents, err := os.ReadDir(sessions)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() || !strings.HasPrefix(e.Name(), sessionID+".jsonl.reset.") {
				continue
			}
			// Names sort by their trailing timestamp; the last one wins.
			fallback = filepath.Join(sessions, e.Name())
		}
		if fallback != "" {
			return fallback
		}
	}
	return ""
}

// ReadOpenClawTurns reads the OpenClaw transcript for sessionID (the
// conversation's external_id).
func ReadOpenClawTurns(sessionID string) []Turn {
	path := findOpenClawSessionFile(sessionID)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return openclawTurns(data)
}

// openclawRecord is one parsed line of the tree.
type openclawRecord struct {
	id       string
	parentID string
	typ      string
	value    map[string]any
}

// openclawTurns parses the tree, picks the longest chain and folds its message
// records into alternating turns.
func openclawTurns(data []byte) []Turn {
	recs := map[string]*openclawRecord{}
	var order []string
	var roots []string
	children := map[string][]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var v map[string]any
		if json.Unmarshal([]byte(line), &v) != nil || v == nil {
			continue
		}
		typ := strOf(v["type"])
		if typ == "session" {
			continue // header: cwd only
		}
		id := strOf(v["id"])
		if id == "" {
			continue
		}
		parent := strOf(v["parentId"])
		recs[id] = &openclawRecord{id: id, parentID: parent, typ: typ, value: v}
		order = append(order, id)
		if parent == "" {
			roots = append(roots, id)
		} else {
			children[parent] = append(children[parent], id)
		}
	}
	// A file whose records all name a parent still has a head: treat records
	// whose parent is missing as roots, so a truncated log still renders.
	for _, id := range order {
		if p := recs[id].parentID; p != "" {
			if _, ok := recs[p]; !ok {
				roots = append(roots, id)
			}
		}
	}

	chain := openclawLongestChain(roots, children)
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
			Raw: map[string]any{"id": "claw-" + role + "-" + itoa(idx), "role": role}}
		idx++
	}
	for _, id := range chain {
		r := recs[id]
		if r.typ != "message" {
			continue
		}
		msg, _ := r.value["message"].(map[string]any)
		if msg == nil {
			continue
		}
		tsMillis, hasTS := jsonMillis(msg["timestamp"])
		if !hasTS {
			if parsed, err := time.Parse(time.RFC3339, strOf(r.value["timestamp"])); err == nil {
				tsMillis, hasTS = parsed.UnixMilli(), true
			}
		}
		switch strOf(msg["role"]) {
		case "user":
			text := openclawUserText(contentBlocks(msg["content"]))
			if text == "" {
				continue
			}
			start("user", tsMillis, hasTS)
			blocks = []map[string]any{{"type": "text", "text": text}}
		case "assistant":
			next := contentBlocks(msg["content"])
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

// openclawLongestChain returns the longest root-to-leaf path, so a forked
// session reads as its main branch.
func openclawLongestChain(roots []string, children map[string][]string) []string {
	memo := map[string][]string{}
	var walk func(id string) []string
	walk = func(id string) []string {
		if p, ok := memo[id]; ok {
			return p
		}
		var bestChild []string
		for _, c := range children[id] {
			if p := walk(c); len(p) > len(bestChild) {
				bestChild = p
			}
		}
		p := make([]string, 0, len(bestChild)+1)
		p = append(p, id)
		p = append(p, bestChild...)
		memo[id] = p
		return p
	}
	var best []string
	for _, r := range roots {
		if p := walk(r); len(p) > len(best) {
			best = p
		}
	}
	return best
}

// openclawUserText joins a user message's text blocks and strips the prefixes
// OpenClaw wraps them in.
func openclawUserText(blocks []map[string]any) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if strOf(b["type"]) != "text" {
			continue
		}
		if s := strings.TrimSpace(strOf(b["text"])); s != "" {
			parts = append(parts, s)
		}
	}
	text := strings.Join(parts, "\n")
	text = openclawSenderBlock.ReplaceAllString(text, "")
	text = openclawStampPrefix.ReplaceAllString(text, "")
	text = openclawWorkDir.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}
