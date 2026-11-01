package app

// git_ops.go ports upstream's four git-operation windows to this build's
// single window: each is a full-page workbench route instead of a separate OS
// window (upstream opens src/app/commit, /merge, /stash, /push as popups).
//
// References, and what each becomes here:
//
//   - app/commit + layout/commit-dialog.tsx → the Commit page: the branch and
//     author, the working-tree files with their porcelain codes, a message box
//     and Commit (git_commit stages the named files, then commits).
//   - app/stash + layout/stash-dialog.tsx / unstash-dialog.tsx → the Stash
//     page: create a stash (message + keep-index) and the stash list with
//     Pop / Apply / Drop / Clear (the git_stash_* family).
//   - app/push + layout/push-workspace.tsx → the Push page: the current branch,
//     its tracking remote and the unpushed count (git_push_info), the remote /
//     branch pickers, and Push (git_push, which sets upstream on first push).
//   - app/merge + merge/merge-workspace.tsx → the Merge page: pick a branch and
//     merge it (git_merge); a conflicted outcome switches to the conflict state
//     — the unmerged paths (git_list_conflicts) with Abort / Continue
//     (git_abort_operation / git_continue_operation).
//
// The full chrome each upstream component carries (title bar, drag region,
// toaster, remote-connection gate) is the window's own in this build, so the
// pages keep only the workspace body. The entry points are the aux panel's
// Changes / Commits header menu, mirroring upstream's dropdown.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"gocode/locales"
)

// Git-operation workbench routes. All are folder-scoped, whole-page panels.
const (
	gitRouteCommit   = "git-commit"
	gitRouteStash    = "git-stash"
	gitRoutePush     = "git-push"
	gitRouteMerge    = "git-merge"
	gitRouteBranches = "git-branches"
	gitRouteRemotes  = "git-remotes"
)

// isGitRoute reports whether a workbench route is one of the git-operation pages.
func isGitRoute(route string) bool {
	switch route {
	case gitRouteCommit, gitRouteStash, gitRoutePush, gitRouteMerge, gitRouteBranches, gitRouteRemotes:
		return true
	}
	return false
}

// viewGitRoute dispatches a git-operation route to its page.
func (s *Shell) viewGitRoute(c *ui.Context, route string) {
	t := c.Theme()
	root := s.folderPathByID(s.selectedFolder)
	title := ""
	switch route {
	case gitRouteCommit:
		title = cat(c).GitCommitTitle()
	case gitRouteStash:
		title = cat(c).GitStashTitle()
	case gitRoutePush:
		title = cat(c).GitPushTitle()
	case gitRouteMerge:
		title = cat(c).GitMergeTitle()
	case gitRouteBranches:
		title = cat(c).GitBranchesTitle()
	case gitRouteRemotes:
		title = cat(c).GitRemotesLabel()
	}
	// Page header: back to the workspace, the title, the folder name.
	ui.Row(c.Key("git-head")).Gap(8).AlignItems(ui.Center).Children(func() {
		iconButton(c, "git-back", iconBack, cat(c).GitBack(), func() { s.workbench = "" })
		ui.Text(c, title).FontSize(18).Bold()
		if name := s.folderNameByID(s.selectedFolder); name != "" {
			ui.Text(c, "· "+name).FontSize(13).TextColor(t.TextMuted).MaxLines(1)
		}
	})
	if s.selectedFolder == "" || root == "" {
		ui.Text(c, cat(c).GitSelectFolder()).FontSize(12).TextColor(t.TextMuted).Wrap()
		return
	}
	ui.Divider(c)
	// Commit and push keep a two-pane workspace (file/commit list beside the
	// diff), which needs the page's own height; the rest are single-column
	// pages that scroll as a whole.
	switch route {
	case gitRouteCommit:
		s.viewGitCommitPage(c, root)
	case gitRoutePush:
		s.viewGitPushPage(c, root)
	default:
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Gap(12).Children(func() {
				switch route {
				case gitRouteStash:
					s.viewGitStashPage(c, root)
				case gitRouteMerge:
					s.viewGitMergePage(c, root)
				case gitRouteBranches:
					s.viewGitBranchesPage(c, root)
				case gitRouteRemotes:
					s.viewGitRemotesPage(c, root)
				}
			})
		})
	}
}

// gitMenuItems attaches the git-operation entries to a menu, shared by the aux
// panel's Changes and Commits headers — upstream's dropdown with "commit code",
// "push", the merge entry and the stash entry.
func (s *Shell) gitMenuItems(m *ui.Menu, c *ui.Context) {
	if m.Item(cat(c).GitCommitTitle()).Chosen() {
		s.workbench = gitRouteCommit
	}
	if m.Item(cat(c).GitPushTitle()).Chosen() {
		s.workbench = gitRoutePush
	}
	if m.Item(cat(c).GitMergeTitle()).Chosen() {
		s.workbench = gitRouteMerge
	}
	if m.Item(cat(c).GitBranchesTitle()).Chosen() {
		s.workbench = gitRouteBranches
	}
	if m.Item(cat(c).GitRemotesLabel()).Chosen() {
		s.workbench = gitRouteRemotes
	}
	m.Separator()
	if m.Item(cat(c).GitStashTitle()).Chosen() {
		s.workbench = gitRouteStash
	}
}

// ─── commit page ──────────────────────────────────────────────────────────

// commitStatusKey is the commit page's own git_status cache slot. It asks for
// every untracked file (showAllUntracked), unlike the Changes tab's collapsed
// untracked groups, so the two must not share a key.
func commitStatusKey(root string) string { return "commitstatus:" + root }

// viewGitCommitPage renders upstream's commit dialog as a page: the branch and
// author, the tracked/untracked file trees with per-file selection, a native
// HEAD↔worktree diff beside them, the message box, and Commit / Commit and Push.
func (s *Shell) viewGitCommitPage(c *ui.Context, root string) {
	t := c.Theme()
	// Facts: the branch and the configured author, the two things a committer
	// checks before writing a message.
	branch := decodeString(s.ensure("gitbranch:"+root, "get_git_branch", map[string]any{"path": root}))
	author := decodeString(s.ensure("gituser:"+root, "git_current_user", map[string]any{"path": root}))
	ui.Row(c.Key("gc-facts")).Gap(16).Children(func() {
		gitFact(c, cat(c).GitBranchLabel(), firstNonEmpty(branch, "—"))
		gitFact(c, cat(c).GitAuthorLabel(), firstNonEmpty(author, "—"))
	})

	key := commitStatusKey(root)
	files := asList(s.ensure(key, "git_status", map[string]any{"path": root, "showAllUntracked": true}))
	if e := s.errFor(key); e != "" {
		s.errorState(c, key, nil)
		return
	}
	if files == nil {
		loadingState(c, "")
		return
	}
	if len(files) == 0 {
		emptyState(c, iconCheck, cat(c).GitNoChanges(), "")
		return
	}
	s.seedCommitSelection(root, files)

	tracked, untracked := splitCommitFiles(files)
	unmerged := 0
	for _, f := range files {
		if unmergedStatus(str(f, "status")) {
			unmerged++
		}
	}

	// An unresolved merge cannot be committed; say so up front and point at the
	// page that can finish it (upstream's conflict banner).
	if unmerged > 0 {
		ui.Row(c.Key("gc-unmerged")).FillWidth().Radius(8).
			Border(1, t.Warning.Alpha(0.35)).Background(t.Warning.Alpha(0.07)).
			Padding(8, 12, 8, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconAlert).Width(14).Height(14).TextColor(t.Warning)
			ui.Text(c, cat(c).GitCommitUnmerged()).FontSize(12).TextColor(t.Warning).Grow(1)
			if ui.Button(c.Key("gc-open-merge"), cat(c).GitOpenMerge()).Clicked() {
				s.workbench = gitRouteMerge
			}
		})
	}

	ui.Row(c.Key("gc-body")).Grow(1).FillWidth().Gap(14).Children(func() {
		ui.Column(c.Key("gc-left")).Width(420).FillHeight().Gap(6).Children(func() {
			sel := s.commitSelCount(files)
			all := sel == len(files)
			ui.Row(c.Key("gc-sel-head")).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, cat(c).GitSelectionCount(sel, len(files))).FontSize(12).Bold()
				ui.Spacer(c)
				label := cat(c).GitSelectAllFiles()
				if all {
					label = cat(c).GitUnselectAllFiles()
				}
				if ui.Button(c.Key("gc-select-all"), label).Clicked() {
					for _, f := range files {
						p := str(f, "file")
						if all {
							delete(s.commitSelFiles, p)
						} else {
							s.commitSelFiles[p] = true
						}
					}
				}
			})
			ui.Scroll(c.Key("gc-list")).Grow(1).Children(func() {
				ui.Column(c).Gap(1).Children(func() {
					s.viewCommitGroupHeader(c, "gc-tracked",
						cat(c).GitTrackedChanges(len(tracked)), tracked, false, nil)
					s.viewCommitNodes(c, buildGitTree(tracked, "file"), 0)
					s.viewCommitGroupHeader(c, "gc-untracked",
						cat(c).GitUntrackedFiles(len(untracked)), untracked, true, func() {
							s.commitUntracked = !s.commitUntracked
						})
					if s.commitUntracked {
						s.viewCommitNodes(c, buildGitTree(untracked, "file"), 0)
					}
				})
			})
			ui.Divider(c)
			ui.Text(c, cat(c).GitMessageLabel()).FontSize(12).Bold()
			ui.TextInput(c.Key("gc-msg"), &s.commitMsg).
				Placeholder(cat(c).GitMessagePlaceholder()).FillWidth()
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				canCommit := strings.TrimSpace(s.commitMsg) != "" && sel > 0 && unmerged == 0 &&
					!s.commitRunning
				commitBtn := ui.PrimaryButton(c.Key("gc-commit"), cat(c).GitCommitButton(sel)).
					Disabled(!canCommit)
				// A distinct label: the page heading already says "Commit", so an
				// identical button label would be ambiguous to assistive tech (and
				// to the tester's hit-by-text).
				commitBtn.Label(cat(c).GitCommitActionLabel())
				if commitBtn.Clicked() {
					s.commitSelected(root, files, false)
				}
				pushBtn := ui.Button(c.Key("gc-commit-push"), cat(c).GitCommitAndPushButton(sel)).
					Disabled(!canCommit)
				pushBtn.Label(cat(c).GitCommitAndPushLabel())
				if pushBtn.Clicked() {
					s.commitSelected(root, files, true)
				}
				iconButton(c, "gc-refresh", iconRefresh, cat(c).ActionRefresh(), func() {
					s.commitSelSeeded = false
					s.commitPrevFile = ""
					s.invalidate(commitStatusKey(root))
					s.invalidate("gitstatus:" + root)
					s.invalidatePrefix("commithead:" + root + ":")
					s.invalidatePrefix("commitwork:" + root + ":")
				})
			})
			s.viewActionOrError(c)
		})
		ui.Column(c.Key("gc-diff")).Grow(1).FillHeight().Gap(6).Children(func() {
			s.viewCommitFileDiff(c, root, files)
		})
	})
}

