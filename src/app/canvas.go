package app

// canvas.go is the Canvas workbench (UI_GAPS P1-7), mirroring upstream
// src/components/canvas/canvas-board-list.tsx: a searchable board list with
// per-row actions (open / rename / delete), a create/rename form with a theme
// colour, a delete confirmation, and a board view listing the board's nodes.
// (The full spatial canvas — pan/zoom/marquee/drag — is out of scope here; the
// node list is the read-only slice that makes a board inspectable.)
//
// Wire contract (src-go/web/handlers/canvas.go):
//   canvas_list_boards  → [{board:{id,name,description,color,created_at,updated_at},
//                          node_count, terminal_count}]
//   canvas_create_board {input:{name,description,color}} → board
//   canvas_update_board {boardId,patch:{name?,description?,color?}} → board
//   canvas_delete_board {boardId}
//   canvas_list_nodes   {boardId} → {board_id,nodes:[{id,kind,title,content,path,
//                          color,x,y,width,height,…}],revision}
//   canvas_delete_node  {nodeId}
//   canvas_delete_nodes {nodeIds}
//   canvas_move_nodes   {moves:[{id,x,y}]}
//   canvas_update_node  {nodeId,patch:{width,height,…}}
//   canvas_group_into_region {input:{boardId,memberIds,consumeNodeIds,gridColumns,
//                          x,y,width,height}} → {node:{region},deleted_ids}
//
// The surface takes its wheel/pinch through HandleInput (mygo 0.7): a touchpad
// or the plain wheel pans, a pinch or Ctrl+wheel notch zooms around the pointer,
// and Delete/Backspace on the focused surface deletes the selection.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"gocode/locales"
)

// canvasColors are the theme colour names the backend accepts
// (canvasThemeColors). "" means no colour.
var canvasColors = []string{"neutral", "zinc", "slate", "stone", "gray", "red",
	"rose", "orange", "green", "blue", "yellow", "violet"}

// canvasEditor holds the new/rename board form. id == "" means create.
type canvasEditor struct {
	id    string
	name  string
	desc  string
	color string
}

// viewCanvas renders the Canvas workbench: the board list, the create/rename
// form, or an open board's node list.
func (s *Shell) viewCanvas(c *ui.Context) {
	t := c.Theme()

	if s.canvasEditor != nil {
		s.viewCanvasEditor(c)
		return
	}
	if s.canvasSel != "" {
		s.viewCanvasBoard(c, s.canvasSel)
		return
	}

	list := asList(s.ensure("canvas-boards", "canvas_list_boards", nil))
	m := cat(c)
	ui.Row(c.Key("cv-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.NavCanvas()).FontSize(22).Bold()
		ui.Spacer(c)
		iconButton(c, "cv-refresh", iconRefresh, m.ActionRefresh(), func() {
			s.invalidate("canvas-boards")
		})
		if ui.PrimaryButton(c.Key("cv-new"), m.CvNewBoard()).Clicked() {
			s.startNewBoard()
		}
	})
	ui.Text(c, m.CvHint()).FontSize(12).TextColor(t.TextMuted)
	ui.Row(c.Key("cv-search-row")).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconSearch).Width(14).Height(14).TextColor(t.TextMuted)
		ui.TextInput(c.Key("cv-search"), &s.canvasSearch).Grow(1).Placeholder(m.CvSearchPlaceholder())
	})

	if list == nil {
		loadingState(c, "")
		return
	}
	boards := s.filterBoards(list)
	if len(list) == 0 {
		s.viewCanvasEmpty(c)
		return
	}
	if len(boards) == 0 {
		ui.Text(c, m.CvNoMatches()).FontSize(12).TextColor(t.TextMuted)
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(6).Children(func() {
			for _, item := range boards {
				s.canvasBoardRow(c, item)
			}
		})
	})
}

// viewCanvasEmpty is the onboarding empty state.
func (s *Shell) viewCanvasEmpty(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	ui.Column(c.Key("cv-empty")).Fill().Center().Gap(10).Children(func() {
		ui.Text(c, m.CvEmptyTitle()).FontSize(18).Bold()
		ui.Text(c, m.CvEmptyHint()).
			FontSize(13).TextColor(t.TextMuted)
		if ui.PrimaryButton(c.Key("cv-empty-new"), m.CvNewBoard()).Clicked() {
			s.startNewBoard()
		}
	})
}

