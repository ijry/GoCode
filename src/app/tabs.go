package app

// tabs.go ports upstream's conversation tab strip to mygo's native widgets.
// References: components/tabs/tab-bar.tsx + tab-item.tsx (the strip and one
// tab: status dot, title, close affordance, the close verbs, the trailing
// new-conversation button) and stores/tab-store.ts (the ordered open set and
// its switch/close/close-others/close-all actions), persisting through
// web/handlers/opened_tabs.rs (the versioned list_opened_tabs /
// save_opened_tabs compare-and-set pair).
//
// What is deliberately NOT ported, and why: upstream's IDEA-style split groups
// (groupLayout / groupOfTab / tile mode / cross-group drag) and its
// device-local draft tabs. Both are large self-contained subsystems — a
// splitter tree with its own layout algebra, and a draft lifecycle that never
// reaches the DB — and neither is needed for the strip to be usable. This port
// keeps the strip itself (ordered tabs, the active tab, the four close verbs,
// the trailing new-conversation affordance) plus the server-side persistence,
// so the open set survives a restart and mirrors across windows.
//
// A tab here is always conversation-backed. Upstream's unsent drafts exist to
// avoid minting a DB row before the first send; this build's "New chat"
// already calls create_chat_conversation, so a fresh chat IS a real
// conversation and needs no draft concept — tabItem.convID is never empty.

import (
	"encoding/json"
	"fmt"

	"github.com/egoist/mygo/ui"
)

// tabItem is one open conversation tab (upstream's TabItem, minus the
// split-group and draft fields this build has no use for).
type tabItem struct {
	convID   string // the conversation this tab shows (never empty)
	folderID string // its folder, for the wire payload and cross-folder focus
	agent    string // agent_type, required by save_opened_tabs
	title    string // seed title, refreshed from the live list each frame
	status   string // ConversationStatus, for the tab's status dot
	pinned   bool
}

// closedTab is one entry of the reopen stack (upstream's
// ClosedConversationTab): the strip slot it closed from is kept so reopening
// splices it back where it was instead of appending.
type closedTab struct {
	convID   string
	folderID string
	agent    string
	title    string
	index    int
}

// closedTabLimit caps the stack as upstream's CLOSED_TAB_STACK_LIMIT does.
const closedTabLimit = 20

// ─── state ────────────────────────────────────────────────────────────────

// tabIndexOf returns the slot of a conversation in openTabs, or -1. Call with
// s.mu held.
func (s *Shell) tabIndexOf(convID string) int {
	for i := range s.openTabs {
		if s.openTabs[i].convID == convID {
			return i
		}
	}
	return -1
}

// openConversationTab opens (or focuses) a conversation as a tab, in upstream's
// wording: a conversation already open is focused where it is, never
// duplicated. Selecting a tab also selects its folder, since the panels beside
// the detail (files, changes, commits, terminal) are folder-scoped.
func (s *Shell) openConversationTab(convID, folderID, agent, title string) {
	if convID == "" {
		return
	}
	s.mu.Lock()
	s.tabsLoaded = true // the user has spoken: a late hydration must not clobber this
	if s.tabIndexOf(convID) < 0 {
		s.openTabs = append(s.openTabs, tabItem{
			convID: convID, folderID: folderID, agent: agent, title: title,
		})
	}
	if folderID != "" {
		s.selectedFolder = folderID
	}
	s.selectedConv = convID
	s.workbench = ""
	s.actionMsg = ""
	s.mu.Unlock()
	s.persistTabs()
}

// openConversationRow opens a sidebar / pet conversation row as a tab,
// resolving its folder and agent from the row itself when it carries them.
func (s *Shell) openConversationRow(cv map[string]any) {
	convID := fmt.Sprintf("%v", cv["id"])
	folderID := s.selectedFolder
	if cv["folder_id"] != nil {
		folderID = fmt.Sprintf("%v", cv["folder_id"])
	}
	s.openConversationTab(convID, folderID, str(cv, "agent_type"), str(cv, "title"))
}

