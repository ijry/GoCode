package parsers

// qoder.go reads Qoder (Alibaba) transcripts.
//
// Qoder borrows Claude Code's storage wholesale: one JSONL per session under
// `<QODER_CONFIG_DIR or <homeRoot>/.qoder>/projects/<encoded-cwd>/<sessionId>.jsonl`,
// with the same per-line record shape (`type` user|assistant, `message.content`
// of typed blocks, RFC3339 `timestamp`). Upstream's qoder parser literally
// reuses claude's record extractors, so this reader reuses parseClaudeFile and
// only differs in where it looks.
//
// homeRoot is QODER_CLI_HOME, then GEMINI_CLI_HOME (qodercli carries that
// ancestry), then the user's home.
//
// Mirrors upstream parsers/qoder.rs, minus usage, title capture and the
// slash-command display rewrite.

import (
	"os"
	"path/filepath"
	"strings"
)

// qoderProjectsDir resolves the `projects` directory Qoder keeps sessions in.
func qoderProjectsDir() string {
	if v := strings.TrimSpace(os.Getenv("QODER_CONFIG_DIR")); v != "" {
		return filepath.Join(v, "projects")
	}
	home := ""
	if v := strings.TrimSpace(os.Getenv("QODER_CLI_HOME")); v != "" {
		home = v
	} else if v := strings.TrimSpace(os.Getenv("GEMINI_CLI_HOME")); v != "" {
		home = v
	} else {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, ".qoder", "projects")
}

// ReadQoderTurns reads the Qoder transcript for sessionID (the conversation's
// external_id): `<projects>/<encoded-cwd>/<sessionID>.jsonl`.
func ReadQoderTurns(sessionID string) []Turn {
	if !isSafeSessionID(sessionID) {
		return nil
	}
	base := qoderProjectsDir()
	ents, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(base, e.Name(), sessionID+".jsonl")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return parseClaudeFile(p)
		}
	}
	return nil
}