// canvasBoardRow is one board entry: a colour dot, name, description, the
// node/terminal counts and an "edited …" line, with open/rename/delete on its
// right-click menu.
func (s *Shell) canvasBoardRow(c *ui.Context, item map[string]any) {
	t := c.Theme()
	m := cat(c)
	board := asObjVal(item["board"])
	id := fmt.Sprintf("%v", board["id"])
	name := firstNonEmpty(str(board, "name"), m.CvUntitledBoard())
	desc := str(board, "description")
	nodes := intOf(item["node_count"])
	terms := intOf(item["terminal_count"])

	card := ui.Column(c.Key("cv-row-" + id)).Gap(2).Padding(10).Background(t.Surface).Radius(8)
	card.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Width(8).Height(8).Radius(4).Background(canvasColor(t, str(board, "color")))
			ui.Text(c, name).Grow(1).MaxLines(1).TextColor(t.Text)
			ui.Text(c, m.CvCounts(nodes, terms)).FontSize(11).TextColor(t.TextMuted)
		})
		if desc != "" {
			ui.Text(c, desc).FontSize(12).TextColor(t.TextMuted).MaxLines(2).Wrap()
		}
		if upd := str(board, "updated_at"); upd != "" {
			ui.Text(c, m.CvEdited(relTime(upd, time.Now()))).FontSize(11).TextColor(t.TextMuted)
		}
	})
	card.ContextMenu(func(mn *ui.Menu) {
		if mn.Item(m.CvOpen()).Chosen() {
			s.canvasSel = id
			s.canvasConfirm = ""
		}
		if mn.Item(m.ActionRename()).Chosen() {
			s.startRenameBoard(board)
		}
		mn.Separator()
		if mn.Item(m.ActionDelete()).Chosen() {
			s.canvasConfirm = id
		}
	})
	if card.Clicked() {
		s.canvasSel = id
		s.canvasConfirm = ""
	}
	// Inline delete confirmation under the row.
	if s.canvasConfirm == id {
		ui.Row(c.Key("cv-confirm-" + id)).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, m.CvConfirmDelete()).FontSize(12).TextColor(t.Danger)
			if ui.PrimaryButton(c.Key("cv-confirm-yes-"+id), m.ActionDelete()).Clicked() {
				s.canvasConfirm = ""
				s.canvasDeleteBoard(c, id)
			}
			if ui.Button(c.Key("cv-confirm-no-"+id), m.ActionCancel()).Clicked() {
				s.canvasConfirm = ""
			}
		})
	}
}

// canvasDragEntry is one in-flight drag: the node's stored spot plus the
// pointer's accumulated delta, in world DIPs.
type canvasDragEntry struct {
	baseX, baseY float32
	dx, dy       float32
}

// canvasResizeEntry is one in-flight resize: the node's stored size plus the
// resize-handle's accumulated delta, in world DIPs.
type canvasResizeEntry struct {
	baseW, baseH float32
	dx, dy       float32
}

// canvasBand is the in-flight marquee band, in world DIPs.
type canvasBand struct {
	start ui.Point
	end   ui.Point
}

// rect normalizes the band to a positive-size rectangle.
func (b *canvasBand) rect() (x, y, w, h float32) {
	x, y = b.start.X, b.start.Y
	w, h = b.end.X-b.start.X, b.end.Y-b.start.Y
	if w < 0 {
		x, w = x+w, -w
	}
	if h < 0 {
		y, h = y+h, -h
	}
	return x, y, w, h
}

// canvasSurface is the board's world size: nodes keep their own x/y inside it,
// and the viewport pans over it.
const (
	canvasWorldW  = 2400
	canvasWorldH  = 1600
	canvasNodeW   = 220
	canvasNodeH   = 120
	canvasZoomMin = 0.4
	canvasZoomMax = 2
)

// Region frame geometry, mirroring upstream canvas-model.ts: a group region is
// sized to whole card columns/rows inside a padded frame with a header bar.
const (
	canvasRegionPad    = 12
	canvasRegionHeader = 40
	canvasRegionGap    = 12
	canvasRegionCardW  = 224
	canvasRegionCardH  = 132
	canvasRegionCols   = 6 // the most card columns a region grows to
)

// canvasRegionWidthForColumns sizes a region frame to n card columns.
func canvasRegionWidthForColumns(n int) float64 {
	n = max(1, n)
	return canvasRegionPad*2 + float64(n)*canvasRegionCardW + float64(n-1)*canvasRegionGap
}

// canvasRegionHeightForRows sizes a region frame to n card rows.
func canvasRegionHeightForRows(n int) float64 {
	n = max(1, n)
	return canvasRegionHeader + canvasRegionPad*2 + float64(n)*canvasRegionCardH + float64(n-1)*canvasRegionGap
}

// canvasColumnsForRegionWidth is how many card columns fit a region width (at
// least one — upstream would rather clip a card than render an empty frame).
func canvasColumnsForRegionWidth(w float64) int {
	usable := max(w-canvasRegionPad*2, canvasRegionCardW)
	return max(1, int((usable+canvasRegionGap)/(canvasRegionCardW+canvasRegionGap)))
}

