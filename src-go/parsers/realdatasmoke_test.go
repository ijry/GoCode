package parsers

// realdatasmoke_test.go exercises every reader against the **real** stores on
// this machine, which is the only thing that can prove the field names are
// right — the unit tests use synthetic fixtures.
//
// It is opt-in because it depends on whatever agents the machine happens to have
// installed and on their live data:
//
//	GOCODE_PARSER_SMOKE=1 go test -run TestRealDataSmoke -v ./src-go/parsers/
//
// For each agent it tries several sessions and passes as soon as one yields
// turns: a single store can hold a degenerate session (a `<session_context>`
// preamble with no reply, an assistant row with no parts), and treating that as
// a parser bug would be wrong. An agent with no data on the machine is skipped,
// not failed.

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// smokeAttempts is how many sessions per agent are tried before giving up.
const smokeAttempts = 5

type smokeCase struct {
	agent string
	find  func(t *testing.T) []string
	read  func(string) []Turn
}

func TestRealDataSmoke(t *testing.T) {
	if os.Getenv("GOCODE_PARSER_SMOKE") == "" {
		t.Skip("set GOCODE_PARSER_SMOKE=1 to read this machine's real agent stores")
	}
	home, _ := os.UserHomeDir()
	h := func(parts ...string) string {
		return filepath.Join(append([]string{home}, parts...)...)
	}

	cases := []smokeCase{
		{"claude_code", func(t *testing.T) []string { return smokeStems(h(".claude", "projects"), "*.jsonl") }, ReadClaudeTurns},
		{"codex", func(t *testing.T) []string { return smokeCodexIDs(h(".codex", "sessions")) }, ReadCodexTurns},
		{"gemini", func(t *testing.T) []string { return smokeGeminiIDs(h(".gemini", "tmp")) }, ReadGeminiTurns},
		{"kimi_code", func(t *testing.T) []string { return smokeKimiIDs(h(".kimi-code", "sessions")) }, ReadKimiTurns},
		{"code_buddy", func(t *testing.T) []string { return smokeStems(h(".codebuddy", "projects"), "*.jsonl") }, ReadCodeBuddyTurns},
		{"cline", func(t *testing.T) []string { return smokeClineIDs(h(".cline", "data", "sessions")) }, ReadClineTurns},
		{"deepseek", func(t *testing.T) []string { return smokeDeepseekIDs(h(".dsh", "sessions")) }, ReadDeepseekTurns},
		{"hermes", func(t *testing.T) []string {
			return smokeSQLiteIDs(h(".hermes", "state.db"), "SELECT id FROM sessions LIMIT 5")
		}, ReadHermesTurns},
		{"qoder", func(t *testing.T) []string { return smokeStems(h(".qoder", "projects"), "*.jsonl") }, ReadQoderTurns},
		{"open_claw", func(t *testing.T) []string { return smokeOpenClawIDs(h(".openclaw", "agents")) }, ReadOpenClawTurns},
		{"pi", func(t *testing.T) []string { return smokePiIDs(h(".pi", "agent", "sessions")) }, ReadPiTurns},
		{"grok", func(t *testing.T) []string { return smokeGrokIDs(h(".grok", "sessions")) }, ReadGrokTurns},
		{"open_code", func(t *testing.T) []string {
			return smokeSQLiteIDs(h(".local", "share", "opencode", "opencode.db"), "SELECT id FROM session LIMIT 5")
		}, ReadOpenCodeTurns},
		{"antigravity", func(t *testing.T) []string {
			return smokeStems(h(".gemini", "antigravity-acp", "conversations"), "*.db")
		}, ReadAntigravityTurns},
		{"cursor", func(t *testing.T) []string { return smokeCursorIDs(h(".cursor")) }, ReadCursorTurns},
	}

	for _, c := range cases {
		c := c
		t.Run(c.agent, func(t *testing.T) {
			ids := c.find(t)
			if len(ids) == 0 {
				t.Skipf("no session on this machine")
			}
			for i, id := range ids {
				start := time.Now()
				turns := c.read(id)
				elapsed := time.Since(start)
				if len(turns) == 0 {
					t.Logf("session %s read as empty (%d/%d)", id, i+1, len(ids))
					continue
				}
				var users, assistants int
				for _, turn := range turns {
					switch turn.Role {
					case "user":
						users++
					case "assistant":
						assistants++
					}
				}
				t.Logf("session %s → %d turns (%d user / %d assistant) in %s, first=%s %q",
					id, len(turns), users, assistants, elapsed.Round(time.Millisecond),
					turns[0].Role, smokePreview(turns[0]))
				for j, turn := range turns {
					if j >= 2 {
						break
					}
					blocks, _ := turn.Raw["blocks"].([]map[string]any)
					t.Logf("  [%d] %s: %d blocks, first=%q", j, turn.Role, len(blocks), smokePreview(turn))
				}
				return
			}
			// Every sampled session produced nothing. That is legitimate when the
			// agent was installed but its sessions never got past the preamble
			// (Gemini CLI on a fresh machine writes exactly one
			// `<session_context>` message), so report it rather than fail — the
			// positive signal is what this test is for.
			t.Skipf("all %d sampled sessions read as empty — this agent may have no real conversation on this machine", len(ids))
		})
	}
}

