package parsers

// deepseek_test.go covers the DeepSeek event-log reader: zstd decoding, the
// plugin-injected prompt skip, assistant reasoning/tool-call folding, and
// generation/encoding selection.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

const deepseekFixture = `{"type":"session","time":1782276649000,"id":"s1","cwd":"/tmp"}
{"type":"turn/start","time":1782276649000}
{"type":"user/message","time":1782276649227,"data":{"source":{"kind":"user"},"content":[{"type":"text","text":"读一下 /tmp/target.txt"}]}}
{"type":"user/message","time":1782276649300,"data":{"source":{"kind":"plugin"},"content":[{"type":"text","text":"Current runtime context."}]}}
{"type":"assistant/message","time":1782276650000,"data":{"message":{"role":"assistant","content":[{"type":"reasoning","text":"先读文件。"},{"type":"tool-call","id":"call_1","name":"read","arguments":"{\"file_path\": \"/tmp/target.txt\"}"}]}}}
{"type":"tool/result","time":1782276651000,"data":{"message":{"content":[{"type":"tool-result","toolCallId":"call_1","content":[{"type":"text","text":"hello"}]}]}}}
{"type":"assistant/chunk","time":1782276651500,"data":{"content":[{"type":"text","text":"duplicate chunk"}]}}
{"type":"assistant/message","time":1782276652000,"data":{"message":{"content":[{"type":"text","text":"文件内容是 hello。"}]}}}
{"type":"turn/end","time":1782276653000}
{"type":"user/message","time":1782276660000,"data":{"source":{"kind":"user"},"content":[{"type":"text","text":"再读一次"}]}}
`

func writeDeepseekLog(t *testing.T, dir, name, body string, compress bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if !compress {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReadDeepseekTurnsZstd(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "wd-demo", "sess-ds")
	// A lower generation and a raw sibling must both lose to session.v3.
	writeDeepseekLog(t, dir, "session.jsonl.zstd", `{"type":"user/message","time":1,"data":{"source":{"kind":"user"},"content":[{"type":"text","text":"stale"}]}}`, true)
	writeDeepseekLog(t, dir, "session.jsonl", `{"type":"user/message","time":2,"data":{"source":{"kind":"user"},"content":[{"type":"text","text":"raw"}]}}`, false)
	writeDeepseekLog(t, dir, "session.v3.jsonl.zstd", deepseekFixture, true)
	t.Setenv("DEEPSEEK_ACP_SESSIONS_ROOT", root)

	turns := ReadDeepseekTurns("sess-ds")
	if len(turns) != 3 {
		t.Fatalf("turns = %d, want 3: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || !turns[0].HasTS || turns[0].TSMillis != 1782276649227 {
		t.Fatalf("user turn = %+v", turns[0])
	}
	ub, _ := turns[0].Raw["blocks"].([]map[string]any)
	if len(ub) != 1 || ub[0]["text"] != "读一下 /tmp/target.txt" {
		t.Fatalf("user blocks = %+v", ub)
	}

	if turns[1].Role != "assistant" {
		t.Fatalf("assistant turn = %+v", turns[1])
	}
	ab, _ := turns[1].Raw["blocks"].([]map[string]any)
	// reasoning (as text) + tool_use + text; the duplicate chunk, the plugin
	// prompt and the tool result contribute nothing.
	if len(ab) != 3 {
		t.Fatalf("assistant blocks = %d, want 3: %+v", len(ab), ab)
	}
	if ab[0]["text"] != "先读文件。" || ab[2]["text"] != "文件内容是 hello。" {
		t.Fatalf("text blocks = %+v", ab)
	}
	if ab[1]["type"] != "tool_use" || ab[1]["tool_name"] != "read" || ab[1]["tool_use_id"] != "call_1" {
		t.Fatalf("tool block = %+v", ab[1])
	}
	if prev, _ := ab[1]["input_preview"].(string); !strings.Contains(prev, `"file_path"`) {
		t.Fatalf("input preview = %q", prev)
	}

	if turns[2].Role != "user" {
		t.Fatalf("second user turn = %+v", turns[2])
	}
}

func TestReadDeepseekTurnsRawLog(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "wd-demo", "sess-raw")
	writeDeepseekLog(t, dir, "session.jsonl", deepseekFixture, false)
	t.Setenv("DEEPSEEK_ACP_SESSIONS_ROOT", root)

	if turns := ReadDeepseekTurns("sess-raw"); len(turns) != 3 {
		t.Fatalf("raw log turns = %d, want 3", len(turns))
	}
}

func TestDeepseekLogGeneration(t *testing.T) {
	cases := []struct {
		name       string
		gen        int
		compressed bool
		ok         bool
	}{
		{"session.jsonl.zstd", 0, true, true},
		{"session.jsonl", 0, false, true},
		{"session.v3.jsonl.zstd", 3, true, true},
		{"session.v12.jsonl", 12, false, true},
		{"session.v0.jsonl", 0, false, false},
		{"session.v01.jsonl", 0, false, false},
		{"session.vX.jsonl", 0, false, false},
		{"other.jsonl", 0, false, false},
		{"session.lock", 0, false, false},
	}
	for _, c := range cases {
		gen, compressed, ok := deepseekLogGeneration(c.name)
		if ok != c.ok || (ok && (gen != c.gen || compressed != c.compressed)) {
			t.Fatalf("%s => (%d,%v,%v), want (%d,%v,%v)", c.name, gen, compressed, ok, c.gen, c.compressed, c.ok)
		}
	}
}

func TestReadDeepseekTurnsUnknownSession(t *testing.T) {
	t.Setenv("DEEPSEEK_ACP_SESSIONS_ROOT", t.TempDir())
	if got := ReadDeepseekTurns("missing"); got != nil {
		t.Fatalf("unknown session = %+v", got)
	}
}