// viewCanvasBoard renders an open board as a spatial surface: every node sits
// at its stored x/y, drags to a new spot (persisted through canvas_move_nodes),
// and the surface zooms/pans from the header. Nodes are placed absolutely in a
// fixed world that carries the pan/zoom transform, so their stored coordinates
// stay meaningful at any zoom.
func (s *Shell) viewCanvasBoard(c *ui.Context, boardID string) {
	t := c.Theme()
	m := cat(c)
	zoom := s.canvasZoom
	if zoom <= 0 {
		zoom = 1
	}

	// A Delete/Backspace that landed on the focused surface deletes the
	// selection; HandleInput runs before this frame, so the flag is fresh.
	if s.canvasKeyDelete {
		s.canvasKeyDelete = false
		s.canvasDeleteSelected(c)
	}

	snap := asObj(s.ensure("canvas-nodes:"+boardID, "canvas_list_nodes", map[string]any{"boardId": numOrString(boardID)}))
	nodes := asListVal(snap["nodes"])
	// The selection's conversation cards can grow into a region (upstream's
	// groupSelection); the header button carries that plan.
	group, canGroup := canvasRegionPlanOf(nodes, s.canvasSelNodes)

	ui.Row(c.Key("cv-board-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		iconButton(c, "cv-board-back", iconBack, m.CvBack(), func() {
			s.closeCanvasBoard()
		})
		ui.Text(c, m.CvBoardTitle(boardID)).FontSize(18).Bold()
		ui.Spacer(c)
		// Background drag either marquees or pans; the segmented control says
		// which, with the active half tinted.
		for _, mode := range []struct {
			pan   bool
			label string
		}{{false, m.CvSelectMode()}, {true, m.CvPanMode()}} {
			btn := ui.Button(c.Key(fmt.Sprintf("cv-mode-%v", mode.pan)), mode.label)
			if s.canvasPanMode == mode.pan {
				btn.Background(t.SurfacePressed)
			}
			if btn.Clicked() {
				s.canvasPanMode = mode.pan
				s.canvasBand = nil
			}
		}
		if n := len(s.canvasSelNodes); n > 0 {
			ui.Text(c, m.CvSelectedCount(n)).FontSize(11).TextColor(t.TextMuted)
			if ui.Button(c.Key("cv-del-selected"), m.CvDeleteSelected()).Clicked() {
				s.canvasDeleteSelected(c)
			}
		}
		if canGroup && ui.Button(c.Key("cv-group-region"), m.CvGroupRegion()).Clicked() {
			s.canvasGroupRegion(c, group)
		}
		if ui.Button(c.Key("cv-zoom-out"), "−").Clicked() {
			s.setCanvasZoom(zoom - 0.2)
		}
		ui.Textf(c, "%d%%", int(zoom*100+0.5)).FontSize(11).TextColor(t.TextMuted)
		if ui.Button(c.Key("cv-zoom-in"), "+").Clicked() {
			s.setCanvasZoom(zoom + 0.2)
		}
		if ui.Button(c.Key("cv-zoom-reset"), m.CvZoomReset()).Clicked() {
			s.canvasZoom = 0
			s.canvasPan = ui.Point{}
		}
		if ui.PrimaryButton(c.Key("cv-new-note"), m.CvNewNote()).Clicked() {
			s.canvasCreateNote(c, boardID)
		}
		iconButton(c, "cv-board-refresh", iconRefresh, m.ActionRefresh(), func() {
			s.invalidate("canvas-nodes:" + boardID)
		})
	})

	if snap == nil {
		loadingState(c, "")
		return
	}
	// Base spots, so a group drag can move every selected card without each one
	// looking its neighbours up.
	bases := make(map[string]ui.Point, len(nodes))
	for _, n := range nodes {
		bases[fmt.Sprintf("%v", n["id"])] = ui.Point{
			X: float32(floatOf(n["x"])),
			Y: float32(floatOf(n["y"])),
		}
	}

	// The viewport clips; the world inside carries the pan/zoom transform and
	// hosts the absolutely-placed cards. The world is itself absolute so its
	// fixed size never stretches the panel — otherwise the header row (and its
	// zoom controls) would be laid out past the window edge. It is focusable so
	// a click anywhere on the board hands it the keyboard (Delete/Backspace),
	// and it takes the wheel, pinch and keys through HandleInput.
	ui.Box(c.Key("cv-viewport")).Grow(1).FillWidth().Clip().Children(func() {
		world := ui.Box(c.Key("cv-world")).Absolute().Left(0).Top(0).
			Width(canvasWorldW).Height(canvasWorldH).
			TransformOrigin(0, 0).
			Translate(s.canvasPan.X, s.canvasPan.Y).
			Scale(zoom, zoom).
			Focusable().
			HandleInput(s.canvasWorldInput)
		world.Children(func() {
			if len(nodes) == 0 {
				ui.Column(c.Key("cv-empty-hint")).Absolute().Left(24).Top(24).Gap(6).Children(func() {
					ui.Text(c, m.CvNoNodes()).FontSize(13).TextColor(t.TextMuted)
					ui.Text(c, m.CvEmptyBoardHint()).FontSize(12).TextColor(t.TextMuted)
				})
			}
			for _, n := range nodes {
				s.canvasNodeCard(c, n, bases)
			}
			// The marquee band itself, in world coordinates.
			if s.canvasBand != nil {
				bx, by, bw, bh := s.canvasBand.rect()
				ui.Box(c.Key("cv-band")).Absolute().Left(bx).Top(by).Width(bw).Height(bh).
					Background(t.Accent.Alpha(0.12)).Border(1, t.Accent.Alpha(0.55))
			}
		})
		// Background press: pan in pan mode, marquee in select mode. Cards
		// consume their own presses first, so this only fires on empty space.
		if s.canvasPanMode {
			if dx, dy, ok := world.Dragged(); ok {
				s.canvasPan.X += dx
				s.canvasPan.Y += dy
			}
		} else {
			if px, py, over := world.PointerPosition(); over {
				if s.canvasBand == nil && world.Pressed() {
					s.canvasBand = &canvasBand{start: ui.Point{X: px, Y: py}, end: ui.Point{X: px, Y: py}}
				}
				if s.canvasBand != nil && world.Pressed() {
					s.canvasBand.end = ui.Point{X: px, Y: py}
				}
			}
			if s.canvasBand != nil && !world.Pressed() {
				s.commitCanvasBand(nodes, bases)
			}
		}
	})
}

