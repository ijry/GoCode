package app

// terminal.go mirrors upstream's workspace terminal: a toggled bottom panel
// shared by every conversation tab, holding an ordered strip of PTY tabs
// (components/terminal/terminal-panel.tsx + terminal-tab-bar.tsx +
// contexts/terminal-context.tsx). Each tab keeps the folder and working
// directory it was opened in, so a shell opened in a worktree stays bound to
// that worktree while the selection moves elsewhere.
//
// Output arrives live on the terminal://output/{id} firehose channel (which only
// repaints — the frames carry no body), so the text itself comes from
// terminal_snapshot, refetched whenever that terminal's channel fires (see
// ensureStream).

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// termTab is one terminal tab (upstream's TerminalTab). ID doubles as the PTY
// id: it is supplied to terminal_spawn so the tab and its process share one
// stable key. Err keeps a failed spawn visible in that tab instead of silently
// dropping it.
type termTab struct {
	ID         string
	Title      string
	FolderID   string
	WorkingDir string
	Input      string
	Err        string
}

// termTabIndexLocked returns the slot of a tab, or -1. Call with s.mu held.
func (s *Shell) termTabIndexLocked(id string) int {
	for i := range s.termTabs {
		if s.termTabs[i].ID == id {
			return i
		}
	}
	return -1
}

// viewTerminalPanel renders the bottom panel. Collapsed, it is a single strip
// that reopens the panel; expanded, that strip becomes upstream's tab bar
// (tabs + new + hide) with the active tab's snapshot below it.
func (s *Shell) viewTerminalPanel(c *ui.Context) {
	t := c.Theme()
	s.termInputFocused = false
	panel := ui.Column(c.Key("term-panel")).FillWidth().Background(t.Surface)
	panel.Children(func() {
		if s.termOpen {
			s.viewTerminalTabBar(c)
		} else {
			s.viewTerminalCollapsedBar(c)
		}
		if s.termOpen {
			ui.Divider(c)
			ui.Column(c.Key("term-body")).FillWidth().Height(220).Padding(10).Gap(6).Children(func() {
				s.viewTerminalBody(c)
			})
		}
	})
	s.termHovered = panel.Hovered()
}

// viewTerminalCollapsedBar is the always-visible collapsed state: the chevron
// plus the panel title. Clicking anywhere on it expands the panel.
func (s *Shell) viewTerminalCollapsedBar(c *ui.Context) {
	t := c.Theme()
	title := cat(c).ConvTabTerminal()
	if root := s.folderPathByID(s.selectedFolder); root != "" {
		title = title + " · " + root
	}
	s.mu.Lock()
	count := len(s.termTabs)
	s.mu.Unlock()
	toggle := ui.Row(c.Key("term-toggle")).FillWidth().PaddingX(12).PaddingY(6).Gap(6).
		AlignItems(ui.Center).Label(cat(c).TerminalToggleLabel())
	toggle.Children(func() {
		ui.Icon(c, iconChevron).Width(13).Height(13).TextColor(t.TextMuted)
		ui.Text(c, title).FontSize(12).Bold()
		if count > 0 {
			ui.Text(c, fmt.Sprintf("%d", count)).FontSize(11).TextColor(t.TextMuted)
		}
	})
	if toggle.Clicked() {
		s.termOpen = true
	}
}

