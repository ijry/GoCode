package parsers

// antigravity_test.go covers the Antigravity reader end to end: the `steps`
// table, the hand-rolled protobuf decoding of `gemini_coder.Step`, and the
// user_input / planner_response arms. The payloads are encoded by the helpers
// below, which is also what proves the wire reader against a known encoding.

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// ── protobuf encoding helpers (test side) ────────────────────────────────────

func pbVarint(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func pbTag(num, wire int) []byte { return pbVarint(uint64(num)<<3 | uint64(wire)) }

func pbString(num int, s string) []byte {
	out := append([]byte{}, pbTag(num, 2)...)
	out = append(out, pbVarint(uint64(len(s)))...)
	return append(out, []byte(s)...)
}

func pbMessage(num int, body []byte) []byte { return pbString(num, string(body)) }

func pbInt(num int, v int64) []byte {
	out := append([]byte{}, pbTag(num, 0)...)
	return append(out, pbVarint(uint64(v))...)
}

func pbConcat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// ── fixtures ────────────────────────────────────────────────────────────────

// userStep is a `user_input` step stamped at 1782276649 seconds.
func userStep(query string) []byte {
	return pbConcat(
		pbMessage(5, pbMessage(1, pbInt(1, 1782276649))), // metadata.created_at.seconds
		pbMessage(19, pbString(1, query)),                // user_input.query
	)
}

// plannerStep is a `planner_response` step: thinking + prose + one tool call.
func plannerStep(thinking, response string) []byte {
	call := pbConcat(
		pbString(1, "call_1"),
		pbString(2, "run_command"),
		pbString(3, `{"CommandLine":"ls"}`),
	)
	return pbConcat(
		pbMessage(5, pbMessage(1, pbInt(1, 1782276650))),
		pbMessage(20, pbConcat(
			pbString(1, response),
			pbString(3, thinking),
			pbMessage(7, call),
		)),
	)
}

func TestReadAntigravityTurns(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "antigravity-acp", "conversations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "sess-ag.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE steps (idx INTEGER PRIMARY KEY, step_payload BLOB)`); err != nil {
		t.Fatal(err)
	}
	for i, payload := range [][]byte{
		userStep("跑一下 ls"),
		plannerStep("先看看目录", "好的，执行 ls。"),
	} {
		if _, err := db.Exec(`INSERT INTO steps (idx, step_payload) VALUES (?, ?)`, i, payload); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GEMINI_HOME", home)

	turns := ReadAntigravityTurns("sess-ag")
	if len(turns) != 2 {
		t.Fatalf("turns = %d, want 2: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || !turns[0].HasTS || turns[0].TSMillis != 1782276649000 {
		t.Fatalf("user turn = %+v (seconds must become millis)", turns[0])
	}
	ub, _ := turns[0].Raw["blocks"].([]map[string]any)
	if len(ub) != 1 || ub[0]["text"] != "跑一下 ls" {
		t.Fatalf("user blocks = %+v", ub)
	}

	ab, _ := turns[1].Raw["blocks"].([]map[string]any)
	if len(ab) != 3 {
		t.Fatalf("assistant blocks = %d, want 3: %+v", len(ab), ab)
	}
	if ab[0]["text"] != "先看看目录" || ab[1]["text"] != "好的，执行 ls。" {
		t.Fatalf("text blocks = %+v", ab)
	}
	if ab[2]["type"] != "tool_use" || ab[2]["tool_name"] != "run_command" || ab[2]["tool_use_id"] != "call_1" {
		t.Fatalf("tool block = %+v", ab[2])
	}
}

// TestAntigravityEmptyDB locks the "pre-created empty DB is normal" case: no
// `steps` table at all reads as an empty conversation, not an error.
func TestAntigravityEmptyDB(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "antigravity-acp", "conversations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "sess-empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	t.Setenv("GEMINI_HOME", home)

	if got := ReadAntigravityTurns("sess-empty"); len(got) != 0 {
		t.Fatalf("empty db = %+v", got)
	}
}

func TestProtoWireReader(t *testing.T) {
	// Nested + repeated + varint in one payload, decoded back field by field.
	inner := pbConcat(pbString(1, "a"), pbString(1, "b"))
	payload := pbConcat(pbInt(4, 7), pbMessage(19, inner))
	fs := protoFields(payload)
	if got := protoIntAt(fs, 4); got != 7 {
		t.Fatalf("varint = %d, want 7", got)
	}
	all := protoAllBytesAt(protoFields(protoBytesAt(fs, 19)), 1)
	if len(all) != 2 || string(all[0]) != "a" || string(all[1]) != "b" {
		t.Fatalf("repeated strings = %q", all)
	}
	// A truncated payload must not panic: it yields what it could read.
	_ = protoFields(payload[:len(payload)-1])
}
