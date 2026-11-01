package parsers

// hermes_test.go covers the Hermes SQLite reader: the read-only open, the
// active-row filter, epoch-second timestamps, the \x00json: multimodal content
// sentinel and OpenAI-shaped tool_calls.

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestReadHermesTurns(t *testing.T) {
	home := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(home, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, model TEXT, cwd TEXT, title TEXT)`,
		`CREATE TABLE messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT, role TEXT,
			content TEXT, tool_call_id TEXT, tool_calls TEXT, tool_name TEXT,
			reasoning TEXT, reasoning_content TEXT, timestamp REAL,
			active INTEGER NOT NULL DEFAULT 1)`,
		`INSERT INTO sessions (id, model) VALUES ('sess-h','m')`,
		`INSERT INTO messages (session_id, role, content, timestamp, active)
			VALUES ('sess-h','user','读一下 /tmp/target.txt',1782276649.227,1)`,
		`INSERT INTO messages (session_id, role, reasoning, tool_calls, timestamp, active)
			VALUES ('sess-h','assistant','先读文件。',
			'[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"file_path\":\"/tmp/target.txt\"}"}}]',
			1782276650.0,1)`,
		`INSERT INTO messages (session_id, role, content, tool_call_id, timestamp, active)
			VALUES ('sess-h','tool','hello','call_1',1782276651.0,1)`,
		// Multimodal content: a NUL sentinel, then a JSON parts array.
		`INSERT INTO messages (session_id, role, content, timestamp, active)
			VALUES ('sess-h','assistant',char(0) || 'json:[{"type":"text","text":"文件内容是 hello。"}]',1782276652.0,1)`,
		`INSERT INTO messages (session_id, role, content, timestamp, active)
			VALUES ('sess-h','user','再读一次',1782276660.0,1)`,
		// A rewound row must not show up.
		`INSERT INTO messages (session_id, role, content, timestamp, active)
			VALUES ('sess-h','assistant','rewound',1782276670.0,0)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	t.Setenv("HERMES_HOME", home)

	turns := ReadHermesTurns("sess-h")
	if len(turns) != 3 {
		t.Fatalf("turns = %d, want 3: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || !turns[0].HasTS || turns[0].TSMillis != 1782276649227 {
		t.Fatalf("user turn = %+v (epoch seconds must become millis)", turns[0])
	}
	ub, _ := turns[0].Raw["blocks"].([]map[string]any)
	if len(ub) != 1 || ub[0]["text"] != "读一下 /tmp/target.txt" {
		t.Fatalf("user blocks = %+v", ub)
	}

	if turns[1].Role != "assistant" {
		t.Fatalf("assistant turn = %+v", turns[1])
	}
	ab, _ := turns[1].Raw["blocks"].([]map[string]any)
	// reasoning + tool_use + later assistant text; the tool result row is dropped.
	if len(ab) != 3 {
		t.Fatalf("assistant blocks = %d, want 3: %+v", len(ab), ab)
	}
	if ab[0]["text"] != "先读文件。" || ab[2]["text"] != "文件内容是 hello。" {
		t.Fatalf("text blocks = %+v", ab)
	}
	if ab[1]["type"] != "tool_use" || ab[1]["tool_name"] != "read" || ab[1]["tool_use_id"] != "call_1" {
		t.Fatalf("tool block = %+v", ab[1])
	}
	if prev, _ := ab[1]["input_preview"].(string); prev != `{"file_path":"/tmp/target.txt"}` {
		t.Fatalf("input preview = %q", prev)
	}

	if turns[2].Role != "user" {
		t.Fatalf("second user turn = %+v", turns[2])
	}
}

func TestReadHermesTurnsMissingDB(t *testing.T) {
	t.Setenv("HERMES_HOME", t.TempDir())
	if got := ReadHermesTurns("missing"); got != nil {
		t.Fatalf("missing db = %+v", got)
	}
}
