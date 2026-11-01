package mcp

// Companion is the gocode-mcp workbench MCP server — the Go counterpart of
// upstream `codeg-mcp` (src-tauri/src/bin/codeg_mcp.rs) + the frontend
// codeg-mcp-tool.ts tool list. It speaks MCP over stdio (JSON-RPC 2.0) and
// exposes the six workbench companion tools a coding agent calls to report back
// into GoCode (or author automations / tasks):
//
//	get_session_info       → the current session/folder/agent context
//	task_progress          → note a progress message (ack)
//	task_complete          → record a completion verdict (ack)
//	create_automation      → proxies to the app's automation_create
//	create_work_task       → proxies to the app's work_task_create
//	resume_delegation      → deferred (delegation not yet ported; honest error)
//
// The two authoring tools proxy over the loopback /api contract to the running
// GoCode backend. resume_delegation needs the delegation runtime, which this
// port has not ported, so it reports "not supported" rather than faking it.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ServerName / protocol version reported at initialize.
const (
	ServerName      = "gocode-mcp"
	ProtocolVersion = "2024-11-05"
)

const workbenchToolSet = "get_session_info task_progress task_complete create_automation create_work_task resume_delegation"

// Companion serves one agent's MCP connection. FolderID / AgentType / SessionID
// come from the launch environment (the app injects the session context).
type Companion struct {
	APIBase   string // e.g. http://127.0.0.1:PORT
	Token     string
	FolderID  string
	AgentType string
	SessionID string
	Client    *http.Client
}

// NewCompanion builds a Companion with a sane default HTTP client.
func NewCompanion(apiBase, token, folderID, agentType, sessionID string) *Companion {
	return &Companion{
		APIBase: strings.TrimRight(apiBase, "/"), Token: token,
		FolderID: folderID, AgentType: agentType, SessionID: sessionID,
		Client: &http.Client{Timeout: 30 * time.Second},
	}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// HandleLine processes one JSON-RPC message and returns the response bytes, or
// nil when the message is a notification (no reply is due).
func (c *Companion) HandleLine(line []byte) []byte {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return c.errorBytes(nil, -32700, "parse error")
	}
	switch req.Method {
	case "initialize":
		return c.resultBytes(req.ID, map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": ServerName, "version": "0.1.0"},
		})
	case "notifications/initialized", "initialized", "notifications/cancelled":
		return nil // notification, no reply
	case "ping":
		return c.resultBytes(req.ID, map[string]any{})
	case "tools/list":
		return c.resultBytes(req.ID, map[string]any{"tools": c.toolsList()})
	case "tools/call":
		return c.toolsCall(req.ID, req.Params)
	default:
		if req.ID == nil {
			return nil // unknown notification → ignore
		}
		return c.errorBytes(req.ID, -32601, "method not found: "+req.Method)
	}
}

