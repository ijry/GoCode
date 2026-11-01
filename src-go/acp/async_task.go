package acp

// async_task.go tracks the background tasks an agent reports over ACP
// (`async_task_spawned` / `async_task_progress` / `async_task_state_update` on
// session/update) and stops one through the adapter's
// `_session/async_task/stop` extension request. Upstream's async-task strip is
// the UI for exactly this; without it a turn that only ran background work
// looks like an agent that said nothing.

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// AsyncTaskState is one background task as the agent last reported it.
type AsyncTaskState struct {
	ID             string `json:"async_task_id"`
	Name           string `json:"name,omitempty"`
	TaskType       string `json:"task_type,omitempty"`
	Description    string `json:"description,omitempty"`
	State          string `json:"state,omitempty"`
	Summary        string `json:"summary,omitempty"`
	LastToolName   string `json:"last_tool_name,omitempty"`
	CanStop        bool   `json:"can_stop"`
	OutputFilePath string `json:"output_file_path,omitempty"`
}

// asyncTaskTerminal reports whether a task is done (so it leaves the strip).
func asyncTaskTerminal(state string) bool {
	switch state {
	case "completed", "failed", "cancelled", "canceled", "stopped":
		return true
	}
	return false
}

// asyncTaskDelta is the subset of an async-task frame this build reads.
type asyncTaskDelta struct {
	Spawned bool
	Task    AsyncTaskState
}

// parseAsyncTaskUpdate decodes an async-task session/update, or returns ok=false
// for any other update.
func parseAsyncTaskUpdate(raw json.RawMessage) (asyncTaskDelta, bool) {
	var u struct {
		SessionUpdate  string `json:"sessionUpdate"`
		AsyncTaskID    string `json:"asyncTaskId"`
		Name           string `json:"name"`
		TaskType       string `json:"taskType"`
		Description    string `json:"description"`
		State          string `json:"state"`
		Summary        string `json:"summary"`
		LastToolName   string `json:"lastToolName"`
		CanStop        *bool  `json:"canStop"`
		OutputFilePath string `json:"outputFilePath"`
	}
	if json.Unmarshal(raw, &u) != nil || u.AsyncTaskID == "" {
		return asyncTaskDelta{}, false
	}
	var d asyncTaskDelta
	switch u.SessionUpdate {
	case "async_task_spawned":
		d.Spawned = true
	case "async_task_progress", "async_task_state_update":
	default:
		return asyncTaskDelta{}, false
	}
	d.Task = AsyncTaskState{
		ID: u.AsyncTaskID, Name: u.Name, TaskType: u.TaskType,
		Description: u.Description, State: u.State, Summary: u.Summary,
		LastToolName: u.LastToolName, OutputFilePath: u.OutputFilePath,
	}
	if u.CanStop != nil {
		d.Task.CanStop = *u.CanStop
	}
	return d, true
}

// mergeAsyncTask folds a delta into the task list: spawns append, later updates
// patch the fields they carry (blank means "unchanged", so a whitespace-only
// summary cannot wipe a good one), and a terminal state removes the task.
func mergeAsyncTask(tasks []AsyncTaskState, d asyncTaskDelta) []AsyncTaskState {
	for i := range tasks {
		if tasks[i].ID != d.Task.ID {
			continue
		}
		t := tasks[i]
		if d.Task.Name != "" {
			t.Name = d.Task.Name
		}
		if d.Task.TaskType != "" {
			t.TaskType = d.Task.TaskType
		}
		if d.Task.Description != "" {
			t.Description = d.Task.Description
		}
		if d.Task.State != "" {
			t.State = d.Task.State
		}
		if d.Task.Summary != "" {
			t.Summary = d.Task.Summary
		}
		if d.Task.LastToolName != "" {
			t.LastToolName = d.Task.LastToolName
		}
		if d.Task.OutputFilePath != "" {
			t.OutputFilePath = d.Task.OutputFilePath
		}
		if d.Task.CanStop {
			t.CanStop = true
		}
		if asyncTaskTerminal(t.State) {
			return append(tasks[:i], tasks[i+1:]...)
		}
		tasks[i] = t
		return tasks
	}
	if asyncTaskTerminal(d.Task.State) {
		return tasks
	}
	return append(tasks, d.Task)
}

// StopAsyncTask asks the adapter to stop a background task
// (`_session/async_task/stop`). It reports the adapter's own verdict.
func (m *Manager) StopAsyncTask(connID, taskID string) bool {
	m.mu.Lock()
	ls := m.live[connID]
	m.mu.Unlock()
	if ls == nil || taskID == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := ls.session.CallExtension(ctx, "_session/async_task/stop", map[string]any{
		"sessionId":   ls.session.SessionID(),
		"asyncTaskId": taskID,
	})
	if err != nil {
		var unknown *ConnectionUnknownError
		_ = errors.As(err, &unknown)
		return false
	}
	var out struct {
		Stopped bool `json:"stopped"`
	}
	_ = json.Unmarshal(res, &out)
	return out.Stopped
}
