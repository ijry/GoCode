package app

// Workspace is the agent software's core surface, mirroring upstream
// src/app/workspace: open folders → their conversations → a conversation's
// transcript and a composer that drives the ACP agent. Three columns:
// folders | conversations | chat detail.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"gocode/locales"
)

func (s *Shell) viewWorkspace(c *ui.Context) {
	t := c.Theme()
	folders := asList(s.ensure("folders", "list_all_folder_details", nil))

	// Column: the three-column workspace grows to fill, then the bottom
	// collapsible terminal panel sits beneath it (upstream's terminal is a
	// bottom bar shared by every conversation tab, not a main-area tab).
	ui.Column(c.Key("ws-root")).Fill().Children(func() {
		ui.Row(c.Key("ws-cols")).Grow(1).Children(func() {
			// One Surface sidebar: a fixed nav block on top, then a segmented
			// switcher (Recent | Pinned | Folders) over a single scrolling list —
			// NOT the vertical stacking of all three (which scrolls too long).
			// toggle_sidebar (⌘B) hides it; the same chord brings it back.
			if !s.sidebarHidden {
				ui.Column(c.Key("ws-side")).Width(290).FillHeight().Background(t.Surface).Children(func() {
					// Fixed nav block: New chat (primary action) + the route rows
					// upstream's sidebar carries — and only those. import-sessions /
					// project-boot open as SEPARATE windows upstream, never as sidebar
					// rows, so they live on the bottom chrome in this single-window
					// build instead.
					ui.Column(c.Key("ws-nav")).PaddingX(10).PaddingY(10).Gap(2).Children(func() {
						s.navRow(c, "nav-newchat", iconNewChat, cat(c).ActionNewChat(), false, func() {
							s.workbench = ""
							s.createChatConversation(c.Locale())
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
						s.navRow(c, "nav-usage", iconUsage, cat(c).NavUsage(), s.workbench == "token-usage", func() {
							s.workbench = "token-usage"
						})
						s.navRow(c, "nav-browser", iconBrowser, cat(c).NavBrowser(), s.workbench == "browser", func() {
							s.workbench = "browser"
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
									s.openFolder(c.Locale())
								}
								if ui.Button(c.Key("ws-clone"), cat(c).CloneShort()).Clicked() {
									s.workbench = "clone"
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
			}
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
			// workbench route and when toggle_aux_panel (⌘E) collapses it.
			if s.workbench == "" && !s.auxHidden {
				ui.Divider(c)
				s.viewAuxPanel(c)
			}
		})
		// Bottom collapsible terminal panel, shared across conversation tabs.
		// Always present (its header toggles the body), matching upstream's
		// bottom terminal bar.
		s.viewTerminalPanel(c)
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
		s.openConversationRow(cv)
	}, func(m *ui.Menu) {
		if m.Item(cat(c).ActionRename()).Chosen() {
			// Open the detail header's inline rename for this row.
			s.openConversationRow(cv)
			s.renaming = true
			s.renameText = title
		}
		if pinned {
			if m.Item(cat(c).ActionUnpin()).Chosen() {
				s.setPinned(c.Locale(), id, false)
			}
		} else {
			if m.Item(cat(c).ActionPin()).Chosen() {
				s.setPinned(c.Locale(), id, true)
			}
		}
		m.Separator()
		if m.Item(cat(c).ActionDelete()).Chosen() {
			s.deleteConversation(c.Locale(), id)
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
	if isGitRoute(route) {
		s.viewGitRoute(c, route)
		return
	}
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
	case "commands":
		s.viewFolderCommands(c)
	case "clone":
		s.viewCloneRepository(c)
	case "token-usage":
		s.viewTokenUsage(c)
	case "browser":
		s.viewBrowserPanel(c)
	case "workspace-folders":
		s.viewWorkspaceFolders(c)
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
		if ui.PrimaryButton(c.Key("new-conv"), cat(c).ActionNew()).Disabled(s.newAgent == "").Clicked() {
			s.createConversation(c.Locale(), numOrString(s.selectedFolder), s.newAgent)
		}
	})
}

// welcomeTipText picks the empty-state tip once and returns it; the index stays
// fixed for the session so the tip doesn't flicker between frames.
func (s *Shell) welcomeTipText(c *ui.Context) string {
	m := cat(c)
	tips := []string{
		m.WelcomeTipMention(), m.WelcomeTipQueue(), m.WelcomeTipFork(),
		m.WelcomeTipTerminal(), m.WelcomeTipFeedback(), m.WelcomeTipSearch(),
		m.WelcomeTipTasks(), m.WelcomeTipAux(), m.WelcomeTipDrop(),
	}
	if s.welcomeTip < 0 || s.welcomeTip >= len(tips) {
		s.welcomeTip = int(time.Now().UnixNano() % int64(len(tips)))
	}
	return tips[s.welcomeTip]
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
					s.createChatConversation(c.Locale())
				}
				if ui.Button(c.Key("welcome-open"), cat(c).SidebarOpenFolder()).Clicked() {
					s.openFolder(c.Locale())
				}
			})
			// A rotating tip about a real feature (upstream's WelcomeTip), so the
			// empty state teaches rather than just sitting there.
			if tip := s.welcomeTipText(c); tip != "" {
				ui.Row(c.Key("welcome-tip")).Gap(6).AlignItems(ui.Center).
					Padding(6, 12, 6, 12).Radius(12).Background(t.Surface).
					Border(1, t.Border).Children(func() {
					ui.Icon(c, iconInfo).Width(13).Height(13).TextColor(t.Accent)
					ui.Text(c, tip).FontSize(11).TextColor(t.TextMuted).MaxLines(2)
				})
			}
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
					ui.TextInput(c.Key("rename-input"), &s.renameText).Grow(1).Placeholder(cat(c).ConvTitlePlaceholder())
					if ui.PrimaryButton(c.Key("rename-save"), cat(c).ActionSave()).Disabled(s.renameText == "").Clicked() {
						s.renameConversation(c.Locale(), s.selectedConv, s.renameText)
					}
					if ui.Button(c.Key("rename-cancel"), cat(c).ActionCancel()).Clicked() {
						s.renaming = false
					}
				})
			} else {
				ui.Text(c, title).FontSize(20).Bold()
			}
			ui.Textf(c, "%s · %s", str(cv, "agent_type"), str(cv, "status")).FontSize(12).TextColor(t.TextMuted)
		})
		if !s.renaming {
			if ui.Button(c.Key("act-rename"), cat(c).ActionRename()).Clicked() {
				s.renaming = true
				s.renameText = title
			}
			// Fork branches the live agent session: the pre-fork history moves to
			// a sibling conversation and this row continues from the fork point.
			if s.connByConv[s.selectedConv] != "" {
				if ui.Button(c.Key("act-fork"), cat(c).ActionFork()).Clicked() {
					s.forkConversation(c)
				}
			}
			if ui.Button(c.Key("act-pin"), cat(c).ActionPin()).Clicked() {
				s.setPinned(c.Locale(), s.selectedConv, true)
			}
			if ui.Button(c.Key("act-unpin"), cat(c).ActionUnpin()).Clicked() {
				s.setPinned(c.Locale(), s.selectedConv, false)
			}
			if ui.Button(c.Key("act-delete"), cat(c).ActionDelete()).Clicked() {
				s.deleteConversation(c.Locale(), s.selectedConv)
			}
			// Export as Markdown (upstream's exportConversation; this build has
			// no save dialog, so it copies the whole conversation instead).
			iconButton(c, "act-copy-md", iconFile, cat(c).ActionCopyMarkdown(), func() {
				md := s.conversationMarkdown(cat(c), s.selectedConv)
				if strings.TrimSpace(md) == "" {
					return
				}
				c.WriteClipboard(md)
				s.notify(cat(c).ToastCopied())
			})
			// Export as a standalone HTML file (upstream's exportConversation
			// html arm): the native save dialog picks the path.
			iconButton(c, "act-export-html", iconDownload, cat(c).ActionExportHTML(), func() {
				s.exportHTML(c.Locale(), s.selectedConv)
			})
		}
	})

	// Transcript: parsed historical turns plus the live ACP rows. The history is
	// paged backwards (100 turns at a time) so a long conversation stays usable;
	// viewTranscript heads the list with "Load earlier messages" while older
	// turns remain.
	turns := s.conversationTurns(c, s.selectedConv)
	// Live ACP state for this conversation's active connection: the raw event
	// stream folded into transcript rows, plus the consolidated snapshot
	// (structured tool calls + pending cards + status).
	s.mu.Lock()
	connID := s.connByConv[s.selectedConv]
	s.mu.Unlock()
	if connID == "" {
		connID = s.adoptLiveConnection(s.selectedConv)
	}
	var live []transcriptItem
	var snap map[string]any
	if connID != "" {
		if s.stream != nil {
			live = buildTranscript(s.stream.Events(connID), cat(c))
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
				title:  firstNonEmpty(str(raw, "label"), str(raw, "kind"), cat(c).ToolFallback()),
				kind:   str(raw, "kind"),
				status: str(raw, "status"),
				output: firstNonEmpty(str(raw, "content"), anyText(raw["output"])),
			}})
		}
	}

	// streaming: a turn is in flight — the transcript pins a blinking caret to
	// the tail and follows the end (UI_GAPS P1-5).
	s.viewTranscript(c, turns, live, str(snap, "status") == "prompting")

	// The agent's plan (live event, else the snapshot's copy).
	var planRaw json.RawMessage
	if connID != "" && s.stream != nil {
		planRaw = s.stream.Plan(connID)
	}
	if planRaw == nil {
		if raw, ok := snap["plan"].(json.RawMessage); ok {
			planRaw = raw
		} else if b, err := json.Marshal(snap["plan"]); err == nil && string(b) != "null" {
			planRaw = b
		}
	}
	if len(planRaw) > 0 {
		var entries []any
		if json.Unmarshal(planRaw, &entries) == nil {
			s.viewPlanCard(c, entries)
		}
	}

	// Pending permission / question cards from the live session snapshot.
	s.viewPendingCards(c, connID, snap)

	// Background tasks the agent reported (ACP async_task_* updates).
	if tasks, ok := snap["async_tasks"].([]any); ok && len(tasks) > 0 {
		s.viewAsyncTasks(c, connID, tasks)
	}

	// Sends waiting behind the running turn (the engine queues them, so the
	// user sees what will go out next and can drop one).
	if queued := asStringList(snap["queued_prompts"]); len(queued) > 0 {
		s.viewQueuedPrompts(c, connID, queued)
	}

	// Live-feedback notes the user sent to steer the running turn.
	if notes, ok := snap["feedback"].([]any); ok && len(notes) > 0 {
		s.viewFeedbackNotes(c, notes, str(snap, "status") == "prompting")
	}

	s.viewActionOrError(c)

	// Composer dock: a status strip, then the bordered input with the actions
	// aligned to the right (upstream-style). The context meter prefers the live
	// stream's reading (it moves mid-turn) and falls back to the snapshot's.
	var usage *contextUsage
	if u := asObjVal(snap["context_usage"]); u != nil {
		usage = &contextUsage{Used: uint64(intVal(u, "used")), Size: uint64(intVal(u, "size"))}
	}
	if connID != "" && s.stream != nil {
		if live, ok := s.stream.Usage(connID); ok {
			usage = &live
		}
	}
	s.viewComposerDock(c, connID, str(snap, "status"), "", usage, asBool(snap["feedback_tool_available"]),
		apiRetryLine(cat(c), asObjVal(snap["api_retry"])))
}

