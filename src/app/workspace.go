package app

// Workspace is the agent software's core surface, mirroring upstream
// src/app/workspace: open folders → their conversations → a conversation's
// transcript and a composer that drives the ACP agent. Three columns:
// folders | conversations | chat detail.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (s *Shell) viewWorkspace(c *ui.Context) {
	t := c.Theme()
	folders := asList(s.ensure("folders", "list_all_folder_details", nil))

	ui.Row(c).Fill().Children(func() {
		// One Surface sidebar: a fixed nav block on top, then a segmented
		// switcher (Recent | Pinned | Folders) over a single scrolling list —
		// NOT the vertical stacking of all three (which scrolls too long).
		ui.Column(c.Key("ws-side")).Width(290).FillHeight().Background(t.Surface).Children(func() {
			// Fixed nav block: New chat (primary action) + the route rows
			// upstream's sidebar carries — and only those. import-sessions /
			// project-boot open as SEPARATE windows upstream, never as sidebar
			// rows, so they live on the bottom chrome in this single-window
			// build instead.
			ui.Column(c.Key("ws-nav")).PaddingX(10).PaddingY(10).Gap(2).Children(func() {
				s.navRow(c, "nav-newchat", iconNewChat, cat(c).ActionNewChat(), false, func() {
					s.workbench = ""
					s.createChatConversation()
				})
				s.navRow(c, "nav-autom", iconAutomations, cat(c).NavAutomations(), s.workbench == "automations", func() {
					s.workbench = "automations"
				})
				s.navRow(c, "nav-tasks", iconTasks, cat(c).NavTodos(), s.workbench == "tasks", func() {
					s.workbench = "tasks"
				})
				s.navRow(c, "nav-forge", iconForge, cat(c).NavRepositories(), s.workbench == "forge", func() {
					s.workbench = "forge"
				})
				s.navRow(c, "nav-canvas", iconCanvas, cat(c).NavCanvas(), s.workbench == "canvas", func() {
					s.workbench = "canvas"
				})
			})
			ui.Divider(c)
			ui.Column(c.Key("ws-convs")).Grow(1).Padding(12).Gap(8).Children(func() {
				// Conversation search: client-side title filter over the cached
				// list (UI_GAPS P1-6). Shown for the conversation tabs only —
				// the Folders tab has no titles to search.
				if s.sidebarTab != "folders" {
					ui.Row(c.Key("ws-search-row")).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconSearch).Width(14).Height(14).TextColor(t.TextMuted)
						ui.TextInput(c.Key("ws-search"), &s.sidebarSearch).Grow(1).Placeholder(cat(c).SidebarSearchPlaceholder())
						if s.sidebarSearch != "" {
							iconButton(c, "ws-search-clear", iconX, cat(c).SidebarClearSearch(), func() {
								s.sidebarSearch = ""
							})
						}
					})
				}
				s.sidebarSegmented(c)
				// Only the selected section's list renders — one scroll region,
				// so no vertical pile-up of pinned + recent + folders.
				ui.Scroll(c).Grow(1).Children(func() {
					switch s.sidebarTab {
					case "folders":
						s.foldersList(c, folders)
					case "pinned":
						s.convList(c, "pinned")
					default:
						s.convList(c, "recent")
					}
				})
				if s.sidebarTab == "folders" {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						if ui.Button(c.Key("ws-open"), cat(c).SidebarOpenFolder()).Clicked() {
							s.openFolder()
						}
						iconButton(c, "ws-reload", iconRefresh, cat(c).SidebarReloadFolders(), func() {
							s.invalidate("folders")
						})
					})
				} else {
					s.viewNewConversation(c)
				}
			})
		})
		ui.Divider(c)
		// Main area: a full-page workbench route, or the conversation detail
		// beside the aux panel's detail pane (a file preview, a diff, a commit).
		ui.Column(c.Key("ws-detail")).Grow(1).FillHeight().Padding(20).Gap(10).Children(func() {
			if s.workbench != "" {
				s.viewWorkbenchRoute(c, s.workbench)
				return
			}
			ui.Row(c).Grow(1).Children(func() {
				// A detail opened from the aux panel (a file preview, a diff, a
				// commit) takes the main area; otherwise the conversation shows.
				if s.auxDetailOpen() {
					ui.Column(c.Key("aux-detail")).Grow(1).FillHeight().Padding(12).Gap(6).Children(func() {
						s.viewAuxDetail(c)
					})
					return
				}
				ui.Column(c.Key("ws-main")).Grow(1).FillHeight().Children(func() {
					s.viewDetailTabs(c)
				})
			})
		})
		// The right-hand aux panel (upstream's AuxPanel), hidden on a full-page
		// workbench route.
		if s.workbench == "" {
			ui.Divider(c)
			s.viewAuxPanel(c)
		}
	})
}

