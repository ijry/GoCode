package parsers

// codebuddy_summary.go enumerates CodeBuddy sessions for the import picker.
//
// CodeBuddy borrows Claude Code's directory layout —
// `<CODEBUDDY_CONFIG_DIR or ~/.codebuddy>/projects/<encoded-cwd>/<sessionId>.jsonl`
// — but not its line schema: records are OpenAI Agents SDK "items" typed
// `message` / `reasoning` / `function_call` / `function_call_result`, with
// `timestamp` in epoch milliseconds. Title records (`custom-title` / `ai-title` /
// `topic`) are appended as the session evolves, so the LAST value of each kind
// wins — the same resolution CodeBuddy applies itself.
//
// Cost matters here more than anywhere else in this package: these transcripts are
// line-appended and grow to hundreds of MB (a long session on this machine is
// 146 MB). So the walk is two-phase — JSON-decode only the head, then count the
// rest by byte markers — and runs one worker per file. Decoding every line of a
// 600 MB corpus cost ~9s; this reads the same corpus in well under a second.
//
// Mirrors the picker-relevant subset of upstream parsers/codebuddy.rs.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// codebuddyHeadLines bounds how many lines are JSON-decoded. Every field the
// picker wants (cwd, session id, model, title, first prompt) appears within the
// first few records of a real session.
const codebuddyHeadLines = 400

// Byte markers for the tail pass. CodeBuddy writes `type` then `role` first in
// every message record, which makes these exact enough to count without decoding.
var (
	cbUserMarker      = []byte(`"type":"message","role":"user"`)
	cbAssistantMarker = []byte(`"type":"message","role":"assistant"`)
	cbInjectedMarker  = []byte(`"isMeta":true`)
	cbTimeMarker      = []byte(`"timestamp":`)
	// Title records can be appended anywhere (a mid-session /rename), so the tail
	// pass still decodes those lines — and only those.
	cbTitleMarkers = [][]byte{
		[]byte(`"type":"custom-title"`),
		[]byte(`"type":"ai-title"`),
		[]byte(`"type":"topic"`),
	}
)

// ListCodeBuddyConversations enumerates CodeBuddy transcripts, newest first.
func ListCodeBuddyConversations() []ConversationSummary {
	base := codebuddyProjectsDir()
	projects, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	// Collect first: every transcript is an independent file read.
	type candidate struct{ path, project string }
	paths := []candidate{}
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		dir := filepath.Join(base, p.Name())
		files, ferr := os.ReadDir(dir)
		if ferr != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			paths = append(paths, candidate{filepath.Join(dir, f.Name()), p.Name()})
		}
	}

	results := make([]ConversationSummary, len(paths))
	ok := make([]bool, len(paths))
	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8
	}
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				results[i], ok[i] = parseCodeBuddySummary(paths[i].path)
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
		if !ok[i] {
			continue
		}
		s := results[i]
		// The directory name is Claude's lossy cwd encoding; use it only when the
		// transcript itself never recorded a cwd.
		if s.FolderPath == "" {
			s.FolderPath = decodeClaudeFolderPath(paths[i].project)
		}
		s.FolderName = folderNameFromPath(s.FolderPath)
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

// codebuddyScan accumulates what the picker needs while one transcript is walked.
type codebuddyScan struct {
	cwd, sessionID, model            string
	customTitle, aiTitle, topicTitle string
	firstUser                        string
	firstMillis, lastMillis          int64
	hasFirst                         bool
	msgCount                         int
}

// parseCodeBuddySummary reads one transcript: JSON for the head, byte markers for
// the tail, so a 146 MB session costs a scan rather than a decode.
func parseCodeBuddySummary(path string) (ConversationSummary, bool) {
	file, err := os.Open(path)
	if err != nil {
		return ConversationSummary{}, false
	}
	defer file.Close()

	var st codebuddyScan
	lineNo := 0
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		lineNo++
		if lineNo <= codebuddyHeadLines {
			st.head(line)
		} else {
			st.tail(line)
		}
	}
	if !st.hasFirst {
		// No content records: a metadata-only file is not a conversation.
		return ConversationSummary{}, false
	}
	id := st.sessionID
	if id == "" {
		id = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}
	return ConversationSummary{
		ID:           id,
		AgentType:    "code_buddy",
		FolderPath:   st.cwd,
		Title:        firstNonEmptyString(st.customTitle, st.aiTitle, st.topicTitle, st.firstUser),
		StartedAt:    opencodeMillis(st.firstMillis),
		EndedAt:      opencodeMillis(st.lastMillis),
		MessageCount: st.msgCount,
		Model:        st.model,
	}, true
}