// splitCommitFiles splits porcelain entries into tracked changes and untracked
// (??) files, upstream's UNTRACKED_STATUS boundary.
func splitCommitFiles(files []map[string]any) (tracked, untracked []map[string]any) {
	for _, f := range files {
		if str(f, "status") == "??" {
			untracked = append(untracked, f)
		} else {
			tracked = append(tracked, f)
		}
	}
	return tracked, untracked
}

// unmergedStatus reports a porcelain v1 unmerged code (UU, AA, DD, AU, UA, DU,
// UD) — the states that make `git commit` refuse.
func unmergedStatus(code string) bool {
	switch code {
	case "DD", "AU", "UD", "UA", "DU", "AA", "UU":
		return true
	}
	return false
}

// seedCommitSelection mirrors upstream loadStatus: tracked changes start
// selected, untracked files do not, and the first tracked file (else the first
// file) opens in the diff pane. Re-seeded after a commit or a refresh.
func (s *Shell) seedCommitSelection(root string, files []map[string]any) {
	if s.commitSelSeeded && s.commitSelRoot == root {
		return
	}
	s.commitSelRoot = root
	s.commitSelSeeded = true
	s.commitSelFiles = map[string]bool{}
	first := ""
	for _, f := range files {
		p := str(f, "file")
		if p == "" {
			continue
		}
		if str(f, "status") != "??" {
			s.commitSelFiles[p] = true
			if first == "" {
				first = p
			}
		}
	}
	if first == "" && len(files) > 0 {
		first = str(files[0], "file")
	}
	s.commitPrevFile = first
	s.commitUntracked = false
}

// commitSelCount counts the selected files.
func (s *Shell) commitSelCount(files []map[string]any) int {
	n := 0
	for _, f := range files {
		if s.commitSelFiles[str(f, "file")] {
			n++
		}
	}
	return n
}

// toggleCommitGroup checks every file of a group, or clears it when all were
// checked — the group box's click.
func (s *Shell) toggleCommitGroup(files []map[string]any) {
	all := len(files) > 0
	for _, f := range files {
		if !s.commitSelFiles[str(f, "file")] {
			all = false
			break
		}
	}
	for _, f := range files {
		p := str(f, "file")
		if all {
			delete(s.commitSelFiles, p)
		} else {
			s.commitSelFiles[p] = true
		}
	}
}

// viewCommitGroupHeader renders a "Tracked changes (n)" / "Untracked files (n)"
// header with a group checkbox and, when collapsible, a chevron that toggles it.
func (s *Shell) viewCommitGroupHeader(c *ui.Context, key, title string, files []map[string]any, collapsible bool, toggle func()) {
	t := c.Theme()
	all := len(files) > 0
	for _, f := range files {
		if !s.commitSelFiles[str(f, "file")] {
			all = false
			break
		}
	}
	checked := all
	ui.Row(c.Key(key)).Padding(4, 6, 4, 2).Gap(6).AlignItems(ui.Center).Children(func() {
		box := ui.CheckboxBase(c.Key(key+"-sel"), &checked).Width(15).Height(15).
			Radius(3).Border(1, t.Border)
		if checked {
			box.Background(t.Accent)
		}
		if key == "gc-tracked" {
			if checked {
				box.Label(cat(c).GitUnselectTracked())
			} else {
				box.Label(cat(c).GitSelectTracked())
			}
		} else if checked {
			box.Label(cat(c).GitUnselectUntracked())
		} else {
			box.Label(cat(c).GitSelectUntracked())
		}
		box.Children(func() {
			if checked {
				ui.Icon(c, iconCheck).Width(11).Height(11).TextColor(t.AccentText)
			}
		})
		if box.Changed() {
			s.toggleCommitGroup(files)
		}
		if collapsible {
			chev := iconChevron
			if s.commitUntracked {
				chev = iconChevronD
			}
			titleRow := ui.Row(c).Gap(4).AlignItems(ui.Center)
			titleRow.Children(func() {
				ui.Icon(c, chev).Width(12).Height(12).TextColor(t.TextMuted)
				ui.Text(c, title).FontSize(12).Bold().TextColor(t.Text)
			})
			if titleRow.Clicked() && toggle != nil {
				toggle()
			}
		} else {
			ui.Text(c, title).FontSize(12).Bold().TextColor(t.Text)
		}
	})
}

// viewCommitNodes renders one level of a commit file tree, recursing into
// directories with an extra indent.
func (s *Shell) viewCommitNodes(c *ui.Context, nodes []*gitTreeNode, depth int) {
	t := c.Theme()
	for _, n := range nodes {
		left := float32(6 + depth*14)
		if n.dir {
			ui.Row(c.Key("gcd-"+n.path)).Padding(4, 6, 4, left).Gap(6).AlignItems(ui.Center).Children(func() {
				if iconFolder != nil {
					ui.Icon(c, iconFolder).Width(12).Height(12).TextColor(t.TextMuted)
				}
				ui.Text(c, n.name+"/").FontSize(11).TextColor(t.TextMuted).MaxLines(1)
			})
			s.viewCommitNodes(c, n.children, depth+1)
			continue
		}
		s.viewCommitFileRow(c, n, left)
	}
}

// viewCommitFileRow renders one selectable file: checkbox, clickable name (opens
// the diff), and its porcelain code.
func (s *Shell) viewCommitFileRow(c *ui.Context, n *gitTreeNode, left float32) {
	t := c.Theme()
	checked := s.commitSelFiles[n.path]
	ui.Row(c.Key("gcf-"+n.path)).Padding(3, 6, 3, left).Gap(6).AlignItems(ui.Center).Children(func() {
		box := ui.CheckboxBase(c.Key("gcf-sel-"+n.path), &checked).Width(15).Height(15).
			Radius(3).Border(1, t.Border)
		if checked {
			box.Background(t.Accent)
			box.Label(cat(c).GitUnselectFile(n.path))
		} else {
			box.Label(cat(c).GitSelectFile(n.path))
		}
		box.Children(func() {
			if checked {
				ui.Icon(c, iconCheck).Width(11).Height(11).TextColor(t.AccentText)
			}
		})
		if box.Changed() {
			s.commitSelFiles[n.path] = !s.commitSelFiles[n.path]
		}
		nameRow := ui.Row(c).Grow(1).AlignItems(ui.Center)
		nameRow.Children(func() {
			col := t.Text
			if s.commitPrevFile == n.path {
				col = t.Text
			}
			ui.Text(c, n.name).FontSize(12).TextColor(col).MaxLines(1)
		})
		if nameRow.Clicked() {
			s.commitPrevFile = n.path
		}
		ui.Text(c, n.status).FontSize(11).Width(24).TextColor(gitStatusColor(t, n.status))
	})
}

// viewCommitFileDiff shows the selected file's HEAD↔worktree diff, a loading
// line until both sides land, and the binary/empty hints upstream gives.
func (s *Shell) viewCommitFileDiff(c *ui.Context, root string, files []map[string]any) {
	t := c.Theme()
	file := s.commitPrevFile
	if file == "" {
		ui.Text(c, cat(c).GitClickFileToDiff()).FontSize(12).TextColor(t.TextMuted)
		return
	}
	status := ""
	for _, f := range files {
		if str(f, "file") == file {
			status = str(f, "status")
			break
		}
	}
	ui.Row(c.Key("gc-diff-head")).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, file).FontSize(12).Bold().MaxLines(1).Grow(1)
		ui.Text(c, status).FontSize(11).TextColor(gitStatusColor(t, status))
	})
	ui.Row(c.Key("gc-diff-labels")).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, cat(c).GitHeadLabel()).FontSize(10).TextColor(t.TextMuted)
		ui.Icon(c, iconChevron).Width(10).Height(10).TextColor(t.TextMuted)
		ui.Text(c, cat(c).GitWorkingTreeLabel()).FontSize(10).TextColor(t.TextMuted)
	})

	headKey := "commithead:" + root + ":" + file
	workKey := "commitwork:" + root + ":" + file
	headRaw := s.ensure(headKey, "git_show_file", map[string]any{"path": root, "file": file, "refName": "HEAD"})
	workRaw := s.ensure(workKey, "read_file_preview", map[string]any{"rootPath": root, "path": file})
	headErr, workErr := s.errFor(headKey), s.errFor(workKey)
	if (headRaw == nil && headErr == "") || (workRaw == nil && workErr == "") {
		loadingState(c, "")
		return
	}
	if strings.Contains(strings.ToLower(headErr+" "+workErr), "binary") {
		ui.Text(c, cat(c).GitDiffBinary()).FontSize(12).TextColor(t.TextMuted)
		return
	}
	oldText := decodeString(headRaw)
	newText := ""
	if workRaw != nil {
		newText = str(asObj(workRaw), "content")
	}
	if oldText == newText && strings.TrimSpace(oldText) == "" {
		ui.Text(c, cat(c).ChangesNoDiff()).FontSize(12).TextColor(t.TextMuted)
		return
	}
	d := appDiffState(&s.commitDiffs, "commit:"+root+":"+file, oldText, newText)
	editor.DiffView(c, d).Grow(1).FillWidth()
}

