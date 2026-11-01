package app

// changes.go mirrors the git-changes surface of upstream's workspace aux panel
// (src/components/layout/aux-panel-git-changes-tab): the folder's working-tree
// changes, the selected file's diff, and the write actions — stage a file,
// discard a file (two-step, since it is destructive) and commit everything.
// Built over the already-ported backend: git_status, git_diff, git_add_files,
// git_rollback_file and git_commit.

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
	"gocode/locales"
)

// viewChangesList is the aux panel's Changes tab: the folder's working-tree
// changes (git_status) and the commit box, mirroring upstream's
// aux-panel-git-changes-tab. The selected file's diff opens in the detail pane.
func (s *Shell) viewChangesList(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	root := s.folderPathByID(s.selectedFolder)
	var files []map[string]any
	if root != "" {
		files = asList(s.ensure("gitstatus:"+root, "git_status", map[string]any{"path": root}))
	}
	if root != "" {
		ui.Row(c.Key("ch-head")).PaddingX(8).PaddingY(6).Gap(4).AlignItems(ui.Center).Children(func() {
			// Bulk actions show while the list holds a choice: mygo's
			// ListState.Selection + DragSelect make several files choosable
			// (click, Ctrl/Cmd-click, Shift-click, drag, Ctrl+A).
			if n := s.changesSel.Len(); n > 0 {
				if ui.Button(c.Key("ch-stage-sel"), m.ChangesStageCount(n)).Clicked() {
					s.stageFiles(c.Locale(), root, s.selectedChangeFiles())
					s.changesSel.Clear()
					s.changesDiscardArmed = false
				}
				// Discard is destructive, so the bulk form takes a second click
				// too — the same rule the single-file button follows.
				label := m.ChangesDiscardCount(n)
				if s.changesDiscardArmed {
					label = m.ChangesDiscardConfirmCount(n)
				}
				if ui.Button(c.Key("ch-discard-sel"), label).Clicked() {
					if s.changesDiscardArmed {
						s.discardFiles(c.Locale(), root, s.selectedChangeFiles())
						s.changesSel.Clear()
						s.changesDiscardArmed = false
					} else {
						s.changesDiscardArmed = true
					}
				}
			}
			ui.Spacer(c)
			iconButton(c, "ch-refresh", iconRefresh, m.ActionRefresh(), func() {
				s.invalidate("gitstatus:" + root)
				if s.diffSel != "" {
					s.invalidate("gitdiff:" + s.diffSel)
				}
			})
			// Git-operations menu (upstream's dropdown): commit / push / merge /
			// stash, each a whole-page route in this single-window build.
			more := ui.Row(c.Key("ch-more")).Padding(4).Radius(6).AlignItems(ui.Center).
				Label(m.GitOpsMenu()).Tooltip(m.GitOpsMenu())
			more.Children(func() {
				ui.Icon(c, iconMore).Width(15).Height(15).TextColor(t.TextMuted)
			})
			more.ContextMenu(func(m *ui.Menu) { s.gitMenuItems(m, c) })
			more.Clicked() // mark clickable so the menu target resolves
		})
	}
	if s.selectedFolder == "" || root == "" {
		ui.Text(c, m.ChangesSelectFolder()).FontSize(12).TextColor(t.TextMuted).Padding(10).Wrap()
		return
	}
	if s.errFor("gitstatus:"+root) != "" {
		ui.Column(c).Padding(10).Children(func() { s.errorState(c, "gitstatus:"+root, nil) })
		return
	}
	if files == nil {
		ui.Column(c).Grow(1).Padding(10).Children(func() { loadingState(c, "") })
	} else if len(files) == 0 {
		ui.Column(c).Grow(1).Padding(10).Children(func() {
			ui.Text(c, m.ChangesNoChanges()).FontSize(12).TextColor(t.TextMuted)
		})
	} else {
		// Rows are keyed by path, so a choice follows its file as the status
		// list reorders under it (a stage empties a row, a commit removes it).
		s.changesList.Key = func(row int) any { return str(files[row], "file") }
		s.changesList.Selection = &s.changesSel
		s.changesList.Selected = &s.changesIdx
		s.changesList.DragSelect = true
		s.changesList.Label = func(row int) string { return str(files[row], "file") }
		ui.List(c.Key("ch-list"), &s.changesList, len(files), func(i int) {
			file := str(files[i], "file")
			code := str(files[i], "status")
			row := ui.Row(c.Key("ch-" + file)).PaddingX(8).PaddingY(4).Gap(8).AlignItems(ui.Center)
			// Porcelain code → accent: untracked/added green, deleted red,
			// modified/renamed amber, anything else muted.
			col := t.TextMuted
			switch {
			case code == "??" || strings.HasPrefix(code, "A"):
				col = t.Success
			case strings.HasPrefix(code, "D"):
				col = t.Danger
			case strings.Contains(code, "M") || strings.HasPrefix(code, "R"):
				col = t.Warning
			}
			// One RichText, not two Texts: a list row is as wide as its content,
			// so a second child would overflow the row's clip and never paint.
			// A chosen row paints the accent background, so its text takes the
			// on-accent colour instead of the body colour.
			codeCol, nameCol := col, t.Text
			if s.changesSel.Has(file) {
				codeCol, nameCol = t.AccentText, t.AccentText
			}
			row.Children(func() {
				ui.RichText(c,
					ui.Span{Text: code, Color: codeCol},
					ui.Span{Text: "   " + file, Color: nameCol},
				).FontSize(11).Width(292).MaxLines(1)
			})
		}).Grow(1).FillWidth()
		// The list owns the press (a row's own click handler would take it from
		// the selection), so the detail pane follows the row it chose.
		if s.changesIdx >= 0 && s.changesIdx < len(files) {
			if f := str(files[s.changesIdx], "file"); f != s.diffSel {
				s.diffSel = f
				s.discardConfirm = ""
				s.changesDiscardArmed = false
			}
		}
	}
	ui.Divider(c)
	ui.Column(c.Key("ch-commit")).Padding(8).Gap(6).Children(func() {
		ui.Row(c).Children(func() {
			ui.TextInput(c.Key("ch-msg"), &s.commitMsg).Placeholder(m.ChangesCommitPlaceholder()).Grow(1)
		})
		if ui.PrimaryButton(c.Key("ch-commit-btn"), m.ChangesCommitAll()).Disabled(s.commitMsg == "" || len(files) == 0).Clicked() {
			s.commitAll(root, files)
		}
		if s.actionMsg != "" {
			ui.Text(c, s.actionMsg).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
		}
	})
}

