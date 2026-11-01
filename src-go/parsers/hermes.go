package parsers

// hermes.go reads Hermes Agent (Nous Research) transcripts.
//
// Hermes keeps its history in `<HERMES_HOME or ~/.hermes>/state.db` (SQLite).
// The two tables that matter:
//
//	sessions(id TEXT, model, cwd, title, …)
//	messages(id INTEGER, session_id, role, content, tool_call_id, tool_calls,
//	         tool_name, reasoning, reasoning_content, timestamp, active, …)
//
// Details that are load-bearing:
//   - `timestamp` is Unix epoch **seconds** as a REAL, not milliseconds.
//   - rows are ordered by `id` (insertion order), not by timestamp.
//   - rewound rows carry `active = 0` and must be skipped.
//   - multimodal `content` is prefixed with a NUL-byte `\x00json:` sentinel and
//     then holds a JSON parts array.
//   - `tool_calls` is the OpenAI function-call shape
//     `[{"id","type":"function","function":{"name","arguments"}}]`, where
//     `arguments` is a JSON string.
//
// The database is opened read-only (`mode=ro`), so a live Hermes process that
// is still writing is never disturbed. Mirrors upstream parsers/hermes.rs, minus
// usage/session stats, attachments and tool-result pairing.

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// hermesContentJSONPrefix marks a multimodal `content` value.
const hermesContentJSONPrefix = "\x00json:"

// hermesHomeDir resolves HERMES_HOME (taken verbatim — Hermes does not expand
// `~`), else ~/.hermes.
func hermesHomeDir() string {
	if v := strings.TrimSpace(os.Getenv("HERMES_HOME")); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hermes")
}

// ReadHermesTurns reads the Hermes transcript for sessionID (the conversation's
// external_id).
func ReadHermesTurns(sessionID string) []Turn {
	if !isSafeSessionID(sessionID) {
		return nil
	}
	path := filepath.Join(hermesHomeDir(), "state.db")
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return nil
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query(
		`SELECT id, role, content, tool_calls, reasoning, reasoning_content, timestamp
		 FROM messages
		 WHERE session_id = ? AND active = 1
		 ORDER BY id ASC`, sessionID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return hermesTurns(rows)
}

// hermesTurns folds active message rows into alternating turns: a user row
// opens a user turn, and the assistant rows that follow accumulate into one
// assistant turn. `role:"tool"` result rows are dropped — the paired tool_use
// block already names the tool — and `role:"system"` rows are infra noise.
func hermesTurns(rows *sql.Rows) []Turn {
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
			Raw: map[string]any{"id": "hermes-" + role + "-" + itoa(idx), "role": role}}
		idx++
	}
	for rows.Next() {
		var (
			id               int64
			role             string
			content          sql.NullString
			toolCalls        sql.NullString
			reasoning        sql.NullString
			reasoningContent sql.NullString
			ts               sql.NullFloat64
		)
		if err := rows.Scan(&id, &role, &content, &toolCalls, &reasoning, &reasoningContent, &ts); err != nil {
			continue
		}
		tsMillis, hasTS := int64(0), false
		if ts.Valid && ts.Float64 > 0 {
			// Epoch seconds, as a REAL.
			tsMillis, hasTS = int64(ts.Float64*1000), true
		}
		switch role {
		case "user":
			text := hermesContentText(content.String)
			if text == "" {
				continue
			}
			start("user", tsMillis, hasTS)
			blocks = []map[string]any{{"type": "text", "text": text}}
		case "assistant":
			var next []map[string]any
			// `reasoning` and `reasoning_content` are two spellings of the same
			// thing; take whichever the row carries.
			if r := strings.TrimSpace(firstNonEmptyString(reasoning.String, reasoningContent.String)); r != "" {
				next = append(next, map[string]any{"type": "text", "text": r})
			}
			if text := hermesContentText(content.String); text != "" {
				next = append(next, map[string]any{"type": "text", "text": text})
			}
			next = append(next, hermesToolBlocks(toolCalls.String)...)
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

// hermesContentText renders a `content` value as plain text: the multimodal
// form is a `\x00json:` sentinel followed by a JSON parts array, everything else
// is the text itself.
func hermesContentText(raw string) string {
	if raw == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(raw, hermesContentJSONPrefix); ok {
		var parts []any
		if json.Unmarshal([]byte(rest), &parts) == nil {
			out := make([]string, 0, len(parts))
			for _, part := range parts {
				switch v := part.(type) {
				case string:
					if s := strings.TrimSpace(v); s != "" {
						out = append(out, s)
					}
				case map[string]any:
					if strOf(v["type"]) != "text" {
						continue
					}
					if s := strings.TrimSpace(strOf(v["text"])); s != "" {
						out = append(out, s)
					}
				}
			}
			return strings.Join(out, "\n")
		}
		// Malformed after the sentinel: show the payload, minus the NUL.
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(raw)
}

// hermesToolBlocks maps a `tool_calls` JSON array to wire tool_use blocks.
func hermesToolBlocks(raw string) []map[string]any {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var calls []any
	if json.Unmarshal([]byte(raw), &calls) != nil {
		return nil
	}
	out := make([]map[string]any, 0, len(calls))
	for _, rawCall := range calls {
		call, _ := rawCall.(map[string]any)
		if call == nil {
			continue
		}
		fn, _ := call["function"].(map[string]any)
		name := strOf(fn["name"])
		if name == "" {
			name = "unknown"
		}
		blk := map[string]any{"type": "tool_use", "tool_name": name}
		if id := strOf(call["id"]); id != "" {
			blk["tool_use_id"] = id
		}
		if prev := jsonStringArgsPreview(fn["arguments"]); prev != "" {
			blk["input_preview"] = prev
		}
		out = append(out, blk)
	}
	return out
}

// firstNonEmptyString returns the first non-empty value after trimming.
func firstNonEmptyString(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
