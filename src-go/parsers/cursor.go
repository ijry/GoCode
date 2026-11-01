package parsers

// cursor.go reads Cursor (cursor-agent CLI) transcripts.
//
// Config home: `CURSOR_CONFIG_DIR`, else `$XDG_CONFIG_HOME/cursor`, else
// `~/.cursor`. A conversation is a **SQLite blob store**:
//
//	acp-sessions/<uuid>/store.db        (ACP sessions; a meta.json sidecar sits beside it)
//	chats/<md5-of-cwd>/<uuid>/store.db  (interactive CLI/IDE chats)
//
// `store.db` holds `blobs(id TEXT, data BLOB)` + `meta(key, value)`. `meta` key
// "0" is **hex-encoded UTF-8 JSON** whose `latestRootBlobId` (hex) addresses the
// root of a content-addressed DAG of `agent.v1.*` protobuf messages:
//
//	ConversationStateStructure   8: turns[] (repeated blob ids)
//	AgentConversationTurnStruct  1: user_message (blob id)   2: steps[] (repeated blob ids)
//	UserMessage                  1: text   8: rich_text   18: text_blob_id
//	ConversationStep             1: assistant_message{1:text}
//	                             2: tool_call{oneof → tool variant}
//	                             3: thinking_message{1:text}
//
// Blob ids are raw bytes on the wire and lowercase hex in the table, so every
// lookup hex-encodes first.
//
// Mirrors upstream parsers/cursor.rs, minus token usage, turn-span clocks,
// attachments, sub-agent stats and the tool-call argument previews.

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// cursorConfigDir resolves Cursor's config home.
func cursorConfigDir() string {
	if v := strings.TrimSpace(os.Getenv("CURSOR_CONFIG_DIR")); v != "" {
		return v
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "cursor")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cursor")
}