// viewDiff renders the selected file's unified diff plus its write actions
// (stage, discard).
func (s *Shell) viewDiff(c *ui.Context, root string) {
	t := c.Theme()
	if s.diffSel == "" {
		ui.Text(c, cat(c).ChangesSelectDiff()).FontSize(12).TextColor(t.TextMuted)
		return
	}
	ui.Row(c.Key("ch-diff-head")).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, s.diffSel).FontSize(12).Bold().MaxLines(1)
		ui.Spacer(c)
		if ui.Button(c.Key("ch-stage"), cat(c).ChangesStage()).Clicked() {
			s.stageFiles(c.Locale(), root, []string{s.diffSel})
		}
		// Discard reverts the file to HEAD (and deletes it when untracked), so it
		// takes a second, explicit click.
		if s.discardConfirm == s.diffSel {
			if ui.Button(c.Key("ch-discard-yes"), cat(c).ChangesConfirmDiscard()).Clicked() {
				s.discardFiles(c.Locale(), root, []string{s.diffSel})
				s.discardConfirm = ""
			}
			if ui.Button(c.Key("ch-discard-no"), cat(c).ActionCancel()).Clicked() {
				s.discardConfirm = ""
			}
		} else if ui.Button(c.Key("ch-discard"), cat(c).ChangesDiscard()).Clicked() {
			s.discardConfirm = s.diffSel
		}
		iconButton(c, "ch-diff-close", iconX, cat(c).ActionClose(), func() { s.diffSel = "" })
	})
	key := "gitdiff:" + s.diffSel
	raw := s.ensure(key, "git_diff", map[string]any{"path": root, "file": s.diffSel})
	if e := s.errFor(key); e != "" {
		ui.Text(c, cat(c).ChangesCannotDiff(e)).FontSize(12).TextColor(t.Danger).Wrap()
		return
	}
	if raw == nil {
		loadingState(c, "")
		return
	}
	var text string
	_ = json.Unmarshal(raw, &text) // git_diff returns a bare JSON string
	ui.Divider(c)
	if strings.TrimSpace(text) == "" {
		ui.Text(c, cat(c).ChangesNoDiff()).FontSize(12).TextColor(t.TextMuted)
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		// A real git diff goes through mygo's editor.DiffView (ParsePatch), with
		// line numbers, colored change marks and collapsed context; plain text is
		// the fallback for anything the patch reader rejects.
		if s.viewPatch(c, "ch-"+s.diffSel, text, t) {
			return
		}
		ui.Text(c, text).FontSize(11).TextColor(t.Text).Wrap()
	})
}

// selectedChangeFiles returns the chosen rows' paths in a stable order, so a
// bulk request (and its test) does not depend on map iteration.
func (s *Shell) selectedChangeFiles() []string {
	out := make([]string, 0, s.changesSel.Len())
	for path := range s.changesSel.All() {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// stageFiles stages the given files in one git_add_files call.
func (s *Shell) stageFiles(loc, root string, files []string) {
	if len(files) == 0 {
		return
	}
	svc := s.svc
	go func() {
		_, err := s.B.raw("git_add_files", map[string]any{"path": root, "files": files})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = locales.In(loc).ChangesStageFailed(err.Error())
		} else {
			s.actionMsg = locales.In(loc).ChangesStagedCount(len(files))
		}
		delete(s.cache, "gitstatus:"+root)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// discardFiles reverts each file to HEAD (git_rollback_file, one call per file
// since the route is single-file); an untracked file is removed. Destructive —
// the UI asks for a second click first.
func (s *Shell) discardFiles(loc, root string, files []string) {
	if len(files) == 0 {
		return
	}
	svc := s.svc
	go func() {
		var failed error
		for _, file := range files {
			if _, err := s.B.raw("git_rollback_file", map[string]any{"path": root, "file": file}); err != nil {
				failed = err
				break
			}
			s.mu.Lock()
			delete(s.cache, "gitdiff:"+file)
			s.mu.Unlock()
		}
		s.mu.Lock()
		if failed != nil {
			s.actionMsg = locales.In(loc).ChangesDiscardFailed(failed.Error())
		} else {
			s.actionMsg = locales.In(loc).ChangesDiscardedCount(len(files))
		}
		delete(s.cache, "gitstatus:"+root)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// commitAll stages and commits every working-tree change (git_commit).
func (s *Shell) commitAll(root string, files []map[string]any) {
	msg := s.commitMsg
	if msg == "" || len(files) == 0 {
		return
	}
	names := make([]any, 0, len(files))
	for _, f := range files {
		if n := str(f, "file"); n != "" {
			names = append(names, n)
		}
	}
	s.commitMsg = ""
	svc := s.svc
	go func() {
		_, err := s.B.raw("git_commit", map[string]any{"path": root, "message": msg, "files": names})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = "commit: " + err.Error()
		} else {
			s.actionMsg = "committed"
		}
		delete(s.cache, "gitstatus:"+root)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}
