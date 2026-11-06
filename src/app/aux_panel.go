package app

// aux_panel.go is the workspace's right column — upstream's AuxPanel
// (src/components/layout/aux-panel.tsx): four tabs over the folder's file tree,
// git changes, commit history and the live session. Upstream keeps these in a
// right column, not in the main area, with the folder-scoped tabs shown only
// when a folder workspace is open. The list lives here; the detail (file
// preview / diff / commit) opens in the main area's detail pane (see
// viewWorkspace and viewAuxDetail).

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (s *Shell) viewAuxPanel(c *ui.Context) {
	t := c.Theme()
	if s.auxTab == "" {
		s.auxTab = "session"
	}
	ui.Column(c.Key("ws-aux")).Width(340).FillHeight().Background(t.Surface).Children(func() {
		ui.Row(c.Key("aux-tabs")).PaddingX(8).PaddingY(6).Gap(4).Children(func() {
			for _, tab := range []struct{ id, label string }{
				{"session", cat(c).AuxTabSession()},
				{"files", cat(c).AuxTabFiles()},
				{"changes", cat(c).AuxTabChanges()},
				{"commits", cat(c).AuxTabCommits()},
			} {
				active := s.auxTab == tab.id
				var clicked bool
				if active {
					clicked = ui.PrimaryButton(c.Key("aux-"+tab.id), tab.label).Clicked()
				} else {
					clicked = ui.Button(c.Key("aux-"+tab.id), tab.label).Clicked()
				}
				if clicked {
					s.auxTab = tab.id
				}
			}
		})
		ui.Divider(c)
		ui.Column(c.Key("aux-body")).Grow(1).FillHeight().Children(func() {
			switch s.auxTab {
			case "files":
				s.viewFileTree(c)
			case "changes":
				s.viewChangesList(c)
			case "commits":
				s.viewCommitsList(c)
			default:
				s.viewSessionDetails(c)
			}
		})
	})
}

// viewSessionDetails is the Session tab: the live session's facts, mirroring
// upstream's aux-panel-session-details-tab.
func (s *Shell) viewSessionDetails(c *ui.Context) {
	t := c.Theme()
	root := s.folderPathByID(s.selectedFolder)
	s.mu.Lock()
	connID := s.connByConv[s.selectedConv]
	s.mu.Unlock()
	var snap map[string]any
	if connID != "" {
		snap = asObj(s.ensure("snapshot:"+connID, "acp_get_session_snapshot", map[string]any{"connectionId": connID}))
	}
	ui.Scroll(c).Grow(1).Padding(10).Children(func() {
		ui.Column(c).Gap(6).Children(func() {
			kv := func(k, v string) {
				ui.Row(c).Gap(8).Children(func() {
					ui.Text(c, k).FontSize(11).TextColor(t.TextMuted).Width(92)
					ui.Text(c, firstNonEmpty(v, "—")).FontSize(12).MaxLines(2)
				})
			}
			kv(cat(c).AuxFolder(), root)
			kv(cat(c).AuxConversation(), s.selectedConv)
			kv(cat(c).AuxConnection(), connID)
			kv(cat(c).AuxStatus(), str(snap, "status"))
			kv(cat(c).AuxAgent(), str(s.convByID(s.selectedConv), "agent_type"))
		})
	})
}