// gitTreeNode is one directory or file of a git file tree.
type gitTreeNode struct {
	name      string
	path      string
	status    string
	additions int
	deletions int
	dir       bool
	children  []*gitTreeNode
}

// buildGitTree groups file entries (keyed by pathKey: "file" for git_status,
// "path" for git_log) into a sorted directory tree, directories first.
func buildGitTree(files []map[string]any, pathKey string) []*gitTreeNode {
	root := &gitTreeNode{dir: true}
	for _, f := range files {
		p := str(f, pathKey)
		if p == "" {
			continue
		}
		parts := strings.Split(p, "/")
		cur := root
		for i, part := range parts {
			if i == len(parts)-1 {
				add, del := 0, 0
				if v, ok := f["additions"].(float64); ok {
					add = int(v)
				}
				if v, ok := f["deletions"].(float64); ok {
					del = int(v)
				}
				cur.children = append(cur.children, &gitTreeNode{
					name: part, path: p, status: str(f, "status"),
					additions: add, deletions: del,
				})
				break
			}
			var dir *gitTreeNode
			for _, ch := range cur.children {
				if ch.dir && ch.name == part {
					dir = ch
					break
				}
			}
			if dir == nil {
				dir = &gitTreeNode{name: part, dir: true}
				cur.children = append(cur.children, dir)
			}
			cur = dir
		}
	}
	sortGitTree(root.children)
	return root.children
}

func sortGitTree(nodes []*gitTreeNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].dir != nodes[j].dir {
			return nodes[i].dir
		}
		a, b := strings.ToLower(nodes[i].name), strings.ToLower(nodes[j].name)
		if a != b {
			return a < b
		}
		return nodes[i].name < nodes[j].name
	})
	for _, n := range nodes {
		if n.dir {
			sortGitTree(n.children)
		}
	}
}

// gitStatusColor maps a porcelain status to the list's accent.
func gitStatusColor(t *ui.Theme, code string) ui.Color {
	switch {
	case code == "??" || strings.HasPrefix(code, "A"):
		return t.Success
	case strings.HasPrefix(code, "D"):
		return t.Danger
	case strings.Contains(code, "M") || strings.HasPrefix(code, "R"):
		return t.Warning
	}
	return t.TextMuted
}

// appDiffState keeps one native side-by-side diff state per file, rebuilding
// the buffers only when the texts change so scroll and expansion survive frames.
func appDiffState(store *map[string]*editor.DiffState, key, oldText, newText string) *editor.DiffState {
	if *store == nil {
		*store = map[string]*editor.DiffState{}
	}
	d := (*store)[key]
	if d == nil {
		d = &editor.DiffState{Layout: editor.DiffSplit}
		(*store)[key] = d
	}
	if d.Old == nil || d.Old.String() != oldText {
		d.Old = editor.NewBuffer(oldText)
	}
	if d.New == nil || d.New.String() != newText {
		d.New = editor.NewBuffer(newText)
	}
	return d
}

// commitSelected stages and commits just the checked files (git_commit), then
// optionally pushes the current branch — upstream's Commit / Commit and Push.
func (s *Shell) commitSelected(root string, files []map[string]any, andPush bool) {
	msg := strings.TrimSpace(s.commitMsg)
	names := make([]any, 0, len(files))
	for _, f := range files {
		if p := str(f, "file"); p != "" && s.commitSelFiles[p] {
			names = append(names, p)
		}
	}
	if msg == "" || len(names) == 0 {
		return
	}
	s.commitRunning = true
	svc := s.svc
	folderID := s.selectedFolder
	go func() {
		body := map[string]any{"path": root, "message": msg, "files": names}
		if folderID != "" {
			body["folderId"] = numOrString(folderID)
		}
		raw, err := s.B.raw("git_commit", body)
		var pushErr error
		if err == nil && andPush {
			pushBody := map[string]any{"path": root}
			if folderID != "" {
				pushBody["folderId"] = numOrString(folderID)
			}
			_, pushErr = s.B.raw("git_push", pushBody)
		}
		s.mu.Lock()
		s.commitRunning = false
		switch {
		case err != nil:
			s.actionMsg = locales.GitCommitFailed(err.Error())
		case pushErr != nil:
			s.actionMsg = locales.GitCommitPushFailed(pushErr.Error())
		case andPush:
			n := len(names)
			if res := asObj(raw); res != nil {
				if c, ok := res["committed_files"].(float64); ok && c > 0 {
					n = int(c)
				}
			}
			s.actionMsg = locales.GitCommittedAndPushed(n)
		default:
			n := len(names)
			if res := asObj(raw); res != nil {
				if c, ok := res["committed_files"].(float64); ok && c > 0 {
					n = int(c)
				}
			}
			s.actionMsg = locales.GitCommittedFiles(n)
		}
		if err == nil {
			s.commitSelSeeded = false
			s.commitPrevFile = ""
			if s.commitMsg == msg {
				s.commitMsg = ""
			}
		}
		s.mu.Unlock()
		if err == nil {
			s.invalidate(commitStatusKey(root))
			s.invalidate("gitstatus:" + root)
			s.invalidate("gitlog:" + root)
			s.invalidatePrefix("commithead:" + root + ":")
			s.invalidatePrefix("commitwork:" + root + ":")
			s.invalidatePrefix("pushinfo:" + root + ":")
			s.invalidatePrefix("pushlog:" + root + ":")
		}
		svc.Invalidate()
	}()
}

// ─── stash page ───────────────────────────────────────────────────────────

// viewGitStashPage renders the stash workspace: a create form (message +
// keep-index) and the stash list with Pop / Apply / Drop / Clear, mirroring
// upstream's stash + unstash dialogs.
func (s *Shell) viewGitStashPage(c *ui.Context, root string) {
	t := c.Theme()
	ui.Column(c.Key("gs-create")).Gap(8).MaxWidth(720).Children(func() {
		ui.Text(c, cat(c).GitStashCreate()).FontSize(13).Bold()
		ui.TextInput(c.Key("gs-msg"), &s.stashMsg).
			Placeholder(cat(c).GitStashMessagePlaceholder()).FillWidth()
		ui.Checkbox(c.Key("gs-keep"), &s.stashKeepIndex, cat(c).GitStashKeepIndex())
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			stashBtn := ui.PrimaryButton(c.Key("gs-push"), cat(c).GitStashAction())
			stashBtn.Label(cat(c).GitStashActionLabel())
			if stashBtn.Clicked() {
				s.stashPush(root, s.stashMsg, s.stashKeepIndex)
				s.stashMsg = ""
			}
			// Clear empties every stash — destructive, so it takes a second click.
			if s.stashClearConfirm {
				if ui.Button(c.Key("gs-clear-yes"), cat(c).GitStashClearConfirm()).Clicked() {
					s.stashClear(root)
					s.stashClearConfirm = false
				}
				if ui.Button(c.Key("gs-clear-no"), cat(c).ActionCancel()).Clicked() {
					s.stashClearConfirm = false
				}
			} else if ui.Button(c.Key("gs-clear"), cat(c).GitStashClear()).Clicked() {
				s.stashClearConfirm = true
			}
		})
	})
	ui.Divider(c)
	ui.Text(c, cat(c).GitStashList()).FontSize(13).Bold()
	key := "stashlist:" + root
	stashes := asList(s.ensure(key, "git_stash_list", map[string]any{"path": root}))
	if e := s.errFor(key); e != "" {
		s.errorState(c, key, nil)
		return
	}
	if stashes == nil {
		loadingState(c, "")
		return
	}
	if len(stashes) == 0 {
		emptyState(c, iconInfo, cat(c).GitNoStashes(), "")
		return
	}
	ui.Column(c).Gap(1).MaxWidth(720).Children(func() {
		for i, st := range stashes {
			ref := firstNonEmpty(str(st, "ref_name"), str(st, "ref"), itoa("stash@{", i)+"}")
			ui.Row(c.Key("gs-" + ref)).PaddingX(8).PaddingY(6).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, ref).FontSize(11).TextColor(t.TextMuted).Width(72)
				ui.Column(c).Grow(1).Gap(1).Children(func() {
					ui.Text(c, str(st, "message")).MaxLines(1).TextColor(t.Text)
					ui.Text(c, firstNonEmpty(str(st, "branch"), "—")+" · "+shortDate(str(st, "date"))).
						FontSize(10).TextColor(t.TextMuted).MaxLines(1)
				})
				if ui.Button(c.Key("gs-pop-"+ref), cat(c).GitStashPop()).Clicked() {
					s.stashRefOp(root, "pop", ref)
				}
				if ui.Button(c.Key("gs-apply-"+ref), cat(c).GitStashApply()).Clicked() {
					s.stashRefOp(root, "apply", ref)
				}
				if ui.Button(c.Key("gs-drop-"+ref), cat(c).GitStashDrop()).Clicked() {
					s.stashRefOp(root, "drop", ref)
				}
			})
		}
	})
	s.viewActionOrError(c)
}