// closeConversationTab closes one tab. Upstream picks the neighbor as the next
// focus (the tab that slid into the closed slot, else the one before it), and
// closing the last tab leaves the workspace with nothing selected.
func (s *Shell) closeConversationTab(convID string) {
	s.mu.Lock()
	i := s.tabIndexOf(convID)
	if i < 0 {
		s.mu.Unlock()
		return
	}
	s.pushClosedTabLocked(s.openTabs[i], i)
	s.openTabs = append(s.openTabs[:i:i], s.openTabs[i+1:]...)
	if s.selectedConv == convID {
		switch {
		case len(s.openTabs) == 0:
			s.selectedConv = ""
		case i < len(s.openTabs):
			s.selectedConv = s.openTabs[i].convID
		default:
			s.selectedConv = s.openTabs[len(s.openTabs)-1].convID
		}
	}
	folder := s.folderOfSelectionLocked()
	s.mu.Unlock()
	if folder != "" {
		s.selectedFolder = folder
	}
	s.persistTabs()
}

// folderOfSelectionLocked is the folder of the selected conversation's tab, or
// "" when there is no selection. Call with s.mu held.
func (s *Shell) folderOfSelectionLocked() string {
	if i := s.tabIndexOf(s.selectedConv); i >= 0 {
		return s.openTabs[i].folderID
	}
	return ""
}

// closeOtherConversationTabs keeps only the named tab (upstream's
// "Close Others").
func (s *Shell) closeOtherConversationTabs(convID string) {
	s.mu.Lock()
	i := s.tabIndexOf(convID)
	if i < 0 {
		s.mu.Unlock()
		return
	}
	closed := 0
	for slot, tab := range s.openTabs {
		if tab.convID == convID {
			continue
		}
		// The batch's slots are counted as if the batch closed one tab at a
		// time, so reopening them rebuilds the strip ahead of what remains.
		s.pushClosedTabLocked(tab, slot-closed)
		closed++
	}
	if len(s.openTabs) > 1 {
		s.openTabs = []tabItem{s.openTabs[i]}
	}
	s.selectedConv = convID
	if s.openTabs[0].folderID != "" {
		s.selectedFolder = s.openTabs[0].folderID
	}
	s.workbench = ""
	s.mu.Unlock()
	s.persistTabs()
}

// closeAllConversationTabs empties the strip (upstream's "Close All").
func (s *Shell) closeAllConversationTabs() {
	s.mu.Lock()
	if len(s.openTabs) == 0 {
		s.mu.Unlock()
		return
	}
	for _, tab := range s.openTabs {
		s.pushClosedTabLocked(tab, 0)
	}
	s.openTabs = nil
	s.selectedConv = ""
	s.mu.Unlock()
	s.persistTabs()
}

// pushClosedTabLocked records a closed tab for reopen_last_closed_tab. One
// entry per conversation: recording it again moves it to the top, as upstream's
// pushClosedTab does. Caller holds s.mu.
func (s *Shell) pushClosedTabLocked(tab tabItem, index int) {
	for i := range s.closedTabs {
		if s.closedTabs[i].convID == tab.convID {
			s.closedTabs = append(s.closedTabs[:i:i], s.closedTabs[i+1:]...)
			break
		}
	}
	s.closedTabs = append(s.closedTabs, closedTab{
		convID: tab.convID, folderID: tab.folderID, agent: tab.agent,
		title: tab.title, index: index,
	})
	if len(s.closedTabs) > closedTabLimit {
		s.closedTabs = s.closedTabs[len(s.closedTabs)-closedTabLimit:]
	}
}

// reopenClosedTab restores the newest stack entry at the slot it was closed
// from. An already-open conversation is focused where it is, as upstream's
// openTab does.
func (s *Shell) reopenClosedTab() {
	s.mu.Lock()
	if len(s.closedTabs) == 0 {
		s.mu.Unlock()
		return
	}
	ct := s.closedTabs[len(s.closedTabs)-1]
	s.closedTabs = s.closedTabs[:len(s.closedTabs)-1]
	if s.tabIndexOf(ct.convID) >= 0 {
		s.mu.Unlock()
		s.focusConversationTab(ct.convID)
		return
	}
	index := min(max(ct.index, 0), len(s.openTabs))
	tab := tabItem{convID: ct.convID, folderID: ct.folderID, agent: ct.agent, title: ct.title}
	s.openTabs = append(s.openTabs, tabItem{})
	copy(s.openTabs[index+1:], s.openTabs[index:])
	s.openTabs[index] = tab
	s.tabsLoaded = true
	s.selectedConv = ct.convID
	if ct.folderID != "" {
		s.selectedFolder = ct.folderID
	}
	s.workbench = ""
	s.actionMsg = ""
	s.mu.Unlock()
	s.persistTabs()
}