// sidebarSegmented is the Recent | Pinned | Folders switcher — the single-window
// stand-in for upstream's stacked sidebar sections. One section visible at a time.
func (s *Shell) sidebarSegmented(c *ui.Context) {
	t := c.Theme()
	ui.Row(c.Key("ws-seg")).Gap(6).Children(func() {
		for _, seg := range []struct{ id, label string }{
			{"recent", cat(c).SidebarTabRecent()},
			{"pinned", cat(c).SidebarTabPinned()},
			{"folders", cat(c).SidebarTabFolders()},
		} {
			active := s.sidebarTab == seg.id
			color := t.TextMuted
			if active {
				color = t.Text
			}
			seg := seg
			pill := ui.Row(c.Key("seg-" + seg.id)).PaddingX(11).PaddingY(4).Radius(12).AlignItems(ui.Center)
			if active {
				pill.Background(t.SurfacePressed)
			}
			pill.Children(func() {
				ui.Text(c, seg.label).FontSize(12).TextColor(color)
			})
			if pill.Clicked() {
				s.sidebarTab = seg.id
			}
		}
	})
}

// convList renders the recent (or pinned) conversations of the selected folder.
func (s *Shell) convList(c *ui.Context, which string) {
	t := c.Theme()
	if s.selectedFolder == "" {
		ui.Text(c, cat(c).SidebarSelectFolder()).FontSize(12).TextColor(t.TextMuted)
		return
	}
	convs := asList(s.ensure("convs:"+s.selectedFolder, "list_conversations", map[string]any{"folderId": numOrString(s.selectedFolder)}))
	// Filter by the search box's term (case-insensitive title substring) before
	// splitting into pinned/rest, so the term narrows both sections at once.
	if q := strings.TrimSpace(strings.ToLower(s.sidebarSearch)); q != "" {
		filtered := make([]map[string]any, 0, len(convs))
		for _, cv := range convs {
			if strings.Contains(strings.ToLower(str(cv, "title")), q) {
				filtered = append(filtered, cv)
			}
		}
		convs = filtered
	}
	var pinned, rest []map[string]any
	for _, cv := range convs {
		if isPinned(cv) {
			pinned = append(pinned, cv)
		} else {
			rest = append(rest, cv)
		}
	}
	list := rest
	emptyHint := cat(c).SidebarNoConversations()
	if which == "pinned" {
		list = pinned
		emptyHint = cat(c).SidebarNoPinned()
	}
	ui.Text(c, cat(c).SidebarChatsCount(len(list))).FontSize(11).TextColor(t.TextMuted)
	if strings.TrimSpace(s.sidebarSearch) != "" {
		emptyHint = cat(c).SidebarNoMatches()
	}
	ui.Column(c).Gap(1).Children(func() {
		if len(list) == 0 {
			ui.Text(c, emptyHint).FontSize(12).TextColor(t.TextMuted)
			return
		}
		for _, cv := range list {
			s.convRow(c, cv)
		}
	})
}

// foldersList renders the open folders (the "Folders" segment).
func (s *Shell) foldersList(c *ui.Context, folders []map[string]any) {
	t := c.Theme()
	if folders == nil {
		loadingState(c, "")
		return
	}
	if len(folders) == 0 {
		ui.Text(c, cat(c).SidebarNoFolders()).FontSize(12).TextColor(t.TextMuted)
		return
	}
	ui.Column(c).Gap(1).Children(func() {
		for _, f := range folders {
			id := fmt.Sprintf("%v", f["id"])
			name := str(f, "name")
			if name == "" {
				name = str(f, "path")
			}
			s.sideRow(c, "fd"+id, iconFolder, name, "", id == s.selectedFolder, func() {
				s.selectedFolder = id
				s.selectedConv = ""
				s.workbench = ""
				s.actionMsg = ""
			}, nil)
		}
	})
}