// viewTerminalTabBar renders upstream's 32px strip: the collapse chevron, the
// horizontally scrolling tab list, then new-tab and refresh actions.
func (s *Shell) viewTerminalTabBar(c *ui.Context) {
	t := c.Theme()
	s.mu.Lock()
	tabs := append([]termTab(nil), s.termTabs...)
	active := s.termActive
	s.mu.Unlock()

	ui.Row(c.Key("term-head")).FillWidth().Background(t.SurfacePressed).
		Padding(2, 6, 2, 6).Gap(4).AlignItems(ui.Center).Children(func() {
		iconButton(c, "term-collapse", iconChevronD, cat(c).TerminalToggleLabel(), func() {
			s.termOpen = false
		})
		if len(tabs) > 0 {
			ui.ScrollHorizontal(c.Key("term-tabs-scroll")).Grow(1).Children(func() {
				ui.Row(c.Key("term-tabs")).Gap(2).AlignItems(ui.Center).Children(func() {
					for _, tab := range tabs {
						s.viewTerminalTab(c, tab, tab.ID == active)
					}
				})
			})
		} else {
			ui.Row(c).Grow(1).PaddingX(6).Children(func() {
				ui.Text(c, cat(c).TerminalNoTabs()).FontSize(12).TextColor(t.TextMuted)
			})
		}
		// New tab opens in the selected folder, like upstream's createTerminal.
		root := s.folderPathByID(s.selectedFolder)
		add := ui.Row(c.Key("term-new")).Width(24).Height(24).Radius(5).
			AlignItems(ui.Center).Justify(ui.Center).Label(cat(c).TerminalNewTab())
		if root == "" {
			add.Tooltip(cat(c).TerminalOpenFolderFirst())
		}
		add.Children(func() {
			ui.Icon(c, iconPlus).Width(13).Height(13).TextColor(condColor(root != "", t.TextMuted, t.TextMuted.Alpha(0.4)))
		})
		if add.Clicked() && root != "" {
			s.spawnTerminal(root)
		}
		if active != "" {
			iconButton(c, "term-refresh", iconRefresh, cat(c).TerminalRefresh(), func() {
				s.invalidate("term:" + active)
			})
		}
	})
}

// viewTerminalTab renders one tab: the folder-aware tooltip, the title, and a
// close affordance, with upstream's close verbs on a right-click. While
// renaming, the title yields to an inline input with save/cancel buttons.
func (s *Shell) viewTerminalTab(c *ui.Context, tab termTab, active bool) {
	t := c.Theme()
	if s.termRenameID == tab.ID {
		ui.Row(c.Key("term-tab-rename-" + tab.ID)).Gap(3).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("term-rename-input"), &s.termRenameDraft).
				Placeholder(cat(c).TerminalRenamePlaceholder()).Width(120)
			iconButton(c, "term-rename-save", iconCheck, cat(c).TerminalSaveName(), func() {
				s.commitTerminalRename(tab.ID)
			})
			iconButton(c, "term-rename-cancel", iconX, cat(c).TerminalCancelRename(), func() {
				s.termRenameID = ""
			})
		})
		return
	}

	card := ui.Row(c.Key("term-tab-"+tab.ID)).Padding(2, 8, 2, 5).Gap(4).
		Radius(5).MaxWidth(190).AlignItems(ui.Center)
	if active {
		card.Background(t.Background)
	}
	if folder := s.folderNameByID(tab.FolderID); folder != "" {
		card.Tooltip(folder + " — " + tab.Title)
	} else {
		card.Tooltip(tab.Title)
	}
	card.Children(func() {
		ui.Text(c, tab.Title).FontSize(12).MaxLines(1).
			TextColor(condColor(active, t.Text, t.TextMuted))
		closeBtn := ui.Row(c.Key("term-tab-x-" + tab.ID)).Width(16).Height(16).Radius(4).
			AlignItems(ui.Center).Justify(ui.Center).
			Label(cat(c).TerminalCloseTabNamed(tab.Title)).Tooltip(cat(c).TerminalCloseTabNamed(tab.Title))
		closeBtn.Children(func() {
			ui.Icon(c, iconX).Width(10).Height(10).TextColor(t.TextMuted)
		})
		if closeBtn.Clicked() {
			s.closeTerminalTab(tab.ID)
		}
	})
	card.ContextMenu(func(m *ui.Menu) {
		if m.Item(cat(c).TerminalRenameTab()).Chosen() {
			s.mu.Lock()
			s.termRenameID = tab.ID
			s.termRenameDraft = tab.Title
			s.mu.Unlock()
		}
		m.Separator()
		if m.Item(cat(c).TerminalCloseTab()).Chosen() {
			s.closeTerminalTab(tab.ID)
		}
		if m.Item(cat(c).TerminalCloseOthers()).Disabled(len(s.termTabs) <= 1).Chosen() {
			s.closeOtherTerminalTabs(tab.ID)
		}
		if m.Item(cat(c).TerminalCloseAll()).Chosen() {
			s.closeAllTerminalTabs()
		}
	})
	if card.Clicked() {
		s.switchTerminalTab(tab.ID)
	}
}