// forkConversation branches the live session at the tail: the backend moves the
// pre-fork history onto a sibling conversation row and re-points this one at the
// forked session, so the sidebar gains a row and this conversation continues.
func (s *Shell) forkConversation(c *ui.Context) {
	convID := s.selectedConv
	s.mu.Lock()
	connID := s.connByConv[convID]
	folderID := s.selectedFolder
	s.mu.Unlock()
	if connID == "" {
		return
	}
	svc := s.svc
	loc := c.Locale()
	go func() {
		raw, err := s.B.raw("acp_fork", map[string]any{
			"connectionId":   connID,
			"conversationId": numOrString(convID),
			"folderId":       numOrString(folderID),
		})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).ToastForkFailed(err.Error()))
		} else {
			res := asObj(raw)
			s.notify(locales.In(loc).ToastForked())
			// The sibling holds the pre-fork history: show it in the sidebar now.
			if sib := intVal(res, "siblingConversationId"); sib > 0 {
				delete(s.cache, "convs:"+folderID)
				delete(s.cache, "all-convs")
			}
			// The forked session is a new agent session: its transcript starts
			// fresh, so drop the accumulated history for this conversation.
			if s.convTurnsConv == convID {
				s.convTurnsPage = nil
				s.convTurnsOffset = -1
				s.convTurnsTotal = 0
			}
			delete(s.cache, "turns:"+convID)
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// retryLastPrompt resends the prompt of the failed turn: the text is still in
// lastPrompt, so the composer only needs to hand it back to sendPrompt.
func (s *Shell) retryLastPrompt(loc, convID string) {
	s.mu.Lock()
	prompt := s.lastPrompt
	if prompt != "" {
		s.composer = prompt
	}
	s.mu.Unlock()
	if prompt == "" {
		return
	}
	s.sendPrompt(loc, convID)
}

// reconnectLive re-establishes a dropped conversation's live connection without
// sending a prompt (the composer strip offers it when the connection is gone).
// `acp_connect` resumes the agent session via session/load, so history is kept.
func (s *Shell) reconnectLive(loc, convID string) {
	agent := str(s.convByID(convID), "agent_type")
	folderPath := s.folderPathByID(s.selectedFolder)
	svc := s.svc
	go func() {
		s.setAction(locales.In(loc).ToastConnecting(), svc)
		craw, err := s.B.raw("acp_connect", map[string]any{
			"agentType": agent, "folderPath": folderPath, "conversationId": numOrString(convID),
		})
		if err != nil {
			s.setConnectError(err, svc)
			return
		}
		var ci map[string]any
		_ = json.Unmarshal(craw, &ci)
		connID := fmt.Sprintf("%v", ci["id"])
		s.mu.Lock()
		s.connByConv[convID] = connID
		delete(s.cache, "liveconn:"+convID)
		delete(s.cache, "snapshot:"+connID)
		s.mu.Unlock()
		s.stream.Attach(connID) // subscribe before the first snapshot arrives
		s.setAction("", svc)    // the refreshed snapshot carries the new status
	}()
}

// adoptLiveConnection finds a live agent session for a conversation this UI has
// no connection id for — the backend keeps sessions alive across a window
// reload, and another client (the mobile app) may have started one. Adopting it
// is what makes its status, in-flight tool calls and permission cards show up
// here instead of the conversation looking idle.
func (s *Shell) adoptLiveConnection(convID string) string {
	raw := s.ensure("liveconn:"+convID, "acp_find_connection_for_conversation",
		map[string]any{"conversationId": numOrString(convID)})
	found := asObj(raw)
	if found == nil {
		return ""
	}
	id := str(found, "connection_id")
	if id == "" {
		return ""
	}
	s.mu.Lock()
	s.connByConv[convID] = id
	s.mu.Unlock()
	if s.stream != nil {
		s.stream.Attach(id)
	}
	return id
}

// forgetLiveConnection drops the cached lookup so the next frame asks again
// (a connection was just created or closed).
func (s *Shell) forgetLiveConnection(convID string) {
	s.mu.Lock()
	delete(s.cache, "liveconn:"+convID)
	s.mu.Unlock()
}

// cancelTurn aborts the in-flight turn on a connection.
func (s *Shell) cancelTurn(loc, connID string) {
	svc := s.svc
	go func() {
		_, err := s.B.raw("acp_cancel", map[string]any{"connectionId": connID})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = locales.In(loc).ToastCancelFailed(err.Error())
		} else {
			s.actionMsg = locales.In(loc).ToastCancelRequested()
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

// convByID reads the selected folder's cached conversation list for one row,
// falling back to the all-conversations list — the tab strip already reads
// that one, so the detail header and its tab never disagree about a title.
func (s *Shell) convByID(id string) map[string]any {
	for _, cv := range asList(s.peek("convs:" + s.selectedFolder)) {
		if fmt.Sprintf("%v", cv["id"]) == id {
			return cv
		}
	}
	for _, cv := range asList(s.peek("all-convs")) {
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
func (s *Shell) createConversation(loc string, folderID any, agent string) {
	svc := s.svc
	folderKey := "convs:" + s.selectedFolder
	go func() {
		raw, err := s.B.raw("create_conversation", map[string]any{"folderId": folderID, "agentType": agent})
		s.mu.Lock()
		newID := ""
		if err != nil {
			s.actionMsg = locales.In(loc).ToastCreateFailed(err.Error())
		} else {
			// create_conversation answers with a bare id (Json<i32>), the same
			// shape upstream's endpoint returns.
			var id float64
			_ = json.Unmarshal(raw, &id)
			if id != 0 {
				newID = fmt.Sprintf("%v", int64(id))
			}
			s.actionMsg = locales.In(loc).ToastCreated()
			delete(s.cache, folderKey)
		}
		s.mu.Unlock()
		if newID != "" {
			// Open the new conversation as a tab (upstream: creating a
			// conversation opens its tab).
			s.openConversationTab(newID, s.selectedFolder, agent, "")
		}
		svc.Invalidate()
	}()
}

// sendPrompt connects the conversation's agent at the folder cwd and sends the
// composer text. Errors (e.g. the agent binary is not installed) surface in
// actionMsg rather than blocking the UI.
func (s *Shell) sendPrompt(loc, convID string) {
	agent := str(s.convByID(convID), "agent_type")
	folderPath := s.folderPathByID(s.selectedFolder)
	s.mu.Lock()
	prompt := s.composer
	// Snapshot and clear staged attachments so a re-entrant prompt can't double
	// send them; an empty prompt with attachments is still a valid turn.
	atts := append([]attachmentItem(nil), s.composerAttachments...)
	s.composer = ""
	s.composerAttachments = nil
	// A new turn starts a fresh feedback list, so retire the previous turn's
	// dismissals (the server clears its notes on the next prompt).
	s.feedbackDismissed = nil
	if prompt != "" {
		s.lastPrompt = prompt
	}
	s.mu.Unlock()
	if prompt == "" && len(atts) == 0 {
		return
	}
	svc := s.svc
	go func() {
		s.setAction(locales.In(loc).ToastConnecting(), svc)
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
		// The lookup that would have answered "no live session" is stale now.
		delete(s.cache, "liveconn:"+convID)
		s.mu.Unlock()
		s.stream.Attach(connID) // subscribe before prompting so no events are missed

		// Upload every staged file, then reference the stored copies in the
		// prompt's content blocks.
		uploaded := make([]attachmentItem, 0, len(atts))
		for _, a := range atts {
			ua, e := s.uploadAttachmentHTTP(connID, a.Path)
			if e != nil {
				s.setAction(locales.In(loc).ToastUploadFailed(e.Error()), svc)
				return
			}
			uploaded = append(uploaded, ua)
		}
		blocks := buildPromptBlocks(prompt, uploaded)
		if _, err := s.B.raw("acp_prompt", map[string]any{"connectionId": connID, "blocks": blocks}); err != nil {
			s.setAction(locales.In(loc).ToastPromptFailed(err.Error()), svc)
			return
		}
		// Echo the user's own turn (text + attachments) into the local
		// transcript so it renders like the agent's reply.
		s.stream.AppendLocal(connID, map[string]any{
			"type":        "user_message",
			"text":        prompt,
			"attachments": attachmentsToRaw(uploaded),
		})
		s.setAction(locales.In(loc).ToastPromptSent(), svc)
	}()
}

// notify queues a transient outcome message for the window's toast manager —
// mygo's c.Toast is a view-time call, so a mutation running on a goroutine
// queues its message here and the next frame hands it over. notifyErr is the
// same with an error toast, for localized messages that no longer carry the
// English "failed" marker. Replaces the per-panel status lines this app used
// to render itself.
func (s *Shell) notify(msg string) {
	to := ui.Toast{Title: msg}
	if strings.Contains(msg, "failed") {
		to.Type = "error"
	}
	s.enqueueToast(to)
}

// notifyErr queues a transient outcome message as an error toast.
func (s *Shell) notifyErr(msg string) {
	s.enqueueToast(ui.Toast{Title: msg, Type: "error"})
}

func (s *Shell) enqueueToast(to ui.Toast) {
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
				toast.CloseButton().Label(cat(c).ActionCloseToast()).Children(func() {
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
				ui.Text(c, cat(c).PermRequested()).FontSize(13).Bold()
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
						s.respondPermission(c.Locale(), connID, reqID, oid)
					}
				}
			})
		})
	}
	if pq, ok := snap["pending_question"].(map[string]any); ok {
		reqID := str(pq, "request_id")
		ui.Column(c.Key("ques")).FillWidth().Radius(8).Border(1, t.Accent.Alpha(0.45)).
			Background(t.Accent.Alpha(0.06)).Padding(12, 12, 12, 12).Gap(8).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconInfo).Width(14).Height(14).TextColor(t.Accent)
				ui.Text(c, cat(c).PermQuestion()).FontSize(13).Bold()
			})
			qs, _ := pq["questions"].([]any)
			for i, raw := range qs {
				q, _ := raw.(map[string]any)
				if text := firstNonEmpty(str(q, "question"), str(q, "prompt"), str(q, "text")); text != "" {
					ui.Text(c.Key(itoa("q", i)), text).FontSize(13).TextColor(t.Text).Wrap()
				}
				// Preset options answer in one click; otherwise the user types.
				if opts, ok := q["options"].([]any); ok && len(opts) > 0 {
					ui.Row(c).Gap(6).Wrap().Children(func() {
						for j, rawOpt := range opts {
							label := ""
							switch o := rawOpt.(type) {
							case string:
								label = o
							case map[string]any:
								label = firstNonEmpty(str(o, "label"), str(o, "name"), str(o, "value"))
							}
							if label == "" {
								continue
							}
							if ui.Button(c.Key(itoa("qopt", j)), label).Clicked() {
								s.answerQuestion(c.Locale(), connID, reqID, label)
							}
						}
					})
				} else {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.TextInput(c.Key("ques-input"), &s.questionAnswer).Grow(1).
							Placeholder(cat(c).PermAnswerPlaceholder())
						if ui.PrimaryButton(c.Key("ques-send"), cat(c).PermAnswerSend()).
							Disabled(s.questionAnswer == "").Clicked() {
							s.answerQuestion(c.Locale(), connID, reqID, s.questionAnswer)
						}
					})
				}
			}
			ui.Text(c, cat(c).PermAnswerHint()).FontSize(11).TextColor(t.TextMuted)
		})
	}
}