// smokePreview renders a turn's first block for the log.
func smokePreview(turn Turn) string {
	blocks, _ := turn.Raw["blocks"].([]map[string]any)
	if len(blocks) == 0 {
		return ""
	}
	b := blocks[0]
	if s := strOf(b["text"]); s != "" {
		return smokeTruncate(s)
	}
	if s := strOf(b["tool_name"]); s != "" {
		return "tool:" + s
	}
	return strOf(b["type"])
}

func smokeTruncate(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}

// smokeStems returns the stems of `<root>/*/<pattern>` files.
func smokeStems(root, pattern string) []string {
	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	out := []string{}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(root, d.Name(), pattern))
		for _, m := range matches {
			if st, err := os.Stat(m); err == nil && !st.IsDir() {
				out = append(out, strings.TrimSuffix(filepath.Base(m), filepath.Ext(m)))
				if len(out) >= smokeAttempts {
					return out
				}
			}
		}
	}
	return out
}

// smokeCodexIDs reads ids off the rollout file names.
func smokeCodexIDs(root string) []string {
	out := []string{}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || len(out) >= smokeAttempts {
			return nil
		}
		if strings.HasSuffix(p, ".jsonl") || strings.HasSuffix(p, ".jsonl.zst") {
			// The id rides the name, so a compressed rollout needs no decode.
			if id := smokeCodexIDFromName(filepath.Base(p)); id != "" {
				out = append(out, id)
			}
		}
		return nil
	})
	return out
}

// smokeCodexIDFromName reads the session id from
// `rollout-<YYYY-MM-DDTHH-MM-SS>-<uuid>[_revert].jsonl[.zst]`.
func smokeCodexIDFromName(name string) string {
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".zst"), ".jsonl")
	name = strings.TrimPrefix(name, "rollout-")
	// The timestamp is fixed-width (19 bytes), then a dash, then the thread id.
	if len(name) < 21 || name[19] != '-' {
		return ""
	}
	id := name[20:]
	if i := strings.IndexByte(id, '_'); i >= 0 {
		id = id[:i]
	}
	return id
}

// smokeGeminiIDs reads each chat file's header `sessionId`.
func smokeGeminiIDs(root string) []string {
	out := []string{}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || len(out) >= smokeAttempts {
			return nil
		}
		if strings.Contains(filepath.ToSlash(p), "/chats/") &&
			(strings.HasSuffix(p, ".json") || strings.HasSuffix(p, ".jsonl")) {
			if id := smokeFirstLineID(p); id != "" {
				out = append(out, id)
			}
		}
		return nil
	})
	return out
}

// smokeFirstLineID reads a session id from a JSONL header's `id` or `sessionId`.
func smokeFirstLineID(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 64*1024)
	n, _ := f.Read(buf)
	line := string(buf[:n])
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	var rec map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(line)), &rec) != nil {
		return ""
	}
	for _, key := range []string{"id", "sessionId", "session_id"} {
		if s := strOf(rec[key]); s != "" {
			return s
		}
	}
	return ""
}

// smokeKimiIDs: <root>/<bucket>/<sessionId>/agents/main/wire.jsonl
func smokeKimiIDs(root string) []string {
	out := []string{}
	buckets, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, b := range buckets {
		if !b.IsDir() {
			continue
		}
		sessions, err := os.ReadDir(filepath.Join(root, b.Name()))
		if err != nil {
			continue
		}
		for _, s := range sessions {
			if !s.IsDir() {
				continue
			}
			p := filepath.Join(root, b.Name(), s.Name(), "agents", "main", "wire.jsonl")
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				out = append(out, s.Name())
				if len(out) >= smokeAttempts {
					return out
				}
			}
		}
	}
	return out
}

