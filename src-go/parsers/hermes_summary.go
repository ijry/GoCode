package parsers

// hermes_summary.go enumerates Hermes Agent sessions for the import picker.
//
// Hermes keeps everything in `<HERMES_HOME or ~/.hermes>/state.db`, and the
// `sessions` table already holds what the picker needs, so one query answers the
// scan:
//
//	sessions(id, source, model, model_config, parent_session_id, started_at,
//	         ended_at, cwd, title, archived, …)
//
// Two details are load-bearing, both mirrored from the reader and upstream:
//   - the `cwd` COLUMN is NULL for ACP sessions — the working directory then
//     lives in `model_config` JSON, which `json_valid` guards so a non-JSON blob
//     cannot abort the whole SELECT;
//   - `started_at` / `ended_at` are Unix epoch **seconds** as a REAL.
//
// Archived sessions are hidden, sessions with no real messages are skipped, and
// a `parent_session_id` child belongs to its parent rather than being importable
// on its own.

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"
)

// ListHermesConversations enumerates Hermes sessions, newest first.
func ListHermesConversations() []ConversationSummary {
	path := filepath.Join(hermesHomeDir(), "state.db")
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return nil
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query(`
		SELECT s.id,
		       COALESCE(NULLIF(s.cwd, ''),
		                CASE WHEN json_valid(s.model_config)
		                     THEN json_extract(s.model_config, '$.cwd') END),
		       COALESCE(s.title, ''),
		       COALESCE(s.model, ''),
		       s.started_at,
		       s.ended_at,
		       COALESCE(s.parent_session_id, ''),
		       (SELECT COUNT(*) FROM messages m
		         WHERE m.session_id = s.id AND m.active = 1 AND m.role <> 'system')
		FROM sessions s
		WHERE COALESCE(s.archived, 0) = 0
		ORDER BY s.started_at DESC`)
	if err != nil {
		return nil // an older schema is an honest empty, not a failure
	}
	defer rows.Close()
	out := []ConversationSummary{}
	for rows.Next() {
		var (
			id, title, model, parent string
			cwd                      sql.NullString
			started, ended           sql.NullFloat64
			messageCount             int64
		)
		if err := rows.Scan(&id, &cwd, &title, &model, &started, &ended, &parent, &messageCount); err != nil {
			continue
		}
		if id == "" || parent != "" || messageCount == 0 {
			continue
		}
		sum := ConversationSummary{
			ID:           id,
			AgentType:    "hermes",
			Title:        title,
			Model:        model,
			MessageCount: int(messageCount),
		}
		if cwd.Valid && cwd.String != "" {
			sum.FolderPath = cwd.String
			sum.FolderName = baseNameOfPath(cwd.String)
		}
		if started.Valid {
			sum.StartedAt = hermesSeconds(started.Float64)
		}
		if ended.Valid {
			sum.EndedAt = hermesSeconds(ended.Float64)
		} else {
			sum.EndedAt = sum.StartedAt
		}
		out = append(out, sum)
	}
	return out
}

// hermesSeconds renders an epoch-seconds column as RFC3339 UTC.
func hermesSeconds(sec float64) string {
	if sec <= 0 {
		return ""
	}
	return time.Unix(int64(sec), 0).UTC().Format(time.RFC3339)
}