// focusConversationTab selects one already-open tab. The new focus is written
// back too: upstream mirrors the active tab through opened_tabs.is_active, so
// other windows (and the next launch) see which tab was left showing.
func (s *Shell) focusConversationTab(convID string) {
	s.mu.Lock()
	i := s.tabIndexOf(convID)
	if i < 0 {
		s.mu.Unlock()
		return
	}
	if s.openTabs[i].folderID != "" {
		s.selectedFolder = s.openTabs[i].folderID
	}
	changed := s.selectedConv != convID
	s.selectedConv = convID
	s.workbench = ""
	s.actionMsg = ""
	s.mu.Unlock()
	if changed {
		s.persistTabs()
	}
}

// ─── hydration & persistence ──────────────────────────────────────────────

// loadTabs seeds the strip from a list_opened_tabs response, once. The response
// carries the CAS version, which the next save sends back as its
// expectedVersion.
func (s *Shell) loadTabs(raw json.RawMessage) {
	page := asObj(raw)
	if page == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tabsLoaded {
		return
	}
	s.tabsLoaded = true
	s.tabsVersion = intVal(page, "version")
	active := ""
	for _, it := range asListVal(page["items"]) {
		if it["conversation_id"] == nil {
			continue
		}
		tab := tabItem{
			convID:   fmt.Sprintf("%v", it["conversation_id"]),
			folderID: fmt.Sprintf("%v", it["folder_id"]),
			agent:    str(it, "agent_type"),
			pinned:   truthy(it["is_pinned"]),
		}
		if truthy(it["is_active"]) {
			active = tab.convID
		}
		s.openTabs = append(s.openTabs, tab)
	}
	// Restore the focused conversation only when nothing is selected yet, so a
	// hydration landing after the user picked a row never yanks them away.
	if s.selectedConv == "" && active != "" {
		s.selectedConv = active
		if i := s.tabIndexOf(active); i >= 0 && s.openTabs[i].folderID != "" {
			s.selectedFolder = s.openTabs[i].folderID
		}
	}
}

