package handlers

import (
	"encoding/json"
	"errors"
	"strconv"

	"gocode/src-go/acp"
	"gocode/src-go/app_state"
	"gocode/src-go/web"
)

// RegisterACP wires the acp_* command family.
func RegisterACP(reg *Registry) {
	reg.web.Register("acp_list_agents", func(c *web.Context) (any, error) {
		// Faithful AcpAgentInfo rows from the built-in catalog, with `available`
		// stamped from whether the agent's launcher resolves on this machine
		// (npx/uvx runner present, or the binary itself on PATH). Without this
		// the composer dropdown can't tell which agents will actually start.
		list := acp.BuiltinAgentInfoList()
		for _, a := range list {
			if at, _ := a["agent_type"].(string); at != "" {
				a["available"] = agentLauncherAvailable(at)
			}
		}
		return list, nil
	})
	reg.web.Register("acp_list_connections", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		conns := []map[string]any{}
		for _, conn := range st.ACP.ListConnections() {
			conns = append(conns, connectionInfo(conn))
		}
		return conns, nil
	})
	reg.web.Register("acp_connect", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		agentType := strVal(c.Body["agentType"])
		if agentType == "" {
			return nil, web.ErrInvalidInput("agentType required")
		}
		// The client sends `workingDir` (upstream's field name); folderPath/path
		// stay accepted for in-app callers.
		folderPath := strVal(c.Body["workingDir"], c.Body["folderPath"], c.Body["path"])
		convID := int64Num(c.Body["conversationId"])
		conn, err := st.ACP.ConnectLive(agentType, folderPath, convID, acp.ConnectOptions{
			SessionID:             strVal(c.Body["sessionId"], c.Body["session_id"]),
			PreferredModeID:       strVal(c.Body["preferredModeId"], c.Body["preferred_mode_id"]),
			PreferredConfigValues: stringMap(c.Body["preferredConfigValues"], c.Body["preferred_config_values"]),
		})
		if err != nil {
			return nil, acpError(err)
		}
		return connectionInfo(conn), nil
	})
	reg.web.Register("acp_disconnect", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		id := strVal(c.Body["connectionId"], c.Body["connection_id"])
		st.ACP.DisconnectLive(id)
		return nil, nil
	})
	reg.web.Register("acp_prompt", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		connID := strVal(c.Body["connectionId"], c.Body["connection_id"])
		if connID == "" {
			return nil, web.ErrInvalidInput("connectionId required")
		}
		blocks := acpPromptBlocks(c)
		if len(blocks) == 0 {
			return nil, web.ErrInvalidInput("prompt or blocks required")
		}
		if err := st.ACP.PromptLive(connID, blocks); err != nil {
			return nil, acpError(err)
		}
		return nil, nil
	})
	reg.web.Register("acp_cancel", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		connID := strVal(c.Body["connectionId"], c.Body["connection_id"])
		st.ACP.CancelLive(connID)
		return nil, nil
	})
	reg.web.Register("acp_goal_control", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		connID := strVal(c.Body["connectionId"], c.Body["connection_id"])
		if connID == "" {
			return nil, web.ErrInvalidInput("connectionId required")
		}
		action := strVal(c.Body["action"])
		if action != "pause" && action != "clear" {
			return nil, web.ErrInvalidInput("action must be 'pause' or 'clear'")
		}
		if !st.ACP.GoalControlLive(connID, action) {
			return nil, web.ErrNotFound("connection not found")
		}
		return nil, nil
	})
	reg.web.Register("acp_touch_connection", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		connID := strVal(c.Body["connectionId"], c.Body["connection_id"])
		return st.ACP.Touch(connID), nil
	})
	// acp_stop_async_task asks the adapter to stop a background task through
	// its `_session/async_task/stop` extension. A false result means the
	// adapter declined (or does not implement the extension) — the same
	// 200-with-false the upstream returns in that case.
	reg.web.Register("acp_stop_async_task", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		connID := strVal(c.Body["connectionId"], c.Body["connection_id"])
		if connID == "" {
			return nil, web.ErrInvalidInput("connectionId required")
		}
		taskID := strVal(c.Body["asyncTaskId"], c.Body["async_task_id"])
		if taskID == "" {
			return nil, web.ErrInvalidInput("asyncTaskId required")
		}
		return st.ACP.StopAsyncTask(connID, taskID), nil
	})
	// acp_answer_plan_approval: plan-mode approval is a per-agent protocol
	// feature (e.g. Grok plan mode) the Go engine does not yet surface, so no
	// approval is ever pending here. Accept the answer as a no-op rather than
	// error, so a client that sends one against a non-plan agent is not broken.
	reg.web.Register("acp_answer_plan_approval", func(c *web.Context) (any, error) {
		return nil, nil
	})
	reg.web.Register("acp_get_agent_status", func(c *web.Context) (any, error) {
		return map[string]any{"installed": false}, nil
	})
	reg.web.Register("acp_describe_agent_options", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		agentType := strVal(c.Body["agentType"])
		if agentType == "" {
			return nil, web.ErrInvalidInput("agentType required")
		}
		// The client sends `workingDir` (upstream's field name); folderPath/path
		// stay accepted for in-app callers.
		workingDir := strVal(c.Body["workingDir"], c.Body["working_dir"], c.Body["folderPath"], c.Body["path"])
		snap, err := st.ACP.ProbeAgentOptions(agentType, workingDir)
		if err != nil {
			return nil, acpError(err)
		}
		return snap, nil
	})
	// acp_fork branches a live session (ACP `session/fork`): the agent gets a new
	// session id, the pre-fork history is preserved on a sibling conversation
	// row, and the current row is re-pointed to the forked session.
	reg.web.Register("acp_fork", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		connID := strVal(c.Body["connectionId"], c.Body["connection_id"])
		if connID == "" {
			return nil, web.ErrInvalidInput("connectionId required")
		}
		convID := intVal(c.Body["conversationId"], c.Body["conversation_id"])
		folderID := intVal(c.Body["folderId"], c.Body["folder_id"])
		// forkFromTurnId is accepted but unused: resolving a per-turn fork point
		// needs the transcript parsing this build does not port, so the fork lands
		// at the tail (upstream's own fallback).
		_ = strVal(c.Body["forkFromTurnId"], c.Body["fork_from_turn_id"])
		res, err := st.ACP.ForkSession(connID, convID, folderID)
		if err != nil {
			var tip *acp.TurnInProgressError
			if errors.As(err, &tip) {
				return nil, web.ErrTurnInProgress("a turn is already in progress")
			}
			return nil, acpError(err)
		}
		Emit(c, "conversation://changed", map[string]any{"conversationId": res.SiblingConversationID})
		return res, nil
	})
	reg.web.Register("acp_preflight", func(c *web.Context) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	reg.web.Register("acp_set_mode", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		connID := strVal(c.Body["connectionId"], c.Body["connection_id"])
		modeID := strVal(c.Body["modeId"], c.Body["mode_id"])
		if connID == "" {
			return nil, web.ErrInvalidInput("connectionId required")
		}
		if modeID == "" {
			return nil, web.ErrInvalidInput("modeId required")
		}
		if err := st.ACP.SetMode(connID, modeID); err != nil {
			return nil, acpError(err)
		}
		return nil, nil
	})
	reg.web.Register("acp_respond_permission", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		connID := strVal(c.Body["connectionId"], c.Body["connection_id"])
		requestID := strVal(c.Body["requestId"], c.Body["request_id"])
		optionID := strVal(c.Body["optionId"], c.Body["option_id"])
		st.ACP.RespondPermission(connID, requestID, optionID)
		return nil, nil
	})
	reg.web.Register("acp_get_session_snapshot", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		connID := strVal(c.Body["connectionId"], c.Body["connection_id"])
		state := st.Session.GetState(connID)
		if state == nil {
			return nil, nil
		}
		return state.Snapshot(), nil
	})
	// acp_answer_question resolves a parked ask_user_question (the companion's
	// tool call is waiting on it — see acp/ask_user.go).
	reg.web.Register("acp_answer_question", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		connID := strVal(c.Body["connectionId"], c.Body["connection_id"])
		requestID := strVal(c.Body["requestId"], c.Body["request_id"])
		if connID == "" || requestID == "" {
			return nil, web.ErrInvalidInput("connectionId and requestId required")
		}
		answer := strVal(c.Body["answer"], c.Body["text"])
		if answer == "" {
			// Some clients send the answers as a list, one per question.
			if list, ok := c.Body["answers"].([]any); ok && len(list) > 0 {
				answer = asString(list[0])
			}
		}
		if answer == "" {
			return nil, web.ErrInvalidInput("answer required")
		}
		return map[string]any{"ok": st.ACP.AnswerQuestion(connID, requestID, answer)}, nil
	})

	// acp_drop_queued_prompt removes one send waiting behind the running turn
	// (a local route: upstream's queue is client-side, so it has no equivalent).
	reg.web.Register("acp_drop_queued_prompt", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		connID := strVal(c.Body["connectionId"], c.Body["connection_id"])
		if connID == "" {
			return nil, web.ErrInvalidInput("connectionId required")
		}
		index := int(intVal(c.Body["index"], c.Body["position"]))
		return map[string]any{"ok": st.ACP.DropQueuedPrompt(connID, index)}, nil
	})

	// companion_ask_user_question parks the companion's ask_user_question tool
	// call until the user answers (a local route: upstream keeps this bridge
	// inside codeg-mcp, which this build owns as cmd/gocode-mcp).
	reg.web.Register("companion_ask_user_question", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		connID := strVal(c.Body["sessionId"], c.Body["session_id"], c.Body["connectionId"], c.Body["connection_id"])
		if connID == "" {
			return nil, web.ErrInvalidInput("sessionId required")
		}
		question := strVal(c.Body["question"], c.Body["prompt"])
		if question == "" {
			return nil, web.ErrInvalidInput("question required")
		}
		q := map[string]any{"id": "q1", "question": question}
		if opts, ok := c.Body["options"].([]any); ok && len(opts) > 0 {
			q["options"] = opts
		}
		if multi, ok := c.Body["allowMultiple"].(bool); ok {
			q["allow_multiple"] = multi
		}
		questions, _ := json.Marshal([]any{q})
		answer, err := st.ACP.AskUser(connID, questions)
		if err != nil {
			if errors.Is(err, acp.ErrNoLiveSession) {
				return nil, web.ErrNotFound(err.Error())
			}
			return nil, web.ErrTaskExecution(err.Error())
		}
		return map[string]any{"answer": answer}, nil
	})

	// companion_check_user_feedback delivers the turn's pending live-feedback
	// notes to the agent and marks them read (the companion's
	// `check_user_feedback` tool calls this; the pull channel — see
	// acp/feedback.go). A local route: upstream keeps it inside codeg-mcp.
	reg.web.Register("companion_check_user_feedback", func(c *web.Context) (any, error) {
		st := c.State.(*appstate.AppState)
		connID := strVal(c.Body["sessionId"], c.Body["session_id"], c.Body["connectionId"], c.Body["connection_id"])
		if connID == "" {
			return nil, web.ErrInvalidInput("sessionId required")
		}
		notes := st.ACP.CheckUserFeedback(connID)
		out := make([]map[string]any, len(notes))
		for i, n := range notes {
			item := map[string]any{
				"id":         n.ID,
				"text":       n.Text,
				"created_at": n.CreatedAt,
				"status":     n.Status,
			}
			if n.DeliveredAt != "" {
				item["delivered_at"] = n.DeliveredAt
			}
			out[i] = item
		}
		return map[string]any{"notes": out}, nil
	})
}