// ─── push page ────────────────────────────────────────────────────────────

// pushInfoKey / pushLogKey scope the push page's fetches to the branch and
// remote they describe, so switching either never shows stale commits.
func pushInfoKey(root, branch string) string { return "pushinfo:" + root + ":" + branch }

func pushLogKey(root, branch, remote string) string {
	return "pushlog:" + root + ":" + branch + ":" + remote
}

// viewGitPushPage renders upstream's push workspace: the branch → remote target,
// the unpushed commits with their file trees, a native before↔after diff beside
// them, and Push.
func (s *Shell) viewGitPushPage(c *ui.Context, root string) {
	t := c.Theme()
	branch := decodeString(s.ensure("gitbranch:"+root, "get_git_branch", map[string]any{"path": root}))
	if branch == "" {
		loadingState(c, "")
		return
	}
	// Per-branch state (expanded commits, the previewed file) resets whenever the
	// page retargets another branch.
	if pageBranch := root + "\x00" + branch; s.pushPageBranch != pageBranch {
		s.pushPageBranch = pageBranch
		s.pushRemote = ""
		s.pushOpen = map[string]bool{}
		s.pushSelCommit, s.pushSelFile = "", ""
	}

	infoKey := pushInfoKey(root, branch)
	info := asObj(s.ensure(infoKey, "git_push_info", map[string]any{"path": root, "branch": branch}))
	if e := s.errFor(infoKey); e != "" {
		s.errorState(c, infoKey, nil)
		return
	}
	if info == nil {
		loadingState(c, "")
		return
	}
	remotes := asListVal(info["remotes"])
	names := make([]string, 0, len(remotes))
	seen := map[string]bool{}
	for _, r := range remotes {
		n := str(r, "name")
		if n != "" && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		emptyState(c, iconInfo, cat(c).GitNoRemotes(), cat(c).GitNoRemotesHelp())
		return
	}
	if s.pushRemote == "" || !containsStr(names, s.pushRemote) {
		s.pushRemote = str(info, "tracking_remote")
		if !containsStr(names, s.pushRemote) {
			s.pushRemote = names[0]
		}
	}

	// Target header: branch, an arrow, then the remote (a picker when there is
	// more than one) and the remote-side branch name.
	ui.Row(c.Key("gp-target")).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconBranch).Width(14).Height(14).TextColor(t.TextMuted)
		ui.Text(c, branch).FontSize(13).Bold().TextColor(t.Text).MaxLines(1)
		ui.Icon(c, iconArrowRight).Width(12).Height(12).TextColor(t.TextMuted)
		if len(names) > 1 {
			ui.Select(c.Key("gp-remote"), &s.pushRemote, names).Width(150)
			ui.Text(c, "/"+branch).FontSize(13).TextColor(t.TextMuted).MaxLines(1)
		} else {
			ui.Text(c, s.pushRemote+"/"+branch).FontSize(13).TextColor(t.TextMuted).MaxLines(1)
		}
	})
	ui.Divider(c)

	logKey := pushLogKey(root, branch, s.pushRemote)
	if s.pushPageKey != logKey {
		s.pushPageKey = logKey
		s.pushOpen = map[string]bool{}
		s.pushSelCommit, s.pushSelFile = "", ""
	}
	logObj := asObj(s.ensure(logKey, "git_log", map[string]any{
		"path": root, "branch": branch, "remote": s.pushRemote, "limit": 100,
	}))
	if e := s.errFor(logKey); e != "" {
		s.errorState(c, logKey, nil)
		return
	}
	hasUpstream := true
	var unpushed []map[string]any
	if logObj != nil {
		if v, ok := logObj["has_upstream"]; ok {
			hasUpstream = asBool(v)
		}
		for _, e := range asListVal(logObj["entries"]) {
			if b, ok := e["pushed"].(bool); ok && !b {
				unpushed = append(unpushed, e)
			}
		}
	}

	ui.Row(c.Key("gp-body")).Grow(1).FillWidth().Gap(14).Children(func() {
		ui.Column(c.Key("gp-left")).Width(420).FillHeight().Gap(6).Children(func() {
			ui.Scroll(c.Key("gp-list")).Grow(1).Children(func() {
				if logObj == nil {
					loadingState(c, "")
					return
				}
				if len(unpushed) == 0 {
					hint := cat(c).GitNoUnpushedCommits()
					if !hasUpstream {
						hint = cat(c).GitNewBranchNoCommits()
					}
					ui.Text(c, hint).FontSize(12).TextColor(t.TextMuted).Wrap()
					return
				}
				ui.Column(c).Gap(6).Children(func() {
					for _, e := range unpushed {
						s.viewPushCommitRow(c, e)
					}
				})
			})
			ui.Divider(c)
			n := len(unpushed)
			label := cat(c).GitPushAction()
			if n > 0 {
				label = cat(c).GitPushWithCount(n)
			}
			if s.pushRunning {
				label = cat(c).GitPushing()
			}
			pushBtn := ui.PrimaryButton(c.Key("gp-push"), label).FillWidth()
			pushBtn.Label(cat(c).GitPushActionLabel())
			if logObj == nil || s.pushRunning || (hasUpstream && n == 0) {
				pushBtn.Disabled(true)
			}
			if pushBtn.Clicked() {
				s.pushRunning = true
				s.gitPush(root, s.pushRemote, branch, n)
			}
			s.viewActionOrError(c)
		})
		ui.Column(c.Key("gp-diff")).Grow(1).FillHeight().Gap(6).Children(func() {
			s.viewPushFileDiff(c, root)
		})
	})
}

// viewPushCommitRow renders one unpushed commit: a clickable header (message,
// author, date, hash) and, when open, its changed-file tree.
func (s *Shell) viewPushCommitRow(c *ui.Context, e map[string]any) {
	t := c.Theme()
	hash := str(e, "full_hash")
	open := s.pushOpen[hash]
	ui.Column(c.Key("gpc-"+hash)).FillWidth().Radius(6).Border(1, t.Border).
		Padding(6, 8, 6, 8).Gap(4).Children(func() {
		head := ui.Column(c).Gap(3).FillWidth()
		head.Children(func() {
			ui.Text(c, firstLine(str(e, "message"))).FontSize(12).Bold().
				TextColor(t.Text).MaxLines(1)
			ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconCloudOff).Width(11).Height(11).TextColor(t.Warning)
				ui.Text(c, str(e, "author")).FontSize(10).TextColor(t.TextMuted).MaxLines(1)
				ui.Text(c, shortDate(str(e, "date"))).FontSize(10).TextColor(t.TextMuted)
				ui.Text(c, str(e, "hash")).FontSize(10).TextColor(t.TextMuted)
			})
		})
		if head.Clicked() {
			s.pushOpen[hash] = !open
		}
		if !open {
			return
		}
		files := asListVal(e["files"])
		if len(files) == 0 {
			ui.Text(c, cat(c).GitNoFileChangeDetails()).FontSize(10).TextColor(t.TextMuted)
			return
		}
		s.viewPushNodes(c, buildGitTree(files, "path"), 0, hash)
	})
}

// viewPushNodes renders one level of a commit's file tree; clicking a file opens
// its before↔after diff on the right.
func (s *Shell) viewPushNodes(c *ui.Context, nodes []*gitTreeNode, depth int, hash string) {
	t := c.Theme()
	for _, n := range nodes {
		left := float32(4 + depth*12)
		if n.dir {
			ui.Row(c.Key("gpd-"+hash+"-"+n.path)).Padding(2, 4, 2, left).Gap(5).
				AlignItems(ui.Center).Children(func() {
				if iconFolder != nil {
					ui.Icon(c, iconFolder).Width(11).Height(11).TextColor(t.TextMuted)
				}
				ui.Text(c, n.name+"/").FontSize(10).TextColor(t.TextMuted).MaxLines(1)
			})
			s.viewPushNodes(c, n.children, depth+1, hash)
			continue
		}
		selected := s.pushSelCommit == hash && s.pushSelFile == n.path
		row := ui.Row(c.Key("gpf-"+hash+"-"+n.path)).Padding(2, 4, 2, left).Gap(5).
			AlignItems(ui.Center)
		if selected {
			row.Background(t.SurfacePressed).Radius(4)
		}
		row.Children(func() {
			ui.Text(c, n.status).FontSize(10).Width(18).TextColor(gitStatusColor(t, n.status))
			ui.Text(c, n.name).FontSize(11).TextColor(t.Text).MaxLines(1).Grow(1)
			if n.additions > 0 {
				ui.Text(c, "+"+fmt.Sprintf("%d", n.additions)).FontSize(9).TextColor(t.Success)
			}
			if n.deletions > 0 {
				ui.Text(c, "-"+fmt.Sprintf("%d", n.deletions)).FontSize(9).TextColor(t.Danger)
			}
		})
		if row.Clicked() {
			s.pushSelCommit, s.pushSelFile = hash, n.path
		}
	}
}

