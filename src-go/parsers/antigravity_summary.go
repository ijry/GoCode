package parsers

// antigravity_summary.go enumerates Antigravity conversations for the import
// picker.
//
// One `.db` per conversation under
// `<GEMINI_HOME or ~/.gemini>/antigravity-acp/conversations/<session id>.db`,
// with a `<session id>.meta` JSON sidecar beside it. The sidecar carries `cwd`
// (and `mode_id`), but deliberately NO title: the server synthesizes
// "Session <first 8 chars>" for its own listing, which is not worth mirroring, so
// the title comes from the first user prompt in the steps.
//
// A brand new session's DB exists before its schema does, so "no such table:
// steps" is a normal state and reads as an empty conversation — not an error.
//
// Mirrors the picker-relevant subset of upstream parsers/antigravity.rs: no
// per-step token accounting, no tool-result pairing.

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListAntigravityConversations enumerates Antigravity sessions, newest first.
func ListAntigravityConversations() []ConversationSummary {
	base := antigravityConversationsDir()
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	out := []ConversationSummary{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".db")
		if !isSafeSessionID(id) {
			continue
		}
		if s, ok := antigravitySummaryOf(filepath.Join(base, e.Name()), id); ok {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

// antigravitySummaryOf reads one session DB. It decodes only each step's
// metadata (clock, model) and the first user prompt — never the assistant text or
// tool calls — so listing a large session stays cheap.
func antigravitySummaryOf(path, id string) (ConversationSummary, bool) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return ConversationSummary{}, false
	}
	defer db.Close()

	rows, err := db.Query(`SELECT step_payload FROM steps ORDER BY idx ASC`)
	if err != nil {
		// A session created but not yet written has no `steps` table.
		return ConversationSummary{}, false
	}
	defer rows.Close()

	var title, model string
	var firstMillis, lastMillis int64
	count := 0
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil || len(payload) == 0 {
			continue
		}
		count++
		fs := protoFields(payload)
		if meta := protoBytesAt(fs, 5); meta != nil {
			m := protoFields(meta)
			if ts := protoBytesAt(m, 1); ts != nil {
				if secs := protoIntAt(protoFields(ts), 1); secs > 0 {
					if firstMillis == 0 {
						firstMillis = secs * 1000
					}
					lastMillis = secs * 1000
				}
			}
			// model_info {8 model_name, 20 display_name}; the newest wins, as
			// upstream resolves it.
			if mi := protoBytesAt(m, 24); mi != nil {
				mfs := protoFields(mi)
				if name := firstNonEmptyString(strings.TrimSpace(protoStringAt(mfs, 20)),
					strings.TrimSpace(protoStringAt(mfs, 8))); name != "" {
					model = name
				}
			}
		}
		if title == "" {
			if ui := protoBytesAt(fs, 19); ui != nil {
				title = strings.TrimSpace(antigravityUserText(ui))
			}
		}
	}
	if count == 0 {
		return ConversationSummary{}, false
	}

	// cwd: the sidecar only. No fallback to the Gemini home — an unknown cwd must
	// read as unknown rather than as a folder the user never worked in.
	folderPath := antigravitySidecarCwd(filepath.Join(filepath.Dir(path), id+".meta"))
	return ConversationSummary{
		ID:           id,
		AgentType:    "antigravity",
		FolderPath:   folderPath,
		FolderName:   folderNameFromPath(folderPath),
		Title:        summaryTitle(title),
		StartedAt:    opencodeMillis(firstMillis),
		EndedAt:      opencodeMillis(lastMillis),
		MessageCount: count,
		Model:        model,
	}, true
}

// antigravityUserText reads CortexStepUserInput: `query`, `user_response`, then
// the structured `items` list editors send.
func antigravityUserText(raw []byte) string {
	u := protoFields(raw)
	if s := firstNonEmptyString(protoStringAt(u, 1), protoStringAt(u, 2)); s != "" {
		return s
	}
	parts := []string{}
	for _, item := range protoAllBytesAt(u, 3) {
		if s := strings.TrimSpace(protoStringAt(protoFields(item), 1)); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

// antigravitySidecarCwd reads `cwd` from the `.meta` sidecar.
func antigravitySidecarCwd(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var m struct {
		Cwd string `json:"cwd"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	return strings.TrimSpace(m.Cwd)
}