// head decodes one record fully. Everything the picker wants lands in the first
// few records of a real session, so decoding stops being worth it quickly.
func (st *codebuddyScan) head(line []byte) {
	var v map[string]any
	if json.Unmarshal(line, &v) != nil || v == nil {
		return
	}
	recType := strOf(v["type"])
	role := strOf(v["role"])
	injected := codebuddyInjectedUser(v)
	if st.cwd == "" {
		st.cwd = strOf(v["cwd"])
	}
	if st.sessionID == "" {
		st.sessionID = strOf(v["sessionId"])
	}
	if st.model == "" {
		st.model = codebuddyModel(v)
	}
	// Only content records define the session's span; metadata records (ai-title,
	// summary, snapshots) carry timestamps too and would stretch it.
	if codebuddyIsContentRecord(recType) {
		if ms, ok := codebuddyLineMillis(line); ok {
			st.mark(ms)
		}
	}
	st.applyTitleValue(v)
	switch recType {
	case "message":
		// An injected user record is not a prompt: its boilerplate must not
		// become the fallback title, and it does not count as a message.
		if injected {
			return
		}
		st.msgCount++
		if role == "user" && st.firstUser == "" {
			if text := strings.TrimSpace(codebuddyText(v, "input_text")); text != "" {
				st.firstUser = summaryTitle(text)
			}
		}
	}
}

// applyTitleLine decodes one line from the tail pass and takes its title, if it
// carries one.
func (st *codebuddyScan) applyTitleLine(line []byte) {
	var v map[string]any
	if json.Unmarshal(line, &v) != nil || v == nil {
		return
	}
	st.applyTitleValue(v)
}

// applyTitleValue takes a title record's value. The three kinds are appended
// (never rewritten) and repeat over a session's life, so the LAST value of each
// kind wins — CodeBuddy resolves them the same way.
func (st *codebuddyScan) applyTitleValue(v map[string]any) {
	switch strOf(v["type"]) {
	case "custom-title":
		if t := strings.TrimSpace(strOf(v["customTitle"])); t != "" {
			st.customTitle = t
		}
	case "ai-title":
		if t := strings.TrimSpace(strOf(v["aiTitle"])); t != "" && !codebuddyPlaceholderTitle(t) {
			st.aiTitle = t
		}
	case "topic":
		if t := strings.TrimSpace(strOf(v["topic"])); t != "" && !codebuddyPlaceholderTitle(t) {
			st.topicTitle = t
		}
	}
}

// tail counts without decoding: `type` then `role` come first in every message
// record, so the markers below are exact, and `isMeta` still excludes an injected
// record.
func (st *codebuddyScan) tail(line []byte) {
	if bytes.Contains(line, cbInjectedMarker) {
		return
	}
	// The common case first: a message record, counted by marker and done.
	if n := bytes.Count(line, cbUserMarker) + bytes.Count(line, cbAssistantMarker); n > 0 {
		st.msgCount += n
		if i := bytes.LastIndex(line, cbTimeMarker); i >= 0 {
			if ms, ok := codebuddyParseMillis(line[i+len(cbTimeMarker):]); ok {
				st.mark(ms)
			}
		}
		return
	}
	// Not a message. A title record is the only other kind worth decoding, and it
	// can sit anywhere (a mid-session /rename).
	for _, m := range cbTitleMarkers {
		if bytes.Contains(line, m) {
			st.applyTitleLine(line)
			return
		}
	}
}

// mark records a content timestamp as the session's span.
func (st *codebuddyScan) mark(ms int64) {
	if !st.hasFirst {
		st.firstMillis, st.hasFirst = ms, true
	}
	st.lastMillis = ms
}

// codebuddyLineMillis reads the record's `timestamp` (epoch millis) by decoding
// the field alone — the head pass already has the line in hand.
func codebuddyLineMillis(line []byte) (int64, bool) {
	i := bytes.Index(line, cbTimeMarker)
	if i < 0 {
		return 0, false
	}
	return codebuddyParseMillis(line[i+len(cbTimeMarker):])
}

// codebuddyParseMillis parses the digits starting at b.
func codebuddyParseMillis(b []byte) (int64, bool) {
	i := 0
	for i < len(b) && (b[i] == ' ' || b[i] == '\t') {
		i++
	}
	start := i
	var v int64
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		v = v*10 + int64(b[i]-'0')
		i++
	}
	if i == start {
		return 0, false
	}
	return v, true
}

// codebuddyIsContentRecord reports whether a record type carries conversation
// content, as opposed to a metadata record.
func codebuddyIsContentRecord(recType string) bool {
	switch recType {
	case "message", "reasoning", "function_call", "function_call_result":
		return true
	}
	return false
}

// codebuddyInjectedUser reports a system-injected user record (`providerData.
// isMeta`), which CodeBuddy writes for task notifications and goal kick-offs.
func codebuddyInjectedUser(v map[string]any) bool {
	pd, _ := v["providerData"].(map[string]any)
	if pd == nil {
		return false
	}
	meta, _ := pd["isMeta"].(bool)
	return meta
}

// codebuddyModel prefers the display name over the lowercase model id.
func codebuddyModel(v map[string]any) string {
	pd, _ := v["providerData"].(map[string]any)
	if pd == nil {
		return ""
	}
	return firstNonEmptyString(strings.TrimSpace(strOf(pd["requestModelName"])),
		strings.TrimSpace(strOf(pd["model"])))
}

// codebuddyPlaceholderTitle filters the values CodeBuddy itself refuses to show.
func codebuddyPlaceholderTitle(text string) bool {
	if text == "(No content)" || text == "/compact" {
		return true
	}
	return strings.HasPrefix(text, "<image_local_path>") && strings.HasSuffix(text, "</image_local_path>")
}
