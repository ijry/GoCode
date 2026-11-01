package parsers

// codex_summary.go enumerates Codex rollouts for the import picker.
//
// The import scan needs one summary per local session — id, cwd, title, times —
// and a machine can hold thousands of rollouts (1273 here), so this reads only
// each file's HEAD: the first `session_meta` line carries the id, cwd and
// timestamp, and the lines after it supply the first user message as the title.
// MessageCount stays 0 because an exact count would mean parsing every rollout
// in full; the picker shows what it knows.
//
// Importing only writes a conversation row (agent_type + external_id), so the
// history itself comes from ReadCodexTurns on demand.

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// codexHeadLimit bounds how much of a rollout is read for its summary. The
// session_meta line alone runs 18-22 KB (it embeds the base instructions) and the
// first user message lands at 8-35 KB, so 96 KB covers the title for nearly every
// session while keeping a machine with 1300 rollouts fast. The scan also stops as
// soon as both the id and the title are known.
const codexHeadLimit = 192 * 1024

// ListCodexConversations enumerates Codex rollouts, newest first.
func ListCodexConversations() []ConversationSummary {
	base := filepath.Join(codexConfigDir(), "sessions")
	paths := []string{}
	_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, "rollout-") &&
			(strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".jsonl.zst")) {
			paths = append(paths, path)
		}
		return nil
	})
	// One worker per CPU: every file is an independent head read, and decompressing
	// a thousand rollouts serially took ~25s.
	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8
	}
	if workers < 1 {
		workers = 1
	}
	results := make([]ConversationSummary, len(paths))
	ok := make([]bool, len(paths))
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				results[i], ok[i] = codexSummaryOf(paths[i])
			}
		}()
	}
	for i := range paths {
		next <- i
	}
	close(next)
	wg.Wait()

	out := make([]ConversationSummary, 0, len(paths))
	for i := range results {
		if ok[i] {
			out = append(out, results[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

// codexSummaryOf reads one rollout's head into a summary.
func codexSummaryOf(path string) (ConversationSummary, bool) {
	f, err := os.Open(path)
	if err != nil {
		return ConversationSummary{}, false
	}
	defer f.Close()
	var r io.Reader = io.LimitReader(f, codexHeadLimit)
	if strings.HasSuffix(path, ".zst") {
		zr, err := zstd.NewReader(f)
		if err != nil {
			return ConversationSummary{}, false
		}
		defer zr.Close()
		r = io.LimitReader(zr, codexHeadLimit)
	}

	var sum ConversationSummary
	sum.AgentType = "codex"
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		switch strOf(rec["type"]) {
		case "session_meta":
			payload, _ := rec["payload"].(map[string]any)
			if payload == nil {
				continue
			}
			sum.ID = firstNonEmptyString(strOf(payload["id"]), strOf(payload["session_id"]))
			if cwd := strOf(payload["cwd"]); cwd != "" {
				sum.FolderPath = cwd
				sum.FolderName = baseNameOfPath(cwd)
			}
			sum.StartedAt = firstNonEmptyString(strOf(payload["timestamp"]), strOf(rec["timestamp"]))
		case "response_item":
			if sum.ID != "" && sum.Title != "" {
				return sum, true // nothing left to learn from the head
			}
			payload, _ := rec["payload"].(map[string]any)
			if payload == nil || strOf(payload["type"]) != "message" || strOf(payload["role"]) != "user" {
				continue
			}
			text := codexMessageText(payload["content"])
			if text == "" || codexInjectedContext(text) {
				continue // AGENTS.md / environment_context are not the prompt
			}
			if sum.Title == "" {
				sum.Title = summaryTitle(text)
			}
		}
	}
	if sum.ID == "" {
		// A rollout without a readable header still has its id in the file name.
		sum.ID = codexIDFromFileName(filepath.Base(path))
	}
	if sum.ID == "" {
		return ConversationSummary{}, false
	}
	if sum.EndedAt == "" {
		sum.EndedAt = sum.StartedAt
	}
	return sum, true
}

// codexIDFromFileName reads the thread id out of
// `rollout-<YYYY-MM-DDTHH-MM-SS>-<uuid>[_revert].jsonl[.zst]`.
func codexIDFromFileName(name string) string {
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".zst"), ".jsonl")
	name = strings.TrimPrefix(name, "rollout-")
	if len(name) < 21 || name[19] != '-' {
		return ""
	}
	id := name[20:]
	if i := strings.IndexByte(id, '_'); i >= 0 {
		id = id[:i]
	}
	return id
}

// codexMessageText joins a codex message's text parts.
func codexMessageText(content any) string {
	parts, _ := content.([]any)
	out := make([]string, 0, len(parts))
	for _, raw := range parts {
		part, _ := raw.(map[string]any)
		if part == nil {
			continue
		}
		switch strOf(part["type"]) {
		case "input_text", "text", "output_text":
			if s := strings.TrimSpace(strOf(part["text"])); s != "" {
				out = append(out, s)
			}
		}
	}
	return strings.Join(out, "\n")
}

// summaryTitle trims a prompt into a one-line title: first line, capped.
func summaryTitle(text string) string {
	line := strings.TrimSpace(text)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if r := []rune(line); len(r) > 80 {
		return strings.TrimSpace(string(r[:80])) + "…"
	}
	return line
}

// baseNameOfPath is the last path element, whichever separator the agent used.
// A trailing separator is dropped first: a cwd recorded as `D://repo//` would
// otherwise yield the whole path as its "name".
func baseNameOfPath(p string) string {
	p = strings.TrimRight(p, "/\\")
	if p == "" {
		return ""
	}
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 && i+1 < len(p) {
		return p[i+1:]
	}
	return p
}