// commitCanvasBand turns the finished marquee into a selection and drops the
// band. An empty band (a plain background click) clears the selection.
func (s *Shell) commitCanvasBand(nodes []map[string]any, bases map[string]ui.Point) {
	band := s.canvasBand
	s.canvasBand = nil
	if band == nil {
		return
	}
	bx, by, bw, bh := band.rect()
	sel := map[string]bool{}
	if bw >= 3 || bh >= 3 {
		for _, n := range nodes {
			id := fmt.Sprintf("%v", n["id"])
			base := bases[id]
			nw, nh := canvasNodeSize(n)
			if base.X < bx+bw && bx < base.X+nw && base.Y < by+bh && by < base.Y+nh {
				sel[id] = true
			}
		}
	}
	s.canvasSelNodes = sel
}

// canvasNodeSize is a node's card size. A stored width/height is honoured
// verbatim (the backend clamps to 48–20000); only a missing/zero dimension
// falls back to the default card size so legacy nodes still render.
func canvasNodeSize(n map[string]any) (w, h float32) {
	w, h = float32(floatOf(n["width"])), float32(floatOf(n["height"]))
	if w <= 0 {
		w = canvasNodeW
	}
	if h <= 0 {
		h = canvasNodeH
	}
	return w, h
}

// closeCanvasBoard leaves the board view and drops its transient state.
func (s *Shell) closeCanvasBoard() {
	s.canvasSel = ""
	s.canvasZoom = 0
	s.canvasPan = ui.Point{}
	s.canvasSelNodes = nil
	s.canvasBand = nil
	s.canvasDrag = nil
	s.canvasResize = nil
	s.canvasKeyDelete = false
}

// canvasNodeCard draws one node at its stored spot. A drag accumulates in
// canvasDrag (world DIPs) and commits through canvas_move_nodes on release.
func (s *Shell) canvasNodeCard(c *ui.Context, n map[string]any, bases map[string]ui.Point) {
	t := c.Theme()
	m := cat(c)
	id := fmt.Sprintf("%v", n["id"])
	baseX, baseY := float32(floatOf(n["x"])), float32(floatOf(n["y"]))
	x, y := baseX, baseY
	if off, ok := s.canvasDrag[id]; ok {
		x, y = off.baseX+off.dx, off.baseY+off.dy
	}
	w, h := canvasNodeSize(n)
	// A live resize overrides the stored size so the card tracks the handle.
	ew, eh := w, h
	if re, ok := s.canvasResize[id]; ok {
		ew, eh = re.baseW+re.dx, re.baseH+re.dy
	}
	kind := firstNonEmpty(str(n, "kind"), "note")
	label := firstNonEmpty(str(n, "title"), str(n, "content"), str(n, "path"), m.Untitled())
	selected := s.canvasSelNodes[id]

	border := canvasColor(t, str(n, "color")).Alpha(0.35)
	if selected {
		border = t.Accent
	}
	card := ui.Column(c.Key("cv-node-"+id)).Absolute().Left(x).Top(y).Width(ew).Height(eh).
		Padding(8).Gap(4).Radius(8).Background(t.Surface).
		Border(1, border)
	card.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Width(8).Height(8).Radius(4).Background(canvasColor(t, str(n, "color")))
			ui.Text(c, canvasKindLabel(m, kind)).FontSize(10).TextColor(t.TextMuted)
			ui.Spacer(c)
			iconButton(c, "cv-node-del-"+id, iconX, m.ActionDelete(), func() {
				s.canvasDeleteNode(c, id)
			})
		})
		ui.Text(c, label).FontSize(12).TextColor(t.Text).MaxLines(3).Wrap()
		// A region card renders its member conversations inside (upstream does
		// too): a title chip per member with a detach ✕ that drops it back on
		// the board beside the region. Long regions cap at four chips.
		if kind == "custom" {
			s.viewRegionMembers(c, n, baseX, baseY, ew, eh)
		}
	})
	// Drag first: the release frame of a drag also reports Clicked, and a
	// click must not collapse a group selection that was just moved.
	dragged := false
	if dx, dy, ok := card.Dragged(); ok {
		if s.canvasDrag == nil {
			s.canvasDrag = map[string]canvasDragEntry{}
		}
		// Dragging a selected card moves the whole selection; dragging an
		// unselected one moves just it (and makes it the selection).
		targets := []string{id}
		if selected {
			targets = targets[:0]
			for sel := range s.canvasSelNodes {
				targets = append(targets, sel)
			}
		} else if len(s.canvasSelNodes) > 0 {
			s.canvasSelNodes = map[string]bool{id: true}
		}
		for _, tID := range targets {
			e, ok := s.canvasDrag[tID]
			if !ok {
				base := bases[tID]
				e = canvasDragEntry{baseX: base.X, baseY: base.Y}
			}
			e.dx += dx
			e.dy += dy
			s.canvasDrag[tID] = e
		}
	}
	if _, _, ended := card.DragEnded(); ended {
		s.commitCanvasDrag(c)
		dragged = true
	}
	// A click selects this card alone; clicking the sole selected card clears
	// the selection (so the marquee and the click never fight).
	if !dragged && card.Clicked() {
		if len(s.canvasSelNodes) == 1 && selected {
			s.canvasSelNodes = nil
		} else {
			s.canvasSelNodes = map[string]bool{id: true}
		}
	}

	// Resize handle: appears on the selected card's bottom-right corner. Its
	// drag accumulates in world DIPs and commits through canvas_update_node.
	if selected {
		handle := ui.Box(c.Key("cv-resize-"+id)).Absolute().
			Left(x+ew-12).Top(y+eh-12).Width(20).Height(20).
			Background(t.Surface).Border(2, t.Accent).Radius(3)
		handle.Tooltip(m.CvResize())
		if rdx, rdy, ok := handle.Dragged(); ok {
			if s.canvasResize == nil {
				s.canvasResize = map[string]canvasResizeEntry{}
			}
			e, ok := s.canvasResize[id]
			if !ok {
				e = canvasResizeEntry{baseW: w, baseH: h}
			}
			e.dx += rdx
			e.dy += rdy
			s.canvasResize[id] = e
		}
		if _, _, rended := handle.DragEnded(); rended {
			s.commitCanvasResize(c)
		}
	}
}