// viewCommitsList is the Commits tab: the folder's git history (git_log),
// newest first. Selecting one shows its changed files in the detail pane.
func (s *Shell) viewCommitsList(c *ui.Context) {
	t := c.Theme()
	root := s.folderPathByID(s.selectedFolder)
	if root == "" {
		ui.Text(c, cat(c).AuxSelectFolderCommits()).FontSize(12).TextColor(t.TextMuted).Padding(10).Wrap()
		return
	}
	// Header with the git-operations menu (commit / push / merge / stash),
	// mirroring upstream's commits-header dropdown.
	ui.Row(c.Key("cm-head")).PaddingX(8).PaddingY(6).Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		more := ui.Row(c.Key("cm-more")).Padding(4).Radius(6).AlignItems(ui.Center).
			Label(cat(c).GitOpsMenu()).Tooltip(cat(c).GitOpsMenu())
		more.Children(func() {
			ui.Icon(c, iconMore).Width(15).Height(15).TextColor(t.TextMuted)
		})
		more.ContextMenu(func(m *ui.Menu) { s.gitMenuItems(m, c) })
		more.Clicked()
	})
	key := "gitlog:" + root
	data := asObj(s.ensure(key, "git_log", map[string]any{"path": root, "limit": 60, "withFiles": false}))
	if s.errFor(key) != "" {
		ui.Column(c).Padding(10).Children(func() { s.errorState(c, key, nil) })
		return
	}
	if data == nil {
		ui.Column(c).Padding(10).Children(func() { loadingState(c, "") })
		return
	}
	entries := asListVal(data["entries"])
	if len(entries) == 0 {
		ui.Column(c).Grow(1).Padding(10).Children(func() {
			ui.Text(c, cat(c).AuxNoCommits()).FontSize(12).TextColor(t.TextMuted)
		})
		return
	}
	// A List, not a Scroll+Column: the arrow keys walk the log and the choice
	// follows, the way the Changes tab behaves.
	s.commitsList.Key = func(row int) any { return str(entries[row], "hash") }
	s.commitsList.Selected = &s.commitsIdx
	s.commitsList.Label = func(row int) string {
		return str(entries[row], "hash") + " " + firstLine(str(entries[row], "message"))
	}
	ui.List(c.Key("cm-list"), &s.commitsList, len(entries), func(i int) {
		e := entries[i]
		hash := str(e, "hash")
		// A chosen row paints the accent background, so its text takes the
		// on-accent colour. commitsIdx is already this frame's, the list having
		// taken the press before the build.
		metaCol, msgCol := t.TextMuted, t.Text
		if s.commitsIdx == i {
			metaCol, msgCol = t.AccentText, t.AccentText
		}
		row := ui.Row(c.Key("cm-" + hash)).PaddingX(8).PaddingY(6).Gap(8).AlignItems(ui.Center)
		row.Children(func() {
			// Explicit widths: a list row is as wide as its content, so a
			// flexible child would collapse to nothing.
			ui.Text(c, hash).FontSize(11).TextColor(metaCol).Width(52)
			ui.Column(c).Gap(1).Children(func() {
				ui.Text(c, firstLine(str(e, "message"))).Width(252).MaxLines(1).TextColor(msgCol)
				date := str(e, "date")
				meta := ui.Text(c, str(e, "author")+" · "+relTime(date, time.Now())).
					FontSize(10).TextColor(metaCol).Width(252).MaxLines(1)
				meta.Tooltip(shortDate(date)) // the absolute date stays one hover away
			})
		})
	}).Grow(1)
	// The list owns the press, so the detail pane follows the row it chose.
	if s.commitsIdx >= 0 && s.commitsIdx < len(entries) {
		s.commitSel = str(entries[s.commitsIdx], "hash")
	}
}

// viewCommitDetail is the detail pane for a selected commit: its changed files
// (git_commit_files), each with its +/- line counts.
func (s *Shell) viewCommitDetail(c *ui.Context, root string) {
	t := c.Theme()
	ui.Row(c.Key("aux-detail-head")).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, cat(c).AuxCommitTitle(s.commitSel)).FontSize(12).Bold().MaxLines(1)
		ui.Spacer(c)
		iconButton(c, "commit-close", iconX, cat(c).ActionClose(), func() { s.commitSel = "" })
	})
	key := "commitfiles:" + s.commitSel
	files := asList(s.ensure(key, "git_commit_files", map[string]any{"path": root, "commit": s.commitSel}))
	if s.errFor(key) != "" {
		s.errorState(c, key, nil)
		return
	}
	if files == nil {
		loadingState(c, "")
		return
	}
	if len(files) == 0 {
		emptyState(c, iconInfo, cat(c).AuxNoFileChanges(), "")
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(1).Children(func() {
			for _, f := range files {
				add := intVal(f, "additions")
				del := intVal(f, "deletions")
				ui.Row(c.Key("cf-" + str(f, "path"))).PaddingX(8).PaddingY(4).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, str(f, "status")).FontSize(11).Width(20).TextColor(t.TextMuted)
					ui.Text(c, str(f, "path")).MaxLines(1).Grow(1).TextColor(t.Text)
					ui.Textf(c, "+%d", add).FontSize(11).TextColor(t.Success)
					ui.Textf(c, "-%d", del).FontSize(11).TextColor(t.Danger)
				})
			}
		})
	})
}

// auxDetailOpen reports whether the active aux tab has a selection, so the main
// area shows that detail instead of the conversation.
func (s *Shell) auxDetailOpen() bool {
	switch s.auxTab {
	case "files":
		return s.fileSel != ""
	case "changes":
		return s.diffSel != ""
	case "commits":
		return s.commitSel != ""
	}
	return false
}

// viewAuxDetail is the main area's detail pane: the file preview, diff or
// commit opened from the aux panel (upstream opens these in the file workspace
// beside the conversation).
func (s *Shell) viewAuxDetail(c *ui.Context) {
	root := s.folderPathByID(s.selectedFolder)
	if root == "" {
		return
	}
	switch s.auxTab {
	case "files":
		s.viewFilePreview(c, root)
	case "changes":
		s.viewDiff(c, root)
	case "commits":
		s.viewCommitDetail(c, root)
	}
}

// firstLine is the first line of a commit message.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// shortDate trims an ISO timestamp to its date (YYYY-MM-DD).
func shortDate(iso string) string {
	if len(iso) >= 10 {
		return iso[:10]
	}
	return iso
}