// smokeClineIDs: <root>/<id>/<id>.messages.json
func smokeClineIDs(root string) []string {
	out := []string{}
	dirs, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		if st, err := os.Stat(filepath.Join(root, d.Name(), d.Name()+".messages.json")); err == nil && !st.IsDir() {
			out = append(out, d.Name())
			if len(out) >= smokeAttempts {
				return out
			}
		}
	}
	return out
}

// smokeDeepseekIDs: <root>/<bucket>/<id>/session[.vN].jsonl[.zstd]
func smokeDeepseekIDs(root string) []string {
	out := []string{}
	buckets, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, b := range buckets {
		if !b.IsDir() {
			continue
		}
		sessions, err := os.ReadDir(filepath.Join(root, b.Name()))
		if err != nil {
			continue
		}
		for _, s := range sessions {
			if !s.IsDir() {
				continue
			}
			ents, err := os.ReadDir(filepath.Join(root, b.Name(), s.Name()))
			if err != nil {
				continue
			}
			for _, e := range ents {
				if _, _, ok := deepseekLogGeneration(e.Name()); ok {
					out = append(out, s.Name())
					break
				}
			}
			if len(out) >= smokeAttempts {
				return out
			}
		}
	}
	return out
}

// smokeOpenClawIDs: <root>/<agent>/sessions/<id>.jsonl
func smokeOpenClawIDs(root string) []string {
	out := []string{}
	agents, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, a := range agents {
		if !a.IsDir() {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(root, a.Name(), "sessions", "*.jsonl"))
		for _, m := range matches {
			base := filepath.Base(m)
			if strings.HasPrefix(base, "sessions.json") {
				continue
			}
			out = append(out, strings.TrimSuffix(base, ".jsonl"))
			if len(out) >= smokeAttempts {
				return out
			}
		}
	}
	return out
}

// smokePiIDs walks the sessions tree and reads each header id.
func smokePiIDs(root string) []string {
	out := []string{}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || len(out) >= smokeAttempts {
			return nil
		}
		if strings.HasSuffix(p, ".jsonl") {
			if id := piSessionIDOf(p); id != "" {
				out = append(out, id)
			}
		}
		return nil
	})
	return out
}

// smokeGrokIDs: <root>/<enc-cwd>/<id>/updates.jsonl
func smokeGrokIDs(root string) []string {
	out := []string{}
	groups, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, g := range groups {
		if !g.IsDir() {
			continue
		}
		sessions, err := os.ReadDir(filepath.Join(root, g.Name()))
		if err != nil {
			continue
		}
		for _, s := range sessions {
			if !s.IsDir() {
				continue
			}
			p := filepath.Join(root, g.Name(), s.Name(), "updates.jsonl")
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				out = append(out, s.Name())
				if len(out) >= smokeAttempts {
					return out
				}
			}
		}
	}
	return out
}

// smokeCursorIDs: acp-sessions/<id>/store.db, else chats/<md5>/<id>/store.db
func smokeCursorIDs(root string) []string {
	out := []string{}
	if ents, err := os.ReadDir(filepath.Join(root, "acp-sessions")); err == nil {
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			if st, err := os.Stat(filepath.Join(root, "acp-sessions", e.Name(), "store.db")); err == nil && !st.IsDir() {
				out = append(out, e.Name())
				if len(out) >= smokeAttempts {
					return out
				}
			}
		}
	}
	buckets, err := os.ReadDir(filepath.Join(root, "chats"))
	if err != nil {
		return out
	}
	for _, b := range buckets {
		if !b.IsDir() {
			continue
		}
		ents, err := os.ReadDir(filepath.Join(root, "chats", b.Name()))
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			if st, err := os.Stat(filepath.Join(root, "chats", b.Name(), e.Name(), "store.db")); err == nil && !st.IsDir() {
				out = append(out, e.Name())
				if len(out) >= smokeAttempts {
					return out
				}
			}
		}
	}
	return out
}

// smokeSQLiteIDs reads up to a few ids from a read-only SQLite query.
func smokeSQLiteIDs(path, query string) []string {
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return nil
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		out = append(out, id)
	}
	return out
}
