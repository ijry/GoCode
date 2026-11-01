package parsers

// opencode_summary.go enumerates OpenCode sessions for the import picker.
//
// Unlike the file-based agents this needs no transcript parsing at all: the
// `session` table already holds the id, the working directory and the title, so
// one query answers the whole scan. Sub-agent children (`parent_id` set) are
// skipped — the picker imports top-level conversations, and a child's history
// belongs to its parent.

import (
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// ListOpenCodeConversations enumerates OpenCode's top-level sessions, newest
// first.
func ListOpenCodeConversations() []ConversationSummary {
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
		`SELECT id, COALESCE(directory,''), COALESCE(title,''), time_created, time_updated
		 FROM session
		 WHERE parent_id IS NULL OR parent_id = ''
		 ORDER BY time_created DESC`)
	if err != nil {
		return nil // an older schema without these columns is an honest empty
	}
	defer rows.Close()
	out := []ConversationSummary{}
	for rows.Next() {
		var (
			id, dir, title string
			created        sql.NullInt64
			updated        sql.NullInt64
		)
		if err := rows.Scan(&id, &dir, &title, &created, &updated); err != nil || id == "" {
			continue
		}
		if title == "" || strings.HasPrefix(title, "New session") {
			// OpenCode names a fresh session "New session - <timestamp>"; the
			// first prompt makes a far better row than that.
			if prompt := opencodeFirstPrompt(db, id); prompt != "" {
				title = summaryTitle(prompt)
			}
		}
		sum := ConversationSummary{ID: id, AgentType: "open_code", Title: title}
		if dir != "" {
			sum.FolderPath = dir
			sum.FolderName = baseNameOfPath(dir)
		}
		if created.Valid {
			sum.StartedAt = opencodeMillis(created.Int64)
		}
		if updated.Valid {
			sum.EndedAt = opencodeMillis(updated.Int64)
		} else {
			sum.EndedAt = sum.StartedAt
		}
		out = append(out, sum)
	}
	return out
}

// opencodeFirstPrompt reads the first real text part of a session: the model's
// injected prose is flagged `synthetic` and skipped.
func opencodeFirstPrompt(db *sql.DB, sessionID string) string {
	rows, err := db.Query(
		`SELECT data FROM part
		 WHERE message_id IN (SELECT id FROM message WHERE session_id = ?)
		 ORDER BY time_created ASC, id ASC
		 LIMIT 40`, sessionID)
	if err != nil {
		return ""
	}
	defer rows.Close()
	for rows.Next() {
		var data sql.NullString
		if err := rows.Scan(&data); err != nil {
			continue
		}
		var part map[string]any
		if json.Unmarshal([]byte(data.String), &part) != nil || part == nil {
			continue
		}
		if strOf(part["type"]) != "text" {
			continue
		}
		if synthetic, _ := part["synthetic"].(bool); synthetic {
			continue
		}
		if text := strings.TrimSpace(strOf(part["text"])); text != "" {
			return text
		}
	}
	return ""
}

// opencodeMillis renders an epoch-millisecond column as RFC3339 UTC, the shape
// the conversation row stores.
func opencodeMillis(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}