// convRow renders one conversation entry in the sidebar list, with a
// right-click menu (Rename / Pin↔Unpin / Delete) wired to the same Shell
// actions the detail header uses (UI_GAPS P1-6).
func (s *Shell) convRow(c *ui.Context, cv map[string]any) {
	id := fmt.Sprintf("%v", cv["id"])
	title := str(cv, "title")
	if title == "" {
		title = cat(c).Untitled()
	}
	sub := fmt.Sprintf("%s · %s", str(cv, "agent_type"), str(cv, "status"))
	pinned := isPinned(cv)
	s.sideRow(c, "cv"+id, iconChat, title, sub, id == s.selectedConv, func() {
		s.selectedConv = id
		s.workbench = ""
		s.actionMsg = ""
	}, func(m *ui.Menu) {
		if m.Item(cat(c).ActionRename()).Chosen() {
			// Open the detail header's inline rename for this row.
			s.selectedConv = id
			s.renaming = true
			s.renameText = title
		}
		if pinned {
			if m.Item(cat(c).ActionUnpin()).Chosen() {
				s.setPinned(id, false)
			}
		} else {
			if m.Item(cat(c).ActionPin()).Chosen() {
				s.setPinned(id, true)
			}
		}
		m.Separator()
		if m.Item(cat(c).ActionDelete()).Chosen() {
			s.deleteConversation(id)
		}
	})
}

// isPinned reports whether a conversation row carries a truthy pinned flag.
func isPinned(cv map[string]any) bool {
	switch v := cv["pinned"].(type) {
	case bool:
		return v
	case float64:
		return v != 0
	}
	if v, ok := cv["pinned_at"]; ok && v != nil {
		return true
	}
	return false
}

// sectionHeader renders a muted section label with a leading glyph.
func (s *Shell) sectionHeader(c *ui.Context, key string, icon *ui.SVG, label string) {
	t := c.Theme()
	ui.Row(c.Key(key)).PaddingX(8).PaddingY(4).Gap(6).AlignItems(ui.Center).Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Width(11).Height(11).TextColor(t.TextMuted)
		}
		ui.Text(c, label).FontSize(10).Bold().TextColor(t.TextMuted)
	})
}

// navRow renders a fixed top-of-sidebar nav/action row (pill-ish), selected
// when it is the active workbench route.
func (s *Shell) navRow(c *ui.Context, key string, icon *ui.SVG, label string, active bool, onClick func()) {
	t := c.Theme()
	row := ui.Row(c.Key(key)).PaddingX(8).PaddingY(6).Gap(9).Radius(7).AlignItems(ui.Center)
	if active {
		row.Background(t.SurfacePressed)
	}
	row.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Width(15).Height(15).TextColor(t.Text)
		}
		ui.Text(c, label).TextColor(t.Text)
	})
	if row.Clicked() {
		onClick()
	}
}

// viewWorkbenchRoute dispatches a non-conversation route to its panel.
func (s *Shell) viewWorkbenchRoute(c *ui.Context, route string) {
	switch route {
	case "automations":
		s.viewAutomations(c)
	case "tasks":
		s.viewTasks(c)
	case "canvas":
		s.viewCanvas(c)
	case "forge":
		s.viewForge(c)
	case "import-sessions":
		s.viewImportSessions(c)
	case "project-boot":
		s.viewProjectBoot(c)
	default:
		s.viewRoutePlaceholder(c, route)
	}
}

// viewRoutePlaceholder is the fallback for a workbench route whose panel is not
// built yet.
func (s *Shell) viewRoutePlaceholder(c *ui.Context, route string) {
	t := c.Theme()
	ui.Text(c, route).FontSize(22).Bold()
	ui.Text(c, cat(c).RouteNotBuilt()).TextColor(t.TextMuted).Wrap()
}

// asObjVal converts an already-decoded value into an object, or nil.
func asObjVal(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// intOf coerces a JSON number (float64) or int to an int.
func intOf(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

// truthy coerces a JSON-decoded value (bool / number) into a boolean.
func truthy(v any) bool {
	switch n := v.(type) {
	case bool:
		return n
	case float64:
		return n != 0
	}
	return false
}

// sideRow renders a selectable sidebar row: an icon, a title and an optional
// muted subtitle, highlighted when selected. onContextMenu, when non-nil,
// attaches a right-click menu (and the ⋯ affordance the OS shows for one).
func (s *Shell) sideRow(c *ui.Context, key string, icon *ui.SVG, title, subtitle string, selected bool, onClick func(), onContextMenu func(m *ui.Menu)) {
	t := c.Theme()
	row := ui.Row(c.Key(key)).PaddingX(8).PaddingY(5).Gap(8).Radius(6).AlignItems(ui.Center)
	if selected {
		row.Background(t.SurfacePressed) // subtle gray selection, upstream-style
	}
	row.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Width(14).Height(14).TextColor(t.TextMuted)
		}
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			ui.Text(c, title).MaxLines(1).TextColor(t.Text)
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(10).MaxLines(1).TextColor(t.TextMuted)
			}
		})
	})
	if onContextMenu != nil {
		row.ContextMenu(onContextMenu)
	}
	if row.Clicked() {
		onClick()
	}
}

