package parsers

// openclaw_summary.go enumerates OpenClaw sessions for the import picker.
//
// Layout and id resolution are the reader's (see openclaw.go):
// `<~/.openclaw/agents>/<agent>/sessions/<id>.jsonl` (plus `.jsonl.reset.*`
// archives), with a `sessions.json` index beside them that is not a session. The
// header carries the cwd; the id is in the header and, redundantly, the name.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListOpenClawConversations enumerates OpenClaw sessions, newest first.
func ListOpenClawConversations() []ConversationSummary {
	base := openclawAgentsDir()
	agents, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	out := []ConversationSummary{}
	for _, a := range agents {
		if !a.IsDir() {
			continue
		}
		sessions := filepath.Join(base, a.Name(), "sessions")
		ents, err := os.ReadDir(sessions)
		if err != nil {
			continue
		}
		for _, e := range ents {
			name := e.Name()
			if e.IsDir() || name == "sessions.json" || !strings.Contains(name, ".jsonl") {
				continue
			}
			path := filepath.Join(sessions, name)
			hdr, title := readSessionHeader(path, func(msg map[string]any) string {
				return openclawUserText(contentBlocks(msg["content"]))
			})
			id := hdr.ID
			if id == "" {
				// <id>.jsonl, or <id>.jsonl.reset.<ts> for a rotated archive.
				id = strings.TrimSuffix(name, ".jsonl")
				if i := strings.Index(id, ".jsonl.reset."); i >= 0 {
					id = id[:i]
				}
			}
			if id == "" {
				continue
			}
			sum := ConversationSummary{
				ID:        id,
				AgentType: "open_claw",
				Title:     title,
				StartedAt: hdr.Timestamp,
				EndedAt:   hdr.Timestamp,
			}
			if hdr.Cwd != "" {
				sum.FolderPath = hdr.Cwd
				sum.FolderName = baseNameOfPath(hdr.Cwd)
			}
			out = append(out, sum)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}
