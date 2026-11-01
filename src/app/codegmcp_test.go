package app

// codegmcp_test.go covers the gocode-mcp workbench companion tool parser (the
// codeg-mcp-tool.ts port) and its one-liner card rendering in the transcript.

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestNormalizeMcpToolName(t *testing.T) {
	cases := map[string]string{
		"mcp__gocode-mcp__task_progress": "task_progress",
		"mcp__codeg-mcp__task_progress":    "task_progress",
		"gocode-mcp/get_session_info":    "get_session_info",
		"CodegMcpCard":                     "codegmcpcard",
		"task_complete":                    "task_complete",
	}
	for in, want := range cases {
		if got := normalizeMcpToolName(in); got != want {
			t.Errorf("normalizeMcpToolName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCodegMcpToolCallParse(t *testing.T) {
	// Plain args → detail.
	m := parseCodegMcpToolCall("task_progress", `{"message":"reading files"}`, "")
	if m.Detail != "reading files" {
		t.Errorf("task_progress detail = %q", m.Detail)
	}
	// task_complete → verdict + detail(summary).
	m = parseCodegMcpToolCall("task_complete", `{"verdict":"blocked","summary":"needs creds"}`, "")
	if m.Verdict != "blocked" || m.Detail != "needs creds" {
		t.Errorf("task_complete parse = %+v", m)
	}
	// Double-wrapped under `arguments` (a relay envelope) → unwrap.
	m = parseCodegMcpToolCall("create_work_task", `{"arguments":"{\"title\":\"Fix CI\",\"prompt\":\"rerun\"}"}`, "")
	if m.Detail != "Fix CI" || m.Prompt != "rerun" {
		t.Errorf("create_work_task wrapper unwrap = %+v", m)
	}
	// get_session_info with a numeric session id.
	m = parseCodegMcpToolCall("get_session_info", `{"session_id":42}`, "")
	if m.Detail != "42" {
		t.Errorf("session numeric detail = %q", m.Detail)
	}
	// resume_delegation reason.
	m = parseCodegMcpToolCall("resume_delegation", `{"task_id":"t9","reason":"retry"}`, "")
	if m.Detail != "t9" || m.Reason != "retry" {
		t.Errorf("resume_delegation parse = %+v", m)
	}
}

func TestCodegMcpResultPeel(t *testing.T) {
	cases := []struct{ out, want string }{
		{`{"content":[{"type":"text","text":"done ok"}]}`, "done ok"},
		{`{"result":{"content":[{"text":"inner"}]},"error":null}`, "inner"},
		{`{"structuredContent":{"note":"noted"}}`, "noted"},
		{`{"error":"boom"}`, "boom"},
		{"Wall time: 1s\nOutput:\n{\"content\":[{\"text\":\"y\"}]}", "y"},
		{`plain text`, "plain text"},
	}
	for _, c := range cases {
		if got := parseToolResultText(c.out); got != c.want {
			t.Errorf("parseToolResultText(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}

func TestCodegMcpIsWorkbenchTool(t *testing.T) {
	if !isCodegMcpWorkbenchTool("mcp__gocode-mcp__create_automation") {
		t.Errorf("companion tool should be recognized")
	}
	if isCodegMcpWorkbenchTool("read") {
		t.Errorf("a generic tool must not be treated as a companion card")
	}
}

func TestCodegMcpCardRendersOneLiner(t *testing.T) {
	s := conversationShellForTest(t)
	push := func(ev map[string]any) {
		ev["connection_id"] = "c1"
		s.stream.handle(&wsFrame{Type: "event", Envelope: ev})
	}
	push(map[string]any{"seq": 20, "type": "tool_call", "tool_call_id": "tp1",
		"kind": "mcp__gocode-mcp__task_progress", "status": "success",
		"input": `{"message":"reading files"}`})
	push(map[string]any{"seq": 21, "type": "tool_call", "tool_call_id": "ca1",
		"kind": "create_automation", "status": "success",
		"input": `{"name":"nightly","prompt":"summarize"}`})

	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("task_progress · reading files") {
		t.Errorf("companion one-liner not rendered; texts = %v", tt.Texts())
	}
	if !tt.HasText("create_automation · nightly") {
		t.Errorf("companion authoring card not rendered; texts = %v", tt.Texts())
	}
}
