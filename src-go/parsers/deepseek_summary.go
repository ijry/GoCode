package parsers

// deepseek_summary.go enumerates DeepSeek Harness (deepseek-acp) sessions for the
// import picker.
//
// Layout — one directory per session, bucketed by the munged cwd:
//
//	$DEEPSEEK_ACP_SESSIONS_ROOT | $DSH_HOME/sessions | ~/.dsh/sessions
//	└── <munged cwd>/
//	    └── session-<uuid>/
//	        ├── session.jsonl.zstd      generation 0
//	        └── session.v3.jsonl.zstd   generation 3 (the live one)
//
// The bucket name is lossy, but the log's own first line is the session header —
// `{type:"session", id, createdAt, cwd, delegationDepth}` — so nothing has to be
// reverse-engineered from it.
//
// The title comes from `session/title` (the title service re-emits it as it
// improves, so the LAST one wins), falling back to the first real user prompt.
// `user/message` rows carry injected runtime context too; only `data.source.kind
// == "user"` is a prompt.

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// deepseekSummaryReadLimit caps how much decompressed log one summary reads. The
// header, the title and the first prompt all land in the first few KB; the cap
// only exists so a pathological multi-hundred-MB log cannot stall the picker, and
// a truncated read simply reports the last event it saw as the session's end.
const deepseekSummaryReadLimit = 8 << 20

// ListDeepseekConversations enumerates DeepSeek sessions, newest first.
func ListDeepseekConversations() []ConversationSummary {
	root := deepseekSessionsRoot()
	buckets, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	out := []ConversationSummary{}
	for _, b := range buckets {
		if !b.IsDir() {
			continue
		}
		sessions, serr := os.ReadDir(filepath.Join(root, b.Name()))
		if serr != nil {
			continue
		}
		for _, sd := range sessions {
			if !sd.IsDir() {
				continue
			}
			if s, ok := deepseekSummaryOf(filepath.Join(root, b.Name(), sd.Name()), sd.Name()); ok {
				out = append(out, s)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

// deepseekSummaryOf reads one session's live log head.
func deepseekSummaryOf(dir, dirID string) (ConversationSummary, bool) {
	logPath := deepseekBestLogIn(dir)
	if logPath == "" {
		return ConversationSummary{}, false
	}
	f, err := os.Open(logPath)
	if err != nil {
		return ConversationSummary{}, false
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(logPath, ".zstd") {
		zr, zerr := zstd.NewReader(f)
		if zerr != nil {
			return ConversationSummary{}, false
		}
		defer zr.Close()
		r = zr
	}
	r = io.LimitReader(r, deepseekSummaryReadLimit)

	var id, cwd, title, firstUser, model string
	var createdMillis, lastMillis int64
	count := 0

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var v map[string]any
		if json.Unmarshal([]byte(line), &v) != nil || v == nil {
			continue
		}
		recType := strOf(v["type"])
		// The header keeps its fields at the top level; every later event puts
		// its payload under `data`.
		if recType == "session" {
			id = firstNonEmptyString(id, strOf(v["id"]))
			cwd = firstNonEmptyString(cwd, strings.TrimSpace(strOf(v["cwd"])))
			if ms, ok := jsonMillis(v["createdAt"]); ok && createdMillis == 0 {
				createdMillis = ms
			}
			continue
		}
		if ms, ok := jsonMillis(v["time"]); ok {
			lastMillis = ms
		}
		data, _ := v["data"].(map[string]any)
		switch recType {
		case "session/title":
			if t := strings.TrimSpace(strOf(data["title"])); t != "" {
				title = t // last one wins: the service re-emits as it improves
			}
		case "user/message":
			src, _ := data["source"].(map[string]any)
			// "plugin" records are injected runtime context and compaction
			// summaries, not prompts.
			if src == nil || strOf(src["kind"]) != "user" {
				continue
			}
			count++
			if firstUser == "" {
				if text := strings.TrimSpace(deepseekTextParts(data["content"])); text != "" {
					firstUser = text
				}
			}
		case "model/selection":
			// The harness records the pick itself, before any request.
			model = firstNonEmptyString(model, strOf(data["model"]))
		case "request/header":
			// {header:{config:{provider, model}, tools:[…]}}
			if model == "" {
				if header, ok := data["header"].(map[string]any); ok {
					if cfg, ok := header["config"].(map[string]any); ok {
						model = strOf(cfg["model"])
					}
				}
			}
		}
	}
	if id == "" {
		id = dirID
	}
	if id == "" || (createdMillis == 0 && lastMillis == 0) {
		return ConversationSummary{}, false
	}
	started := createdMillis
	if started == 0 {
		started = lastMillis
	}
	return ConversationSummary{
		ID:           id,
		AgentType:    "deepseek",
		FolderPath:   cwd,
		FolderName:   folderNameFromPath(cwd),
		Title:        summaryTitle(firstNonEmptyString(title, firstUser)),
		StartedAt:    opencodeMillis(started),
		EndedAt:      opencodeMillis(lastMillis),
		MessageCount: count,
		Model:        model,
	}, true
}