// viewAsyncTasks renders one compact row per background task the agent is
// running: the name, the "is it making progress" evidence (phase, last tool,
// output size), and Stop when the agent says the task can be stopped. Mirrors
// upstream's async-task strip.
func (s *Shell) viewAsyncTasks(c *ui.Context, connID string, tasks []any) {
	t := c.Theme()
	m := cat(c)
	ui.Column(c.Key("async-tasks")).FillWidth().Gap(4).Children(func() {
		for i, raw := range tasks {
			task, _ := raw.(map[string]any)
			if task == nil {
				continue
			}
			name := firstNonEmpty(str(task, "name"), str(task, "description"), m.Untitled())
			state := str(task, "state")
			paused := state == "paused"
			row := ui.Row(c.Key(itoa("atask", i))).FillWidth().Gap(8).AlignItems(ui.Center).
				Padding(5, 10, 5, 10).Radius(6).Background(t.Surface).Border(1, t.Border).
				Label(name)
			row.Children(func() {
				icon, col := iconRefresh, t.Accent
				if paused {
					icon, col = iconClock, t.TextMuted
				}
				ui.Icon(c, icon).Width(13).Height(13).TextColor(col)
				ui.Text(c, name).FontSize(12).TextColor(t.Text).MaxLines(1)
				// The evidence line: what phase it is in and the last tool it
				// ran. Absent until the adapter reports them.
				meta := []string{}
				for _, key := range []string{"phase", "last_tool_name"} {
					if v := str(task, key); v != "" {
						meta = append(meta, v)
					}
				}
				if len(meta) > 0 {
					ui.Text(c, strings.Join(meta, " · ")).FontSize(11).
						TextColor(t.TextMuted).MaxLines(1).Grow(1)
				} else {
					ui.Spacer(c)
				}
				// The output file has no open route in this build, so the
				// button lifts its path instead of previewing it.
				if path := str(task, "output_file_path"); path != "" {
					if ui.Button(c.Key(itoa("atask-out", i)), m.AsyncTaskOpenOutput()).Clicked() {
						c.WriteClipboard(path)
						s.notify(m.ToastCopied())
					}
				}
				if asBool(task["can_stop"]) {
					if ui.Button(c.Key(itoa("atask-stop", i)), m.AsyncTaskStop()).Clicked() {
						s.stopAsyncTask(c.Locale(), connID, str(task, "async_task_id"))
					}
				}
			})
		}
	})
}