// viewPushFileDiff shows the selected commit file as `<hash>~1` ↔ `<hash>`.
func (s *Shell) viewPushFileDiff(c *ui.Context, root string) {
	t := c.Theme()
	hash, file := s.pushSelCommit, s.pushSelFile
	if hash == "" || file == "" {
		ui.Text(c, cat(c).GitSelectFileToViewDiff()).FontSize(12).TextColor(t.TextMuted)
		return
	}
	ui.Row(c.Key("gpd-head")).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, file).FontSize(12).Bold().MaxLines(1).Grow(1)
		short := hash
		if len(short) > 7 {
			short = short[:7]
		}
		ui.Text(c, short+"~ ("+cat(c).GitBefore()+")").FontSize(10).TextColor(t.TextMuted)
		ui.Text(c, short+" ("+cat(c).GitAfter()+")").FontSize(10).TextColor(t.TextMuted)
	})
	oldKey := "pushold:" + root + ":" + hash + ":" + file
	newKey := "pushnew:" + root + ":" + hash + ":" + file
	oldRaw := s.ensure(oldKey, "git_show_file", map[string]any{
		"path": root, "file": file, "refName": hash + "~1",
	})
	newRaw := s.ensure(newKey, "git_show_file", map[string]any{
		"path": root, "file": file, "refName": hash,
	})
	oldErr, newErr := s.errFor(oldKey), s.errFor(newKey)
	if (oldRaw == nil && oldErr == "") || (newRaw == nil && newErr == "") {
		loadingState(c, "")
		return
	}
	if strings.Contains(strings.ToLower(oldErr+" "+newErr), "binary") {
		ui.Text(c, cat(c).GitDiffBinary()).FontSize(12).TextColor(t.TextMuted)
		return
	}
	d := appDiffState(&s.pushDiffs, "push:"+root+":"+hash+":"+file,
		decodeString(oldRaw), decodeString(newRaw))
	editor.DiffView(c, d).Grow(1).FillWidth()
}

// ─── merge page ───────────────────────────────────────────────────────────

// viewGitMergePage renders the merge workspace: pick a branch and merge it. A
// conflicted outcome switches to the conflict state, where the unmerged paths
// are listed with Abort / Continue.
func (s *Shell) viewGitMergePage(c *ui.Context, root string) {
	// A merge already in progress (MERGE_HEAD) takes over the page: resolving or
	// aborting it is the only meaningful next step — upstream's gate.
	raw := s.ensure("mergehead:"+root, "git_has_merge_head", map[string]any{"path": root})
	if raw == nil {
		loadingState(c, "")
		return
	}
	if asBool(decodedVal(raw)) {
		s.viewMergeConflictState(c, root)
		return
	}
	branch := decodeString(s.ensure("gitbranch:"+root, "get_git_branch", map[string]any{"path": root}))
	all := asObj(s.ensure("allbranches:"+root, "git_list_all_branches", map[string]any{"path": root}))
	if all == nil {
		loadingState(c, "")
		return
	}
	local := asStringList(all["local"])
	opts := make([]string, 0, len(local))
	for _, b := range local {
		if b != branch { // merging a branch into itself is a no-op
			opts = append(opts, b)
		}
	}
	ui.Row(c.Key("gm-facts")).Gap(16).Children(func() {
		gitFact(c, cat(c).GitCurrentLabel(), firstNonEmpty(branch, "—"))
	})
	ui.Divider(c)
	if len(opts) == 0 {
		emptyState(c, iconInfo, cat(c).GitNoMergeTargets(), "")
		return
	}
	if s.mergeBranch == "" || !containsStr(opts, s.mergeBranch) {
		s.mergeBranch = opts[0]
	}
	ui.Column(c.Key("gm-form")).Gap(10).MaxWidth(720).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, cat(c).GitMergeIntoLabel()).FontSize(12).Width(110)
			ui.Select(c.Key("gm-branch"), &s.mergeBranch, opts).Grow(1)
		})
		mergeBtn := ui.PrimaryButton(c.Key("gm-merge"), cat(c).GitMergeAction())
		mergeBtn.Label(cat(c).GitMergeActionLabel())
		if mergeBtn.Clicked() {
			s.gitMerge(root, s.mergeBranch)
		}
		s.viewActionOrError(c)
	})
}

// viewMergeConflictState renders the in-progress-merge state: the unmerged
// paths, then Abort / Continue.
func (s *Shell) viewMergeConflictState(c *ui.Context, root string) {
	t := c.Theme()
	conflicts := asStringList(decodedVal(s.ensure("conflicts:"+root, "git_list_conflicts", map[string]any{"path": root})))
	ui.Row(c.Key("gm-conflict-banner")).FillWidth().MaxWidth(720).Radius(8).
		Border(1, t.Warning.Alpha(0.35)).Background(t.Warning.Alpha(0.07)).
		Padding(10, 12, 10, 12).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconAlert).Width(14).Height(14).TextColor(t.Warning)
		ui.Text(c, cat(c).GitMergeInProgress()).FontSize(13).TextColor(t.Warning)
	})
	ui.Text(c, cat(c).GitConflictedFiles(len(conflicts))).FontSize(12).Bold()
	if len(conflicts) == 0 {
		ui.Text(c, cat(c).GitResolveHint()).FontSize(12).TextColor(t.TextMuted).Wrap()
	} else {
		ui.Column(c).Gap(1).MaxWidth(720).Children(func() {
			for _, f := range conflicts {
				ui.Row(c.Key("gmc-" + f)).PaddingX(8).PaddingY(4).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconAlert).Width(12).Height(12).TextColor(t.Danger)
					ui.Text(c, f).MaxLines(1).TextColor(t.Text)
				})
			}
		})
	}
	ui.Text(c, cat(c).GitResolveHint()).FontSize(11).TextColor(t.TextMuted).Wrap()
	ui.Row(c.Key("gm-actions")).Gap(8).Children(func() {
		if ui.Button(c.Key("gm-abort"), cat(c).GitAbort()).Clicked() {
			s.gitAbort(root)
		}
		if ui.PrimaryButton(c.Key("gm-continue"), cat(c).GitContinue()).Clicked() {
			s.gitContinue(root)
		}
	})
	s.viewActionOrError(c)
}

// ─── shared bits ──────────────────────────────────────────────────────────

// viewGitFileRow renders one working-tree change: its porcelain code and path.
func (s *Shell) viewGitFileRow(c *ui.Context, f map[string]any, keyPrefix string) {
	t := c.Theme()
	file := str(f, "file")
	code := str(f, "status")
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
	ui.Row(c.Key(keyPrefix + file)).PaddingX(8).PaddingY(4).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, code).FontSize(11).Width(24).TextColor(col)
		ui.Text(c, file).MaxLines(1).TextColor(t.Text)
	})
}

// gitFact renders one labelled read-only fact in a facts row.
func gitFact(c *ui.Context, label, value string) {
	t := c.Theme()
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(t.TextMuted)
		ui.Text(c, value).FontSize(12).TextColor(t.Text).MaxLines(1)
	})
}

// decodedVal decodes an already-fetched raw response into a generic value.
func decodedVal(raw json.RawMessage) any {
	if raw == nil {
		return nil
	}
	var v any
	_ = json.Unmarshal(raw, &v)
	return v
}

// asStringList converts a decoded JSON array of strings.
func asStringList(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, el := range arr {
		if s, ok := el.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// containsStr reports whether a string slice holds s.
func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// folderNameByID is the display name of an open folder.
func (s *Shell) folderNameByID(id string) string {
	for _, f := range asList(s.peek("folders")) {
		if fmt.Sprintf("%v", f["id"]) == id {
			return firstNonEmpty(str(f, "name"), str(f, "path"))
		}
	}
	return ""
}

// ─── branch picker ────────────────────────────────────────────────────────

// gitBranches is the folder's branch inventory (git_list_all_branches):
// local, remote, worktree lists plus the main worktree's branch.
type gitBranches struct {
	local              []string
	remote             []string
	worktreeBranches   []string
	mainWorktreeBranch string
}

// branchesOf decodes a git_list_all_branches response.
func branchesOf(raw json.RawMessage) *gitBranches {
	m := asObj(raw)
	if m == nil {
		return nil
	}
	b := &gitBranches{
		local:            asStringList(m["local"]),
		remote:           asStringList(m["remote"]),
		worktreeBranches: asStringList(m["worktree_branches"]),
	}
	if v, ok := m["main_worktree_branch"].(string); ok {
		b.mainWorktreeBranch = v
	}
	return b
}

// viewBranchChip is the composer's branch picker — upstream's BranchDropdown
// entry point: a one-line "① branch" chip beneath the input that opens the
// Branches page. It self-hides outside a git folder (a folderless chat has no
// branch), mirroring upstream's isChatMode self-hide.
func (s *Shell) viewBranchChip(c *ui.Context, root string) {
	if root == "" {
		return
	}
	t := c.Theme()
	branch := decodeString(s.peek("gitbranch:" + root))
	if branch == "" {
		// Not a repo (or not fetched yet): the chip stays hidden rather than
		// showing a promise it cannot keep.
		s.ensure("gitbranch:"+root, "get_git_branch", map[string]any{"path": root})
		return
	}
	ui.Row(c.Key("composer-branch")).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
		chip := ui.Row(c.Key("branch-chip")).Padding(2, 8, 2, 8).Radius(6).
			Gap(5).AlignItems(ui.Center).Background(t.SurfaceHover).
			Label(cat(c).GitBranchChip(branch))
		chip.Children(func() {
			ui.Icon(c, iconBranch).Width(12).Height(12).TextColor(t.TextMuted)
			ui.Text(c, branch).FontSize(11).TextColor(t.Text)
		})
		if chip.Clicked() {
			s.workbench = gitRouteBranches
		}
	})
}