// viewRegionMembers renders a region's member conversations as detachable
// chips inside the region card. Titles resolve from the app-wide conversation
// list (the tab strip keeps it warm); unknown ids fall back to themselves.
func (s *Shell) viewRegionMembers(c *ui.Context, n map[string]any, baseX, baseY, ew, eh float32) {
	t := c.Theme()
	m := cat(c)
	ids, ok := n["member_ids"].([]any)
	if !ok || len(ids) == 0 {
		return
	}
	regionID := fmt.Sprintf("%v", n["id"])
	byID := map[string]map[string]any{}
	for _, cv := range asList(s.ensure("all-convs", "list_all_conversations", nil)) {
		byID[fmt.Sprintf("%v", cv["id"])] = cv
	}
	ui.Column(c.Key("cv-region-members-" + regionID)).Absolute().
		Left(baseX + 10).Top(baseY + 58).Width(ew - 20).Gap(2).Children(func() {
		shown := ids
		if len(shown) > 4 {
			shown = shown[:4]
		}
		for _, mid := range shown {
			ms := fmt.Sprintf("%v", mid)
			title, agent := ms, ""
			if cv := byID[ms]; cv != nil {
				title = firstNonEmpty(str(cv, "title"), ms)
				agent = str(cv, "agent_type")
			}
			ui.Row(c.Key("cv-rm-" + regionID + "-" + ms)).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontSize(10).TextColor(t.Text).Grow(1).MaxLines(1)
				if agent != "" {
					ui.Text(c, agent).FontSize(9).TextColor(t.TextMuted).MaxLines(1)
				}
				x := ui.Row(c.Key("cv-rm-x-" + regionID + "-" + ms)).Width(13).Height(13).
					AlignItems(ui.Center).Justify(ui.Center).Label(m.CvDetach())
				x.Children(func() {
					ui.Icon(c, iconX).Width(8).Height(8).TextColor(t.TextMuted)
				})
				if x.Clicked() {
					s.canvasDetachMember(c, regionID, ms, float64(baseX+ew+16), float64(baseY))
				}
			})
		}
		if len(ids) > len(shown) {
			ui.Text(c, m.CvRegionMembers(len(ids))).FontSize(10).TextColor(t.TextMuted)
		}
	})
}