// stopAsyncTask asks the agent to stop a background task. The verdict comes
// from the adapter: a decline leaves the row alone (the agent may finish on
// its own), a stop removes it once the task's terminal state arrives.
func (s *Shell) stopAsyncTask(loc, connID, taskID string) {
	if connID == "" || taskID == "" {
		return
	}
	svc := s.svc
	go func() {
		raw, err := s.B.raw("acp_stop_async_task", map[string]any{
			"connectionId": connID,
			"asyncTaskId":  taskID,
		})
		s.mu.Lock()
		switch {
		case err != nil:
			s.notifyErr(locales.In(loc).ToastPromptFailed(err.Error()))
		case !asBool(raw):
			s.notify(locales.In(loc).AsyncTaskStopDeclined())
		default:
			s.notify(locales.In(loc).AsyncTaskStopped())
		}
		delete(s.cache, "snapshot:"+connID)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// viewFeedbackNotes renders the turn's live-feedback notes above the composer.
// While a turn is running each note shows waiting (clock) or received (check).
// Once the turn has ended, only the notes the agent never read remain, in an
// "expired" form that offers to resend each as an ordinary message or dismiss
// it — the pull channel is cooperative, so "never read" is ordinary, not an
// error, and the user's text would otherwise vanish.
func (s *Shell) viewFeedbackNotes(c *ui.Context, notes []any, prompting bool) {
	t := c.Theme()
	m := cat(c)
	visible := make([]map[string]any, 0, len(notes))
	for _, raw := range notes {
		n, _ := raw.(map[string]any)
		if n == nil {
			continue
		}
		if id := str(n, "id"); id != "" && s.feedbackDismissed[id] {
			continue
		}
		// Past the turn, a delivered note has done its job and retires; only
		// the unread ones stay up so they can be salvaged.
		if !prompting && str(n, "status") != "pending" {
			continue
		}
		visible = append(visible, n)
	}
	if len(visible) == 0 {
		return
	}
	ui.Column(c.Key("feedback-notes")).FillWidth().Gap(3).Children(func() {
		if !prompting {
			ui.Text(c, m.FeedbackExpired()).FontSize(10).TextColor(t.TextMuted).Padding(0, 4, 2, 4)
		}
		for i, n := range visible {
			text := str(n, "text")
			status := str(n, "status")
			row := ui.Row(c.Key(itoa("fbnote", i))).FillWidth().Gap(6).AlignItems(ui.Center).
				Padding(4, 8, 4, 8).Radius(6).Background(t.Surface).Border(1, t.Border).
				Label(text)
			row.Children(func() {
				switch {
				case !prompting:
					ui.Icon(c, iconAlert).Width(12).Height(12).TextColor(t.Warning)
				case status == "delivered":
					ui.Icon(c, iconCheck).Width(12).Height(12).TextColor(t.Success)
				default:
					ui.Icon(c, iconClock).Width(12).Height(12).TextColor(t.TextMuted)
				}
				ui.Text(c, text).FontSize(11).TextColor(t.Text).MaxLines(1).Grow(1)
				if !prompting {
					if ui.Button(c.Key(itoa("fbnote-send", i)), m.FeedbackSendAsMessage()).Clicked() {
						s.mu.Lock()
						s.composer = text
						if s.feedbackDismissed == nil {
							s.feedbackDismissed = map[string]bool{}
						}
						s.feedbackDismissed[str(n, "id")] = true
						s.mu.Unlock()
						s.sendPrompt(c.Locale(), s.selectedConv)
					}
					if ui.Button(c.Key(itoa("fbnote-x", i)), m.FeedbackDismiss()).Clicked() {
						s.mu.Lock()
						if s.feedbackDismissed == nil {
							s.feedbackDismissed = map[string]bool{}
						}
						s.feedbackDismissed[str(n, "id")] = true
						s.mu.Unlock()
						s.svc.Invalidate()
					}
				} else {
					label := m.FeedbackPending()
					if status == "delivered" {
						label = m.FeedbackDelivered()
					}
					ui.Text(c, label).FontSize(10).TextColor(t.TextMuted)
				}
			})
		}
	})
}

// submitFeedback sends the note composer's draft to the running turn. On
// success the note lands in the list above the composer; on failure the draft
// is restored so the text isn't lost.
func (s *Shell) submitFeedback(loc, connID string) {
	s.mu.Lock()
	text := strings.TrimSpace(s.feedbackDraft)
	if text == "" || connID == "" {
		s.mu.Unlock()
		return
	}
	s.feedbackDraft = ""
	s.feedbackOpen = false
	s.mu.Unlock()
	svc := s.svc
	go func() {
		_, err := s.B.raw("submit_session_feedback", map[string]any{
			"connectionId": connID,
			"text":         text,
		})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).FeedbackSubmitFailed())
			s.feedbackDraft = text
			s.feedbackOpen = true
		}
		delete(s.cache, "snapshot:"+connID)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// viewQueuedPrompts lists the sends waiting behind the running turn, each with
// a remove button.
func (s *Shell) viewQueuedPrompts(c *ui.Context, connID string, queued []string) {
	t := c.Theme()
	m := cat(c)
	ui.Column(c.Key("queued")).FillWidth().Gap(4).Children(func() {
		for i, text := range queued {
			if text == "" {
				text = "(attachment)"
			}
			row := ui.Row(c.Key(itoa("queued", i))).FillWidth().Gap(8).AlignItems(ui.Center).
				Padding(5, 10, 5, 10).Radius(6).Background(t.Surface).Border(1, t.Border)
			row.Children(func() {
				ui.Icon(c, iconClock).Width(12).Height(12).TextColor(t.TextMuted)
				ui.Text(c, m.ComposerQueued()).FontSize(11).TextColor(t.TextMuted)
				ui.Text(c, text).FontSize(12).TextColor(t.Text).MaxLines(1).Grow(1)
				if ui.Button(c.Key(itoa("queued-drop", i)), m.ActionRemove()).Clicked() {
					s.dropQueuedPrompt(c.Locale(), connID, i)
				}
			})
		}
	})
}

// dropQueuedPrompt removes one waiting send and refreshes the snapshot.
func (s *Shell) dropQueuedPrompt(loc, connID string, index int) {
	svc := s.svc
	go func() {
		_, err := s.B.raw("acp_drop_queued_prompt", map[string]any{
			"connectionId": connID,
			"index":        index,
		})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).ToastPromptFailed(err.Error()))
		} else {
			s.notify(locales.In(loc).ToastQueueRemoved())
		}
		delete(s.cache, "snapshot:"+connID)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// answerQuestion resolves the agent's parked ask_user_question: the companion's