// pickDefaultAgent chooses the initially-selected agent. It prefers an agent
// whose launcher is a direct binary (installed by the user, no package runner)
// and is available — that starts fastest and most reliably — then the first
// available agent, then the first listed. On a machine with only npx agents
// this reduces to upstream's "first listed" default.
func pickDefaultAgent(agents []map[string]any, options []string) string {
	firstAvailable := ""
	for _, a := range agents {
		at := str(a, "agent_type")
		if at == "" {
			continue
		}
		if avail, _ := a["available"].(bool); !avail {
			continue
		}
		if firstAvailable == "" {
			firstAvailable = at
		}
		if str(a, "distribution_type") == "binary" {
			return at
		}
	}
	if firstAvailable != "" {
		return firstAvailable
	}
	return options[0]
}

// viewNewConversation renders the agent picker + Create button beneath the
// conversation list.
func (s *Shell) viewNewConversation(c *ui.Context) {
	agents := asList(s.ensure("agents", "acp_list_agents", nil))
	options := make([]string, 0, len(agents))
	for _, a := range agents {
		if at := str(a, "agent_type"); at != "" {
			options = append(options, at)
		}
	}
	if s.newAgent == "" && len(options) > 0 {
		s.newAgent = pickDefaultAgent(agents, options)
	}
	ui.Row(c).Gap(6).Children(func() {
		if len(options) > 0 {
			ui.Select(c.Key("new-agent"), &s.newAgent, options).Grow(1)
		}
		if ui.PrimaryButton(c.Key("new-conv"), "New").Disabled(s.newAgent == "").Clicked() {
			s.createConversation(numOrString(s.selectedFolder), s.newAgent)
		}
	})
}

// viewConversationDetail renders the selected conversation's header, transcript
// and composer. The transcript source (per-agent JSONL parsing) is not yet
// ported, so historical turns are usually empty; the composer drives the live
// ACP agent, which populates events going forward.
func (s *Shell) viewConversationDetail(c *ui.Context) {
	t := c.Theme()
	if s.selectedConv == "" {
		// Welcome / empty state, mirroring upstream's "no conversation selected"
		// canvas: a centered product mark, a one-line hint and the two primary
		// ways in (start a chat, or open a folder to work in).
		ui.Column(c.Key("ws-welcome")).Fill().Center().Gap(14).Children(func() {
			ui.Column(c).Center().Gap(6).Children(func() {
				ui.Text(c, cat(c).AppTitle()).FontSize(28).Bold()
				ui.Text(c, cat(c).AppTagline()).
					FontSize(13).TextColor(t.TextMuted)
			})
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				if ui.PrimaryButton(c.Key("welcome-newchat"), cat(c).ActionNewChat()).Clicked() {
					s.workbench = ""
					s.createChatConversation()
				}
				if ui.Button(c.Key("welcome-open"), cat(c).SidebarOpenFolder()).Clicked() {
					s.openFolder()
				}
			})
		})
		return
	}
	cv := s.convByID(s.selectedConv)
	title := str(cv, "title")
	if title == "" {
		title = cat(c).Untitled()
	}
	// Header: title (or inline rename) + management actions.
	ui.Row(c.Key("conv-header")).Gap(8).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			if s.renaming {
				ui.Row(c).Gap(6).Children(func() {
					ui.TextInput(c.Key("rename-input"), &s.renameText).Grow(1).Placeholder("Conversation title")
					if ui.PrimaryButton(c.Key("rename-save"), "Save").Disabled(s.renameText == "").Clicked() {
						s.renameConversation(s.selectedConv, s.renameText)
					}
					if ui.Button(c.Key("rename-cancel"), "Cancel").Clicked() {
						s.renaming = false
					}
				})
			} else {
				ui.Text(c, title).FontSize(20).Bold()
			}
			ui.Textf(c, "%s · %s", str(cv, "agent_type"), str(cv, "status")).FontSize(12).TextColor(t.TextMuted)
		})
		if !s.renaming {
			if ui.Button(c.Key("act-rename"), "Rename").Clicked() {
				s.renaming = true
				s.renameText = title
			}
			if ui.Button(c.Key("act-pin"), "Pin").Clicked() {
				s.setPinned(s.selectedConv, true)
			}
			if ui.Button(c.Key("act-unpin"), "Unpin").Clicked() {
				s.setPinned(s.selectedConv, false)
			}
			if ui.Button(c.Key("act-delete"), "Delete").Clicked() {
				s.deleteConversation(s.selectedConv)
			}
		}
	})

	// Transcript: parsed historical turns plus the live ACP rows.
	turnsKey := "turns:" + s.selectedConv
	turnsRaw := s.ensure(turnsKey, "get_folder_conversation_turns", map[string]any{"conversationId": numOrString(s.selectedConv)})
	turns := []any{}
	if turnsRaw != nil {
		var page map[string]any
		_ = json.Unmarshal(turnsRaw, &page)
		if arr, ok := page["turns"].([]any); ok {
			turns = arr
		}
	}
	// Live ACP state for this conversation's active connection: the raw event
	// stream folded into transcript rows, plus the consolidated snapshot
	// (structured tool calls + pending cards + status).
	s.mu.Lock()
	connID := s.connByConv[s.selectedConv]
	s.mu.Unlock()
	var live []transcriptItem
	var snap map[string]any
	if connID != "" {
		if s.stream != nil {
			live = buildTranscript(s.stream.Events(connID))
		}
		snap = asObj(s.ensure("snapshot:"+connID, "acp_get_session_snapshot", map[string]any{"connectionId": connID}))
	}
	// A cold attach hands us the in-flight tool calls only through the snapshot,
	// so fold in any the event stream has not already surfaced.
	if snap != nil {
		seen := map[string]bool{}
		for _, it := range live {
			if it.kind == "tool" && it.tool != nil {
				seen[it.tool.id] = true
			}
		}
		for _, raw := range asListVal(snap["active_tool_calls"]) {
			id := str(raw, "id")
			if id != "" && seen[id] {
				continue
			}
			live = append(live, transcriptItem{kind: "tool", tool: &toolCard{
				id:     id,
				title:  firstNonEmpty(str(raw, "label"), str(raw, "kind"), "Tool"),
				kind:   str(raw, "kind"),
				status: str(raw, "status"),
				output: firstNonEmpty(str(raw, "content"), anyText(raw["output"])),
			}})
		}
	}

	// streaming: a turn is in flight — the transcript pins a blinking caret to
	// the tail and follows the end (UI_GAPS P1-5).
	s.viewTranscript(c, turns, live, str(snap, "status") == "prompting")

	// Pending permission / question cards from the live session snapshot.
	s.viewPendingCards(c, connID, snap)

	s.viewActionOrError(c)

	// Composer dock: a status strip, then the bordered input with the actions
	// aligned to the right (upstream-style).
	s.viewComposerDock(c, connID, str(snap, "status"), "")
}

