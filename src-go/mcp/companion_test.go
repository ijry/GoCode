package mcp_test

// Companion coverage: MCP JSON-RPC over stdio — initialize handshake,
// tools/list, the six workbench tools (authoring ones proxied to a stub app
// backend), name normalization, and error/notification handling.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yoyocode/src-go/mcp"
)

func decodeResult(t *testing.T, resp []byte) map[string]any {
	t.Helper()
	var env struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp, &env); err != nil {
		t.Fatalf("decode %s: %v", resp, err)
	}
	if env.Error != nil {
		t.Fatalf("unexpected rpc error %d: %s", env.Error.Code, env.Error.Message)
	}
	return env.Result
}

func TestCompanionInitializeAndList(t *testing.T) {
	c := mcp.NewCompanion("", "", "1", "claude_code", "s1")

	res := decodeResult(t, c.HandleLine([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)))
	si, _ := res["serverInfo"].(map[string]any)
	if si["name"] != "yoyocode-mcp" {
		t.Fatalf("serverInfo = %v", res["serverInfo"])
	}
	if res["protocolVersion"] != mcp.ProtocolVersion {
		t.Fatalf("protocolVersion = %v", res["protocolVersion"])
	}

	// A notification yields no reply.
	if got := c.HandleLine([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); got != nil {
		t.Fatalf("notification should not be answered, got %s", got)
	}

	tools := decodeResult(t, c.HandleLine([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)))["tools"].([]any)
	want := []string{"get_session_info", "task_progress", "task_complete", "create_automation", "create_work_task", "resume_delegation"}
	got := map[string]bool{}
	for _, tv := range tools {
		got[tv.(map[string]any)["name"].(string)] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Fatalf("tools/list missing %q", w)
		}
	}
}

func TestCompanionToolNameNormalization(t *testing.T) {
	c := mcp.NewCompanion("", "", "", "claude_code", "sess-9")
	// Host spelling mcp__yoyocode-mcp__get_session_info must dispatch.
	res := decodeResult(t, c.HandleLine([]byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mcp__yoyocode-mcp__get_session_info","arguments":{}}}`)))
	content := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(content, "sess-9") || !strings.Contains(content, "claude_code") {
		t.Fatalf("get_session_info should report the session context, got %q", content)
	}
	if res["isError"].(bool) {
		t.Fatalf("get_session_info should not be an error")
	}
}

func TestCompanionProgressAndCompleteAck(t *testing.T) {
	c := mcp.NewCompanion("", "", "", "", "")
	res := decodeResult(t, c.HandleLine([]byte(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"task_progress","arguments":{"message":"reading files"}}}`)))
	if txt := res["content"].([]any)[0].(map[string]any)["text"].(string); !strings.Contains(txt, "reading files") {
		t.Fatalf("task_progress ack wrong: %q", txt)
	}
}

func TestCompanionResumeDelegationDeferred(t *testing.T) {
	c := mcp.NewCompanion("", "", "", "", "")
	res := decodeResult(t, c.HandleLine([]byte(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"resume_delegation","arguments":{"task_id":"t1"}}}`)))
	if !res["isError"].(bool) {
		t.Fatalf("resume_delegation should report isError (deferred)")
	}
}

func TestCompanionCreateWorkTaskProxies(t *testing.T) {
	var gotDraft map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/work_task_create" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body struct {
			Draft map[string]any `json:"draft"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotDraft = body.Draft
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, `{"id":7,"title":"Fix CI"}`)
	}))
	defer srv.Close()

	c := mcp.NewCompanion(srv.URL, "tok", "42", "claude_code", "s1")
	res := decodeResult(t, c.HandleLine([]byte(`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"create_work_task","arguments":{"title":"Fix CI","prompt":"rerun tests"}}}`)))
	if res["isError"].(bool) {
		t.Fatalf("create_work_task should succeed")
	}
	if txt := res["content"].([]any)[0].(map[string]any)["text"].(string); !strings.Contains(txt, "7") {
		t.Fatalf("result should mention the created id, got %q", txt)
	}
	if gotDraft["folder_id"] != "42" || gotDraft["title"] != "Fix CI" {
		t.Fatalf("forwarded draft wrong: %v", gotDraft)
	}
	if cfg, _ := gotDraft["config"].(map[string]any); cfg == nil || cfg["prompt"] != "rerun tests" {
		t.Fatalf("prompt should ride in config: %v", gotDraft["config"])
	}
}

func TestCompanionCreateAutomationProxies(t *testing.T) {
	var gotDraft map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/automation_create" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body struct {
			Draft map[string]any `json:"draft"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotDraft = body.Draft
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, `{"id":3,"name":"nightly"}`)
	}))
	defer srv.Close()

	c := mcp.NewCompanion(srv.URL, "tok", "42", "claude_code", "s1")
	res := decodeResult(t, c.HandleLine([]byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"create_automation","arguments":{"name":"nightly","cron":"0 3 * * *","prompt":"summarize"}}}`)))
	if res["isError"].(bool) {
		t.Fatalf("create_automation should succeed")
	}
	if gotDraft["name"] != "nightly" || gotDraft["cron"] != "0 3 * * *" || gotDraft["agent_type"] != "claude_code" {
		t.Fatalf("forwarded automation draft wrong: %v", gotDraft)
	}
	if cfg, _ := gotDraft["config"].(map[string]any); cfg == nil || cfg["prompt"] != "summarize" {
		t.Fatalf("prompt should ride in config: %v", gotDraft["config"])
	}
}

func TestCompanionUnknownMethod(t *testing.T) {
	c := mcp.NewCompanion("", "", "", "", "")
	resp := c.HandleLine([]byte(`{"jsonrpc":"2.0","id":9,"method":"nope"}`))
	var env struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(resp, &env)
	if env.Error == nil || env.Error.Code != -32601 {
		t.Fatalf("unknown method should be -32601, got %s", resp)
	}
}
