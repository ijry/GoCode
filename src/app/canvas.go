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
	ui.Row(c.Key("cv-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Canvas").FontSize(22).Bold()
		ui.Spacer(c)
		if ui.Button(c.Key("cv-refresh"), "Refresh").Clicked() {
			s.invalidate("canvas-boards")
		}
		if ui.PrimaryButton(c.Key("cv-new"), "New board").Clicked() {
			s.startNewBoard()
		}
	})
	ui.Text(c, "Spatial boards of conversations, notes and terminals.").FontSize(12).TextColor(t.TextMuted)
	ui.Row(c.Key("cv-search-row")).Children(func() {
		ui.TextInput(c.Key("cv-search"), &s.canvasSearch).Grow(1).Placeholder("Search boards")
	})

	if list == nil {
		ui.Text(c, "Loading…").TextColor(t.TextMuted)
		return
	}
	boards := s.filterBoards(list)
	if len(list) == 0 {
		s.viewCanvasEmpty(c)
		return
	}
	if len(boards) == 0 {
		ui.Text(c, "No matches").FontSize(12).TextColor(t.TextMuted)
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
	ui.Column(c.Key("cv-empty")).Fill().Center().Gap(10).Children(func() {
		ui.Text(c, "No canvas boards yet").FontSize(18).Bold()
		ui.Text(c, "Arrange conversations, notes and terminals on a spatial board.").
			FontSize(13).TextColor(t.TextMuted)
		if ui.PrimaryButton(c.Key("cv-empty-new"), "New board").Clicked() {
			s.startNewBoard()
		}
	})
}

// canvasBoardRow is one board entry: a colour dot, name, description, the
// node/terminal counts and an "edited …" line, with open/rename/delete on its
// right-click menu.
func (s *Shell) canvasBoardRow(c *ui.Context, item map[string]any) {
	t := c.Theme()
	board := asObjVal(item["board"])
	id := fmt.Sprintf("%v", board["id"])
	name := firstNonEmpty(str(board, "name"), "(untitled board)")
	desc := str(board, "description")
	nodes := intOf(item["node_count"])
	terms := intOf(item["terminal_count"])

	card := ui.Column(c.Key("cv-row-"+id)).Gap(2).Padding(10).Background(t.Surface).Radius(8)
	card.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Width(8).Height(8).Radius(4).Background(canvasColor(t, str(board, "color")))
			ui.Text(c, name).Grow(1).MaxLines(1).TextColor(t.Text)
			ui.Textf(c, "%d node(s) · %d terminal(s)", nodes, terms).FontSize(11).TextColor(t.TextMuted)
		})
		if desc != "" {
			ui.Text(c, desc).FontSize(12).TextColor(t.TextMuted).MaxLines(2).Wrap()
		}
		if upd := str(board, "updated_at"); upd != "" {
			ui.Text(c, "edited "+relTime(upd, time.Now())).FontSize(11).TextColor(t.TextMuted)
		}
	})
	card.ContextMenu(func(m *ui.Menu) {
		if m.Item("Open").Chosen() {
			s.canvasSel = id
			s.canvasConfirm = ""
		}
		if m.Item("Rename").Chosen() {
			s.startRenameBoard(board)
		}
		m.Separator()
		if m.Item("Delete").Chosen() {
			s.canvasConfirm = id
		}
	})
	if card.Clicked() {
		s.canvasSel = id
		s.canvasConfirm = ""
	}
	// Inline delete confirmation under the row.
	if s.canvasConfirm == id {
		ui.Row(c.Key("cv-confirm-"+id)).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Delete this board and its nodes?").FontSize(12).TextColor(t.Danger)
			if ui.PrimaryButton(c.Key("cv-confirm-yes-"+id), "Delete").Clicked() {
				s.canvasConfirm = ""
				s.canvasDeleteBoard(id)
			}
			if ui.Button(c.Key("cv-confirm-no-"+id), "Cancel").Clicked() {
				s.canvasConfirm = ""
			}
		})
	}
}