// cancelTurn aborts the in-flight turn on a connection.
func (s *Shell) cancelTurn(connID string) {
	svc := s.svc
	go func() {
		_, err := s.B.raw("acp_cancel", map[string]any{"connectionId": connID})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = "cancel failed: " + err.Error()
		} else {
			s.actionMsg = "cancel requested"
		}
		delete(s.cache, "snapshot:"+connID)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// turnText pulls a human-readable string out of a MessageTurn's content.
func turnText(m map[string]any) string {
	if m == nil {
		return ""
	}
	if t := str(m, "text"); t != "" {
		return t
	}
	if t := str(m, "content"); t != "" {
		return t
	}
	return ""
}

// convByID reads the selected folder's cached conversation list for one row.
func (s *Shell) convByID(id string) map[string]any {
	for _, cv := range asList(s.peek("convs:" + s.selectedFolder)) {
		if fmt.Sprintf("%v", cv["id"]) == id {
			return cv
		}
	}
	return nil
}

func (s *Shell) folderPathByID(id string) string {
	for _, f := range asList(s.peek("folders")) {
		if fmt.Sprintf("%v", f["id"]) == id {
			return str(f, "path")
		}
	}
	return ""
}

// createConversation inserts a conversation and selects it. Mutation runs off
// the UI thread; the view repaints when it lands.
func (s *Shell) createConversation(folderID any, agent string) {
	svc := s.svc
	folderKey := "convs:" + s.selectedFolder
	go func() {
		raw, err := s.B.raw("create_conversation", map[string]any{"folderId": folderID, "agentType": agent})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = "create failed: " + err.Error()
		} else {
			var cv map[string]any
			_ = json.Unmarshal(raw, &cv)
			if id, ok := cv["id"]; ok {
				s.selectedConv = fmt.Sprintf("%v", id)
			}
			s.actionMsg = "conversation created"
			delete(s.cache, folderKey)
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// sendPrompt connects the conversation's agent at the folder cwd and sends the
// composer text. Errors (e.g. the agent binary is not installed) surface in
// actionMsg rather than blocking the UI.
func (s *Shell) sendPrompt(convID string) {
	agent := str(s.convByID(convID), "agent_type")
	folderPath := s.folderPathByID(s.selectedFolder)
	s.mu.Lock()
	prompt := s.composer
	// Snapshot and clear staged attachments so a re-entrant prompt can't double
	// send them; an empty prompt with attachments is still a valid turn.
	atts := append([]attachmentItem(nil), s.composerAttachments...)
	s.composer = ""
	s.composerAttachments = nil
	s.mu.Unlock()
	if prompt == "" && len(atts) == 0 {
		return
	}
	svc := s.svc
	go func() {
		s.setAction("connecting…", svc)
		craw, err := s.B.raw("acp_connect", map[string]any{"agentType": agent, "folderPath": folderPath, "conversationId": numOrString(convID)})
		if err != nil {
			s.setConnectError(err, svc)
			return
		}
		var ci map[string]any
		_ = json.Unmarshal(craw, &ci)
		connID := fmt.Sprintf("%v", ci["id"])
		s.mu.Lock()
		s.connByConv[convID] = connID
		s.mu.Unlock()
		s.stream.Attach(connID) // subscribe before prompting so no events are missed

		// Upload every staged file, then reference the stored copies in the
		// prompt's content blocks.
		uploaded := make([]attachmentItem, 0, len(atts))
		for _, a := range atts {
			ua, e := s.uploadAttachmentHTTP(connID, a.Path)
			if e != nil {
				s.setAction("upload failed: "+e.Error(), svc)
				return
			}
			uploaded = append(uploaded, ua)
		}
		blocks := buildPromptBlocks(prompt, uploaded)
		if _, err := s.B.raw("acp_prompt", map[string]any{"connectionId": connID, "blocks": blocks}); err != nil {
			s.setAction("prompt failed: "+err.Error(), svc)
			return
		}
		// Echo the user's own turn (text + attachments) into the local
		// transcript so it renders like the agent's reply.
		s.stream.AppendLocal(connID, map[string]any{
			"type":        "user_message",
			"text":        prompt,
			"attachments": attachmentsToRaw(uploaded),
		})
		s.setAction("prompt sent", svc)
	}()
}

// notify queues a transient outcome message for the window's toast manager —
// mygo's c.Toast is a view-time call, so a mutation running on a goroutine
// queues its message here and the next frame hands it over. A message that
// reads as a failure (our error paths all say "… failed: …") shows as an error
// toast. Replaces the per-panel status lines this app used to render itself.
func (s *Shell) notify(msg string) {
	to := ui.Toast{Title: msg}
	if strings.Contains(msg, "failed") {
		to.Type = "error"
	}
	s.toastMu.Lock()
	s.toastQueue = append(s.toastQueue, to)
	s.toastMu.Unlock()
}

// drainToasts hands the queued messages to mygo's toast manager. Called once a
// frame from View. The lock is dropped BEFORE any toast is shown: showing one
// asks the engine for a frame, which re-enters the view, and the view reaches
// for s.mu — never hold one of our locks across AddToast.
func (s *Shell) drainToasts(c *ui.Context) {
	s.toastMu.Lock()
	pending := s.toastQueue
	s.toastQueue = nil
	s.toastMu.Unlock()
	for _, to := range pending {
		c.AddToast(to)
	}
}

// viewToasts draws the window's toasts with mygo's ToastBase (the same parts
// the theme's look uses), in this view's own tree: bottom-centre, inverse
// surface, an alert glyph for an error, and a close affordance. Building the
// viewport ourselves — rather than leaving it to the theme's look — keeps it in
// this tree, so it paints in offscreen renders too. Call it once a frame.
func (s *Shell) viewToasts(c *ui.Context) {
	t := c.Theme()
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		viewport.Bottom(t.Space(6)).AlignItems(ui.Center).Gap(t.Space(2))
		for _, ts := range toasts {
			toast := ui.ToastBase(c, ts)
			root := toast.Root
			root.Row().AlignItems(ui.Center).Gap(t.Space(3)).Padding(t.Space(2.5), t.Space(4)).
				Radius(t.Space(2)).Background(t.Inverse).TextColor(t.InverseText).
				MaxWidth(420).Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.25))
			root.Children(func() {
				if ts.Type == "error" {
					ui.Icon(c, iconAlert).Width(14).Height(14).TextColor(t.Danger)
				}
				ui.Text(c, ts.Title)
				toast.CloseButton().Label("Close toast").Children(func() {
					ui.Icon(c, iconX).Width(12).Height(12).TextColor(t.InverseText)
				})
			})
		}
	})
}

