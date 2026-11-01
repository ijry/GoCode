package app

// conversation_history_test.go covers the transcript's history paging: the
// route pages backwards from beforeIndex and floors a missing limit at one
// turn, so the client must send a limit and offer "Load earlier messages"
// while turns_offset is above zero.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestParseTurnsPage(t *testing.T) {
	first := json.RawMessage(`{"turns":[{"role":"user"},{"role":"assistant"}],"turns_offset":2,"turns_total":5}`)
	turns, offset, total := parseTurnsPage(first, nil)
	if len(turns) != 2 || offset != 2 || total != 5 {
		t.Fatalf("first page = (%d turns, offset %d, total %d), want (2, 2, 5)", len(turns), offset, total)
	}

	older := json.RawMessage(`{"turns":[{"role":"user"},{"role":"assistant"}],"turns_offset":0,"turns_total":5}`)
	turns, offset, total = parseTurnsPage(older, turns)
	if len(turns) != 4 || offset != 0 || total != 5 {
		t.Fatalf("merged = (%d turns, offset %d, total %d), want (4, 0, 5)", len(turns), offset, total)
	}
	// The older page must come first.
	if role := asObjVal(turns[0])["role"]; role != "user" {
		t.Fatalf("merged page starts with %v", role)
	}

	// A malformed body keeps the previous page.
	if got, _, _ := parseTurnsPage(json.RawMessage(`not json`), turns); len(got) != 4 {
		t.Fatalf("malformed page dropped the loaded turns")
	}
}

func TestConversationLoadEarlierRow(t *testing.T) {
	s := conversationShellForTest(t)
	// Pretend the newest page was loaded and older turns remain. convTurnsConv
	// must match the open conversation or the view resets the page state.
	s.convTurnsConv = "1"
	s.convTurnsPage = []any{
		map[string]any{"role": "user", "blocks": []any{map[string]any{"type": "text", "text": "newest"}}},
	}
	s.convTurnsOffset = 3
	s.convTurnsTotal = 9

	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Load earlier messages") {
		t.Fatalf("no load-earlier row; texts = %v", tt.Texts())
	}
	if !tt.HasText("newest") {
		t.Fatalf("transcript missing the loaded turn; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "conversation-history")

	// Clicking fetches the page before the earliest turn; with no transcript on
	// disk the backend answers with an empty page, so the row goes away.
	if err := tt.Click("Load earlier messages"); err != nil {
		t.Fatalf("click load earlier: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return !tt.HasText("Load earlier messages") }) {
		t.Fatalf("load-earlier row did not clear; texts = %v", tt.Texts())
	}
}

// TestConversationTurnsRequestShape pins the history request: the route floors
// a missing limit at one turn, so the transcript must ask for a page, and the
// "load earlier" request must page backwards from the earliest loaded turn.
func TestConversationTurnsRequestShape(t *testing.T) {
	type call struct {
		cmd  string
		body map[string]any
	}
	calls := []call{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		cmd := strings.TrimPrefix(r.URL.Path, "/api/")
		calls = append(calls, call{cmd: cmd, body: body})
		switch len(calls) {
		case 1:
			// Newest page: two turns loaded, three older ones remain.
			_, _ = w.Write([]byte(`{"turns":[{"role":"user"},{"role":"assistant"}],"turns_offset":3,"turns_total":5}`))
		default:
			_, _ = w.Write([]byte(`{"turns":[{"role":"user"}],"turns_offset":2,"turns_total":5}`))
		}
	}))
	defer srv.Close()

	s := New(&Backend{URL: srv.URL, Token: "tok"})
	s.stream = newStreamForTest()
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)

	// ensure() fetches in the background, so the first call returns nil and the
	// page lands on a later frame.
	var turns []any
	for i := 0; i < 200 && len(turns) == 0; i++ {
		turns = s.conversationTurns(nil, "7")
		if len(turns) == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if len(turns) != 2 {
		t.Fatalf("first page = %d turns, want 2", len(turns))
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 request, got %d", len(calls))
	}
	if got := intVal(calls[0].body, "limit"); got != convTurnsPageSize {
		t.Fatalf("first page asked for limit %d, want %d", got, convTurnsPageSize)
	}
	if _, ok := calls[0].body["beforeIndex"]; ok {
		t.Fatalf("first page must not send beforeIndex: %v", calls[0].body)
	}
	if s.convTurnsOffset != 3 || s.convTurnsTotal != 5 {
		t.Fatalf("offset/total = %d/%d, want 3/5", s.convTurnsOffset, s.convTurnsTotal)
	}
}

// TestHistoricalTurnRendersBlocks locks the shape every transcript reader
// produces: prose blocks read as text, `tool_use` blocks become tool cards. If
// this drifts, history from all 15 agents renders blank.
func TestHistoricalTurnRendersBlocks(t *testing.T) {
	s := conversationShellForTest(t)
	s.convTurnsConv = "1"
	s.convTurnsPage = []any{
		map[string]any{"role": "user", "blocks": []any{
			map[string]any{"type": "text", "text": "跑一下构建"},
		}},
		map[string]any{"role": "assistant", "blocks": []any{
			map[string]any{"type": "text", "text": "先看看。"},
			map[string]any{"type": "tool_use", "tool_name": "Bash", "tool_use_id": "call_1",
				"input_preview": `{"command":"pnpm build"}`},
			map[string]any{"type": "text", "text": "好的。"},
		}},
	}
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{"跑一下构建", "先看看。", "好的。", "Bash"} {
		if !tt.HasText(want) {
			t.Errorf("transcript missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "history-tool-card")
}