// canvasDetachMember moves one conversation out of a region back onto the
// board (canvas_detach_member's x/y is where its loose card lands).
func (s *Shell) canvasDetachMember(c *ui.Context, regionID, convID string, x, y float64) {
	svc := s.svc
	loc := c.Locale()
	board := s.canvasSel
	go func() {
		_, err := s.B.raw("canvas_detach_member", map[string]any{
			"regionId": numOrString(regionID), "conversationId": numOrString(convID),
			"x": x, "y": y,
		})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).CvMoveFailed(err.Error()))
		}
		if board != "" {
			delete(s.cache, "canvas-nodes:"+board)
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// commitCanvasDrag persists every dragged card's new spot in one
// canvas_move_nodes call, then clears the in-flight offsets.
func (s *Shell) commitCanvasDrag(c *ui.Context) {
	if len(s.canvasDrag) == 0 {
		return
	}
	moves := make([]any, 0, len(s.canvasDrag))
	first := true
	for id, e := range s.canvasDrag {
		if e.dx == 0 && e.dy == 0 {
			continue
		}
		moves = append(moves, map[string]any{
			"id": numOrString(id),
			"x":  float64(e.baseX + e.dx),
			"y":  float64(e.baseY + e.dy),
		})
		if first {
			first = false
		}
	}
	s.canvasDrag = nil
	if len(moves) == 0 {
		return
	}
	s.canvasMoveNodes(c, moves)
}

// commitCanvasResize persists every resized card's new size through
// canvas_update_node, then clears the in-flight offsets. The backing call clamps
// each dimension to 48–20000, so oversized drags land at that ceiling.
func (s *Shell) commitCanvasResize(c *ui.Context) {
	if len(s.canvasResize) == 0 {
		return
	}
	svc := s.svc
	loc := c.Locale()
	board := s.canvasSel
	jobs := make([]map[string]any, 0, len(s.canvasResize))
	for id, e := range s.canvasResize {
		jobs = append(jobs, map[string]any{
			"nodeId": numOrString(id),
			"patch": map[string]any{
				"width":  float64(e.baseW + e.dx),
				"height": float64(e.baseH + e.dy),
			},
		})
	}
	s.canvasResize = nil
	go func() {
		var firstErr error
		for _, job := range jobs {
			if _, err := s.B.raw("canvas_update_node", job); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		s.mu.Lock()
		if firstErr != nil {
			s.notifyErr(locales.In(loc).CvMoveFailed(firstErr.Error()))
		}
		if board != "" {
			delete(s.cache, "canvas-nodes:"+board)
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// canvasKindLabel names a node kind in the window's language.
func canvasKindLabel(m locales.Messages, kind string) string {
	switch kind {
	case "note":
		return m.CvKindNote()
	case "group":
		return m.CvKindGroup()
	case "agent":
		return m.CvKindAgent()
	case "conversation":
		return m.CvKindConversation()
	case "folder":
		return m.CvKindFolder()
	case "file":
		return m.CvKindFile()
	case "terminal":
		return m.CvKindTerminal()
	case "custom":
		return m.CvKindCustom()
	}
	return kind
}

// setCanvasZoom clamps and stores the surface zoom.
func (s *Shell) setCanvasZoom(z float32) {
	s.canvasZoom = min(max(z, canvasZoomMin), canvasZoomMax)
}

// zoomCanvasAt sets a new zoom keeping the world point (wx, wy) — the pointer's
// spot on the world — under the pointer. Cards stay put under the cursor while
// the world grows or shrinks around it.
func (s *Shell) zoomCanvasAt(wx, wy, z float32) {
	cur := s.canvasZoom
	if cur <= 0 {
		cur = 1
	}
	z = min(max(z, canvasZoomMin), canvasZoomMax)
	s.canvasZoom = z
	s.canvasPan.X += wx * (cur - z)
	s.canvasPan.Y += wy * (cur - z)
}

// canvasWorldInput takes the world's wheel, pinch and keys (HandleInput, mygo
// 0.7): a touchpad's scroll or the plain wheel pans (DX/DY arrive in world
// axes), a pinch or a Ctrl+wheel notch zooms around the pointer, and
// Delete/Backspace on the focused surface queues deleting the selection. It
// declines pointer events, so card clicks and drags are untouched. It runs on
// the main thread before the next frame, ahead of View.
func (s *Shell) canvasWorldInput(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputZoom:
		cur := s.canvasZoom
		if cur <= 0 {
			cur = 1
		}
		s.zoomCanvasAt(ev.X, ev.Y, cur*ev.Scale)
		return true
	case ui.InputScroll:
		cur := s.canvasZoom
		if cur <= 0 {
			cur = 1
		}
		if ev.Mods&ui.Ctrl != 0 && !ev.Precise {
			// Windows reports pinches as Ctrl-scroll, but a mouse wheel's
			// notches stay InputScroll with Ctrl in Mods: zoom a quarter each.
			switch {
			case ev.DY < 0:
				s.zoomCanvasAt(ev.X, ev.Y, cur*1.25)
			case ev.DY > 0:
				s.zoomCanvasAt(ev.X, ev.Y, cur*0.8)
			}
			return true
		}
		// A touchpad's fingers or the plain wheel: pan, as a scroll container
		// would. Positive DY moves the view down the content, so the pan
		// shrinks by the scrolled distance.
		s.canvasPan.X -= cur * ev.DX
		s.canvasPan.Y -= cur * ev.DY
		return true
	case ui.InputKeyDown:
		if ev.Repeat || (ev.Key != ui.KeyDelete && ev.Key != ui.KeyBackspace) {
			return false
		}
		if len(s.canvasSelNodes) == 0 {
			return false
		}
		s.canvasKeyDelete = true
		return true
	}
	return false
}

// canvasRegionPlan is a pending group-into-region action: the conversations the
// selection covers (members), the loose conversation cards to consume, and the
// frame the new region grows into.
type canvasRegionPlan struct {
	members []any
	consume []any
	x, y    float64
	columns int
	rows    int
}

// canvasRegionPlanOf walks the selection the way upstream's groupSelection
// does: conversation nodes contribute their conversation as a member and
// themselves as consumed cards; the plan's frame bounds those cards, padded
// and rounded up to whole card columns (at most six).
func canvasRegionPlanOf(nodes []map[string]any, sel map[string]bool) (canvasRegionPlan, bool) {
	var plan canvasRegionPlan
	minX, minY := float32(1e18), float32(1e18)
	maxX, maxY := float32(-1e18), float32(-1e18)
	for _, n := range nodes {
		id := fmt.Sprintf("%v", n["id"])
		if !sel[id] || str(n, "kind") != "conversation" {
			continue
		}
		cid := intOf(n["conversation_id"])
		if cid == 0 {
			continue
		}
		plan.members = append(plan.members, numOrString(fmt.Sprintf("%v", cid)))
		plan.consume = append(plan.consume, numOrString(id))
		x, y := float32(floatOf(n["x"])), float32(floatOf(n["y"]))
		w, h := canvasNodeSize(n)
		minX, minY = min(minX, x), min(minY, y)
		maxX, maxY = max(maxX, x+w), max(maxY, y+h)
	}
	if len(plan.members) == 0 {
		return canvasRegionPlan{}, false
	}
	plan.x = float64(minX) - canvasRegionPad
	plan.y = float64(minY) - canvasRegionPad
	plan.columns = min(canvasRegionCols,
		canvasColumnsForRegionWidth(float64(maxX-minX)+canvasRegionPad*2))
	plan.rows = (len(plan.members) + plan.columns - 1) / plan.columns
	return plan, true
}

// viewCanvasEditor renders the new/rename board form.
func (s *Shell) viewCanvasEditor(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	e := s.canvasEditor
	if e == nil {
		return
	}
	title := m.CvNewBoard()
	if e.id != "" {
		title = m.CvRenameTitle()
	}
	ui.Column(c.Key("cv-form")).Gap(10).Children(func() {
		ui.Text(c, title).FontSize(18).Bold()
		ui.Text(c, m.AuName()).FontSize(12).TextColor(t.TextMuted)
		ui.TextInput(c.Key("cv-f-name"), &e.name).Placeholder(m.CvNamePlaceholder())
		ui.Text(c, m.CvDescription()).FontSize(12).TextColor(t.TextMuted)
		ui.TextArea(c.Key("cv-f-desc"), &e.desc).Placeholder(m.CvDescPlaceholder()).MinHeight(60)
		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, m.CvColour()).FontSize(12).TextColor(t.TextMuted)
			ui.Select(c.Key("cv-f-color"), &e.color, canvasColors)
		})
		ui.Row(c).Gap(6).Children(func() {
			if ui.PrimaryButton(c.Key("cv-f-save"), m.ActionSave()).Disabled(strings.TrimSpace(e.name) == "").Clicked() {
				s.canvasSave(c)
			}
			if ui.Button(c.Key("cv-f-cancel"), m.ActionCancel()).Clicked() {
				s.canvasEditor = nil
			}
		})
	})
}

// ── helpers ──

// filterBoards applies the search box (case-insensitive name/description).
func (s *Shell) filterBoards(list []map[string]any) []map[string]any {
	q := strings.TrimSpace(strings.ToLower(s.canvasSearch))
	if q == "" {
		return list
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		b := asObjVal(item["board"])
		if strings.Contains(strings.ToLower(str(b, "name")), q) ||
			strings.Contains(strings.ToLower(str(b, "description")), q) {
			out = append(out, item)
		}
	}
	return out
}

// canvasColor maps a theme colour name to a palette colour.
func canvasColor(t *ui.Theme, name string) ui.Color {
	switch name {
	case "red", "rose":
		return t.Danger
	case "orange", "yellow":
		return t.Warning
	case "green":
		return t.Success
	case "blue", "violet":
		return t.Accent
	}
	return t.TextMuted
}

// floatOf coerces a decoded JSON number to float64.
func floatOf(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

// ── mutations ──

func (s *Shell) startNewBoard() {
	s.canvasEditor = &canvasEditor{color: "neutral"}
	s.canvasConfirm = ""
}

func (s *Shell) startRenameBoard(board map[string]any) {
	s.canvasEditor = &canvasEditor{
		id:    fmt.Sprintf("%v", board["id"]),
		name:  str(board, "name"),
		desc:  str(board, "description"),
		color: firstNonEmpty(str(board, "color"), "neutral"),
	}
	s.canvasConfirm = ""
}

// canvasSave creates or renames a board off the UI thread.
func (s *Shell) canvasSave(c *ui.Context) {
	e := s.canvasEditor
	if e == nil {
		return
	}
	id := e.id
	name := strings.TrimSpace(e.name)
	desc := strings.TrimSpace(e.desc)
	color := e.color
	svc := s.svc
	loc := c.Locale()
	go func() {
		var err error
		if id == "" {
			_, err = s.B.raw("canvas_create_board", map[string]any{"input": map[string]any{
				"name": name, "description": desc, "color": color,
			}})
		} else {
			_, err = s.B.raw("canvas_update_board", map[string]any{"boardId": numOrString(id), "patch": map[string]any{
				"name": name, "description": desc, "color": color,
			}})
		}
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).CvSaveFailed(err.Error()))
		} else {
			s.notify(locales.In(loc).CvSaved())
			s.canvasEditor = nil
		}
		delete(s.cache, "canvas-boards")
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

func (s *Shell) canvasDeleteBoard(c *ui.Context, id string) {
	svc := s.svc
	loc := c.Locale()
	go func() {
		_, err := s.B.raw("canvas_delete_board", map[string]any{"boardId": numOrString(id)})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).CvDeleteFailed(err.Error()))
		} else {
			s.notify(locales.In(loc).CvBoardDeleted())
			if s.canvasSel == id {
				s.canvasSel = ""
			}
		}
		delete(s.cache, "canvas-boards")
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

