package parsers

// grok_test.go covers the Grok reader: the ACP session/update stream, delta
// chunk folding (one block per run of chunks), the hidden-reminder skip and
// Unix-second timestamps.

import (
	"os"
	"path/filepath"
	"testing"
)

const grokFixture = `{"method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"你会"},"_meta":{"modelId":"grok-4.5","promptIndex":0}}},"timestamp":1783584019}
{"method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"做什么"},"_meta":{"promptIndex":0}}},"timestamp":1783584019}
{"method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"<system-reminder>bg done</system-reminder>"},"_meta":{"hideFromScrollback":true}}},"timestamp":1783584020}
{"method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"Thinking "}}},"timestamp":1783584021}
{"method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"about it"}}},"timestamp":1783584021}
{"method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"我是 "}}},"timestamp":1783584024}
{"method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Grok"}}},"timestamp":1783584024}
{"method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"tool_call","toolCallId":"call_1","title":"Read file","kind":"read","rawInput":{"path":"/tmp/x"},"_meta":{"x.ai/tool":{"name":"read_file"}}}},"timestamp":1783584025}
{"method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_1","status":"completed","content":[{"type":"text","text":"ok"}]}},"timestamp":1783584026}
{"method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"turn_completed","stop_reason":"end_turn"}},"timestamp":1783584026}
`

func TestReadGrokTurns(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "%2Ftmp%2Fdemo", "sess-grok")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "updates.jsonl"), []byte(grokFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GROK_HOME", home)

	turns := ReadGrokTurns("sess-grok")
	if len(turns) != 2 {
		t.Fatalf("turns = %d, want 2: %+v", len(turns), turns)
	}
	// The two user chunks are one prompt and must fold into ONE text block.
	if turns[0].Role != "user" || !turns[0].HasTS || turns[0].TSMillis != 1783584019000 {
		t.Fatalf("user turn = %+v (seconds must become millis)", turns[0])
	}
	ub, _ := turns[0].Raw["blocks"].([]map[string]any)
	if len(ub) != 1 || ub[0]["text"] != "你会做什么" {
		t.Fatalf("user blocks = %+v", ub)
	}

	ab, _ := turns[1].Raw["blocks"].([]map[string]any)
	// thinking run + text run + tool_use. The hidden reminder must not appear.
	if len(ab) != 3 {
		t.Fatalf("assistant blocks = %d, want 3: %+v", len(ab), ab)
	}
	if ab[0]["text"] != "Thinking about it" {
		t.Fatalf("thinking block = %+v", ab[0])
	}
	if ab[1]["text"] != "我是 Grok" {
		t.Fatalf("text block = %+v", ab[1])
	}
	if ab[2]["type"] != "tool_use" || ab[2]["tool_name"] != "read_file" || ab[2]["tool_use_id"] != "call_1" {
		t.Fatalf("tool block = %+v", ab[2])
	}
	for _, tk := range turns {
		b, _ := tk.Raw["blocks"].([]map[string]any)
		for _, blk := range b {
			if s, _ := blk["text"].(string); s != "" && len(s) > 15 && s[:15] == "<system-reminder" {
				t.Fatalf("hidden reminder leaked: %+v", blk)
			}
		}
	}
}

func TestReadGrokTurnsUnknownSession(t *testing.T) {
	t.Setenv("GROK_HOME", t.TempDir())
	if got := ReadGrokTurns("missing"); got != nil {
		t.Fatalf("unknown session = %+v", got)
	}
}