// findCursorStore locates the conversation's `store.db`: the ACP layout first,
// then the cwd-bucketed chat layout.
func findCursorStore(sessionID string) string {
	if !isSafeSessionID(sessionID) {
		return ""
	}
	base := cursorConfigDir()
	if st, err := os.Stat(filepath.Join(base, "acp-sessions", sessionID, "store.db")); err == nil && !st.IsDir() {
		return filepath.Join(base, "acp-sessions", sessionID, "store.db")
	}
	chats, err := os.ReadDir(filepath.Join(base, "chats"))
	if err != nil {
		return ""
	}
	for _, bucket := range chats {
		if !bucket.IsDir() {
			continue
		}
		p := filepath.Join(base, "chats", bucket.Name(), sessionID, "store.db")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// ReadCursorTurns reads the Cursor transcript for sessionID (the conversation's
// external_id).
func ReadCursorTurns(sessionID string) []Turn {
	store := findCursorStore(sessionID)
	if store == "" {
		return nil
	}
	db, err := sql.Open("sqlite", "file:"+store+"?mode=ro")
	if err != nil {
		return nil
	}
	defer db.Close()
	rootID := cursorRootBlobID(db)
	if rootID == "" {
		return nil
	}
	root := cursorReadBlob(db, rootID)
	if root == nil {
		return nil
	}
	// The root addresses a list of turn blobs; each turn holds the user message
	// and the step blobs of one exchange.
	turnRefs := protoAllBytesAt(protoFields(root), 8)
	turns := []Turn{}
	idx := 0
	for _, turnRef := range turnRefs {
		turnBytes := cursorReadBlob(db, hex.EncodeToString(turnRef))
		if turnBytes == nil {
			turnBytes = turnRef // a small store may inline the turn
		}
		agentTurn := protoBytesAt(protoFields(turnBytes), 1)
		if agentTurn == nil {
			continue
		}
		fields := protoFields(agentTurn)
		if userRef := protoBytesAt(fields, 1); userRef != nil {
			msg := cursorReadBlob(db, hex.EncodeToString(userRef))
			if msg == nil {
				msg = userRef
			}
			if text := strings.TrimSpace(cursorUserText(db, msg)); text != "" {
				turns = append(turns, Turn{Role: "user", Raw: map[string]any{
					"id": "cursor-user-" + itoa(idx), "role": "user",
					"blocks": []map[string]any{{"type": "text", "text": text}},
				}})
				idx++
			}
		}
		blocks := []map[string]any{}
		var kinds []string
		for _, stepRef := range protoAllBytesAt(fields, 2) {
			step := cursorReadBlob(db, hex.EncodeToString(stepRef))
			if step == nil {
				step = stepRef
			}
			blocks, kinds = cursorStepBlocks(step, blocks, kinds)
		}
		if len(blocks) > 0 {
			turns = append(turns, Turn{Role: "assistant", Raw: map[string]any{
				"id": "cursor-asst-" + itoa(idx), "role": "assistant", "blocks": blocks,
			}})
			idx++
		}
	}
	return turns
}

// cursorRootBlobID reads meta key "0": hex-encoded JSON holding latestRootBlobId.
func cursorRootBlobID(db *sql.DB) string {
	var raw string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = '0'`).Scan(&raw); err != nil {
		return ""
	}
	decoded, err := hex.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	var meta struct {
		LatestRootBlobID string `json:"latestRootBlobId"`
	}
	if json.Unmarshal(decoded, &meta) != nil {
		return ""
	}
	return meta.LatestRootBlobID
}

// cursorReadBlob returns a blob's bytes by hex id.
func cursorReadBlob(db *sql.DB, hexID string) []byte {
	if hexID == "" {
		return nil
	}
	var data []byte
	if err := db.QueryRow(`SELECT data FROM blobs WHERE id = ?`, hexID).Scan(&data); err != nil {
		return nil
	}
	return data
}

// cursorUserText reads a UserMessage: plain text, then the referenced text blob,
// then the rich-text fallback.
func cursorUserText(db *sql.DB, msg []byte) string {
	fields := protoFields(msg)
	if s := protoStringAt(fields, 1); s != "" {
		return s
	}
	if ref := protoBytesAt(fields, 18); ref != nil {
		if blob := cursorReadBlob(db, hex.EncodeToString(ref)); len(blob) > 0 {
			if s := strings.ToValidUTF8(string(blob), ""); s != "" {
				return s
			}
		}
	}
	return protoStringAt(fields, 8)
}

// cursorStepBlocks appends one ConversationStep's blocks, folding consecutive
// assistant text (and thinking) into the block before it — the steps are deltas.
func cursorStepBlocks(step []byte, blocks []map[string]any, kinds []string) ([]map[string]any, []string) {
	appendText := func(kind, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		if n := len(blocks); n > 0 && n <= len(kinds) && kinds[n-1] == kind {
			blocks[n-1]["text"] = strOf(blocks[n-1]["text"]) + text
			return
		}
		blocks = append(blocks, map[string]any{"type": "text", "text": text})
		kinds = append(kinds, kind)
	}
	for _, f := range protoFields(step) {
		if f.wire != 2 {
			continue
		}
		switch f.num {
		case 1: // assistant_message { 1: text }
			appendText("text", protoStringAt(protoFields(f.raw), 1))
		case 3: // thinking_message { 1: text }
			appendText("thinking", protoStringAt(protoFields(f.raw), 1))
		case 2: // tool_call { oneof … }
			name := "unknown"
			for _, tf := range protoFields(f.raw) {
				if tf.wire == 2 {
					if n, ok := cursorToolNames[tf.num]; ok {
						name = n
					}
					break
				}
			}
			blocks = append(blocks, map[string]any{"type": "tool_use", "tool_name": name})
			kinds = append(kinds, "tool")
		}
	}
	return blocks, kinds
}

// cursorToolNames maps an `agent.v1.ToolCall` oneof field number to its name, so
// an undecoded variant still renders a named card.
var cursorToolNames = map[int]string{
	1: "shell", 3: "delete", 4: "glob", 5: "grep", 8: "read", 9: "update_todos",
	10: "read_todos", 12: "edit", 13: "ls", 14: "read_lints", 15: "mcp",
	16: "sem_search", 17: "create_plan", 18: "web_search", 19: "task",
	20: "list_mcp_resources", 21: "read_mcp_resource", 22: "apply_agent_diff",
	23: "ask_question", 24: "fetch", 25: "switch_mode", 28: "generate_image",
	29: "record_screen", 30: "computer_use", 31: "write_shell_stdin", 32: "reflect",
	33: "setup_vm_environment", 34: "truncated_tool_call", 37: "web_fetch",
	40: "pr_management", 41: "mcp_auth", 42: "await", 44: "get_mcp_tools",
	45: "report_bug", 46: "set_active_branch", 48: "communicate_update",
	49: "send_final_summary", 55: "send_message", 58: "send_to_user",
}
