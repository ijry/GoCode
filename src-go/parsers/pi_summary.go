package parsers

// pi_summary.go enumerates pi sessions for the import picker.
//
// Layout and id resolution are the reader's (see pi.go): the sessions directory
// resolves from PI_CODING_AGENT_SESSION_DIR, then the agent dir's settings.json,
// then `<agent dir>/sessions`, and a session file is
// `--<dashed-cwd>--/<timestamp>_<uuid>.jsonl` (flat when a custom sessionDir is
// set). The id is in the file's header, not its name.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListPiConversations enumerates pi sessions, newest first.
func ListPiConversations() []ConversationSummary {
	base := piSessionsDir()
	out := []ConversationSummary{}
	_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		hdr, title := readSessionHeader(path, func(msg map[string]any) string {
			return piUserText(msg["content"])
		})
		if hdr.ID == "" {
			return nil
		}
		sum := ConversationSummary{
			ID:        hdr.ID,
			AgentType: "pi",
			Title:     title,
			StartedAt: hdr.Timestamp,
			EndedAt:   hdr.Timestamp,
		}
		if hdr.Cwd != "" {
			sum.FolderPath = hdr.Cwd
			sum.FolderName = baseNameOfPath(hdr.Cwd)
		}
		out = append(out, sum)
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}