// viewGitBranchesPage renders the branch workspace: the current branch, the
// create-branch form (git_new_branch), the worktree workspace (git_worktree_add
// / git_remove_worktree, with switch navigating to the worktree folder), and
// the prefix-grouped local branches with switch / merge / rebase / delete
// (git_checkout / git_merge / git_rebase / git_delete_branch). Remote branches
// are checked out in the root working tree as tracking branches — upstream's
// isRemote rule, so a remote ref is never mistaken for a same-named local branch
// that happens to be held by a worktree.
func (s *Shell) viewGitBranchesPage(c *ui.Context, root string) {
	t := c.Theme()
	current := decodeString(s.ensure("gitbranch:"+root, "get_git_branch", map[string]any{"path": root}))
	raw := s.ensure("allbranches:"+root, "git_list_all_branches", map[string]any{"path": root})
	if raw == nil {
		loadingState(c, "")
		return
	}
	br := branchesOf(raw)
	if br == nil {
		loadingState(c, "")
		return
	}
	ui.Row(c.Key("gb-facts")).Gap(16).Children(func() {
		gitFact(c, cat(c).GitCurrentLabel(), firstNonEmpty(current, "—"))
		gitFact(c, cat(c).GitWorktreesLabel(), itoa("", len(br.worktreeBranches)))
		gitFact(c, cat(c).GitRemoteBranchesLabel(), itoa("", len(br.remote)))
	})
	ui.Divider(c)

	// Create a branch (git_new_branch): a name box + Create, then it becomes
	// the checked-out branch.
	ui.Column(c.Key("gb-create")).Gap(6).MaxWidth(720).Children(func() {
		ui.Text(c, cat(c).GitNewBranch()).FontSize(12).Bold()
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("gb-new-name"), &s.branchNewName).
				Placeholder(cat(c).GitNewBranchPlaceholder()).Grow(1)
			createBtn := ui.PrimaryButton(c.Key("gb-create-btn"), cat(c).GitCreateBranchAction())
			createBtn.Label(cat(c).GitCreateBranchLabel())
			if createBtn.Disabled(strings.TrimSpace(s.branchNewName) == "").Clicked() {
				s.gitNewBranch(root, s.branchNewName)
				s.branchNewName = ""
			}
		})
	})
	ui.Divider(c)

	s.viewWorktreeCreateForm(c, root, current)
	ui.Divider(c)
	s.viewWorktreeSection(c, root, br)
	ui.Divider(c)

	ui.Text(c, cat(c).GitLocalBranches(len(br.local))).FontSize(12).Bold()
	if len(br.local) == 0 {
		emptyState(c, iconInfo, cat(c).GitNoBranches(), "")
	} else {
		held := make(map[string]bool, len(br.worktreeBranches))
		for _, b := range br.worktreeBranches {
			held[b] = true
		}
		ui.Column(c).Gap(1).MaxWidth(720).Children(func() {
			s.viewBranchNodes(c, root, buildBranchTree(br.local, "local"), 0, current, held)
		})
	}
	if len(br.remote) > 0 {
		ui.Divider(c)
		ui.Text(c, cat(c).GitRemoteBranches(len(br.remote))).FontSize(12).Bold()
		ui.Column(c).Gap(1).MaxWidth(720).Children(func() {
			for _, rb := range br.remote {
				ui.Row(c.Key("gbr-" + rb)).PaddingX(8).PaddingY(5).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconBranch).Width(12).Height(12).TextColor(t.TextMuted)
					ui.Text(c, rb).Grow(1).MaxLines(1).TextColor(t.Text)
					if ui.Button(c.Key("gbr-co-"+rb), cat(c).GitCheckoutRemoteAction()).Clicked() {
						s.gitCheckoutRemote(root, rb)
					}
				})
			}
		})
	}
	s.viewActionOrError(c)
}

// viewWorktreeCreateForm renders the new-worktree form (upstream's worktree
// dialog): a branch name and a target directory, prefilled with a sibling path
// so the create is one click away. On success the new checkout is registered as
// a workspace folder and opened, mirroring upstream's openWorktreeFolder +
// new-conversation-tab flow.
func (s *Shell) viewWorktreeCreateForm(c *ui.Context, root, current string) {
	if s.worktreeSeedRoot != root {
		s.worktreeSeedRoot = root
		s.worktreeBranchName = ""
		s.worktreePath = suggestWorktreePath(root, current)
	}
	ui.Column(c.Key("gb-wt-create")).Gap(6).MaxWidth(720).Children(func() {
		ui.Text(c, cat(c).GitNewWorktree()).FontSize(12).Bold()
		ui.TextInput(c.Key("gb-wt-branch"), &s.worktreeBranchName).
			Placeholder(cat(c).GitWorktreeBranchPlaceholder()).FillWidth()
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("gb-wt-path"), &s.worktreePath).
				Placeholder(cat(c).GitWorktreePathPlaceholder()).Grow(1)
			createBtn := ui.PrimaryButton(c.Key("gb-wt-create-btn"), cat(c).GitWorktreeCreateAction())
			createBtn.Label(cat(c).GitWorktreeCreateLabel())
			empty := strings.TrimSpace(s.worktreeBranchName) == "" || strings.TrimSpace(s.worktreePath) == ""
			if createBtn.Disabled(empty).Clicked() {
				branch, path := s.worktreeBranchName, s.worktreePath
				s.worktreeBranchName, s.worktreePath = "", ""
				s.gitWorktreeAdd(root, branch, path)
			}
		})
	})
}

// viewWorktreeSection lists the branches checked out in the repo's other
// worktrees: switch opens that checkout as the workspace folder, and removal
// either keeps the branch (the checkout goes) or takes both, with a second
// click then a force escalation when git refuses to discard local work.
func (s *Shell) viewWorktreeSection(c *ui.Context, root string, br *gitBranches) {
	t := c.Theme()
	ui.Text(c, cat(c).GitOtherWorktrees(len(br.worktreeBranches))).FontSize(12).Bold()
	if len(br.worktreeBranches) == 0 {
		emptyState(c, iconFolder, cat(c).GitNoOtherWorktrees(), "")
		return
	}
	ui.Column(c).Gap(1).MaxWidth(720).Children(func() {
		for _, b := range br.worktreeBranches {
			isMain := b == br.mainWorktreeBranch
			ui.Row(c.Key("gb-wt-" + b)).PaddingX(8).PaddingY(5).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconFolder).Width(12).Height(12).TextColor(t.TextMuted)
				ui.Text(c, b).Grow(1).MaxLines(1).TextColor(t.Text)
				if isMain {
					ui.Text(c, cat(c).GitMainWorktreeBadge()).FontSize(10).TextColor(t.TextMuted)
				}
				if ui.Button(c.Key("gb-wt-open-"+b), cat(c).GitSwitchAction()).Clicked() {
					s.gitSwitchBranch(root, b)
				}
				// The main working tree cannot be removed; a linked one can.
				if isMain {
					return
				}
				if s.worktreeForce == b {
					label := cat(c).GitWorktreeForceAction()
					if s.worktreeForceBranch {
						label = cat(c).GitWorktreeForceBranchAction()
					}
					if ui.Button(c.Key("gb-wt-force-"+b), label).Clicked() {
						withBranch := s.worktreeForceBranch
						s.worktreeForce, s.worktreeForceBranch = "", false
						s.gitRemoveWorktree(root, b, withBranch, true)
					}
					if ui.Button(c.Key("gb-wt-force-no-"+b), cat(c).ActionCancel()).Clicked() {
						s.worktreeForce, s.worktreeForceBranch = "", false
					}
					return
				}
				if s.worktreeRemove == b {
					label := cat(c).GitWorktreeRemoveAction()
					if s.worktreeRemoveBranch {
						label = cat(c).GitWorktreeRemoveBranchAction()
					}
					if ui.Button(c.Key("gb-wt-rm-yes-"+b), label).Clicked() {
						withBranch := s.worktreeRemoveBranch
						s.worktreeRemove, s.worktreeRemoveBranch = "", false
						s.gitRemoveWorktree(root, b, withBranch, false)
					}
					if ui.Button(c.Key("gb-wt-rm-no-"+b), cat(c).ActionCancel()).Clicked() {
						s.worktreeRemove, s.worktreeRemoveBranch = "", false
					}
					return
				}
				if ui.Button(c.Key("gb-wt-rm-"+b), cat(c).GitWorktreeRemoveAction()).Clicked() {
					s.worktreeRemove, s.worktreeRemoveBranch = b, false
				}
				if ui.Button(c.Key("gb-wt-rmb-"+b), cat(c).GitWorktreeRemoveBranchAction()).Clicked() {
					s.worktreeRemove, s.worktreeRemoveBranch = b, true
				}
			})
		}
	})
}

// viewBranchNodes renders a prefix-grouped branch tree. Groups collapse on
// click; leaves keep their full ref for git operations and show the label
// relative to the nearest surviving group, so "feature/auth/login" reads as
// "feature/auth/" → "login" once the chain has something to divide.
func (s *Shell) viewBranchNodes(c *ui.Context, root string, nodes []*branchTreeNode, depth int, current string, held map[string]bool) {
	t := c.Theme()
	for _, n := range nodes {
		if n.full != "" {
			s.viewLocalBranchRow(c, root, n.full, n.label, depth, n.full == current, held[n.full])
			continue
		}
		collapsed := s.branchCollapsed[n.key]
		row := ui.Row(c.Key("gbgrp-"+n.key)).
			Padding(0, 8, 0, float32(8+depth*14)).PaddingY(4).Gap(6).AlignItems(ui.Center).
			Label(n.label)
		row.Children(func() {
			icon := iconChevronD
			if collapsed {
				icon = iconChevron
			}
			ui.Icon(c, icon).Width(11).Height(11).TextColor(t.TextMuted)
			ui.Text(c, n.label).FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, "("+itoa("", n.count)+")").FontSize(10).TextColor(t.TextMuted)
		})
		if row.Clicked() {
			if s.branchCollapsed == nil {
				s.branchCollapsed = map[string]bool{}
			}
			s.branchCollapsed[n.key] = !collapsed
		}
		if !collapsed {
			s.viewBranchNodes(c, root, n.children, depth+1, current, held)
		}
	}
}

