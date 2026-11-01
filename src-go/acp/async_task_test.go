package acp

// async_task_test.go covers the background-task strip's data path: parsing the
// ACP async_task_* updates, folding them into a list (spawn appends, progress
// patches, terminal removes), and exposing it on the snapshot.

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestParseAsyncTaskUpdate(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantOK  bool
		spawned bool
		id      string
		state   string
		canStop bool
	}{
		{"spawn", `{"sessionUpdate":"async_task_spawned","asyncTaskId":"t1","name":"deep scan","canStop":true}`, true, true, "t1", "", true},
		{"progress", `{"sessionUpdate":"async_task_progress","asyncTaskId":"t1","state":"running","lastToolName":"grep"}`, true, false, "t1", "running", false},
		{"state", `{"sessionUpdate":"async_task_state_update","asyncTaskId":"t1","state":"completed"}`, true, false, "t1", "completed", false},
		{"other update", `{"sessionUpdate":"agent_message_chunk"}`, false, false, "", "", false},
		{"no id", `{"sessionUpdate":"async_task_progress"}`, false, false, "", "", false},
		{"garbage", `not json`, false, false, "", "", false},
	}
	for _, c := range cases {
		d, ok := parseAsyncTaskUpdate(json.RawMessage(c.raw))
		if ok != c.wantOK {
			t.Fatalf("%s: ok = %v, want %v", c.name, ok, c.wantOK)
		}
		if !ok {
			continue
		}
		if d.Spawned != c.spawned || d.Task.ID != c.id || d.Task.State != c.state || d.Task.CanStop != c.canStop {
			t.Fatalf("%s: delta = %+v", c.name, d)
		}
	}
}

func TestMergeAsyncTask(t *testing.T) {
	// A spawn appends; later updates patch only the fields they carry, so a
	// progress frame without a name cannot wipe it.
	tasks := mergeAsyncTask(nil, asyncTaskDelta{Task: AsyncTaskState{ID: "t1", Name: "scan", CanStop: true}})
	tasks = mergeAsyncTask(tasks, asyncTaskDelta{Task: AsyncTaskState{ID: "t1", State: "running", LastToolName: "grep"}})
	if len(tasks) != 1 || tasks[0].Name != "scan" || tasks[0].State != "running" ||
		tasks[0].LastToolName != "grep" || !tasks[0].CanStop {
		t.Fatalf("after progress = %+v", tasks)
	}

	// A terminal state removes the task from the strip.
	tasks = mergeAsyncTask(tasks, asyncTaskDelta{Task: AsyncTaskState{ID: "t1", State: "completed"}})
	if len(tasks) != 0 {
		t.Fatalf("terminal state left %+v", tasks)
	}
	// A terminal frame for an unknown task is a no-op, not an append.
	if got := mergeAsyncTask(tasks, asyncTaskDelta{Task: AsyncTaskState{ID: "x", State: "failed"}}); len(got) != 0 {
		t.Fatalf("unknown terminal task appended: %+v", got)
	}
}

func TestAsyncTasksInSnapshot(t *testing.T) {
	client, agent, done := pipePair(t)
	defer done()
	agent.HandleRequest("initialize", func(json.RawMessage) (any, error) {
		return map[string]any{"protocolVersion": ACPProtocolVersion}, nil
	})
	agent.HandleRequest("session/new", func(json.RawMessage) (any, error) {
		return map[string]any{"sessionId": "sess-a"}, nil
	})

	state := NewSessionState("conn-a")
	sess := NewAgentSession(client, state, "claude_code")
	if err := sess.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := sess.NewSession(t.Context(), "/tmp/x", nil); err != nil {
		t.Fatalf("new session: %v", err)
	}

	_ = agent.Notify("session/update", map[string]any{
		"sessionId": "sess-a",
		"update": map[string]any{
			"sessionUpdate": "async_task_spawned", "asyncTaskId": "t1",
			"name": "review PR", "state": "running", "canStop": true,
		},
	})
	_ = agent.Notify("session/update", map[string]any{
		"sessionId": "sess-a",
		"update": map[string]any{
			"sessionUpdate": "async_task_progress", "asyncTaskId": "t1",
			"lastToolName": "read_file",
		},
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		tasks := state.Snapshot().AsyncTasks
		if len(tasks) == 1 && tasks[0].LastToolName == "read_file" {
			if tasks[0].Name != "review PR" || !tasks[0].CanStop {
				t.Fatalf("task = %+v", tasks[0])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("async tasks not folded: %+v", state.Snapshot().AsyncTasks)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStopAsyncTaskUsesExtension(t *testing.T) {
	client, agent, done := pipePair(t)
	defer done()
	stopped := ""
	agent.HandleRequest("initialize", func(json.RawMessage) (any, error) {
		return map[string]any{"protocolVersion": ACPProtocolVersion}, nil
	})
	agent.HandleRequest("session/new", func(json.RawMessage) (any, error) {
		return map[string]any{"sessionId": "sess-s"}, nil
	})
	agent.HandleRequest("_session/async_task/stop", func(params json.RawMessage) (any, error) {
		var p struct {
			AsyncTaskID string `json:"asyncTaskId"`
		}
		_ = json.Unmarshal(params, &p)
		stopped = p.AsyncTaskID
		return map[string]any{"stopped": true}, nil
	})

	state := NewSessionState("conn-s")
	sess := NewAgentSession(client, state, "claude_code")
	if err := sess.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := sess.NewSession(t.Context(), "/tmp/x", nil); err != nil {
		t.Fatalf("new session: %v", err)
	}

	m := NewManager(nil, nil)
	m.live["conn-s"] = &liveSession{session: sess, state: state}
	if !m.StopAsyncTask("conn-s", "t9") {
		t.Fatal("stop reported failure")
	}
	if stopped != "t9" {
		t.Fatalf("adapter saw task %q, want t9", stopped)
	}
	// An unknown connection and an empty id are refused without a call.
	if m.StopAsyncTask("nope", "t9") || m.StopAsyncTask("conn-s", "") {
		t.Fatal("stop accepted an invalid request")
	}
}

func TestStopAsyncTaskDeclined(t *testing.T) {
	client, agent, done := pipePair(t)
	defer done()
	agent.HandleRequest("initialize", func(json.RawMessage) (any, error) {
		return map[string]any{"protocolVersion": ACPProtocolVersion}, nil
	})
	agent.HandleRequest("session/new", func(json.RawMessage) (any, error) {
		return map[string]any{"sessionId": "sess-d"}, nil
	})
	// The adapter does not implement the extension: a JSON-RPC error.
	agent.HandleRequest("_session/async_task/stop", func(json.RawMessage) (any, error) {
		return nil, context.DeadlineExceeded
	})

	state := NewSessionState("conn-d")
	sess := NewAgentSession(client, state, "claude_code")
	if err := sess.Initialize(t.Context()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := sess.NewSession(t.Context(), "/tmp/x", nil); err != nil {
		t.Fatalf("new session: %v", err)
	}
	m := NewManager(nil, nil)
	m.live["conn-d"] = &liveSession{session: sess, state: state}
	if m.StopAsyncTask("conn-d", "t1") {
		t.Fatal("a declined stop reported success")
	}
}