// connectionInfo renders acp::types::ConnectionInfo { id, agent_type, status }.
func connectionInfo(conn *acp.Connection) map[string]any {
	return map[string]any{
		"id":         conn.ID,
		"agent_type": conn.AgentType,
		"status":     conn.Status,
	}
}

// acpPromptBlocks builds the ACP prompt content blocks from the request body:
// explicit "blocks" pass through; otherwise a "prompt" string is wrapped as a
// single text block.
func acpPromptBlocks(c *web.Context) []any {
	if raw, ok := c.Body["blocks"].([]any); ok && len(raw) > 0 {
		return raw
	}
	if p := strVal(c.Body["prompt"]); p != "" {
		return []any{map[string]any{"type": "text", "text": p}}
	}
	return nil
}

// acpError maps the engine's typed errors onto the HTTP error contract.
func acpError(err error) error {
	var unavailable *acp.AgentUnavailableError
	if errors.As(err, &unavailable) {
		return web.ErrDependencyMissing(err.Error())
	}
	var spawn *acp.SpawnError
	if errors.As(err, &spawn) {
		return web.ErrDependencyMissing(err.Error())
	}
	var unknown *acp.ConnectionUnknownError
	if errors.As(err, &unknown) {
		return web.ErrNotFound(err.Error())
	}
	// Probe-specific failures (timeout, or the agent's own handshake error)
	// surface as external_command so the client can tell the user the agent
	// process failed rather than the request being malformed.
	var probeTimedOut *acp.ProbeTimedOutError
	if errors.As(err, &probeTimedOut) {
		return web.ErrExternalCommand(err.Error())
	}
	var probeAgent *acp.ProbeAgentError
	if errors.As(err, &probeAgent) {
		return web.ErrExternalCommand(err.Error())
	}
	return web.ErrTaskExecution(err.Error())
}

// stringMap reads a JSON object of string→string (upstream's
// BTreeMap<String, String> params, e.g. preferredConfigValues). Anything that is
// not an object of strings is reported as absent rather than coerced.
func stringMap(values ...any) map[string]string {
	for _, v := range values {
		raw, ok := v.(map[string]any)
		if !ok {
			continue
		}
		out := map[string]string{}
		for k, val := range raw {
			switch s := val.(type) {
			case string:
				out[k] = s
			case float64:
				out[k] = strconv.FormatFloat(s, 'f', -1, 64)
			case bool:
				out[k] = strconv.FormatBool(s)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}