// viewLocalBranchRow renders one local branch with its switch / merge / rebase
// actions and the two-step delete confirmation. A branch checked out in another
// worktree gets no plain delete — git refuses it, and the worktree section owns
// the removal verbs instead.
func (s *Shell) viewLocalBranchRow(c *ui.Context, root, branch, label string, depth int, isCurrent, heldByWorktree bool) {
	t := c.Theme()
	ui.Row(c.Key("gb-"+branch)).Padding(0, 8, 0, float32(8+depth*14)).PaddingY(5).
		Gap(8).AlignItems(ui.Center).Children(func() {
		if isCurrent {
			ui.Icon(c, iconCheck).Width(12).Height(12).TextColor(t.Success)
		} else {
			ui.Icon(c, iconBranch).Width(12).Height(12).TextColor(t.TextMuted)
		}
		ui.Text(c, label).Grow(1).MaxLines(1).
			TextColor(condColor(isCurrent, t.Text, t.TextMuted))
		if isCurrent {
			ui.Text(c, cat(c).GitCurrentBadge()).FontSize(10).TextColor(t.Success)
			return
		}
		if ui.Button(c.Key("gb-co-"+branch), cat(c).GitSwitchAction()).Clicked() {
			s.gitSwitchBranch(root, branch)
		}
		if ui.Button(c.Key("gb-mg-"+branch), cat(c).GitMergeAction()).Clicked() {
			s.gitMerge(root, branch)
		}
		if ui.Button(c.Key("gb-rb-"+branch), cat(c).GitRebaseAction()).Clicked() {
			s.gitRebase(root, branch)
		}
		if heldByWorktree {
			return
		}
		// Delete asks for a second click: -d refuses unmerged work, and the
		// force variant would throw it away.
		if s.branchDeleteConfirm == branch {
			if ui.Button(c.Key("gb-del-yes-"+branch), cat(c).GitForceDelete()).Clicked() {
				s.gitDeleteBranch(root, branch, true)
				s.branchDeleteConfirm = ""
			}
			if ui.Button(c.Key("gb-del-no-"+branch), cat(c).ActionCancel()).Clicked() {
				s.branchDeleteConfirm = ""
			}
		} else if ui.Button(c.Key("gb-del-"+branch), cat(c).ActionDelete()).Clicked() {
			s.branchDeleteConfirm = branch
		}
	})
}

// ─── branch tree ──────────────────────────────────────────────────────────

// branchTreeNode is a prefix-compressed branch tree node: a leaf carries the
// full ref, a group carries its children and leaf count. Mirrors upstream's
// lib/branch-tree.ts (collapse single-child chains, groups sort first).
type branchTreeNode struct {
	label    string
	full     string
	key      string
	count    int
	children []*branchTreeNode
}

type branchTrie struct {
	children map[string]*branchTrie
	full     string
}

// buildBranchTree groups slash-delimited branch names into a compressed tree.
func buildBranchTree(branches []string, scope string) []*branchTreeNode {
	root := &branchTrie{children: map[string]*branchTrie{}}
	for _, b := range branches {
		node := root
		segs := strings.Split(b, "/")
		for i, seg := range segs {
			if seg == "" {
				continue
			}
			child := node.children[seg]
			if child == nil {
				child = &branchTrie{children: map[string]*branchTrie{}}
				node.children[seg] = child
			}
			if i == len(segs)-1 {
				child.full = b
			}
			node = child
		}
	}
	return branchTrieNodes(root, "", scope)
}

// branchTrieNodes converts trie children into sorted nodes, collapsing pure
// single-child chains into one group label.
func branchTrieNodes(t *branchTrie, prefix, scope string) []*branchTreeNode {
	names := make([]string, 0, len(t.children))
	for name := range t.children {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*branchTreeNode, 0, len(names))
	for _, name := range names {
		node := t.children[name]
		label := name
		for node.full == "" && len(node.children) == 1 {
			only := ""
			for childName := range node.children {
				only = childName
			}
			label += "/" + only
			node = node.children[only]
		}
		fullPath := label
		if prefix != "" {
			fullPath = prefix + "/" + label
		}
		if node.full != "" && len(node.children) == 0 {
			out = append(out, &branchTreeNode{
				label: label, full: node.full, key: scope + " " + fullPath,
			})
			continue
		}
		children := branchTrieNodes(node, fullPath, scope)
		out = append(out, &branchTreeNode{
			label: label + "/", key: scope + " " + fullPath + "/",
			count: countBranchLeaves(children), children: children,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		gi, gj := out[i].full == "", out[j].full == ""
		if gi != gj {
			return gi
		}
		return strings.ToLower(out[i].label) < strings.ToLower(out[j].label)
	})
	return out
}

// countBranchLeaves counts a node list's leaf descendants.
func countBranchLeaves(nodes []*branchTreeNode) int {
	n := 0
	for _, node := range nodes {
		if node.full != "" {
			n++
			continue
		}
		n += countBranchLeaves(node.children)
	}
	return n
}

// suggestWorktreePath is upstream's prefill: a sibling directory named after
// the repo and its checked-out branch, made unique if that path already exists.
func suggestWorktreePath(root, branch string) string {
	base := filepath.Base(root)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "project"
	}
	suffix := strings.ReplaceAll(firstNonEmpty(branch, "main"), "/", "-")
	candidate := filepath.Join(filepath.Dir(root), base+"-"+suffix)
	for i := 2; ; i++ {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = filepath.Join(filepath.Dir(root),
			fmt.Sprintf("%s-%s-%d", base, suffix, i))
	}
}

// ─── write operations ─────────────────────────────────────────────────────

