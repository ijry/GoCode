package parsers

// opencode.go reads OpenCode transcripts.
//
// The store is `<XDG_DATA_HOME or ~/.local/share>/opencode/opencode.db`
// (SQLite), with two tables that matter:
//
//	message(id, session_id, time_created, data)   data.role + data.time.created
//	part(id, message_id, time_created, data)      data.type: text / reasoning / tool
//
// `data` is JSON. A `text` part flagged `synthetic` is prose OpenCode injected
// for the model (plan/build reminders, the post-compaction "continue" prompt),
// not something a person typed, so it is dropped — rendering it put whole system
// prompts in the user's bubble.
//
// A `tool` part is `{tool, callID, state:{status, input}}`.
//
// The database is opened read-only. Mirrors upstream parsers/opencode.rs, minus
// usage, sub-agent stats, the `task` → Agent card rewrite and file-change parts.

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// opencodeDBPath resolves the SQLite store.
func opencodeDBPath() string {
	base := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "opencode", "opencode.db")
}

// ReadOpenCodeTurns reads the OpenCode transcript for sessionID (the
// conversation's external_id).
func ReadOpenCodeTurns(sessionID string) []Turn {
	if !isSafeSessionID(sessionID) {
		return nil
	}
	path := opencodeDBPath()
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return nil
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query(
		`SELECT id, time_created, data FROM message
		 WHERE session_id = ?
		 ORDER BY time_created ASC, id ASC`, sessionID)
	if err != nil {
		return nil
	}
	defer rows.Close()

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
			Raw: map[string]any{"id": "oc-" + role + "-" + itoa(idx), "role": role}}
		idx++
	}
	for rows.Next() {
		var (
			msgID       string
			timeCreated sql.NullInt64
			data        sql.NullString
		)
		if err := rows.Scan(&msgID, &timeCreated, &data); err != nil {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(data.String), &payload) != nil || payload == nil {
			continue
		}
		role := strOf(payload["role"])
		if role != "user" && role != "assistant" {
			continue // system prompts and tool rows carry no turn of their own
		}
		tsMillis, hasTS := int64(0), false
		if t, ok := payload["time"].(map[string]any); ok {
			tsMillis, hasTS = jsonMillis(t["created"])
		}
		if !hasTS && timeCreated.Valid {
			tsMillis, hasTS = timeCreated.Int64, true
		}
		next := opencodeParts(db, msgID)
		if len(next) == 0 {
			continue
		}
		if role == "user" {
			start("user", tsMillis, hasTS)
			blocks = next
			continue
		}
		if cur == nil || cur.Role != "assistant" {
			start("assistant", tsMillis, hasTS)
		}
		blocks = append(blocks, next...)
	}
	flush()
	return turns
}

// opencodeParts loads one message's parts as wire blocks.
func opencodeParts(db *sql.DB, messageID string) []map[string]any {
	rows, err := db.Query(
		`SELECT data FROM part WHERE message_id = ? ORDER BY time_created ASC, id ASC`, messageID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var data sql.NullString
		if err := rows.Scan(&data); err != nil {
			continue
		}
		var part map[string]any
		if json.Unmarshal([]byte(data.String), &part) != nil || part == nil {
			continue
		}
		switch strOf(part["type"]) {
		case "text":
			// Synthetic prose is the model's context, not the user's words.
			if synthetic, _ := part["synthetic"].(bool); synthetic {
				continue
			}
			if s := strings.TrimSpace(strOf(part["text"])); s != "" {
				out = append(out, map[string]any{"type": "text", "text": s})
			}
		case "reasoning":
			if s := strings.TrimSpace(strOf(part["text"])); s != "" {
				out = append(out, map[string]any{"type": "text", "text": s})
			}
		case "tool":
			name := strOf(part["tool"])
			if name == "" {
				name = "unknown"
			}
			blk := map[string]any{"type": "tool_use", "tool_name": name}
			if id := strOf(part["callID"]); id != "" {
				blk["tool_use_id"] = id
			}
			if state, ok := part["state"].(map[string]any); ok {
				if prev := inputPreview(state["input"]); prev != "" {
					blk["input_preview"] = prev
				}
			}
			out = append(out, blk)
		}
	}
	return out
}
