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

import (
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

	snap := asObj(s.ensure("canvas-nodes:"+boardID, "canvas_list_nodes", map[string]any{"boardId": numOrString(boardID)}))
	if snap == nil {
		loadingState(c, "")
		return
	}
	nodes := asListVal(snap["nodes"])
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
	// zoom controls) would be laid out past the window edge.
	ui.Box(c.Key("cv-viewport")).Grow(1).FillWidth().Clip().Children(func() {
		world := ui.Box(c.Key("cv-world")).Absolute().Left(0).Top(0).
			Width(canvasWorldW).Height(canvasWorldH).
			TransformOrigin(0, 0).
			Translate(s.canvasPan.X, s.canvasPan.Y).
			Scale(zoom, zoom)
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
			Left(x + ew - 12).Top(y + eh - 12).Width(20).Height(20).
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