func (s *Shell) canvasDeleteNode(c *ui.Context, id string) {
	svc := s.svc
	loc := c.Locale()
	go func() {
		_, err := s.B.raw("canvas_delete_node", map[string]any{"nodeId": numOrString(id)})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).CvDeleteFailed(err.Error()))
		} else {
			s.notify(locales.In(loc).CvNodeDeleted())
		}
		if s.canvasSel != "" {
			delete(s.cache, "canvas-nodes:"+s.canvasSel)
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// canvasMoveNode persists a dragged node's new spot (canvas_move_nodes).
func (s *Shell) canvasMoveNodes(c *ui.Context, moves []any) {
	svc := s.svc
	loc := c.Locale()
	board := s.canvasSel
	go func() {
		_, err := s.B.raw("canvas_move_nodes", map[string]any{"moves": moves})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).CvMoveFailed(err.Error()))
		}
		if board != "" {
			delete(s.cache, "canvas-nodes:"+board)
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// canvasGroupRegion grows a region around the selection's conversations
// (canvas_group_into_region): their loose cards are consumed, the frame is
// sized to whole card columns like upstream's groupSelection, and the new
// region becomes the selection.
func (s *Shell) canvasGroupRegion(c *ui.Context, plan canvasRegionPlan) {
	board := s.canvasSel
	if board == "" || len(plan.members) == 0 {
		return
	}
	svc := s.svc
	loc := c.Locale()
	input := map[string]any{
		"board_id":         numOrString(board),
		"member_ids":       plan.members,
		"consume_node_ids": plan.consume,
		"grid_columns":     plan.columns,
		"x":                plan.x,
		"y":                plan.y,
		"width":            canvasRegionWidthForColumns(plan.columns),
		"height":           canvasRegionHeightForRows(plan.rows),
	}
	go func() {
		raw, err := s.B.raw("canvas_group_into_region", map[string]any{"input": input})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).CvGroupFailed(err.Error()))
		} else {
			s.notify(locales.In(loc).CvGrouped())
			var res map[string]any
			_ = json.Unmarshal(raw, &res)
			node := asObjVal(asObjVal(res["value"])["node"])
			if id := fmt.Sprintf("%v", node["id"]); id != "" && id != "<nil>" {
				s.canvasSelNodes = map[string]bool{id: true}
			} else {
				s.canvasSelNodes = nil
			}
		}
		delete(s.cache, "canvas-nodes:"+board)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// canvasDeleteSelected removes every selected card in one canvas_delete_nodes
