package app

// canvas_test.go covers the Canvas workbench (UI_GAPS P1-7): the empty state and
// create round-trip, the board list's search and row menu, opening a board to
// see its nodes (and deleting one), and the board delete confirmation.

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// canvasShellForTest boots the real backend, creates the named boards, and
// returns a Shell on the Canvas route with the board-list cache seeded.
func canvasShellForTest(t *testing.T, names ...string) (*Shell, *Backend, []string) {
	t.Helper()
	b := newBackendForTest(t)
	ids := make([]string, 0, len(names))
	for _, n := range names {
		raw, err := b.raw("canvas_create_board", map[string]any{"input": map[string]any{
			"name": n, "description": n + " description", "color": "blue",
		}})
		if err != nil {
			t.Fatalf("create board %q: %v", n, err)
		}
		var board map[string]any
		_ = json.Unmarshal(raw, &board)
		ids = append(ids, fmt.Sprintf("%v", board["id"]))
	}
	s := New(b)
	s.stream = newStreamForTest()
	s.workbench = "canvas"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	listRaw, err := b.raw("canvas_list_boards", map[string]any{})
	if err != nil {
		t.Fatalf("canvas_list_boards: %v", err)
	}
	s.cache["canvas-boards"] = listRaw
	return s, b, ids
}

// TestCanvasEmptyThenCreate walks the empty state into the create form.
func TestCanvasEmptyThenCreate(t *testing.T) {
	s, _, _ := canvasShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("No canvas boards yet") {
		t.Fatalf("empty state missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("New board"); err != nil {
		t.Fatalf("click New board: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Save") || !tt.HasText("Colour") {
		t.Fatalf("form not shown; texts = %v", tt.Texts())
	}
	s.canvasEditor.name = "My board"
	s.canvasEditor.desc = "notes and terminals"
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("click Save: %v", err)
	}
	if !waitText(t, tt, "My board", true) {
		t.Fatalf("created board not listed; texts = %v", tt.Texts())
	}
	if !tt.HasText("0 node(s) · 0 terminal(s)") {
		t.Errorf("board row missing counts; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "canvas-created")
}

// TestCanvasBoardListAndSearch checks the search filter and the row menu.
func TestCanvasBoardListAndSearch(t *testing.T) {
	s, _, _ := canvasShellForTest(t, "Alpha board", "Beta board")
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{"Alpha board", "Beta board"} {
		if !tt.HasText(want) {
			t.Fatalf("board list missing %q; texts = %v", want, tt.Texts())
		}
	}
	// Search narrows to the matching board.
	s.canvasSearch = "alpha"
	tt.Frame()
	if !tt.HasText("Alpha board") {
		t.Errorf("search dropped the match; texts = %v", tt.Texts())
	}
	if tt.HasText("Beta board") {
		t.Errorf("search kept the non-match; texts = %v", tt.Texts())
	}
	s.canvasSearch = ""
	tt.Frame()

	// The row menu offers open / rename / delete.
	if err := tt.RightClick("Alpha board"); err != nil {
		t.Fatalf("right-click board: %v", err)
	}
	if got, want := tt.Menu(), []string{"Open", "Rename", "-", "Delete"}; !slices.Equal(got, want) {
		t.Fatalf("menu %v, want %v", got, want)
	}
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatalf("choose Rename: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Rename board") || !tt.HasText("Save") {
		t.Fatalf("rename form not shown; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "canvas-list")
}

// TestCanvasOpenBoardNodes opens a board and deletes one of its nodes.
func TestCanvasOpenBoardNodes(t *testing.T) {
	s, b, ids := canvasShellForTest(t, "Node board")
	boardID := ids[0]
	raw, err := b.raw("canvas_create_node", map[string]any{"input": map[string]any{
		"kind": "note", "board_id": numOrString(boardID), "title": "Idea",
		"content": "hello", "x": 10, "y": 20, "width": 224, "height": 132,
	}})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	var mut map[string]any
	_ = json.Unmarshal(raw, &mut)
	node := asObjVal(mut["value"])
	nodeID := fmt.Sprintf("%v", node["id"])
	nodesRaw, err := b.raw("canvas_list_nodes", map[string]any{"boardId": numOrString(boardID)})
	if err != nil {
		t.Fatalf("canvas_list_nodes: %v", err)
	}
	s.cache["canvas-nodes:"+boardID] = nodesRaw

	tt := ui.NewTester(s.View, 1240, 820)
	// Open the board.
	if err := tt.Click("Node board"); err != nil {
		t.Fatalf("click board: %v", err)
	}
	tt.Frame()
	for _, want := range []string{"Back to boards", "note", "Idea"} {
		if !tt.HasText(want) {
			t.Fatalf("board view missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "canvas-board")
	// Delete the node → the board empties.
	if err := tt.Click("Delete"); err != nil {
		t.Fatalf("click node Delete: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("This board has no nodes yet.") }) {
		t.Fatalf("node delete not reflected; texts = %v", tt.Texts())
	}
	_ = nodeID
}

// TestCanvasDeleteBoard drives the row menu's delete through the confirmation.
func TestCanvasDeleteBoard(t *testing.T) {
	s, _, _ := canvasShellForTest(t, "Doomed board")
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Doomed board") {
		t.Fatalf("precondition: board missing; texts = %v", tt.Texts())
	}
	if err := tt.RightClick("Doomed board"); err != nil {
		t.Fatalf("right-click: %v", err)
	}
	if err := tt.ChooseMenuItem("Delete"); err != nil {
		t.Fatalf("choose Delete: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Delete this board and its nodes?") {
		t.Fatalf("confirmation not shown; texts = %v", tt.Texts())
	}
	if err := tt.Click("Delete"); err != nil {
		t.Fatalf("confirm Delete: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("No canvas boards yet") }) {
		t.Fatalf("board delete not reflected; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "canvas-deleted")
}
