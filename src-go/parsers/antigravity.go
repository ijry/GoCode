package parsers

// antigravity.go reads Google Antigravity's transcripts.
//
// Layout — everything hangs off the **Gemini home**, which `GEMINI_HOME` names
// directly (unlike Gemini CLI's `GEMINI_CLI_HOME`, which names the parent):
//
//	<GEMINI_HOME or ~/.gemini>/antigravity-acp/conversations/<session id>.db
//
// The `.db` is a plain SQLite file with a single `steps(idx, step_payload)`
// table; each payload is a serialized `gemini_coder.Step` **protobuf**. A brand
// new session's DB is pre-created empty before the harness writes the schema, so
// "no such table: steps" is a normal state that reads as an empty conversation.
//
// The real Step has ~130 fields, almost all one arm of a `oneof step`. This
// reader declares only the arms a transcript needs, at their real field numbers
// — a oneof is just ordinary fields on the wire, so decoding them as independent
// optionals is faithful:
//
//	Step        status=4  metadata=5  user_input=19  planner_response=20  error=31
//	metadata    created_at=1  tool_call=4  completed_at=8  model_info=24
//	Timestamp   seconds=1  nanos=2
//	UserInput   query=1  user_response=2  items=3(repeated TextOrScopeItem{text=1})
//	Planner     response=1  thinking=3  tool_calls=7(repeated ChatToolCall)
//	ChatToolCall id=1  name=2  arguments_json=3
//
// Tool RESULTS are not rendered (the `tool_use` announcement already names the
// tool), matching this package's other minimal readers.

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
)

// antigravityConversationsDir resolves GEMINI_HOME (naming `.gemini` itself),
// else ~/.gemini, + "/antigravity-acp/conversations".
func antigravityConversationsDir() string {
	if v := strings.TrimSpace(os.Getenv("GEMINI_HOME")); v != "" {
		return filepath.Join(v, "antigravity-acp", "conversations")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", "antigravity-acp", "conversations")
}

// ReadAntigravityTurns reads the Antigravity transcript for sessionID (the
// conversation's external_id).
func ReadAntigravityTurns(sessionID string) []Turn {
	if !isSafeSessionID(sessionID) {
		return nil
	}
	path := filepath.Join(antigravityConversationsDir(), sessionID+".db")
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return nil
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil
	}
	defer db.Close()
	// A session created but not yet written has no `steps` table at all.
	rows, err := db.Query(`SELECT step_payload FROM steps ORDER BY idx ASC`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	turns := []Turn{}
	var cur *Turn
	var blocks []map[string]any
	flush := func() {
		if cur != nil && len(blocks) > 0 {
			cur.Raw["blocks"] = blocks
			turns = append(turns, *cur)
		}
		cur = nil
		blocks = nil
	}
	idx := 0
	start := func(role string, tsMillis int64, hasTS bool) {
		flush()
		cur = &Turn{Role: role, TSMillis: tsMillis, HasTS: hasTS,
			Raw: map[string]any{"id": "agy-" + role + "-" + itoa(idx), "role": role}}
		idx++
	}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil || len(payload) == 0 {
			continue
		}
		step := antigravityStepOf(payload)
		if text := strings.TrimSpace(step.userText); text != "" {
			start("user", step.createdMillis, step.hasTS)
			blocks = []map[string]any{{"type": "text", "text": text}}
			continue
		}
		var next []map[string]any
		if s := strings.TrimSpace(step.thinking); s != "" {
			next = append(next, map[string]any{"type": "text", "text": s})
		}
		if s := strings.TrimSpace(step.assistantText); s != "" {
			next = append(next, map[string]any{"type": "text", "text": s})
		}
		for _, call := range step.toolCalls {
			name := call.name
			if name == "" {
				name = "unknown"
			}
			blk := map[string]any{"type": "tool_use", "tool_name": name}
			if call.id != "" {
				blk["tool_use_id"] = call.id
			}
			if prev := jsonStringArgsPreview(call.args); prev != "" {
				blk["input_preview"] = prev
			}
			next = append(next, blk)
		}
		if len(next) == 0 {
			continue
		}
		if cur == nil || cur.Role != "assistant" {
			start("assistant", step.createdMillis, step.hasTS)
		}
		blocks = append(blocks, next...)
	}
	flush()
	return turns
}

// antigravityToolCall is one `ChatToolCall` of a planner response.
type antigravityToolCall struct {
	id   string
	name string
	args string // JSON text
}

