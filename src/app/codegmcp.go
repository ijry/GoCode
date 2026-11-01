package app

// codegmcp.go ports upstream src/lib/codeg-mcp-tool.ts: it recognizes the six
// gocode-mcp workbench companion tools inside a tool call and distills the one
// argument (and verdict / prompt / reason) worth stating, plus the peeled result
// text — so CodegMcpToolCard-equivalent rendering can show a one-liner instead
// of a raw argument dump.

import (
	"encoding/json"
	"strconv"
	"strings"
)

var codegMcpWorkbenchTools = map[string]bool{
	"get_session_info":  true,
	"task_progress":     true,
	"task_complete":     true,
	"create_automation": true,
	"create_work_task":  true,
	"resume_delegation": true,
}

// normalizeMcpToolName collapses host spellings onto the canonical tool name:
// mcp__gocode-mcp__task_progress / gocode-mcp/task_progress → task_progress.
func normalizeMcpToolName(name string) string {
	name = strings.TrimSpace(name)
	for _, p := range []string{"mcp__gocode-mcp__", "mcp__codeg-mcp__", "gocode-mcp/", "codeg-mcp/", "gocode-mcp__", "codeg-mcp__"} {
		if strings.HasPrefix(name, p) {
			return name[len(p):]
		}
	}
	return strings.ToLower(name)
}

func isCodegMcpWorkbenchTool(name string) bool {
	return codegMcpWorkbenchTools[normalizeMcpToolName(name)]
}

// codegMcpModel is what a companion tool card states in one line.
type codegMcpModel struct {
	Tool       string
	Detail     string // the one argument worth naming (session id / message / summary / name / title / task id)
	Verdict    string // task_complete only
	Prompt     string // create_automation / create_work_task (audited in the expanded body)
	Reason     string // resume_delegation
	ResultText string // envelopes peeled
	IsError    bool   // errorText or output-error state
}

// codegMcpModelFor builds the model from a live tool card, or (ok=false) when
// the tool is not a companion workbench tool.
func codegMcpModelFor(tc *toolCard) (codegMcpModel, bool) {
	if tc == nil {
		return codegMcpModel{}, false
	}
	name := normalizeMcpToolName(firstNonEmpty(tc.kind, tc.title))
	if !isCodegMcpWorkbenchTool(name) {
		return codegMcpModel{}, false
	}
	m := parseCodegMcpToolCall(name, tc.rawInput, tc.output)
	if tc.status == "failed" || tc.status == "rejected" {
		m.IsError = true
	}
	return m, true
}

func argFields(tool string) []string {
	switch tool {
	case "get_session_info":
		return []string{"session_id", "sessionId"}
	case "task_progress":
		return []string{"message"}
	case "task_complete":
		return []string{"verdict", "summary"}
	case "create_automation":
		return []string{"name", "cron", "prompt"}
	case "create_work_task":
		return []string{"title", "prompt"}
	case "resume_delegation":
		return []string{"task_id"}
	}
	return nil
}

func parseCodegMcpToolCall(tool, input, output string) codegMcpModel {
	fields := argFields(tool)
	args := parseToolArgs(input, func(o map[string]any) bool { return hasAnyKey(o, fields) })
	m := codegMcpModel{Tool: tool, Detail: detailFor(tool, args)}
	switch tool {
	case "task_complete":
		m.Verdict = str(args, "verdict")
	case "create_automation", "create_work_task":
		m.Prompt = str(args, "prompt")
	case "resume_delegation":
		m.Reason = str(args, "reason")
	}
	m.ResultText = parseToolResultText(output)
	return m
}

func detailFor(tool string, args map[string]any) string {
	switch tool {
	case "get_session_info":
		return argString(args, "session_id", "sessionId")
	case "task_progress":
		return str(args, "message")
	case "task_complete":
		return str(args, "summary")
	case "create_automation":
		return str(args, "name")
	case "create_work_task":
		return str(args, "title")
	case "resume_delegation":
		return str(args, "task_id")
	}
	return ""
}

// ── argument parsing (wrapper-key walk + double-encoded JSON) ──────────────────

var argsWrapperKeys = []string{"arguments", "input", "params", "payload", "_meta", "args"}

func parseToolArgs(raw string, hasFields func(map[string]any) bool) map[string]any {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return nil
	}
	return findToolArgs(v, hasFields, 0)
}

// findToolArgs walks to the object carrying this tool's arguments, peeling any
// host wrapper (and one layer of double-encoding). Deepest match wins.
func findToolArgs(value any, hasFields func(map[string]any) bool, depth int) map[string]any {
	if depth > 4 || value == nil {
		return nil
	}
	if s, ok := value.(string); ok {
		var v any
		if json.Unmarshal([]byte(s), &v) == nil {
			return findToolArgs(v, hasFields, depth+1)
		}
		return nil
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range argsWrapperKeys {
		if child, present := obj[key]; present {
			if found := findToolArgs(child, hasFields, depth+1); found != nil {
				return found
			}
		}
	}
	if hasFields(obj) {
		return obj
	}
	return nil
}

// ── result parsing (envelope peeling) ──────────────────────────────────────────

func parseToolResultText(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var parsed any
	if json.Unmarshal([]byte(raw), &parsed) != nil {
		// Not JSON; may be Codex's "Wall time:…\nOutput:\n<json>" wrap.
		if emb := extractEmbeddedJSON(raw); emb != "" {
			var e any
			if json.Unmarshal([]byte(emb), &e) == nil {
				if s := readResultText(e); s != "" {
					return s
				}
			}
		}
		return raw
	}
	// Some hosts re-stringify the result; unwrap one layer of JSON-of-JSON.
	if s, ok := parsed.(string); ok {
		return parseToolResultText(s)
	}
	if obj, ok := parsed.(map[string]any); ok {
		if s := readResultObject(obj); s != "" {
			return s
		}
	}
	return raw
}

func readResultText(v any) string {
	obj, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	return readResultObject(obj)
}

// readResultObject peels a codex-acp {result,error} envelope and reads the human
// text from an MCP CallToolResult (content[0].text) or structuredContent.note.
func readResultObject(obj map[string]any) string {
	if e := str(obj, "error"); e != "" {
		return e // host error wins
	}
	inner := obj
	if r, ok := obj["result"].(map[string]any); ok {
		inner = r
	}
	if arr, ok := inner["content"].([]any); ok && len(arr) > 0 {
		if first, ok := arr[0].(map[string]any); ok {
			if tx := str(first, "text"); tx != "" {
				return tx
			}
		}
	}
	if st, ok := inner["structuredContent"].(map[string]any); ok {
		if n := str(st, "note"); n != "" {
			return n
		}
	}
	return ""
}

// extractEmbeddedJSON finds the first balanced {...} in s (a Codex "Output:\n<json>"
// wrap), ignoring braces inside strings.
func extractEmbeddedJSON(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// ── small helpers ────────────────────────────────────────────────────────────

func hasAnyKey(m map[string]any, keys []string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func argString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return v
			}
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	return ""
}
