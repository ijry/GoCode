package app

// canvas_test.go covers the Canvas workbench (UI_GAPS P1-7): the empty state and
// create round-trip, the board list's search and row menu, opening a board to
// see its nodes (and deleting one), and the board delete confirmation.

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

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

// TestCanvasNodeDragAndZoom drives the spatial surface: dragging a card
// persists its new spot through canvas_move_nodes, and the header controls
// zoom and reset the view.
func TestCanvasNodeDragAndZoom(t *testing.T) {
	s, b, ids := canvasShellForTest(t, "Drag board")
	boardID := ids[0]
	if _, err := b.raw("canvas_create_node", map[string]any{"input": map[string]any{
		"kind": "note", "board_id": numOrString(boardID), "title": "Draggable",
		"content": "move me", "x": 40, "y": 60, "width": 200, "height": 120,
	}}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	nodesRaw, err := b.raw("canvas_list_nodes", map[string]any{"boardId": numOrString(boardID)})
	if err != nil {
		t.Fatalf("canvas_list_nodes: %v", err)
	}
	s.cache["canvas-nodes:"+boardID] = nodesRaw

	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Drag board"); err != nil {
		t.Fatalf("open board: %v", err)
	}
	tt.Frame()

	// Zoom controls: the header shows the surface zoom.
	if !tt.HasText("100%") {
		t.Fatalf("zoom label missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("+"); err != nil {
		t.Fatalf("zoom in: %v", err)
	}
	tt.Frame()
	if !tt.HasText("120%") {
		t.Fatalf("zoom in did not apply; texts = %v", tt.Texts())
	}
	if err := tt.Click("Reset view"); err != nil {
		t.Fatalf("reset view: %v", err)
	}
	tt.Frame()
	if !tt.HasText("100%") {
		t.Fatalf("reset did not restore 100%%; texts = %v", tt.Texts())
	}

	// Drag the card 60 right and 40 down; the move persists to the backend.
	rect, ok := tt.Find("Draggable")
	if !ok {
		t.Fatalf("card not found; texts = %v", tt.Texts())
	}
	cx, cy := rect.X+rect.W/2, rect.Y+rect.H/2
	tt.Press(cx, cy)
	tt.Frame()
	tt.Move(cx+30, cy+20)
	tt.Frame()
	tt.Move(cx+60, cy+40)
	tt.Frame()
	tt.Release(cx+60, cy+40)
	tt.Frame()

	deadline := time.Now().Add(3 * time.Second)
	for {
		raw, err := b.raw("canvas_list_nodes", map[string]any{"boardId": numOrString(boardID)})
		if err != nil {
			t.Fatalf("list nodes: %v", err)
		}
		var snap map[string]any
		_ = json.Unmarshal(raw, &snap)
		nodes, _ := snap["nodes"].([]any)
		if len(nodes) == 1 {
			n, _ := nodes[0].(map[string]any)
			x, _ := n["x"].(float64)
			y, _ := n["y"].(float64)
			if x == 100 && y == 100 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("drag did not persist: x=%v y=%v", x, y)
			}
		} else if time.Now().After(deadline) {
			t.Fatalf("nodes changed shape: %v", snap)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestCanvasPanAndNewNote pans the surface by dragging empty space and adds a
// note card from the header.
func TestCanvasPanAndNewNote(t *testing.T) {
	s, b, ids := canvasShellForTest(t, "Pan board")
	boardID := ids[0]
	if _, err := b.raw("canvas_create_node", map[string]any{"input": map[string]any{
		"kind": "note", "board_id": numOrString(boardID), "title": "Anchor",
		"x": 40, "y": 60, "width": 200, "height": 120,
	}}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	nodesRaw, err := b.raw("canvas_list_nodes", map[string]any{"boardId": numOrString(boardID)})
	if err != nil {
		t.Fatalf("canvas_list_nodes: %v", err)
	}
	s.cache["canvas-nodes:"+boardID] = nodesRaw

	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Pan board"); err != nil {
		t.Fatalf("open board: %v", err)
	}
	tt.Frame()
	before, ok := tt.Find("Anchor")
	if !ok {
		t.Fatalf("card not found; texts = %v", tt.Texts())
	}

	dumpFrame(t, tt, "canvas-board-spatial")
	// Background drag pans in pan mode (in select mode it marquees).
	if err := tt.Click("Pan"); err != nil {
		t.Fatalf("switch to pan mode: %v", err)
	}
	tt.Frame()
	// Drag the empty background: the whole surface pans, so the card follows.
	tt.Press(before.X-20, before.Y+200)
	tt.Frame()
	tt.Move(before.X+30, before.Y+240)
	tt.Frame()
	tt.Release(before.X+30, before.Y+240)
	tt.Frame()
	after, ok := tt.Find("Anchor")
	if !ok {
		t.Fatalf("card lost after pan; texts = %v", tt.Texts())
	}
	if dx, dy := after.X-before.X, after.Y-before.Y; dx != 50 || dy != 40 {
		t.Fatalf("pan moved the card by (%v,%v), want (50,40)", dx, dy)
	}

	// New note lands on the board.
	if err := tt.Click("New note"); err != nil {
		t.Fatalf("new note: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		raw, err := b.raw("canvas_list_nodes", map[string]any{"boardId": numOrString(boardID)})
		if err != nil {
			t.Fatalf("list nodes: %v", err)
		}
		var snap map[string]any
		_ = json.Unmarshal(raw, &snap)
		if nodes, _ := snap["nodes"].([]any); len(nodes) == 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("note not created: %v", snap)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestCanvasMarqueeSelectMoveAndDelete covers the multi-select path: a marquee
// over two of three cards selects them, dragging one moves both (one
// canvas_move_nodes call with two entries), and Delete selected removes both.
func TestCanvasMarqueeSelectMoveAndDelete(t *testing.T) {
	s, b, ids := canvasShellForTest(t, "Multi board")
	boardID := ids[0]
	seeds := []struct {
		title string
		x, y  float64
	}{
		{"Alpha", 40, 40},
		{"Beta", 320, 40},
		{"Gamma", 40, 400},
	}
	for _, seed := range seeds {
		if _, err := b.raw("canvas_create_node", map[string]any{"input": map[string]any{
			"kind": "note", "board_id": numOrString(boardID), "title": seed.title,
			"x": seed.x, "y": seed.y, "width": 200, "height": 100,
		}}); err != nil {
			t.Fatalf("create %s: %v", seed.title, err)
		}
	}
	nodesRaw, err := b.raw("canvas_list_nodes", map[string]any{"boardId": numOrString(boardID)})
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	s.cache["canvas-nodes:"+boardID] = nodesRaw

	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Multi board"); err != nil {
		t.Fatalf("open board: %v", err)
	}
	tt.Frame()

	alpha, ok := tt.Find("Alpha")
	if !ok {
		t.Fatalf("Alpha missing; texts = %v", tt.Texts())
	}
	beta, ok := tt.Find("Beta")
	if !ok {
		t.Fatalf("Beta missing; texts = %v", tt.Texts())
	}
	// Marquee from above-left of Alpha to below-right of Beta, missing Gamma.
	tt.Press(alpha.X-10, alpha.Y-10)
	tt.Frame()
	tt.Move(beta.X+210, alpha.Y+140)
	tt.Frame()
	tt.Release(beta.X+210, alpha.Y+140)
	tt.Frame()
	if !tt.HasText("2 selected") {
		t.Fatalf("marquee selected %v; texts = %v", tt.Texts(), tt.Texts())
	}
	dumpFrame(t, tt, "canvas-board-selection")

	// Drag Alpha: both selected cards move together.
	tt.Press(alpha.X+60, alpha.Y+40)
	tt.Frame()
	tt.Move(alpha.X+110, alpha.Y+80)
	tt.Frame()
	tt.Release(alpha.X+110, alpha.Y+80)
	tt.Frame()

	deadline := time.Now().Add(3 * time.Second)
	for {
		snap := canvasNodesOf(t, b, boardID)
		byTitle := map[string][2]float64{}
		for _, n := range snap {
			x, _ := n["x"].(float64)
			y, _ := n["y"].(float64)
			byTitle[fmt.Sprintf("%v", n["title"])] = [2]float64{x, y}
		}
		if byTitle["Alpha"] == [2]float64{90, 80} && byTitle["Beta"] == [2]float64{370, 80} {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("group drag did not persist: %v", byTitle)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Gamma stayed put.
	if snap := canvasNodesOf(t, b, boardID); len(snap) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(snap))
	}
	if g := canvasNodeByTitle(t, b, boardID, "Gamma"); g[0] != 40 || g[1] != 400 {
		t.Fatalf("Gamma moved to %v, want (40,400)", g)
	}

	// Delete selected removes Alpha and Beta in one call.
	if err := tt.Click("Delete selected"); err != nil {
		t.Fatalf("delete selected: %v", err)
	}
	tt.Frame()
	deadline = time.Now().Add(3 * time.Second)
	for {
		if snap := canvasNodesOf(t, b, boardID); len(snap) == 1 {
			if title := fmt.Sprintf("%v", snap[0]["title"]); title != "Gamma" {
				t.Fatalf("wrong node survived: %v", title)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("batch delete did not apply")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestCanvasNodeResizeDrag resizes a selected card by dragging its bottom-right
// handle: the new size persists through canvas_update_node (width/height patch).
func TestCanvasNodeResizeDrag(t *testing.T) {
	s, b, ids := canvasShellForTest(t, "Resize board")
	boardID := ids[0]
	if _, err := b.raw("canvas_create_node", map[string]any{"input": map[string]any{
		"kind": "note", "board_id": numOrString(boardID), "title": "Sizable",
		"content": "resize me", "x": 40, "y": 60, "width": 200, "height": 120,
	}}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	nodesRaw, err := b.raw("canvas_list_nodes", map[string]any{"boardId": numOrString(boardID)})
	if err != nil {
		t.Fatalf("canvas_list_nodes: %v", err)
	}
	s.cache["canvas-nodes:"+boardID] = nodesRaw

	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Resize board"); err != nil {
		t.Fatalf("open board: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Sizable") {
		t.Fatalf("node missing; texts = %v", tt.Texts())
	}
	// Select the card so the resize handle appears.
	if err := tt.Click("Sizable"); err != nil {
		t.Fatalf("select node: %v", err)
	}
	tt.Frame()

	// The handle sits at the card's bottom-right corner. The card left edge is 8
	// DIPs right of the title (column padding), and the card bottom is exactly
	// 88 DIPs below the title's top (independent of text height), so the handle
	// centre is (title.X + width - 10, title.Y + 86) at 1:1 zoom.
	title, ok := tt.Find("Sizable")
	if !ok {
		t.Fatalf("title lost after select; texts = %v", tt.Texts())
	}
	hx := title.X + 200 - 10
	hy := title.Y + 86
	tt.Press(hx, hy)
	tt.Frame()
	tt.Move(hx+40, hy+30)
	tt.Frame()
	tt.Move(hx+80, hy+60)
	tt.Frame()
	tt.Release(hx+80, hy+60)
	tt.Frame()

	deadline := time.Now().Add(3 * time.Second)
	for {
		for _, n := range canvasNodesOf(t, b, boardID) {
			if fmt.Sprintf("%v", n["title"]) != "Sizable" {
				continue
			}
			w, _ := n["width"].(float64)
			h, _ := n["height"].(float64)
			if w == 280 && h == 180 {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("resize did not persist: %v", canvasNodesOf(t, b, boardID))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestCanvasWheelPanAndPinchZoom drives the surface through mygo 0.7's
// HandleInput: a touchpad scroll pans (a scroll container's direction), and a
// pinch zooms around the pointer — the world point under the pointer stays put.
func TestCanvasWheelPanAndPinchZoom(t *testing.T) {
	s, b, ids := canvasShellForTest(t, "Wheel board")
	boardID := ids[0]
	if _, err := b.raw("canvas_create_node", map[string]any{"input": map[string]any{
		"kind": "note", "board_id": numOrString(boardID), "title": "Anchor",
		"x": 40, "y": 60, "width": 200, "height": 100,
	}}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	nodesRaw, err := b.raw("canvas_list_nodes", map[string]any{"boardId": numOrString(boardID)})
	if err != nil {
		t.Fatalf("canvas_list_nodes: %v", err)
	}
	s.cache["canvas-nodes:"+boardID] = nodesRaw

	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Wheel board"); err != nil {
		t.Fatalf("open board: %v", err)
	}
	tt.Frame()
	rect, ok := tt.Find("Anchor")
	if !ok {
		t.Fatalf("card not found; texts = %v", tt.Texts())
	}
	// Touchpad scroll up-left: the view walks down-right the content, so the
	// card follows the fingers.
	tt.Scroll(rect.X+rect.W/2, rect.Y+rect.H/2, -50, -40)
	tt.Frame()
	after, ok := tt.Find("Anchor")
	if !ok {
		t.Fatalf("card lost after scroll; texts = %v", tt.Texts())
	}
	if dx, dy := after.X-rect.X, after.Y-rect.Y; dx != 50 || dy != 40 {
		t.Fatalf("scroll panned the card by (%v,%v), want (50,40)", dx, dy)
	}

	// Pinch: the zoom anchors at the pointer — the world point under the
	// pinch site becomes the anchor, so pinching at two sites a known screen
	// distance apart must anchor their world points exactly that far apart
	// (pan2 - pan1 = -(siteB - siteA) at zoom 1). Find rects are not trusted
	// under the Scale transform, so this asserts on the pan/zoom state.
	if err := tt.Click("Reset view"); err != nil {
		t.Fatalf("reset view: %v", err)
	}
	tt.Frame()
	pinchA := ui.Point{X: rect.X, Y: rect.Y}
	tt.Pinch(pinchA.X, pinchA.Y, 2)
	tt.Frame()
	if !tt.HasText("200%") {
		t.Fatalf("pinch did not zoom; texts = %v", tt.Texts())
	}
	if s.canvasZoom != 2 {
		t.Fatalf("zoom = %v, want 2", s.canvasZoom)
	}
	panA := s.canvasPan
	if err := tt.Click("Reset view"); err != nil {
		t.Fatalf("reset view: %v", err)
	}
	tt.Frame()
	pinchB := ui.Point{X: pinchA.X + 60, Y: pinchA.Y + 30}
	tt.Pinch(pinchB.X, pinchB.Y, 2)
	tt.Frame()
	panB := s.canvasPan
	if dx, dy := panB.X-panA.X, panB.Y-panA.Y; dx != -60 || dy != -30 {
		t.Fatalf("pinch anchors did not track the pointer: pan moved (%v,%v), want (-60,-30)", dx, dy)
	}
	dumpFrame(t, tt, "canvas-zoom-pinch")
}

// TestCanvasDeleteKeySelection selects a card and presses Delete on the focused
// surface: the node is removed; the untouched neighbour stays.
func TestCanvasDeleteKeySelection(t *testing.T) {
	s, b, ids := canvasShellForTest(t, "Key board")
	boardID := ids[0]
	for _, seed := range []struct {
		title string
		x     float64
	}{
		{"Alpha", 40},
		{"Beta", 320},
	} {
		if _, err := b.raw("canvas_create_node", map[string]any{"input": map[string]any{
			"kind": "note", "board_id": numOrString(boardID), "title": seed.title,
			"x": seed.x, "y": 40, "width": 200, "height": 100,
		}}); err != nil {
			t.Fatalf("create %s: %v", seed.title, err)
		}
	}
	nodesRaw, err := b.raw("canvas_list_nodes", map[string]any{"boardId": numOrString(boardID)})
	if err != nil {
		t.Fatalf("canvas_list_nodes: %v", err)
	}
	s.cache["canvas-nodes:"+boardID] = nodesRaw

	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Key board"); err != nil {
		t.Fatalf("open board: %v", err)
	}
	tt.Frame()
	// Clicking the card selects it and hands the world the keyboard focus.
	if err := tt.Click("Alpha"); err != nil {
		t.Fatalf("select node: %v", err)
	}
	tt.Frame()
	if !tt.HasText("1 selected") {
		t.Fatalf("card not selected; texts = %v", tt.Texts())
	}
	tt.Key(0, ui.KeyDelete)
	tt.Frame()
	deadline := time.Now().Add(3 * time.Second)
	for {
		nodes := canvasNodesOf(t, b, boardID)
		if len(nodes) == 1 && fmt.Sprintf("%v", nodes[0]["title"]) == "Beta" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("delete key did not remove the selection: %v", nodes)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestCanvasGroupIntoRegion marquees two conversation cards and grows a region
// around them: canvas_group_into_region consumes the loose cards, sizes the
// frame to whole card columns (upstream's groupSelection), and the new region
// becomes the selection.
func TestCanvasGroupIntoRegion(t *testing.T) {
	s, b, ids := canvasShellForTest(t, "Region board")
	boardID := ids[0]
	fraw, err := b.raw("add_folder_to_history", map[string]any{"path": t.TempDir()})
	if err != nil {
		t.Fatalf("add_folder: %v", err)
	}
	var folder map[string]any
	_ = json.Unmarshal(fraw, &folder)
	cids := []float64{}
	for _, title := range []string{"Conv One", "Conv Two"} {
		craw, err := b.raw("create_conversation", map[string]any{"folderId": folder["id"], "agentType": "claude_code"})
		if err != nil {
			t.Fatalf("create_conversation: %v", err)
		}
		var cid float64
		if err := json.Unmarshal(craw, &cid); err != nil {
			t.Fatalf("create_conversation returned %s, want a bare id: %v", craw, err)
		}
		cids = append(cids, cid)
		if _, err := b.raw("canvas_create_node", map[string]any{"input": map[string]any{
			"kind": "conversation", "board_id": numOrString(boardID),
			"conversation_id": cid, "title": title,
			"x": 40 + float64(len(cids)-1)*280, "y": 40, "width": 200, "height": 100,
		}}); err != nil {
			t.Fatalf("create %s node: %v", title, err)
		}
	}
	nodesRaw, err := b.raw("canvas_list_nodes", map[string]any{"boardId": numOrString(boardID)})
	if err != nil {
		t.Fatalf("canvas_list_nodes: %v", err)
	}
	s.cache["canvas-nodes:"+boardID] = nodesRaw

	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Region board"); err != nil {
		t.Fatalf("open board: %v", err)
	}
	tt.Frame()
	one, ok := tt.Find("Conv One")
	if !ok {
		t.Fatalf("Conv One missing; texts = %v", tt.Texts())
	}
	two, ok := tt.Find("Conv Two")
	if !ok {
		t.Fatalf("Conv Two missing; texts = %v", tt.Texts())
	}
	// Marquee both cards.
	tt.Press(one.X-10, one.Y-10)
	tt.Frame()
	tt.Move(two.X+210, one.Y+140)
	tt.Frame()
	tt.Release(two.X+210, one.Y+140)
	tt.Frame()
	if !tt.HasText("2 selected") {
		t.Fatalf("marquee selected %v; texts = %v", tt.Texts(), tt.Texts())
	}
	if !tt.HasText("Group into region") {
		t.Fatalf("group button missing for a conversation selection; texts = %v", tt.Texts())
	}
	if err := tt.Click("Group into region"); err != nil {
		t.Fatalf("group into region: %v", err)
	}

	// The region replaces both cards: one custom node holding both
	// conversations, framed at the selection's padded bounds (2 card columns).
	deadline := time.Now().Add(3 * time.Second)
	for {
		nodes := canvasNodesOf(t, b, boardID)
		if len(nodes) == 1 && fmt.Sprintf("%v", nodes[0]["kind"]) == "custom" {
			n := nodes[0]
			members, _ := n["member_ids"].([]any)
			want := []any{cids[0], cids[1]}
			if slices.Equal(members, want) {
				x, _ := n["x"].(float64)
				y, _ := n["y"].(float64)
				w, _ := n["width"].(float64)
				h, _ := n["height"].(float64)
				if x != 28 || y != 28 || w != canvasRegionWidthForColumns(2) ||
					h != canvasRegionHeightForRows(1) {
					t.Fatalf("region frame = (%v,%v %vx%v), want (28,28 %vx%v)",
						x, y, w, h, canvasRegionWidthForColumns(2), canvasRegionHeightForRows(1))
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("region members = %v, want %v", members, want)
			}
		} else if time.Now().After(deadline) {
			t.Fatalf("group did not consume the cards: %v", nodes)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The region is the selection now, and its card names its members.
	if !waitText(t, tt, "2 members", true) {
		t.Fatalf("region card missing the member count; texts = %v", tt.Texts())
	}
	if !tt.HasText("1 selected") {
		t.Errorf("region not selected; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "canvas-region")
}

// canvasNodesOf lists a board's nodes from the backend.
func canvasNodesOf(t *testing.T, b *Backend, boardID string) []map[string]any {
	t.Helper()
	raw, err := b.raw("canvas_list_nodes", map[string]any{"boardId": numOrString(boardID)})
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	var snap map[string]any
	_ = json.Unmarshal(raw, &snap)
	out := []map[string]any{}
	for _, rawNode := range asListVal(snap["nodes"]) {
		out = append(out, rawNode)
	}
	return out
}

// canvasNodeByTitle returns a node's (x,y) by title.
func canvasNodeByTitle(t *testing.T, b *Backend, boardID, title string) [2]float64 {
	t.Helper()
	for _, n := range canvasNodesOf(t, b, boardID) {
		if fmt.Sprintf("%v", n["title"]) == title {
			x, _ := n["x"].(float64)
			y, _ := n["y"].(float64)
			return [2]float64{x, y}
		}
	}
	t.Fatalf("node %q not found", title)
	return [2]float64{}
}
