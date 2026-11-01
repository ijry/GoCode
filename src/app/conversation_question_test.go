package app

// conversation_question_test.go covers the agent's pending question card: preset
// options answer in one click, and a free-form question takes typed text.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func questionSnapshot(questions string) json.RawMessage {
	return json.RawMessage(`{"connection_id":"c1","status":"prompting","active_tool_calls":[],
		"pending_question":{"request_id":"r1","questions":` + questions + `}}`)
}

func TestConversationQuestionOptions(t *testing.T) {
	s := conversationShellForTest(t)
	s.cache["snapshot:c1"] = questionSnapshot(`[{"id":"q1","question":"which branch?","options":["main","dev"]}]`)

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	for _, want := range []string{"which branch?", "main", "dev"} {
		if !tt.HasText(want) {
			t.Fatalf("question card missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "conversation-question")

	if err := tt.Click("main"); err != nil {
		t.Fatalf("click option: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("answer sent") }) {
		t.Fatalf("no answer toast; texts = %v", tt.Texts())
	}
}

func TestConversationQuestionFreeText(t *testing.T) {
	s := conversationShellForTest(t)
	s.cache["snapshot:c1"] = questionSnapshot(`[{"id":"q1","question":"what should I name it?"}]`)

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if !tt.HasText("what should I name it?") {
		t.Fatalf("question text missing; texts = %v", tt.Texts())
	}
	s.questionAnswer = "gocode"
	tt.Frame()
	if err := tt.Click("Answer"); err != nil {
		t.Fatalf("click answer: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("answer sent") }) {
		t.Fatalf("no answer toast; texts = %v", tt.Texts())
	}
}