func (s *Shell) setAction(msg string, svc ui.Services) {
	s.mu.Lock()
	s.actionMsg = msg
	if msg != "" { // any fresh status clears a stale error banner
		s.errorMsg, s.errorHint = "", ""
	}
	s.mu.Unlock()
	svc.Invalidate()
}

// setConnectError shows a failed connect/prompt as a banner (friendly message +
// actionable hint) instead of a muted one-liner with a raw JSON body.
func (s *Shell) setConnectError(err error, svc ui.Services) {
	s.mu.Lock()
	s.actionMsg = ""
	s.errorMsg = err.Error()
	s.errorHint = ""
	if ae, ok := err.(*apiError); ok {
		s.errorHint = ae.hint()
	}
	s.mu.Unlock()
	svc.Invalidate()
}

// viewActionOrError renders the action line, or — when the last action failed —
// a red banner carrying the friendly message plus an actionable hint.
func (s *Shell) viewActionOrError(c *ui.Context) {
	t := c.Theme()
	if s.errorMsg != "" {
		ui.Column(c.Key("error-banner")).FillWidth().Radius(8).
			Border(1, t.Danger.Alpha(0.4)).Background(t.Danger.Alpha(0.08)).
			Padding(10, 12, 10, 12).Gap(4).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
				ui.Icon(c, iconAlert).Width(14).Height(14).TextColor(t.Danger)
				ui.Text(c, s.errorMsg).FontSize(13).TextColor(t.Danger).Wrap()
			})
			if s.errorHint != "" {
				ui.Text(c, s.errorHint).FontSize(12).TextColor(t.TextMuted).Wrap()
			}
		})
		return
	}
	if s.actionMsg != "" {
		ui.Text(c, s.actionMsg).FontSize(12).TextColor(t.TextMuted)
	}
}