// gitWrite runs a git mutation off the UI thread, refreshing the git caches on
// success and reporting the outcome through the shared action line. extraKeys
// are additional cache keys to drop (e.g. the branch list after a merge).
func (s *Shell) gitWrite(keys []string, success string, cmd string, body map[string]any) {
	svc := s.svc
	go func() {
		_, err := s.B.raw(cmd, body)
		s.mu.Lock()
		if err != nil {
			s.actionMsg = cmd + ": " + err.Error()
		} else {
			s.actionMsg = success
		}
		for _, k := range keys {
			delete(s.cache, k)
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// stashPush creates a stash (git_stash_push).
func (s *Shell) stashPush(root, message string, keepIndex bool) {
	s.gitWrite([]string{"stashlist:" + root, "gitstatus:" + root},
		catMsg("stashed"), "git_stash_push", map[string]any{
			"path": root, "message": message, "keepIndex": keepIndex,
		})
}

// stashRefOp applies/pops/drops one stash (git_stash_pop | _apply | _drop).
func (s *Shell) stashRefOp(root, op, ref string) {
	s.gitWrite([]string{"stashlist:" + root, "gitstatus:" + root},
		catMsg("stash "+op), "git_stash_"+op, map[string]any{
			"path": root, "stashRef": ref,
		})
}

// stashClear drops every stash (git_stash_clear).
func (s *Shell) stashClear(root string) {
	s.gitWrite([]string{"stashlist:" + root}, catMsg("stashes cleared"), "git_stash_clear", map[string]any{"path": root})
}

// gitPush pushes the branch to the remote (git_push); the first push also sets
// upstream, which the backend reports back through upstream_set. count is the
// number of commits the page listed as unpushed, for the result message.
func (s *Shell) gitPush(root, remote, branch string, count int) {
	svc := s.svc
	folderID := s.selectedFolder
	go func() {
		body := map[string]any{"path": root, "remote": remote, "branch": branch}
		if folderID != "" {
			body["folderId"] = numOrString(folderID)
		}
		raw, err := s.B.raw("git_push", body)
		n, upstreamSet := count, false
		if res := asObj(raw); res != nil {
			// The backend counts commits against the existing upstream; a first
			// push has none yet and reports 0, so keep the page's own count.
			if v, ok := res["pushed_commits"].(float64); ok && v > 0 {
				n = int(v)
			}
			if v, ok := res["upstream_set"].(bool); ok {
				upstreamSet = v
			}
		}
		s.mu.Lock()
		s.pushRunning = false
		switch {
		case err != nil:
			s.actionMsg = locales.GitPushFailed(err.Error())
		case upstreamSet:
			s.actionMsg = locales.GitPushedUpstream(n)
		default:
			s.actionMsg = locales.GitPushedCommits(n)
		}
		if err == nil {
			s.pushSelCommit, s.pushSelFile = "", ""
		}
		s.mu.Unlock()
		if err == nil {
			s.invalidatePrefix("pushinfo:" + root + ":")
			s.invalidatePrefix("pushlog:" + root + ":")
			s.invalidatePrefix("pushold:" + root + ":")
			s.invalidatePrefix("pushnew:" + root + ":")
			s.invalidate("gitlog:" + root)
			s.invalidate("allbranches:" + root)
		}
		svc.Invalidate()
	}()
}

// gitMerge merges branch into the checked-out one (git_merge). A conflicted
// outcome is a 200 with a conflict block, and the page's next frame shows the
// conflict state because git_has_merge_head turns true.
func (s *Shell) gitMerge(root, branch string) {
	s.gitWrite([]string{"mergehead:" + root, "conflicts:" + root, "gitstatus:" + root, "gitlog:" + root},
		catMsg("merged"), "git_merge", map[string]any{"path": root, "branchName": branch})
}

// gitAbort abandons the in-progress merge (git_abort_operation).
func (s *Shell) gitAbort(root string) {
	s.gitWrite([]string{"mergehead:" + root, "conflicts:" + root, "gitstatus:" + root},
		catMsg("merge aborted"), "git_abort_operation", map[string]any{"path": root, "operation": "merge"})
}

// gitContinue commits the resolved merge (git_continue_operation).
func (s *Shell) gitContinue(root string) {
	s.gitWrite([]string{"mergehead:" + root, "conflicts:" + root, "gitstatus:" + root, "gitlog:" + root},
		catMsg("merge completed"), "git_continue_operation", map[string]any{"path": root, "operation": "merge"})
}

// gitNewBranch creates a branch and checks it out (git_new_branch).
func (s *Shell) gitNewBranch(root, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	s.gitWrite([]string{"gitbranch:" + root, "allbranches:" + root, "gitstatus:" + root},
		catMsg("branch created"), "git_new_branch", map[string]any{"path": root, "branchName": name})
}

// gitDeleteBranch deletes a local branch (git_delete_branch); force uses -D,
// which discards unmerged commits.
func (s *Shell) gitDeleteBranch(root, branch string, force bool) {
	s.gitWrite([]string{"allbranches:" + root, "gitbranch:" + root},
		catMsg("branch deleted"), "git_delete_branch", map[string]any{
			"path": root, "branchName": branch, "force": force,
		})
}

// gitRebase replays the checked-out branch on top of branch (git_rebase). A
// conflicted rebase leaves the operation in progress; the branch list and status
// refresh either way.
func (s *Shell) gitRebase(root, branch string) {
	s.gitWrite([]string{"gitbranch:" + root, "allbranches:" + root,
		"gitstatus:" + root, "gitlog:" + root, "conflicts:" + root},
		catMsg("rebased"), "git_rebase", map[string]any{"path": root, "branchName": branch})
}

// gitCheckoutRemote checks a remote branch out by its short name (git_checkout),
// which creates the tracking local branch in the root working tree — git's DWIM,
// exactly upstream's isRemote path.
func (s *Shell) gitCheckoutRemote(root, remoteBranch string) {
	local := remoteBranch
	if i := strings.IndexByte(local, '/'); i >= 0 {
		local = local[i+1:]
	}
	if local == "" {
		return
	}
	s.gitWrite([]string{"gitbranch:" + root, "allbranches:" + root,
		"gitstatus:" + root, "gitlog:" + root},
		catMsg("switched"), "git_checkout", map[string]any{"path": root, "branchName": local})
}

// gitWorktreeAdd creates a worktree on a new branch (git_worktree_add), then
// registers the checkout as a workspace folder and selects it — upstream's
// openWorktreeFolder + fresh conversation so the new tree is reachable.
func (s *Shell) gitWorktreeAdd(root, branch, path string) {
	branch, path = strings.TrimSpace(branch), strings.TrimSpace(path)
	if branch == "" || path == "" {
		return
	}
	svc := s.svc
	source := s.selectedFolder
	go func() {
		_, err := s.B.raw("git_worktree_add", map[string]any{
			"path": root, "branchName": branch, "worktreePath": path,
		})
		newFolder := ""
		if err == nil {
			raw, oerr := s.B.raw("open_worktree_folder", map[string]any{
				"path": path, "sourceFolderId": numOrString(source),
			})
			if oerr != nil {
				err = oerr
			} else {
				var f map[string]any
				_ = json.Unmarshal(raw, &f)
				if id, ok := f["id"]; ok {
					newFolder = fmt.Sprintf("%v", id)
				}
			}
		}
		s.mu.Lock()
		if err != nil {
			s.actionMsg = "worktree add: " + err.Error()
		} else {
			s.actionMsg = catMsg("worktree created")
			s.worktreeSeedRoot = ""
			if newFolder != "" {
				s.selectedFolder = newFolder
				s.selectedConv = ""
				s.workbench = ""
			}
		}
		delete(s.cache, "folders")
		delete(s.cache, "allbranches:"+root)
		delete(s.cache, "gitbranch:"+root)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// gitSwitchBranch switches the working environment to a branch. git refuses a
// checkout when another worktree holds the branch, so resolve it first: a held
// branch navigates (registering the checkout if needed) to that worktree folder,
// while a free branch is checked out in place — upstream's planBranchSwitch.
func (s *Shell) gitSwitchBranch(root, branch string) {
	svc := s.svc
	source := s.selectedFolder
	go func() {
		raw, err := s.B.raw("resolve_worktree_folder", map[string]any{
			"repoPath": root, "branch": branch,
		})
		var res map[string]any
		if err == nil {
			_ = json.Unmarshal(raw, &res)
		}
		path, folderID := str(res, "path"), ""
		if fid := res["folder_id"]; fid != nil {
			folderID = fmt.Sprintf("%v", fid)
		}
		if err == nil && path == "" {
			// Free branch: a plain checkout in the root working tree.
			_, err = s.B.raw("git_checkout", map[string]any{"path": root, "branchName": branch})
			s.mu.Lock()
			if err != nil {
				s.actionMsg = "checkout: " + err.Error()
			} else {
				s.actionMsg = catMsg("switched")
				delete(s.cache, "gitbranch:"+root)
				delete(s.cache, "allbranches:"+root)
				delete(s.cache, "gitstatus:"+root)
				delete(s.cache, "gitlog:"+root)
			}
			s.mu.Unlock()
			svc.Invalidate()
			return
		}
		if err == nil && folderID == "" {
			folderID, err = s.registerWorktreeFolder(path, source)
		}
		s.mu.Lock()
		if err != nil || folderID == "" {
			s.actionMsg = "worktree switch: " + firstNonEmpty(errText(err), "folder not resolved")
		} else {
			s.selectedFolder = folderID
			s.selectedConv = ""
			s.workbench = ""
			s.actionMsg = catMsg("switched to worktree")
			delete(s.cache, "folders")
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// registerWorktreeFolder opens a checkout as a workspace folder and returns its
// id (upstream's openWorktreeFolder).
func (s *Shell) registerWorktreeFolder(path, source string) (string, error) {
	raw, err := s.B.raw("open_worktree_folder", map[string]any{
		"path": path, "sourceFolderId": numOrString(source),
	})
	if err != nil {
		return "", err
	}
	var f map[string]any
	_ = json.Unmarshal(raw, &f)
	if id, ok := f["id"]; ok {
		return fmt.Sprintf("%v", id), nil
	}
	return "", fmt.Errorf("worktree folder id missing")
}

// gitRemoveWorktree removes the checkout holding branch (git_remove_worktree),
// optionally deleting the branch and the worktree's workspace folder too. When
// git refuses because the worktree has local work, the row escalates to Force
// remove instead of surfacing an error the user cannot act on.
func (s *Shell) gitRemoveWorktree(root, branch string, withBranch, force bool) {
	svc := s.svc
	source := s.selectedFolder
	go func() {
		_, err := s.B.raw("git_remove_worktree", map[string]any{
			"path": root, "branchName": branch,
			"sourceFolderId": numOrString(source),
			"deleteBranch":   withBranch, "force": force,
		})
		s.mu.Lock()
		switch {
		case err == nil:
			s.actionMsg = catMsg("worktree removed")
			s.worktreeRemove, s.worktreeRemoveBranch = "", false
			s.worktreeForce, s.worktreeForceBranch = "", false
		case !force && worktreeForceRequired(err.Error()):
			s.worktreeRemove, s.worktreeRemoveBranch = "", false
			s.worktreeForce, s.worktreeForceBranch = branch, withBranch
			s.actionMsg = catMsg("worktree force needed")
		default:
			s.actionMsg = "remove worktree: " + err.Error()
		}
		delete(s.cache, "allbranches:"+root)
		delete(s.cache, "gitbranch:"+root)
		delete(s.cache, "folders")
		delete(s.cache, "convs:"+source)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// worktreeForceRequired reports git's "this would throw work away" refusals.
func worktreeForceRequired(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "--force") || strings.Contains(m, "not fully merged")
}

// errText is err.Error(), "" for nil.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// catMsg says a short status message in the app's language when the catalogs
// carry one, else the English fallback. These outcomes are written from a
// goroutine (no *ui.Context), so they go through the package-level accessors,
// which read the app's language rather than a frame's.
func catMsg(key string) string {
	switch key {
	case "stashed":
		return locales.GitStashed()
	case "stash pop":
		return locales.GitStashPopped()
	case "stash apply":
		return locales.GitStashApplied()
	case "stash drop":
		return locales.GitStashDropped()
	case "stashes cleared":
		return locales.GitStashCleared()
	case "pushed":
		return locales.GitPushed()
	case "merged":
		return locales.GitMerged()
	case "merge aborted":
		return locales.GitMergeAborted()
	case "merge completed":
		return locales.GitMergeCompleted()
	case "switched":
		return locales.GitSwitched()
	case "branch created":
		return locales.GitBranchCreated()
	case "branch deleted":
		return locales.GitBranchDeleted()
	case "rebased":
		return locales.GitRebased()
	case "worktree created":
		return locales.GitWorktreeCreated()
	case "worktree removed":
		return locales.GitWorktreeRemoved()
	case "switched to worktree":
		return locales.GitWorktreeSwitched()
	case "worktree force needed":
		return locales.GitWorktreeForceNeeded()
	default:
		return key
	}
}