// persistTabs writes the open set back through save_opened_tabs. The write is
// compare-and-set on the version: an accepted save advances it, and a rejected
// one (another window wrote first) hands back the server's truth, which we
// adopt — the same reconcile upstream's applyRemoteSnapshot does, minus the
// device-local drafts it has to carry.
func (s *Shell) persistTabs() {
	s.mu.Lock()
	items := make([]map[string]any, 0, len(s.openTabs))
	for i, t := range s.openTabs {
		items = append(items, map[string]any{
			"folder_id":       numOrString(t.folderID),
			"conversation_id": numOrString(t.convID),
			"agent_type":      t.agent,
			"position":        i,
			"is_active":       t.convID == s.selectedConv,
			"is_pinned":       t.pinned,
		})
	}
	expected := s.tabsVersion
	origin := s.tabsOrigin
	svc := s.svc
	s.mu.Unlock()

	go func() {
		raw, err := s.B.raw("save_opened_tabs", map[string]any{
			"expectedVersion": expected,
			"items":           items,
			"origin":          origin,
		})
		if err != nil {
			return // a failed save keeps the local strip; the next edit retries
		}
		page := asObj(raw)
		if page == nil {
			return
		}
		s.mu.Lock()
		s.tabsVersion = intVal(page, "version")
		if !truthy(page["accepted"]) {
			s.adoptServerTabsLocked(asListVal(page["tabs"]))
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// adoptServerTabsLocked replaces the local strip with the server's snapshot
// after a rejected (stale) save. Caller holds s.mu. The selection follows the
// server's active tab, falling back to the first tab that still exists.
func (s *Shell) adoptServerTabsLocked(items []map[string]any) {
	next := make([]tabItem, 0, len(items))
	active := ""
	for _, it := range items {
		if it["conversation_id"] == nil {
			continue
		}
		convID := fmt.Sprintf("%v", it["conversation_id"])
		// Keep any local seed title for tabs that survive the reconcile.
		title := ""
		if i := s.tabIndexOf(convID); i >= 0 {
			title = s.openTabs[i].title
		}
		next = append(next, tabItem{
			convID:   convID,
			folderID: fmt.Sprintf("%v", it["folder_id"]),
			agent:    str(it, "agent_type"),
			title:    title,
			pinned:   truthy(it["is_pinned"]),
		})
		if truthy(it["is_active"]) {
			active = convID
		}
	}
	s.openTabs = next
	s.selectedConv = active
	if s.selectedConv == "" && len(next) > 0 {
		s.selectedConv = next[0].convID
	}
	if i := s.tabIndexOf(s.selectedConv); i >= 0 && s.openTabs[i].folderID != "" {
		s.selectedFolder = s.openTabs[i].folderID
	}
}

// ─── strip rendering ──────────────────────────────────────────────────────

// convTabStatusColor maps a ConversationStatus onto a dot color, mirroring
// upstream's STATUS_COLORS. The theme is deliberately grayscale apart from its
// semantic trio, so the one hue it has no token for (pending_review's blue) is
// named here rather than approximated.
func convTabStatusColor(t *ui.Theme, status string) ui.Color {
	switch status {
	case "in_progress":
		return t.Warning
	case "pending_review":
		return ui.Hex("#3b82f6")
	case "completed":
		return t.Success
	case "cancelled":
		return t.Danger
	default:
		return t.TextMuted
	}
}

// viewConversationTabs renders the strip: one browser-style tab per open
// conversation, then the trailing new-conversation button and the terminal
// toggle. The active tab is a raised card on the muted strip (upstream's white
// browser tab seated on bg-muted); every tab carries a status dot, its title
// and a close affordance, with upstream's close verbs on a right-click.
func (s *Shell) viewConversationTabs(c *ui.Context) {
	t := c.Theme()
	// Hydrate once from the persisted set, then decorate from the live
	// conversation list (title / status), dropping tabs whose conversation has
	// gone away.
	if raw := s.ensure("opened-tabs", "list_opened_tabs", nil); raw != nil {
		s.loadTabs(raw)
	}
	allConvs := s.ensure("all-convs", "list_all_conversations", nil)
	byID := map[string]map[string]any{}
	for _, cv := range asList(allConvs) {
		byID[fmt.Sprintf("%v", cv["id"])] = cv
	}

	s.mu.Lock()
	if s.tabsLoaded && allConvs != nil {
		kept := make([]tabItem, 0, len(s.openTabs))
		for _, tab := range s.openTabs {
			cv, ok := byID[tab.convID]
			if !ok {
				continue // the conversation was deleted: its tab goes with it
			}
			tab.title = str(cv, "title")
			tab.status = str(cv, "status")
			if cv["folder_id"] != nil {
				tab.folderID = fmt.Sprintf("%v", cv["folder_id"])
			}
			if at := str(cv, "agent_type"); at != "" {
				tab.agent = at
			}
			tab.pinned = isPinned(cv)
			kept = append(kept, tab)
		}
		s.openTabs = kept
	}
	// A conversation selected by some path that does not open a tab (a pet
	// session jump, an import) still gets one, so the strip always reflects
	// what the detail is showing. This is a display reconcile only — the next
	// user action persists the set.
	if s.selectedConv != "" && s.tabIndexOf(s.selectedConv) < 0 {
		cv := byID[s.selectedConv]
		folder := s.selectedFolder
		if cv != nil && cv["folder_id"] != nil {
			folder = fmt.Sprintf("%v", cv["folder_id"])
		}
		s.openTabs = append(s.openTabs, tabItem{
			convID: s.selectedConv, folderID: folder, agent: str(cv, "agent_type"), title: str(cv, "title"),
		})
	}
	tabs := append([]tabItem(nil), s.openTabs...)
	active := s.selectedConv
	s.mu.Unlock()

	ui.Row(c.Key("conv-tabs")).FillWidth().Background(t.Surface).
		PaddingY(5).AlignItems(ui.Center).Children(func() {
		if len(tabs) > 0 {
			ui.ScrollHorizontal(c.Key("conv-tabs-scroll")).Grow(1).Children(func() {
				ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
					for _, tab := range tabs {
						s.viewOneTab(c, tab, tab.convID == active)
					}
				})
			})
		} else {
			ui.Row(c).Grow(1).PaddingX(8).Children(func() {
				ui.Text(c, cat(c).ConvTabNone()).FontSize(12).TextColor(t.TextMuted)
			})
		}
		// Trailing new-conversation affordance (upstream's SquarePen
		// button): upstream reuses the active folder's context, falling
		// back to a folderless chat when none resolves — which here means
		// the same create_chat_conversation the sidebar's "New chat" runs.
		iconButton(c, "conv-tab-new", iconNewChat, cat(c).ConvTabNew(), func() {
			s.createChatConversation(c.Locale())
		})
	})
}

// viewOneTab renders a single conversation tab: the status dot, the title and a
// close affordance, with upstream's close verbs on a right-click.
func (s *Shell) viewOneTab(c *ui.Context, tab tabItem, active bool) {
	t := c.Theme()
	title := tab.title
	if title == "" {
		title = cat(c).Untitled()
	}
	// Upstream's browser-style tab: a full-height rounded-top card that reads as
	// seated on the muted strip; the active one is filled with the canvas color
	// so it merges into the detail below.
	card := ui.Row(c.Key("ct-"+tab.convID)).Padding(4, 10, 4, 8).Gap(6).
		Radius(7).MaxWidth(220).AlignItems(ui.Center)
	if active {
		card.Background(t.Background)
	}
	card.Children(func() {
		ui.Box(c).Width(7).Height(7).Radius(4).
			Background(convTabStatusColor(t, tab.status))
		ui.Text(c, title).FontSize(12).MaxLines(1).Grow(1).
			TextColor(condColor(active, t.Text, t.TextMuted))
		if tab.pinned {
			ui.Icon(c, iconPinned).Width(11).Height(11).TextColor(t.TextMuted)
		}
		// The close affordance, mirrored from upstream's tab button.
		closeBtn := ui.Row(c.Key("ct-x-" + tab.convID)).Width(16).Height(16).Radius(4).
			AlignItems(ui.Center).Justify(ui.Center).Label(cat(c).ConvTabClose()).
			Tooltip(cat(c).ConvTabClose())
		closeBtn.Children(func() {
			ui.Icon(c, iconX).Width(11).Height(11).TextColor(t.TextMuted)
		})
		if closeBtn.Clicked() {
			s.closeConversationTab(tab.convID)
		}
	})
	card.ContextMenu(func(m *ui.Menu) {
		if m.Item(cat(c).ConvTabClose()).Chosen() {
			s.closeConversationTab(tab.convID)
		}
		if m.Item(cat(c).ConvTabCloseOthers()).Chosen() {
			s.closeOtherConversationTabs(tab.convID)
		}
		m.Separator()
		if m.Item(cat(c).ConvTabCloseAll()).Chosen() {
			s.closeAllConversationTabs()
		}
	})
	if card.Clicked() {
		s.focusConversationTab(tab.convID)
	}
}

// viewDetailTabs renders the main area: the conversation tabs strip on top, then
// the conversation detail. Files / changes / commits live in the right-hand aux
// panel (see aux_panel.go), and the terminal lives in the bottom collapsible
// panel below the three columns — none of them a main-area tab. This matches
// upstream's workspace skeleton (Sidebar | main | AuxPanel | bottom Terminal).
func (s *Shell) viewDetailTabs(c *ui.Context) {
	s.viewConversationTabs(c)
	ui.Divider(c)
	s.viewConversationDetail(c)
}