// peek returns a cached raw response without triggering a fetch.
func (s *Shell) peek(key string) json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache[key]
}

// numOrString sends folder/conversation ids as numbers when they parse, else
// as strings, so the backend's GetInt sees the right type.
func numOrString(s string) any {
	var n int64
	if _, err := fmt.Sscanf(s, "%d", &n); err == nil {
		return n
	}
	return s
}

// anyText coerces a decoded JSON value to display text: strings pass through,
// objects and arrays are re-encoded compactly, everything else is empty.
func anyText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case map[string]any, []any:
		b, err := json.Marshal(x)
		if err != nil {
			return ""
		}
		return string(b)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// viewPendingCards renders the agent's in-flight permission request (with its
// option buttons wired to acp_respond_permission) and any pending question.
func (s *Shell) viewPendingCards(c *ui.Context, connID string, snap map[string]any) {
	t := c.Theme()
	if snap == nil {
		return
	}
	if pp, ok := snap["pending_permission"].(map[string]any); ok {
		reqID := str(pp, "request_id")
		ui.Column(c.Key("perm")).FillWidth().Radius(8).
			Border(1, t.Warning.Alpha(0.35)).Background(t.Warning.Alpha(0.07)).
			Padding(12, 12, 12, 12).Gap(8).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconAlert).Width(14).Height(14).TextColor(t.Warning)
				ui.Text(c, "Permission requested").FontSize(13).Bold()
			})
			if tc, ok := pp["tool_call"].(map[string]any); ok {
				if lbl := firstNonEmpty(str(tc, "label"), str(tc, "title")); lbl != "" {
					ui.Text(c, lbl).FontSize(13).TextColor(t.TextMuted).Wrap()
				}
			}
			ui.Row(c).Gap(6).Wrap().Children(func() {
				opts, _ := pp["options"].([]any)
				for i, raw := range opts {
					o, _ := raw.(map[string]any)
					oid := firstNonEmpty(str(o, "optionId"), str(o, "option_id"), str(o, "id"))
					name := firstNonEmpty(str(o, "name"), str(o, "label"), oid)
					if ui.Button(c.Key(itoa("opt", i)), name).Clicked() {
						s.respondPermission(connID, reqID, oid)
					}
				}
			})
		})
	}
	if pq, ok := snap["pending_question"].(map[string]any); ok {
		ui.Column(c.Key("ques")).FillWidth().Radius(8).Border(1, t.Border).
			Background(t.Surface).Padding(12, 12, 12, 12).Gap(8).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconInfo).Width(14).Height(14).TextColor(t.TextMuted)
				ui.Text(c, "The agent is asking a question").FontSize(13).Bold()
			})
			if qs, ok := pq["questions"].([]any); ok {
				for i, raw := range qs {
					q, _ := raw.(map[string]any)
					if text := firstNonEmpty(str(q, "question"), str(q, "prompt"), str(q, "text")); text != "" {
						ui.Text(c.Key(itoa("q", i)), text).FontSize(13).TextColor(t.Text).Wrap()
					}
				}
			}
			ui.Text(c, "Answer in the composer below.").FontSize(11).TextColor(t.TextMuted)
		})
	}
}