// viewTerminalBody renders the no-folder hint, the start prompt, or the active
// tab's snapshot with its line input.
func (s *Shell) viewTerminalBody(c *ui.Context) {
	t := c.Theme()
	root := s.folderPathByID(s.selectedFolder)
	if s.selectedFolder == "" || root == "" {
		ui.Text(c, cat(c).TerminalNoFolder()).FontSize(12).TextColor(t.TextMuted)
		return
	}

	s.mu.Lock()
	idx := s.termTabIndexLocked(s.termActive)
	var tab termTab
	if idx >= 0 {
		tab = s.termTabs[idx]
	}
	errMsg := s.termErr
	s.mu.Unlock()

	if idx < 0 {
		if ui.PrimaryButton(c.Key("term-start"), cat(c).TerminalStart()).Clicked() {
			s.spawnTerminal(root)
		}
		ui.Text(c, cat(c).TerminalStartHint(root)).FontSize(12).TextColor(t.TextMuted).Wrap()
		return
	}
	if tab.Err != "" {
		ui.Text(c, cat(c).TerminalError(tab.Err)).FontSize(12).TextColor(t.Danger).Wrap()
		return
	}
	if errMsg != "" {
		ui.Text(c, cat(c).TerminalError(errMsg)).FontSize(12).TextColor(t.Danger).Wrap()
	}
	snap := asObj(s.ensure("term:"+tab.ID, "terminal_snapshot", map[string]any{"terminalId": tab.ID}))
	if e := s.errFor("term:" + tab.ID); e != "" {
		ui.Text(c, cat(c).TerminalError(e)).FontSize(12).TextColor(t.Danger).Wrap()
		return
	}
	if snap == nil {
		loadingState(c, "")
		return
	}
	if alive, _ := snap["alive"].(bool); !alive {
		ui.Text(c, cat(c).TerminalExited()).FontSize(12).TextColor(t.TextMuted)
	}
	ui.Scroll(c.Key("term-out")).Grow(1).Children(func() {
		ui.Text(c, str(snap, "data")).FontSize(11).TextColor(t.Text).Wrap()
	})
	ui.Row(c.Key("term-input")).Gap(6).Children(func() {
		line := ui.TextInput(c.Key("term-line"), &s.termTabs[idx].Input).
			Placeholder(cat(c).TerminalPlaceholder()).Grow(1)
		s.termInputFocused = line.Focused()
		if ui.PrimaryButton(c.Key("term-send"), cat(c).TerminalSend()).
			Disabled(s.termTabs[idx].Input == "").Clicked() {
			s.sendTerminalLine()
		}
	})
}

// spawnTerminal starts a PTY in root as a new "Terminal N" tab.
func (s *Shell) spawnTerminal(root string) {
	s.newTerminalTab("", root, "")
}

// newTerminalTab appends a tab bound to root, focuses it and starts its PTY.
// initial, when non-empty, is handed to the shell at spawn (upstream's
// createTerminalWithCommand). A failed spawn stays as an error inside the tab.
func (s *Shell) newTerminalTab(title, root, initial string) string {
	if root == "" {
		return ""
	}
	svc := s.svc
	s.mu.Lock()
	s.termCounter++
	id := fmt.Sprintf("term-%d", s.termCounter)
	if title == "" {
		title = fmt.Sprintf("Terminal %d", s.termCounter)
	}
	s.termTabs = append(s.termTabs, termTab{
		ID: id, Title: title, FolderID: s.selectedFolder, WorkingDir: root,
	})
	s.termActive = id
	s.termOpen = true
	s.termErr = ""
	s.mu.Unlock()

	go func() {
		_, err := s.B.raw("terminal_spawn", map[string]any{
			"terminalId": id, "workingDir": root, "initialCommand": initial,
		})
		s.mu.Lock()
		if i := s.termTabIndexLocked(id); i >= 0 && err != nil {
			s.termTabs[i].Err = err.Error()
		}
		s.mu.Unlock()
		if err == nil {
			s.invalidate("term:" + id)
		}
		svc.Invalidate()
	}()
	return id
}