// viewCanvasBoard renders an open board's nodes with a back button and a
// per-node delete.
func (s *Shell) viewCanvasBoard(c *ui.Context, boardID string) {
	t := c.Theme()
	ui.Row(c.Key("cv-board-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		if ui.Button(c.Key("cv-board-back"), "← Boards").Clicked() {
			s.canvasSel = ""
		}
		ui.Text(c, "Board "+boardID).FontSize(18).Bold()
		ui.Spacer(c)
		if ui.Button(c.Key("cv-board-refresh"), "Refresh").Clicked() {
			s.invalidate("canvas-nodes:" + boardID)
		}
	})
	snap := asObj(s.ensure("canvas-nodes:"+boardID, "canvas_list_nodes", map[string]any{"boardId": numOrString(boardID)}))
	if snap == nil {
		ui.Text(c, "Loading…").TextColor(t.TextMuted)
		return
	}
	nodes := asListVal(snap["nodes"])
	if len(nodes) == 0 {
		ui.Text(c, "This board has no nodes yet.").FontSize(12).TextColor(t.TextMuted)
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(4).Children(func() {
			for _, n := range nodes {
				id := fmt.Sprintf("%v", n["id"])
				kind := firstNonEmpty(str(n, "kind"), "node")
				label := firstNonEmpty(str(n, "title"), str(n, "content"), str(n, "path"), "(untitled)")
				ui.Row(c.Key("cv-node-"+id)).Gap(8).Padding(8).Background(t.Surface).Radius(6).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Width(8).Height(8).Radius(4).Background(canvasColor(t, str(n, "color")))
					ui.Text(c, kind).FontSize(11).TextColor(t.TextMuted).Width(80)
					ui.Text(c, label).Grow(1).MaxLines(1).TextColor(t.Text)
					ui.Textf(c, "%.0f, %.0f", floatOf(n["x"]), floatOf(n["y"])).FontSize(11).TextColor(t.TextMuted)
					if ui.Button(c.Key("cv-node-del-"+id), "Delete").Clicked() {
						s.canvasDeleteNode(id)
					}
				})
			}
		})
	})
}

// viewCanvasEditor renders the new/rename board form.
func (s *Shell) viewCanvasEditor(c *ui.Context) {
	t := c.Theme()
	e := s.canvasEditor
	if e == nil {
		return
	}
	title := "New board"
	if e.id != "" {
		title = "Rename board"
	}
	ui.Column(c.Key("cv-form")).Gap(10).Children(func() {
		ui.Text(c, title).FontSize(18).Bold()
		ui.Text(c, "Name").FontSize(12).TextColor(t.TextMuted)
		ui.TextInput(c.Key("cv-f-name"), &e.name).Placeholder("My board")
		ui.Text(c, "Description").FontSize(12).TextColor(t.TextMuted)
		ui.TextArea(c.Key("cv-f-desc"), &e.desc).Placeholder("What this board is for").MinHeight(60)
		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, "Colour").FontSize(12).TextColor(t.TextMuted)
			ui.Select(c.Key("cv-f-color"), &e.color, canvasColors)
		})
		ui.Row(c).Gap(6).Children(func() {
			if ui.PrimaryButton(c.Key("cv-f-save"), "Save").Disabled(strings.TrimSpace(e.name) == "").Clicked() {
				s.canvasSave()
			}
			if ui.Button(c.Key("cv-f-cancel"), "Cancel").Clicked() {
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
func (s *Shell) canvasSave() {
	e := s.canvasEditor
	if e == nil {
		return
	}
	id := e.id
	name := strings.TrimSpace(e.name)
	desc := strings.TrimSpace(e.desc)
	color := e.color
	svc := s.svc
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
			s.actionMsg = "save failed: " + err.Error()
		} else {
			s.actionMsg = "board saved"
			s.canvasEditor = nil
		}
		delete(s.cache, "canvas-boards")
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

func (s *Shell) canvasDeleteBoard(id string) {
	svc := s.svc
	go func() {
		_, err := s.B.raw("canvas_delete_board", map[string]any{"boardId": numOrString(id)})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = "delete failed: " + err.Error()
		} else {
			s.actionMsg = "board deleted"
			if s.canvasSel == id {
				s.canvasSel = ""
			}
		}
		delete(s.cache, "canvas-boards")
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

func (s *Shell) canvasDeleteNode(id string) {
	svc := s.svc
	go func() {
		_, err := s.B.raw("canvas_delete_node", map[string]any{"nodeId": numOrString(id)})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = "delete failed: " + err.Error()
		} else {
			s.actionMsg = "node deleted"
		}
		if s.canvasSel != "" {
			delete(s.cache, "canvas-nodes:"+s.canvasSel)
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}