// respondPermission answers an in-flight permission request off the UI thread.
func (s *Shell) respondPermission(connID, requestID, optionID string) {
	svc := s.svc
	go func() {
		_, err := s.B.raw("acp_respond_permission", map[string]any{"connectionId": connID, "requestId": requestID, "optionId": optionID})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = "permission failed: " + err.Error()
		} else {
			s.actionMsg = "permission: " + optionID
		}
		delete(s.cache, "snapshot:"+connID)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// firstNonEmpty returns the first non-empty string argument.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// renameConversation sets a manual title (title_locked) and refreshes the list.
func (s *Shell) renameConversation(convID, title string) {
	svc := s.svc
	folderKey := "convs:" + s.selectedFolder
	go func() {
		_, err := s.B.raw("update_conversation_title", map[string]any{"conversationId": numOrString(convID), "title": title})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = "rename failed: " + err.Error()
		} else {
			s.actionMsg = "renamed"
			s.renaming = false
		}
		delete(s.cache, folderKey)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// setPinned pins or unpins a conversation and refreshes the list.
func (s *Shell) setPinned(convID string, pinned bool) {
	svc := s.svc
	folderKey := "convs:" + s.selectedFolder
	go func() {
		_, err := s.B.raw("update_conversation_pinned", map[string]any{"conversationId": numOrString(convID), "pinned": pinned})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = "pin failed: " + err.Error()
		} else if pinned {
			s.actionMsg = "pinned"
		} else {
			s.actionMsg = "unpinned"
		}
		delete(s.cache, folderKey)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// deleteConversation soft-deletes the conversation and clears the selection.
func (s *Shell) deleteConversation(convID string) {
	svc := s.svc
	folderKey := "convs:" + s.selectedFolder
	go func() {
		_, err := s.B.raw("delete_conversation", map[string]any{"conversationId": numOrString(convID)})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = "delete failed: " + err.Error()
		} else {
			s.actionMsg = "deleted"
			if s.selectedConv == convID {
				s.selectedConv = ""
			}
			s.renaming = false
		}
		delete(s.cache, folderKey)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// openFolder shows the native directory picker and adds the chosen folder to
// the workspace, selecting it. Runs off the UI thread; the picked path goes
// through the same add_folder_to_history the client uses.
func (s *Shell) openFolder() {
	svc := s.svc
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "Open folder", Directory: true})
		if err != nil || len(paths) == 0 {
			return
		}
		raw, rerr := s.B.raw("add_folder_to_history", map[string]any{"path": paths[0]})
		s.mu.Lock()
		if rerr != nil {
			s.actionMsg = "open failed: " + rerr.Error()
		} else {
			var f map[string]any
			_ = json.Unmarshal(raw, &f)
			if id, ok := f["id"]; ok {
				s.selectedFolder = fmt.Sprintf("%v", id)
				s.selectedConv = ""
			}
		}
		delete(s.cache, "folders")
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// createChatConversation starts a no-folder chat (upstream's primary action) and
// selects it. Uses create_chat_conversation, which mints a hidden chat folder
// + conversation server-side; the firehose refresh picks up the new rows.
func (s *Shell) createChatConversation() {
	agent := s.newAgent
	if agent == "" {
		agent = "claude_code"
	}
	svc := s.svc
	go func() {
		raw, err := s.B.raw("create_chat_conversation", map[string]any{"agentType": agent})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = "new chat failed: " + err.Error()
		} else {
			var res map[string]any
			_ = json.Unmarshal(raw, &res)
			if fid, ok := res["folderId"]; ok {
				s.selectedFolder = fmt.Sprintf("%v", fid)
			}
			if cid, ok := res["conversationId"]; ok {
				s.selectedConv = fmt.Sprintf("%v", cid)
			}
			s.actionMsg = ""
			delete(s.cache, "folders")
			if s.selectedFolder != "" {
				delete(s.cache, "convs:"+s.selectedFolder)
			}
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}
