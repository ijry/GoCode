package parsers

// cline_summary.go enumerates cline sessions for the import picker.
//
// cline writes one directory per root session under
// `<CLINE_DIR or ~/.cline/data>/sessions/<id>/`, and the manifest
// `<id>/<id>.json` mirrors the columns of its own `sessions` table: cwd,
// workspace_root, prompt, model, started_at/ended_at and metadata.title. Reading
// manifests therefore yields exactly the root sessions — spawned sub-agents get a
// row in `db/sessions.db` and a sibling messages file, never a manifest of their
// own — and no transcript has to be parsed.
//
// The pre-3.x layout (`state/taskHistory.json` + `tasks/<id>/`) is not walked
// here; it is only a fallback for history written by an older cline.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListClineConversations enumerates cline root sessions, newest first.
func ListClineConversations() []ConversationSummary {
	base := filepath.Join(clineDataDir(), "sessions")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	out := make([]ConversationSummary, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || !isSafeSessionID(e.Name()) {
			continue
		}
		id := e.Name()
		if s, ok := clineSummaryOf(filepath.Join(base, id, id+".json"), id); ok {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

// clineSummaryOf reads one manifest. A manifest whose `session_id` disagrees with
// its directory is still usable: the directory name is authoritative (the same
// rule the reader applies).
func clineSummaryOf(path, id string) (ConversationSummary, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ConversationSummary{}, false
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return ConversationSummary{}, false
	}
	sum := ConversationSummary{
		ID:        id,
		AgentType: "cline",
		Model:     strOf(m["model"]),
		StartedAt: clineRFC3339(strOf(m["started_at"])),
		EndedAt:   clineRFC3339(strOf(m["ended_at"])),
	}
	// cwd is the session's working directory; workspace_root is the folder the
	// editor had open, which is what a project group should key on when cwd is
	// missing (a task started from the home directory, say).
	sum.FolderPath = firstNonEmptyString(strOf(m["cwd"]), strOf(m["workspace_root"]))
	sum.FolderName = folderNameFromPath(sum.FolderPath)

	if meta, ok := m["metadata"].(map[string]any); ok {
		sum.Title = strings.TrimSpace(strOf(meta["title"]))
		if git, ok := meta["git"].(map[string]any); ok {
			sum.GitBranch = strOf(git["branch"])
		}
	}
	if sum.Title == "" {
		// The manifest keeps the task's first prompt; it makes a better row than
		// an empty title.
		sum.Title = summaryTitle(strOf(m["prompt"]))
	}
	return sum, true
}

// clineRFC3339 normalizes a manifest timestamp. cline writes RFC3339 already, but
// a malformed value must not leak into the conversation row.
func clineRFC3339(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	return normalizeRFC3339(v)
}