// Serve reads newline-delimited JSON-RPC from r and writes responses to w until
// r is exhausted. This is the stdio loop cmd/gocode-mcp runs.
func (c *Companion) Serve(r io.Reader, w io.Writer) error {
	dec := bufio.NewReader(r)
	for {
		line, err := dec.ReadBytes('\n')
		if len(line) > 0 {
			if resp := c.HandleLine(line); resp != nil {
				resp = append(resp, '\n')
				if _, werr := w.Write(resp); werr != nil {
					return werr
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// ── tools ────────────────────────────────────────────────────────────────────

func (c *Companion) toolsList() []map[string]any {
	schema := func(props map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": required}
	}
	return []map[string]any{
		{"name": "get_session_info", "description": "Return the calling session's context (session id, folder, agent).",
			"inputSchema": schema(map[string]any{"session_id": map[string]any{"type": "string"}})},
		{"name": "task_progress", "description": "Report a progress message for the running task.",
			"inputSchema": schema(map[string]any{"message": map[string]any{"type": "string"}}, "message")},
		{"name": "task_complete", "description": "Report the task's completion verdict.",
			"inputSchema": schema(map[string]any{
				"verdict": map[string]any{"type": "string", "enum": []string{"success", "needs_review", "blocked"}},
				"summary": map[string]any{"type": "string"}})},
		{"name": "create_automation", "description": "Create a scheduled/event automation.",
			"inputSchema": schema(map[string]any{
				"name":   map[string]any{"type": "string"},
				"cron":   map[string]any{"type": "string"},
				"prompt": map[string]any{"type": "string"}}, "name", "prompt")},
		{"name": "create_work_task", "description": "Create a work task on the board.",
			"inputSchema": schema(map[string]any{
				"title":  map[string]any{"type": "string"},
				"prompt": map[string]any{"type": "string"}}, "title")},
		{"name": "ask_user_question", "description": "Ask the user a question and wait for their answer. Use when a decision genuinely needs the user (the call blocks until they answer).",
			"inputSchema": schema(map[string]any{
				"question": map[string]any{"type": "string"},
				"options": map[string]any{"type": "array", "items": map[string]any{
					"type":       "object",
					"properties": map[string]any{"label": map[string]any{"type": "string"}},
				}}}, "question")},
		{"name": "check_user_feedback", "description": "Fetch any live-feedback notes the user sent for this turn. Call this to see whether the user has steered you; returns the waiting notes (empty when none).",
			"inputSchema": schema(map[string]any{})},
		{"name": "resume_delegation", "description": "Revive a delegated sub-agent (deferred in this build).",
			"inputSchema": schema(map[string]any{
				"task_id": map[string]any{"type": "string"},
				"reason":  map[string]any{"type": "string"}})},
	}
}

func (c *Companion) toolsCall(id json.RawMessage, params json.RawMessage) []byte {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return c.errorBytes(id, -32602, "invalid tools/call params")
	}
	name := normalizeToolName(strings.ToLower(strings.TrimSpace(p.Name)))
	args := p.Arguments
	if args == nil {
		args = map[string]any{}
	}
	switch name {
	case "get_session_info":
		return c.toolResult(id, c.sessionInfoText(args), false, "")
	case "task_progress":
		return c.toolResult(id, "progress noted: "+strOf(args["message"]), false, "")
	case "task_complete":
		return c.toolResult(id, fmt.Sprintf("task marked %s: %s", strOf(args["verdict"]), strOf(args["summary"])), false, "")
	case "create_automation":
		return c.createAutomation(id, args)
	case "create_work_task":
		return c.createWorkTask(id, args)
	case "ask_user_question":
		return c.askUserQuestion(id, args)
	case "check_user_feedback":
		return c.checkUserFeedback(id)
	case "resume_delegation":
		return c.toolResult(id, "", true, "resume_delegation is not supported yet: the delegation runtime is not part of this build.")
	default:
		return c.toolResult(id, "", true, "unknown tool: "+p.Name)
	}
}

func (c *Companion) sessionInfoText(args map[string]any) string {
	sid := c.SessionID
	if v := strOf(args["session_id"]); v != "" {
		sid = v
	}
	info := map[string]any{"session_id": sid, "folder_id": c.FolderID, "agent_type": c.AgentType}
	b, _ := json.Marshal(info)
	return string(b)
}

func (c *Companion) createAutomation(id json.RawMessage, args map[string]any) []byte {
	draft := map[string]any{
		"name":         strOf(args["name"]),
		"enabled":      true,
		"trigger_kind": "schedule",
		"cron":         strOf(args["cron"]),
		"timezone":     "UTC",
		"agent_type":   c.AgentType,
		"config":       map[string]any{"prompt": strOf(args["prompt"])},
	}
	if c.FolderID != "" {
		draft["root_folder_id"] = c.FolderID
	}
	resp, err := c.callApp("automation_create", map[string]any{"draft": draft})
	if err != nil {
		return c.toolResult(id, "", true, "create_automation failed: "+err.Error())
	}
	return c.toolResult(id, renderResult(resp), false, "")
}

func (c *Companion) createWorkTask(id json.RawMessage, args map[string]any) []byte {
	draft := map[string]any{
		"folder_id": c.FolderID,
		"title":     strOf(args["title"]),
		"config":    map[string]any{"prompt": strOf(args["prompt"])},
	}
	resp, err := c.callApp("work_task_create", map[string]any{"draft": draft})
	if err != nil {
		return c.toolResult(id, "", true, "create_work_task failed: "+err.Error())
	}
	return c.toolResult(id, renderResult(resp), false, "")
}

// askUserQuestion parks a question in the app and waits for the user's answer.
// The call blocks server-side until the user answers, so it uses a client with
// no timeout (callApp's default has none).
func (c *Companion) askUserQuestion(id json.RawMessage, args map[string]any) []byte {
	body := map[string]any{
		"sessionId": c.SessionID,
		"question":  strOf(args["question"]),
	}
	if opts, ok := args["options"].([]any); ok && len(opts) > 0 {
		body["options"] = opts
	}
	resp, err := c.callApp("companion_ask_user_question", body)
	if err != nil {
		return c.toolResult(id, "", true, "ask_user_question failed: "+err.Error())
	}
	answer := strOf(resp["answer"])
	if answer == "" {
		answer = "(the user did not answer)"
	}
	return c.toolResult(id, answer, false, "")
}

// checkUserFeedback returns the live-feedback notes the user sent for this
// turn, one per line, and marks them read server-side.
func (c *Companion) checkUserFeedback(id json.RawMessage) []byte {
	resp, err := c.callApp("companion_check_user_feedback", map[string]any{"sessionId": c.SessionID})
	if err != nil {
		return c.toolResult(id, "", true, "check_user_feedback failed: "+err.Error())
	}
	notes, _ := resp["notes"].([]any)
	if len(notes) == 0 {
		return c.toolResult(id, "(no new feedback from the user)", false, "")
	}
	var b strings.Builder
	for i, n := range notes {
		m, _ := n.(map[string]any)
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("- ")
		b.WriteString(strOf(m["text"]))
	}
	return c.toolResult(id, b.String(), false, "")
}

// callApp POSTs to the loopback app backend and returns its decoded JSON.
func (c *Companion) callApp(route string, body any) (map[string]any, error) {
	if c.APIBase == "" {
		return nil, fmt.Errorf("no GoCode backend configured (set GOCODE_API)")
	}
	buf, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, c.APIBase+"/api/"+route, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	if c.Token != "" {
		req.Header.Set("authorization", "Bearer "+c.Token)
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned %d: %s", route, resp.StatusCode, string(raw))
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return map[string]any{"raw": string(raw)}, nil
	}
	return out, nil
}

// ── wire helpers ────────────────────────────────────────────────────────────

func (c *Companion) resultBytes(id json.RawMessage, result any) []byte {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": rawID(id), "result": result})
	return b
}

func (c *Companion) errorBytes(id json.RawMessage, code int, msg string) []byte {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": rawID(id),
		"error": map[string]any{"code": code, "message": msg},
	})
	return b
}

// toolResult is an MCP CallToolResult: {content:[{type:text,text}], isError}.
func (c *Companion) toolResult(id json.RawMessage, text string, isError bool, errText string) []byte {
	content := text
	if isError && errText != "" {
		content = errText
	}
	return c.resultBytes(id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": content}},
		"isError": isError,
	})
}

func rawID(id json.RawMessage) any {
	if len(id) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(id, &v) == nil {
		return v
	}
	return nil
}

func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func inToolSet(name string) bool {
	for _, t := range strings.Fields(workbenchToolSet) {
		if t == name {
			return true
		}
	}
	return false
}

// normalizeToolName collapses host spellings onto the canonical tool name:
// mcp__gocode-mcp__task_progress / gocode-mcp/task_progress → task_progress.
// Mirrors tool-call-normalization upstream.
func normalizeToolName(name string) string {
	name = strings.TrimSpace(name)
	for _, prefix := range []string{"mcp__gocode-mcp__", "mcp__codeg-mcp__", "gocode-mcp/", "gocode-mcp__", "codeg-mcp/", "codeg-mcp__"} {
		if strings.HasPrefix(name, prefix) {
			return name[len(prefix):]
		}
	}
	return name
}

// renderResult renders a proxied backend object as a compact one-liner (the
// app's create_* return full records; the agent wants id + name/title).
func renderResult(m map[string]any) string {
	if s := strOf(m["text"]); s != "" {
		return s
	}
	for _, k := range []string{"id", "name", "title"} {
		if v, ok := m[k]; ok && v != nil {
			return fmt.Sprintf("created (%s=%v)", k, v)
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}
