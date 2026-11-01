package parsers

// grok_summary.go enumerates Grok sessions for the import picker.
//
// Grok writes a `summary.json` beside each session's `updates.jsonl`, and that
// sidecar already holds everything the picker needs — the working directory
// (`info.cwd`), a title, the model, the git branch and both timestamps — so this
// lister never opens the transcript:
//
//	<grok>/sessions/<encoded-cwd>/<session-id>/summary.json
//	                                            /updates.jsonl
//
// `session_kind` marks a `spawn_subagent` child ("subagent"); those belong to
// their parent and are skipped. A session whose sidecar is missing still gets
// listed (its cwd is simply unknown), because its history is importable.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ListGrokConversations enumerates Grok sessions, newest first.
func ListGrokConversations() []ConversationSummary {
	base := grokSessionsDir()
	groups, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	out := []ConversationSummary{}
	for _, g := range groups {
		if !g.IsDir() {
			continue
		}
		sessions, err := os.ReadDir(filepath.Join(base, g.Name()))
		if err != nil {
			continue
		}
		for _, s := range sessions {
			if !s.IsDir() {
				continue
			}
			dir := filepath.Join(base, g.Name(), s.Name())
			if st, err := os.Stat(filepath.Join(dir, "updates.jsonl")); err != nil || st.IsDir() {
				continue // no transcript, nothing to import
			}
			sum := grokSummaryOf(dir, s.Name())
			if sum.ID == "" {
				continue
			}
			out = append(out, sum)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

// grokSummaryOf reads one session's sidecar into a summary.
func grokSummaryOf(dir, id string) ConversationSummary {
	sum := ConversationSummary{ID: id, AgentType: "grok"}
	data, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		return sum
	}
	var v map[string]any
	if json.Unmarshal(data, &v) != nil || v == nil {
		return sum
	}
	if strOf(v["session_kind"]) == "subagent" {
		return ConversationSummary{} // a spawned child, not a conversation
	}
	info, _ := v["info"].(map[string]any)
	if cwd := strings.TrimSpace(strOf(info["cwd"])); cwd != "" {
		sum.FolderPath = cwd
		sum.FolderName = baseNameOfPath(cwd)
	}
	// The model-generated title beats the one-line summary.
	sum.Title = firstNonEmptyString(
		strings.TrimSpace(strOf(v["generated_title"])),
		strings.TrimSpace(strOf(v["session_summary"])),
	)
	sum.Model = strings.TrimSpace(strOf(v["current_model_id"]))
	sum.GitBranch = strings.TrimSpace(strOf(v["head_branch"]))
	sum.StartedAt = normalizeRFC3339(strOf(v["created_at"]))
	sum.EndedAt = firstNonEmptyString(normalizeRFC3339(strOf(v["updated_at"])), sum.StartedAt)
	return sum
}

// grokRFC3339 normalizes a sidecar timestamp to UTC RFC3339, leaving an
// unparseable value empty rather than passing it through.
func normalizeRFC3339(s string) string {
	if s = strings.TrimSpace(s); s == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