// sendTerminalLine writes the active tab's composer line to its PTY followed
// by CR.
func (s *Shell) sendTerminalLine() {
	s.mu.Lock()
	idx := s.termTabIndexLocked(s.termActive)
	if idx < 0 || s.termTabs[idx].Input == "" {
		s.mu.Unlock()
		return
	}
	id := s.termTabs[idx].ID
	line := s.termTabs[idx].Input
	s.termTabs[idx].Input = ""
	s.mu.Unlock()
	svc := s.svc
	go func() {
		_, err := s.B.raw("terminal_write", map[string]any{"terminalId": id, "data": line + "\r"})
		s.mu.Lock()
		if err != nil {
			s.termErr = err.Error()
		}
		delete(s.cache, "term:"+id)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// closeTerminalTab removes one tab and kills its PTY. Upstream focuses the tab
// that slid into the closed slot, else the last one; closing the last tab
// collapses the panel.
func (s *Shell) closeTerminalTab(id string) {
	s.mu.Lock()
	i := s.termTabIndexLocked(id)
	if i < 0 {
		s.mu.Unlock()
		return
	}
	s.termTabs = append(s.termTabs[:i:i], s.termTabs[i+1:]...)
	if s.termRenameID == id {
		s.termRenameID = ""
	}
	if len(s.termTabs) == 0 {
		s.termActive = ""
		s.termOpen = false
	} else if s.termActive == id {
		if i < len(s.termTabs) {
			s.termActive = s.termTabs[i].ID
		} else {
			s.termActive = s.termTabs[len(s.termTabs)-1].ID
		}
	}
	delete(s.cache, "term:"+id)
	s.mu.Unlock()
	s.killTerminalPTY(id)
}

// closeOtherTerminalTabs keeps only the named tab (upstream's "Close Others").
func (s *Shell) closeOtherTerminalTabs(id string) {
	s.mu.Lock()
	i := s.termTabIndexLocked(id)
	if i < 0 {
		s.mu.Unlock()
		return
	}
	var killIDs []string
	for _, tab := range s.termTabs {
		if tab.ID != id {
			killIDs = append(killIDs, tab.ID)
			delete(s.cache, "term:"+tab.ID)
		}
	}
	if len(s.termTabs) > 1 {
		s.termTabs = []termTab{s.termTabs[i]}
	}
	s.termActive = id
	s.termOpen = true
	s.termRenameID = ""
	s.mu.Unlock()
	for _, kid := range killIDs {
		s.killTerminalPTY(kid)
	}
}

// closeAllTerminalTabs empties the strip and collapses the panel.
func (s *Shell) closeAllTerminalTabs() {
	s.mu.Lock()
	if len(s.termTabs) == 0 {
		s.mu.Unlock()
		return
	}
	ids := make([]string, 0, len(s.termTabs))
	for _, tab := range s.termTabs {
		ids = append(ids, tab.ID)
		delete(s.cache, "term:"+tab.ID)
	}
	s.termTabs = nil
	s.termActive = ""
	s.termOpen = false
	s.termRenameID = ""
	s.mu.Unlock()
	for _, id := range ids {
		s.killTerminalPTY(id)
	}
}

// switchTerminalTab focuses one tab and leaves any rename in progress.
func (s *Shell) switchTerminalTab(id string) {
	s.mu.Lock()
	if s.termTabIndexLocked(id) < 0 {
		s.mu.Unlock()
		return
	}
	s.termActive = id
	s.termRenameID = ""
	s.termErr = ""
	s.mu.Unlock()
}

// commitTerminalRename applies a non-empty inline rename.
func (s *Shell) commitTerminalRename(id string) {
	title := strings.TrimSpace(s.termRenameDraft)
	if title == "" {
		return
	}
	s.mu.Lock()
	if i := s.termTabIndexLocked(id); i >= 0 {
		s.termTabs[i].Title = title
	}
	s.termRenameID = ""
	s.mu.Unlock()
}

// killTerminalPTY asks the backend to terminate one PTY. The tab is already
// gone from the strip; this only reaps the process.
func (s *Shell) killTerminalPTY(id string) {
	svc := s.svc
	go func() {
		_, _ = s.B.raw("terminal_kill", map[string]any{"terminalId": id})
		svc.Invalidate()
	}()
}
