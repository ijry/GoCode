package app

// conversation_async_test.go covers the background-task strip: one row per
// task the agent reported, its progress evidence, and the Stop button.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestConversationAsyncTaskStrip(t *testing.T) {
	s := conversationShellForTest(t)
	s.cache["snapshot:c1"] = json.RawMessage(`{"connection_id":"c1","status":"prompting",
		"active_tool_calls":[],"async_tasks":[
		{"async_task_id":"t1","name":"review PR","state":"running","phase":"diff review",
		 "last_tool_name":"read_file","can_stop":true,"output_file_path":"D:/out/review.md"},
		{"async_task_id":"t2","name":"index repo","state":"paused"}]}`)

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	for _, want := range []string{"review PR", "diff review · read_file", "index repo", "Stop", "Open output"} {
		if !tt.HasText(want) {
			t.Fatalf("strip missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "conversation-async-tasks")

	// A task without can_stop offers no Stop for it; the first row's Stop goes
	// through the adapter and reports its verdict.
	if err := tt.Click("Stop"); err != nil {
		t.Fatalf("click Stop: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		return tt.HasText("task stopped") || tt.HasText("the agent declined to stop the task")
	}) {
		t.Fatalf("no stop verdict toast; texts = %v", tt.Texts())
	}
}

func TestConversationNoAsyncTasksWhenIdle(t *testing.T) {
	s := conversationShellForTest(t) // snapshot has no async_tasks
	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if tt.HasText("Open output") || tt.HasText("task stopped") {
		t.Fatalf("strip shown without tasks; texts = %v", tt.Texts())
	}
}
