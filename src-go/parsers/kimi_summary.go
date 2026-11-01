package parsers

// kimi_summary.go enumerates Kimi Code sessions for the import picker.
//
// Kimi Code keeps an index beside its session tree, so the scan reads one small
// file plus one `state.json` per session and never opens a transcript:
//
//	$KIMI_CODE_HOME/session_index.jsonl   {sessionId, sessionDir, workDir}
//	$KIMI_CODE_HOME/sessions/<key>/<id>/state.json   {title, createdAt, updatedAt}
//
// The index is the authority for the working directory (the bucket directory
// name is a hash, not a path). A session missing from the index is not listed:
// without its workDir it could not be grouped, and the index is what Kimi itself
// treats as the session list.

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListKimiConversations enumerates Kimi Code sessions, newest first.
func ListKimiConversations() []ConversationSummary {
	home := strings.TrimSpace(os.Getenv("KIMI_CODE_HOME"))
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, ".kimi-code")
	}
	f, err := os.Open(filepath.Join(home, "session_index.jsonl"))
	if err != nil {
		return nil
	}
	defer f.Close()
	out := []ConversationSummary{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil || rec == nil {
			continue
		}
		id := strOf(rec["sessionId"])
		if id == "" {
			continue
		}
		sum := ConversationSummary{ID: id, AgentType: "kimi_code"}
		if wd := strings.TrimSpace(strOf(rec["workDir"])); wd != "" {
			sum.FolderPath = wd
			sum.FolderName = baseNameOfPath(wd)
		}
		// state.json sits in the session directory the index names.
		dir := strings.TrimSpace(strOf(rec["sessionDir"]))
		if dir != "" && !filepath.IsAbs(dir) {
			dir = filepath.Join(home, dir)
		}
		if dir != "" {
			kimiStateInto(&sum, filepath.Join(dir, "state.json"))
		}
		out = append(out, sum)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

// kimiStateInto fills the title and times from a session's state.json.
func kimiStateInto(sum *ConversationSummary, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var v map[string]any
	if json.Unmarshal(data, &v) != nil || v == nil {
		return
	}
	sum.Title = strings.TrimSpace(strOf(v["title"]))
	sum.StartedAt = strings.TrimSpace(strOf(v["createdAt"]))
	sum.EndedAt = firstNonEmptyString(strings.TrimSpace(strOf(v["updatedAt"])), sum.StartedAt)
}
