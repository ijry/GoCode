package parsers

// cursor_summary.go enumerates Cursor (cursor-agent CLI) conversations for the
// import picker.
//
// Two layouts hold a `store.db`, both walked here:
//
//	acp-sessions/<uuid>/store.db        + meta.json sidecar {schemaVersion, cwd, title}
//	chats/<md5-of-cwd>/<uuid>/store.db
//
// Everything the picker needs is cheap to reach — no turn text is decoded:
//
//	meta key "0" (hex-encoded JSON) → name, createdAt, lastUsedModel, isSubagent
//	meta.json sidecar               → cwd, title
//	root blob (ConversationStateStructure) → 9 workspace_uris, 21 repo_path +
//	  branch_name, 26 conversation_started_timestamp_ms, 8 turns (count only)
//
// The sidecar cwd is authoritative for ACP sessions: their stores are keyed by
// session uuid, so there is no md5(cwd) bucket to fall back on, and the DAG's
// workspace refs only appear once a turn ran. For the chats layout the bucket is
// an md5 of the cwd (not reversible), so the state blob is the only source.

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListCursorConversations enumerates Cursor conversations, newest first.
func ListCursorConversations() []ConversationSummary {
	base := cursorConfigDir()
	out := []ConversationSummary{}

	// ACP sessions: <base>/acp-sessions/<uuid>/store.db
	if entries, err := os.ReadDir(filepath.Join(base, "acp-sessions")); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(base, "acp-sessions", e.Name())
			if s, ok := cursorSummaryOf(dir, e.Name()); ok {
				out = append(out, s)
			}
		}
	}

	// Interactive chats: <base>/chats/<md5-of-cwd>/<uuid>/store.db
	if buckets, err := os.ReadDir(filepath.Join(base, "chats")); err == nil {
		for _, b := range buckets {
			if !b.IsDir() {
				continue
			}
			dirs, derr := os.ReadDir(filepath.Join(base, "chats", b.Name()))
			if derr != nil {
				continue
			}
			for _, d := range dirs {
				if !d.IsDir() {
					continue
				}
				dir := filepath.Join(base, "chats", b.Name(), d.Name())
				if s, ok := cursorSummaryOf(dir, d.Name()); ok {
					out = append(out, s)
				}
			}
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

// cursorSummaryOf reads one conversation directory. It reports false for a
// sub-agent store and for one whose root blob is missing (nothing to import).
func cursorSummaryOf(dir, id string) (ConversationSummary, bool) {
	store := filepath.Join(dir, "store.db")
	if st, err := os.Stat(store); err != nil || st.IsDir() {
		return ConversationSummary{}, false
	}
	db, err := sql.Open("sqlite", "file:"+store+"?mode=ro")
	if err != nil {
		return ConversationSummary{}, false
	}
	defer db.Close()

	meta := cursorChatMetaOf(db)
	if meta.isSubagent {
		return ConversationSummary{}, false
	}
	sidecar := cursorSidecarMetaOf(filepath.Join(dir, "meta.json"))

	root := cursorReadBlob(db, meta.rootBlobID)
	if root == nil {
		return ConversationSummary{}, false
	}
	state := cursorStateSummaryOf(root)

	// Sidecar cwd first (authoritative for ACP), then the DAG's own workspace
	// refs, which only exist once a turn ran.
	folderPath := firstNonEmptyString(sidecar.cwd, state.folderPath)
	title := firstNonEmptyString(sidecar.title, meta.name)
	if title == "" {
		// No title record: the first prompt is the only human-readable name.
		title = cursorFirstPrompt(db, root)
	}

	started := meta.createdAtMs
	if started == 0 {
		started = state.startedMillis
	}
	return ConversationSummary{
		ID:           id,
		AgentType:    "cursor",
		FolderPath:   folderPath,
		FolderName:   folderNameFromPath(folderPath),
		Title:        summaryTitle(title),
		StartedAt:    opencodeMillis(started),
		EndedAt:      opencodeMillis(state.endedMillis),
		MessageCount: len(state.turnRefs) * 2, // user + assistant per turn
		Model:        meta.model,
		GitBranch:    state.gitBranch,
	}, true
}

// cursorChatMeta is the picker-relevant subset of meta key "0".
type cursorChatMeta struct {
	name        string
	model       string
	rootBlobID  string
	createdAtMs int64
	isSubagent  bool
}

// cursorChatMetaOf decodes meta key "0": hex-encoded UTF-8 JSON.
func cursorChatMetaOf(db *sql.DB) cursorChatMeta {
	var raw string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = '0'`).Scan(&raw); err != nil {
		return cursorChatMeta{}
	}
	decoded, err := hex.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return cursorChatMeta{}
	}
	var m struct {
		Name             string `json:"name"`
		LastUsedModel    string `json:"lastUsedModel"`
		LatestRootBlobID string `json:"latestRootBlobId"`
		CreatedAt        int64  `json:"createdAt"`
		IsSubagent       bool   `json:"isSubagent"`
	}
	if json.Unmarshal(decoded, &m) != nil {
		return cursorChatMeta{}
	}
	return cursorChatMeta{
		name:        strings.TrimSpace(m.Name),
		model:       m.LastUsedModel,
		rootBlobID:  m.LatestRootBlobID,
		createdAtMs: m.CreatedAt,
		isSubagent:  m.IsSubagent,
	}
}

// cursorSidecarMeta is the ACP session's `meta.json`: the only place its cwd
// survives.
type cursorSidecarMeta struct {
	cwd   string
	title string
}

func cursorSidecarMetaOf(path string) cursorSidecarMeta {
	raw, err := os.ReadFile(path)
	if err != nil {
		return cursorSidecarMeta{}
	}
	var m struct {
		Cwd   string `json:"cwd"`
		Title string `json:"title"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return cursorSidecarMeta{}
	}
	return cursorSidecarMeta{cwd: strings.TrimSpace(m.Cwd), title: strings.TrimSpace(m.Title)}
}

// cursorStateSummary is the picker-relevant subset of the DAG root.
type cursorStateSummary struct {
	folderPath    string
	gitBranch     string
	startedMillis int64
	endedMillis   int64
	turnRefs      [][]byte
}

// cursorStateOf decodes ConversationStateStructure, decoding no turn content.
func cursorStateSummaryOf(root []byte) cursorStateSummary {
	fs := protoFields(root)
	st := cursorStateSummary{turnRefs: protoAllBytesAt(fs, 8)}
	// 21: tracked_git_repo_branches {1 repo_path, 2 branch_name}
	if repo := protoBytesAt(fs, 21); repo != nil {
		rfs := protoFields(repo)
		st.folderPath = strings.TrimSpace(protoStringAt(rfs, 1))
		st.gitBranch = strings.TrimSpace(protoStringAt(rfs, 2))
	}
	// 9: previous_workspace_uris (repeated string), newest last.
	if st.folderPath == "" {
		for _, raw := range protoAllBytesAt(fs, 9) {
			uri := strings.TrimSpace(string(raw))
			uri = strings.TrimPrefix(uri, "file://")
			if uri != "" {
				st.folderPath = uri
			}
		}
	}
	// 26: conversation_started_timestamp_ms
	st.startedMillis = protoIntAt(fs, 26)
	// 14: turn_timings {1 duration_ms, 2 timestamp_ms} — the last one closes the
	// session's span.
	for _, raw := range protoAllBytesAt(fs, 14) {
		tfs := protoFields(raw)
		if ts := protoIntAt(tfs, 2); ts > 0 {
			st.endedMillis = ts + protoIntAt(tfs, 1)
		}
	}
	return st
}

// cursorFirstPrompt decodes only turn 0 to name a session that carries no title
// record.
func cursorFirstPrompt(db *sql.DB, root []byte) string {
	turns := protoAllBytesAt(protoFields(root), 8)
	if len(turns) == 0 {
		return ""
	}
	turnBytes := cursorReadBlob(db, hex.EncodeToString(turns[0]))
	if turnBytes == nil {
		turnBytes = turns[0]
	}
	agentTurn := protoBytesAt(protoFields(turnBytes), 1)
	if agentTurn == nil {
		return ""
	}
	userRef := protoBytesAt(protoFields(agentTurn), 1)
	if userRef == nil {
		return ""
	}
	msg := cursorReadBlob(db, hex.EncodeToString(userRef))
	if msg == nil {
		msg = userRef
	}
	return strings.TrimSpace(cursorUserText(db, msg))
}