// antigravityStep is the subset of `gemini_coder.Step` a transcript needs.
type antigravityStep struct {
	createdMillis int64
	hasTS         bool
	userText      string
	assistantText string
	thinking      string
	toolCalls     []antigravityToolCall
}

// antigravityStepOf decodes one step payload.
func antigravityStepOf(payload []byte) antigravityStep {
	fs := protoFields(payload)
	var st antigravityStep
	if meta := protoBytesAt(fs, 5); meta != nil {
		m := protoFields(meta)
		if ts := protoBytesAt(m, 1); ts != nil {
			if secs := protoIntAt(protoFields(ts), 1); secs > 0 {
				st.createdMillis, st.hasTS = secs*1000, true
			}
		}
	}
	if ui := protoBytesAt(fs, 19); ui != nil {
		u := protoFields(ui)
		st.userText = firstNonEmptyString(protoStringAt(u, 1), protoStringAt(u, 2))
		if st.userText == "" {
			// The structured form editors send: a list of text/scope items.
			parts := []string{}
			for _, item := range protoAllBytesAt(u, 3) {
				if s := strings.TrimSpace(protoStringAt(protoFields(item), 1)); s != "" {
					parts = append(parts, s)
				}
			}
			st.userText = strings.Join(parts, "\n")
		}
	}
	if pr := protoBytesAt(fs, 20); pr != nil {
		p := protoFields(pr)
		st.assistantText = protoStringAt(p, 1)
		st.thinking = protoStringAt(p, 3)
		for _, raw := range protoAllBytesAt(p, 7) {
			t := protoFields(raw)
			st.toolCalls = append(st.toolCalls, antigravityToolCall{
				id:   protoStringAt(t, 1),
				name: protoStringAt(t, 2),
				args: protoStringAt(t, 3),
			})
		}
	}
	return st
}

// ── minimal protobuf wire decoding ───────────────────────────────────────────
//
// Only what a transcript reader needs: varints and length-delimited fields.
// Groups (wire 3/4) are not used by the schema this reads, so an unexpected one
// ends the parse rather than guessing.

// protoField is one decoded field of a message.
type protoField struct {
	num  int
	wire int
	u64  uint64 // wire 0
	raw  []byte // wire 2
}

// protoVarint reads a base-128 varint, returning the value and its length.
func protoVarint(b []byte) (uint64, int) {
	var v uint64
	var shift uint
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << shift
		if b[i] < 0x80 {
			return v, i + 1
		}
		shift += 7
	}
	return 0, -1
}

// protoFields splits a serialized message into its fields, in wire order.
func protoFields(b []byte) []protoField {
	out := []protoField{}
	i := 0
	for i < len(b) {
		key, n := protoVarint(b[i:])
		if n <= 0 {
			return out
		}
		i += n
		f := protoField{num: int(key >> 3), wire: int(key & 7)}
		switch f.wire {
		case 0:
			v, n := protoVarint(b[i:])
			if n <= 0 {
				return out
			}
			i += n
			f.u64 = v
		case 1:
			if i+8 > len(b) {
				return out
			}
			i += 8
		case 2:
			l, n := protoVarint(b[i:])
			if n <= 0 || l > uint64(len(b)-i-n) {
				return out
			}
			i += n
			f.raw = b[i : i+int(l)]
			i += int(l)
		case 5:
			if i+4 > len(b) {
				return out
			}
			i += 4
		default:
			return out
		}
		out = append(out, f)
	}
	return out
}

// protoBytesAt returns the last length-delimited value of field num.
func protoBytesAt(fs []protoField, num int) []byte {
	var out []byte
	for _, f := range fs {
		if f.num == num && f.wire == 2 {
			out = f.raw
		}
	}
	return out
}

// protoAllBytesAt returns every length-delimited value of field num, in order.
func protoAllBytesAt(fs []protoField, num int) [][]byte {
	out := [][]byte{}
	for _, f := range fs {
		if f.num == num && f.wire == 2 {
			out = append(out, f.raw)
		}
	}
	return out
}

// protoStringAt returns the last string of field num.
func protoStringAt(fs []protoField, num int) string {
	return string(protoBytesAt(fs, num))
}

// protoIntAt returns the last varint of field num.
func protoIntAt(fs []protoField, num int) int64 {
	var out int64
	for _, f := range fs {
		if f.num == num && f.wire == 0 {
			out = int64(f.u64)
		}
	}
	return out
}