// tool call returns this text as its result.
func (s *Shell) answerQuestion(loc, connID, requestID, answer string) {
	if connID == "" || requestID == "" || answer == "" {
		return
	}
	svc := s.svc
	go func() {
		_, err := s.B.raw("acp_answer_question", map[string]any{
			"connectionId": connID,
			"requestId":    requestID,
			"answer":       answer,
		})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).ToastPermissionFailed(err.Error()))
		} else {
			s.questionAnswer = ""
			s.notify(locales.In(loc).ToastAnswerSent())
		}
		delete(s.cache, "snapshot:"+connID)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// respondPermission answers an in-flight permission request off the UI thread.
func (s *Shell) respondPermission(loc, connID, requestID, optionID string) {
	svc := s.svc
	go func() {
		_, err := s.B.raw("acp_respond_permission", map[string]any{"connectionId": connID, "requestId": requestID, "optionId": optionID})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = locales.In(loc).ToastPermissionFailed(err.Error())
		} else {
			s.actionMsg = locales.In(loc).ToastPermissionSent(optionID)
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
func (s *Shell) renameConversation(loc, convID, title string) {
	svc := s.svc
	folderKey := "convs:" + s.selectedFolder
	go func() {
		_, err := s.B.raw("update_conversation_title", map[string]any{"conversationId": numOrString(convID), "title": title})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = locales.In(loc).ToastRenameFailed(err.Error())
		} else {
			s.actionMsg = locales.In(loc).ToastRenamed()
			s.renaming = false
		}
		delete(s.cache, folderKey)
		delete(s.cache, "all-convs")
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// setPinned pins or unpins a conversation and refreshes the list.
func (s *Shell) setPinned(loc, convID string, pinned bool) {
	svc := s.svc
	folderKey := "convs:" + s.selectedFolder
	go func() {
		_, err := s.B.raw("update_conversation_pinned", map[string]any{"conversationId": numOrString(convID), "pinned": pinned})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = locales.In(loc).ToastPinFailed(err.Error())
		} else if pinned {
			s.actionMsg = locales.In(loc).ToastPinned()
		} else {
			s.actionMsg = locales.In(loc).ToastUnpinned()
		}
		delete(s.cache, folderKey)
		delete(s.cache, "all-convs")
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// deleteConversation soft-deletes the conversation and clears the selection.
func (s *Shell) deleteConversation(loc, convID string) {
	svc := s.svc
	folderKey := "convs:" + s.selectedFolder
	go func() {
		_, err := s.B.raw("delete_conversation", map[string]any{"conversationId": numOrString(convID)})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = locales.In(loc).ToastDeleteFailed(err.Error())
		} else {
			s.actionMsg = locales.In(loc).ToastDeleted()
			s.renaming = false
		}
		delete(s.cache, folderKey)
		delete(s.cache, "all-convs")
		s.mu.Unlock()
		if err == nil {
			// The conversation is gone: its tab goes with it (upstream's
			// closeConversationTab{recordForReopen:false}).
			s.closeConversationTab(convID)
		}
		svc.Invalidate()
	}()
}

// openFolder shows the native directory picker and adds the chosen folder to
// the workspace, selecting it. Runs off the UI thread; the picked path goes
// through the same add_folder_to_history the client uses.
func (s *Shell) openFolder(loc string) {
	svc := s.svc
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: locales.In(loc).DialogOpenFolder(), Directory: true})
		if err != nil || len(paths) == 0 {
			return
		}
		raw, rerr := s.B.raw("add_folder_to_history", map[string]any{"path": paths[0]})
		s.mu.Lock()
		if rerr != nil {
			s.actionMsg = locales.In(loc).ToastOpenFailed(rerr.Error())
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
func (s *Shell) createChatConversation(loc string) {
	agent := s.newAgent
	if agent == "" {
		agent = "claude_code"
	}
	svc := s.svc
	go func() {
		raw, err := s.B.raw("create_chat_conversation", map[string]any{"agentType": agent})
		s.mu.Lock()
		newConv, folder := "", ""
		if err != nil {
			s.actionMsg = locales.In(loc).ToastNewChatFailed(err.Error())
		} else {
			var res map[string]any
			_ = json.Unmarshal(raw, &res)
			if fid, ok := res["folderId"]; ok {
				folder = fmt.Sprintf("%v", fid)
				s.selectedFolder = folder
			}
			if cid, ok := res["conversationId"]; ok {
				newConv = fmt.Sprintf("%v", cid)
			}
			s.actionMsg = ""
			delete(s.cache, "folders")
			if folder != "" {
				delete(s.cache, "convs:"+folder)
			}
		}
		s.mu.Unlock()
		if newConv != "" {
			// The new chat opens as a tab, exactly like the sidebar's New chat.
			s.openConversationTab(newConv, folder, agent, "")
		}
		svc.Invalidate()
	}()
}
