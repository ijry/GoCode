package parsers

// opencode_test.go covers the OpenCode SQLite reader: the message/part join, the
// `synthetic` text skip, reasoning and tool parts, and the XDG data home.

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestReadOpenCodeTurns(t *testing.T) {
	data := t.TempDir()
	dir := filepath.Join(data, "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT, time_created INTEGER, data TEXT)`,
		`CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT, time_created INTEGER, data TEXT)`,
		`INSERT INTO message VALUES ('m1','sess-oc',1000,'{"role":"user","time":{"created":1000}}')`,
		// A synthetic text part rides the user message and must not render.
		`INSERT INTO part VALUES ('p0','m1',1000,'{"type":"text","text":"[reminder] plan mode","synthetic":true}')`,
		`INSERT INTO part VALUES ('p1','m1',1001,'{"type":"text","text":"读一下 /tmp/x"}')`,
		`INSERT INTO message VALUES ('m2','sess-oc',2000,'{"role":"assistant","time":{"created":2000}}')`,
		`INSERT INTO part VALUES ('p2','m2',2000,'{"type":"reasoning","text":"先读文件。"}')`,
		`INSERT INTO part VALUES ('p3','m2',2001,'{"type":"text","text":"好的。"}')`,
		`INSERT INTO part VALUES ('p4','m2',2002,'{"type":"tool","tool":"read","callID":"call_1","state":{"status":"completed","input":{"file_path":"/tmp/x"}}}')`,
		`INSERT INTO message VALUES ('m3','sess-oc',3000,'{"role":"assistant","time":{"created":3000}}')`,
		`INSERT INTO part VALUES ('p5','m3',3000,'{"type":"text","text":"读完了。"}')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	t.Setenv("XDG_DATA_HOME", data)

	turns := ReadOpenCodeTurns("sess-oc")
	if len(turns) != 2 {
		t.Fatalf("turns = %d, want 2: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || !turns[0].HasTS || turns[0].TSMillis != 1000 {
		t.Fatalf("user turn = %+v", turns[0])
	}
	ub, _ := turns[0].Raw["blocks"].([]map[string]any)
	if len(ub) != 1 || ub[0]["text"] != "读一下 /tmp/x" {
		t.Fatalf("user blocks = %+v (the synthetic part must be dropped)", ub)
	}

	ab, _ := turns[1].Raw["blocks"].([]map[string]any)
	// reasoning + text + tool, then the later assistant message folds in.
	if len(ab) != 4 {
		t.Fatalf("assistant blocks = %d, want 4: %+v", len(ab), ab)
	}
	if ab[0]["text"] != "先读文件。" || ab[1]["text"] != "好的。" || ab[3]["text"] != "读完了。" {
		t.Fatalf("text blocks = %+v", ab)
	}
	if ab[2]["type"] != "tool_use" || ab[2]["tool_name"] != "read" || ab[2]["tool_use_id"] != "call_1" {
		t.Fatalf("tool block = %+v", ab[2])
	}
}

func TestReadOpenCodeTurnsMissingDB(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if got := ReadOpenCodeTurns("missing"); got != nil {
		t.Fatalf("missing db = %+v", got)
	}
}