// call.
func (s *Shell) canvasDeleteSelected(c *ui.Context) {
	if len(s.canvasSelNodes) == 0 {
		return
	}
	ids := make([]any, 0, len(s.canvasSelNodes))
	for id := range s.canvasSelNodes {
		ids = append(ids, numOrString(id))
	}
	n := len(ids)
	svc := s.svc
	loc := c.Locale()
	board := s.canvasSel
	go func() {
		_, err := s.B.raw("canvas_delete_nodes", map[string]any{"nodeIds": ids})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).CvDeleteFailed(err.Error()))
		} else {
			s.notify(locales.In(loc).CvNodesDeleted(n))
			s.canvasSelNodes = nil
		}
		if board != "" {
			delete(s.cache, "canvas-nodes:"+board)
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// canvasCreateNote adds a note card near the viewport's top-left, offset so a
// burst of notes does not stack on one spot.
func (s *Shell) canvasCreateNote(c *ui.Context, boardID string) {
	svc := s.svc
	loc := c.Locale()
	s.canvasNoteSeq++
	offset := float64(24 + (s.canvasNoteSeq%6)*28)
	x := float64(-s.canvasPan.X) + offset
	y := float64(-s.canvasPan.Y) + offset
	go func() {
		_, err := s.B.raw("canvas_create_node", map[string]any{"input": map[string]any{
			"kind": "note", "board_id": numOrString(boardID),
			"title": locales.In(loc).CvNoteTitle(), "x": x, "y": y,
		}})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).CvCreateFailed(err.Error()))
		} else {
			s.notify(locales.In(loc).CvNoteCreated())
		}
		delete(s.cache, "canvas-nodes:"+boardID)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}
