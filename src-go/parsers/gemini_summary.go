package parsers

// gemini_summary.go enumerates Gemini CLI sessions for the import picker.
//
// Layout: `<gemini>/tmp/<project>/chats/*.jsonl`, where `<project>` is a slug for
// the working directory and the directory's `.project_root` file holds that
// directory's absolute path — the only place the cwd survives, since the chat
// header carries a `projectHash`, not a path.
//
// The header line gives `sessionId`, `startTime`, `lastUpdated` and `kind`; the
// title comes from the first user message that is not the `<session_context>`
// preamble Gemini injects.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListGeminiConversations enumerates Gemini CLI sessions, newest first.
func ListGeminiConversations() []ConversationSummary {
	root := filepath.Join(geminiBaseDir(), "tmp")
	projects, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	out := []ConversationSummary{}
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		dir := filepath.Join(root, p.Name())
		cwd := geminiProjectRoot(dir)
		chats, err := os.ReadDir(filepath.Join(dir, "chats"))
		if err != nil {
			continue
		}
		for _, chat := range chats {
			if chat.IsDir() {
				continue
			}
			name := chat.Name()
			if !strings.HasPrefix(name, "session-") ||
				!(strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".json")) {
				continue
			}
			if s, ok := geminiSummaryOf(filepath.Join(dir, "chats", name), cwd); ok {
				out = append(out, s)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

// geminiProjectRoot reads the `.project_root` sidecar; empty when absent.
func geminiProjectRoot(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, ".project_root"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// geminiSummaryOf builds one summary from a chat file.
func geminiSummaryOf(path, cwd string) (ConversationSummary, bool) {
	msgs, meta := readGeminiChat(path)
	if strOf(meta["kind"]) != "" && strOf(meta["kind"]) != "main" {
		return ConversationSummary{}, false // sub-agent runs are not top-level chats
	}
	sum := ConversationSummary{
		ID:        strOf(meta["sessionId"]),
		AgentType: "gemini",
		StartedAt: strOf(meta["startTime"]),
		EndedAt:   firstNonEmptyString(strOf(meta["lastUpdated"]), strOf(meta["startTime"])),
	}
	if sum.ID == "" {
		return ConversationSummary{}, false
	}
	if cwd != "" {
		sum.FolderPath = cwd
		sum.FolderName = baseNameOfPath(cwd)
	}
	for _, m := range msgs {
		if strOf(m["type"]) != "user" {
			continue
		}
		blocks := geminiBlocks(m, "user")
		if len(blocks) == 0 {
			continue
		}
		text, _ := blocks[0]["text"].(string)
		if strings.HasPrefix(strings.TrimSpace(text), "<session_context>") {
			continue
		}
		sum.Title = summaryTitle(text)
		break
	}
	return sum, true
}
