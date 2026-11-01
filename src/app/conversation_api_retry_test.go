package app

// conversation_api_retry_test.go covers the composer's in-flight API-retry line:
// the cause, HTTP status, attempt counter and next-attempt delay.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestConversationApiRetryLine(t *testing.T) {
	s := conversationShellForTest(t)
	s.cache["snapshot:c1"] = json.RawMessage(`{"connection_id":"c1","status":"prompting","active_tool_calls":[],
		"api_retry":{"attempt":2,"max_retries":5,"error":"rate limited","error_status":429,"retry_delay_ms":3000}}`)

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if want := "rate limited · HTTP 429 · attempt 2/5 · next in 3.0s"; !tt.HasText(want) {
		t.Fatalf("retry line missing %q; texts = %v", want, tt.Texts())
	}
	dumpFrame(t, tt, "conversation-api-retry")
}

func TestConversationNoApiRetryWhenIdle(t *testing.T) {
	s := conversationShellForTest(t) // snapshot has no api_retry
	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if tt.HasText("HTTP 429") {
		t.Fatalf("retry line shown without a retry; texts = %v", tt.Texts())
	}
}
