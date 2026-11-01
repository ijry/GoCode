package parsers

// cursor_test.go covers the Cursor blob-DAG reader: the hex-encoded meta root,
// the turn → user_message/steps indirection, delta folding and the tool_call
// oneof naming. The store is built blob by blob, which is also what proves the
// DAG walk.

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadCursorTurns(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "acp-sessions", "sess-cur")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "store.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE blobs (id TEXT PRIMARY KEY, data BLOB)`,
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	// Blob ids are arbitrary bytes; the table holds their lowercase hex.
	idUser := []byte{0xa1}
	idThink := []byte{0xa2}
	idText := []byte{0xa3}
	idTool := []byte{0xa4}
	idTurn := []byte{0xa5}
	idRoot := []byte{0xa6}

	put := func(id []byte, data []byte) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO blobs (id, data) VALUES (?, ?)`,
			hex.EncodeToString(id), data); err != nil {
			t.Fatal(err)
		}
	}
	put(idUser, pbString(1, "跑一下 ls"))
	put(idThink, pbMessage(3, pbString(1, "先看看目录")))
	put(idText, pbMessage(1, pbString(1, "好的。")))
	// tool_call with oneof arm 1 = shell.
	put(idTool, pbMessage(2, pbString(1, "")))
	put(idTurn, pbMessage(1, pbConcat(
		pbString(1, string(idUser)),
		pbString(2, string(idThink)),
		pbString(2, string(idText)),
		pbString(2, string(idTool)),
	)))
	put(idRoot, pbString(8, string(idTurn)))

	metaJSON, _ := json.Marshal(map[string]any{"latestRootBlobId": hex.EncodeToString(idRoot)})
	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES ('0', ?)`,
		hex.EncodeToString(metaJSON)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CURSOR_CONFIG_DIR", home)

	turns := ReadCursorTurns("sess-cur")
	if len(turns) != 2 {
		t.Fatalf("turns = %d, want 2: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" {
		t.Fatalf("user turn = %+v", turns[0])
	}
	ub, _ := turns[0].Raw["blocks"].([]map[string]any)
	if len(ub) != 1 || ub[0]["text"] != "跑一下 ls" {
		t.Fatalf("user blocks = %+v", ub)
	}

	ab, _ := turns[1].Raw["blocks"].([]map[string]any)
	if len(ab) != 3 {
		t.Fatalf("assistant blocks = %d, want 3: %+v", len(ab), ab)
	}
	if ab[0]["text"] != "先看看目录" || ab[1]["text"] != "好的。" {
		t.Fatalf("text blocks = %+v", ab)
	}
	if ab[2]["type"] != "tool_use" || ab[2]["tool_name"] != "shell" {
		t.Fatalf("tool block = %+v", ab[2])
	}
	// The internal folding key must never reach the wire shape.
	for _, blk := range ab {
		if _, leaked := blk["kind"]; leaked {
			t.Fatalf("internal key leaked into the block: %+v", blk)
		}
	}
}

// TestReadCursorTurnsChatLayout covers the cwd-bucketed chat store, the other
// layout Cursor writes.
func TestReadCursorTurnsChatLayout(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "chats", "0123456789abcdef", "sess-chat")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "store.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE blobs (id TEXT PRIMARY KEY, data BLOB)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	idUser, idTurn, idRoot := []byte{1}, []byte{2}, []byte{3}
	ins := func(id, data []byte) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO blobs (id, data) VALUES (?, ?)`, hex.EncodeToString(id), data); err != nil {
			t.Fatal(err)
		}
	}
	ins(idUser, pbString(1, "hello"))
	ins(idTurn, pbMessage(1, pbString(1, string(idUser))))
	ins(idRoot, pbString(8, string(idTurn)))
	metaJSON, _ := json.Marshal(map[string]any{"latestRootBlobId": hex.EncodeToString(idRoot)})
	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES ('0', ?)`, hex.EncodeToString(metaJSON)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CURSOR_CONFIG_DIR", home)

	turns := ReadCursorTurns("sess-chat")
	if len(turns) != 1 || turns[0].Role != "user" {
		t.Fatalf("turns = %+v", turns)
	}
}

func TestReadCursorTurnsUnknownSession(t *testing.T) {
	t.Setenv("CURSOR_CONFIG_DIR", t.TempDir())
	if got := ReadCursorTurns("missing"); got != nil {
		t.Fatalf("unknown session = %+v", got)
	}
}
