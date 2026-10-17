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
	"strings"

	"github.com/egoist/mygo/ui"

	"yoyocode/locales"
)

// Git-operation workbench routes. All are folder-scoped, whole-page panels.
const (
	gitRouteCommit = "git-commit"
	gitRouteStash  = "git-stash"
	gitRoutePush   = "git-push"
	gitRouteMerge  = "git-merge"
)

// isGitRoute reports whether a workbench route is one of the git-operation pages.
func isGitRoute(route string) bool {
	switch route {
	case gitRouteCommit, gitRouteStash, gitRoutePush, gitRouteMerge:
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
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(12).Children(func() {
			switch route {
			case gitRouteCommit:
				s.viewGitCommitPage(c, root)
			case gitRouteStash:
				s.viewGitStashPage(c, root)
			case gitRoutePush:
				s.viewGitPushPage(c, root)
			case gitRouteMerge:
				s.viewGitMergePage(c, root)
			}
		})
	})
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
	m.Separator()
	if m.Item(cat(c).GitStashTitle()).Chosen() {
		s.workbench = gitRouteStash
	}
}

// ─── commit page ──────────────────────────────────────────────────────────

// viewGitCommitPage renders the commit workspace: the branch and author facts,
// the changed files with their status codes, the message box and Commit.
func (s *Shell) viewGitCommitPage(c *ui.Context, root string) {
	// Facts: the branch and the configured author, the two things a committer
	// checks before writing a message.
	branch := decodeString(s.ensure("gitbranch:"+root, "get_git_branch", map[string]any{"path": root}))
	author := decodeString(s.ensure("gituser:"+root, "git_current_user", map[string]any{"path": root}))
	ui.Row(c.Key("gc-facts")).Gap(16).Children(func() {
		gitFact(c, cat(c).GitBranchLabel(), firstNonEmpty(branch, "—"))
		gitFact(c, cat(c).GitAuthorLabel(), firstNonEmpty(author, "—"))
	})

	files := asList(s.ensure("gitstatus:"+root, "git_status", map[string]any{"path": root}))
	if e := s.errFor("gitstatus:" + root); e != "" {
		s.errorState(c, "gitstatus:"+root, nil)
		return
	}
	if files == nil {
		loadingState(c, "")
		return
	}
	ui.Text(c, cat(c).GitChangedFiles(len(files))).FontSize(12).Bold()
	if len(files) == 0 {
		emptyState(c, iconCheck, cat(c).GitNoChanges(), "")
		return
	}
	ui.Column(c).Gap(1).MaxWidth(720).Children(func() {
		for _, f := range files {
			s.viewGitFileRow(c, f, "gcf-")
		}
	})
	ui.Divider(c)
	ui.Column(c.Key("gc-form")).Gap(8).MaxWidth(720).Children(func() {
		ui.Text(c, cat(c).GitMessageLabel()).FontSize(12).Bold()
		ui.TextInput(c.Key("gc-msg"), &s.commitMsg).
			Placeholder(cat(c).GitMessagePlaceholder()).FillWidth()
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			commitBtn := ui.PrimaryButton(c.Key("gc-commit"), cat(c).GitCommitAction()).
				Disabled(strings.TrimSpace(s.commitMsg) == "")
			// A distinct label: the page heading already says "Commit", so an
			// identical button label would be ambiguous to assistive tech (and
			// to the tester's hit-by-text).
			commitBtn.Label(cat(c).GitCommitActionLabel())
			if commitBtn.Clicked() {
				s.commitAll(root, files)
			}
			iconButton(c, "gc-refresh", iconRefresh, cat(c).ActionRefresh(), func() {
				s.invalidate("gitstatus:" + root)
			})
		})
		s.viewActionOrError(c)
	})
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
			ui.Row(c.Key("gs-"+ref)).PaddingX(8).PaddingY(6).Gap(8).AlignItems(ui.Center).Children(func() {
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

// viewGitPushPage renders the push workspace: the branch, its tracking remote
// and the remotes (git_push_info), the remote picker and Push.
func (s *Shell) viewGitPushPage(c *ui.Context, root string) {
	t := c.Theme()
	key := "pushinfo:" + root
	info := asObj(s.ensure(key, "git_push_info", map[string]any{"path": root}))
	if e := s.errFor(key); e != "" {
		s.errorState(c, key, nil)
		return
	}
	if info == nil {
		loadingState(c, "")
		return
	}
	branch := str(info, "branch")
	remotes := asListVal(info["remotes"])
	names := make([]string, 0, len(remotes))
	for _, r := range remotes {
		if n := str(r, "name"); n != "" {
			names = append(names, n)
		}
	}
	// Seed the pickers from the facts (and whenever the branch changed under
	// us), so Push is never a dead end.
	if s.pushBranch != branch {
		s.pushBranch = branch
	}
	if s.pushRemote == "" {
		if tr := str(info, "tracking_remote"); tr != "" {
			s.pushRemote = tr
		} else if len(names) > 0 {
			s.pushRemote = names[0]
		}
	}
	ui.Row(c.Key("gp-facts")).Gap(16).Children(func() {
		gitFact(c, cat(c).GitBranchLabel(), firstNonEmpty(branch, "—"))
		gitFact(c, cat(c).GitTrackingLabel(), firstNonEmpty(str(info, "tracking_remote"), cat(c).GitNone()))
	})
	if len(names) == 0 {
		emptyState(c, iconInfo, cat(c).GitNoRemotes(), cat(c).GitNoRemotesHelp())
		return
	}
	ui.Divider(c)
	ui.Column(c.Key("gp-form")).Gap(10).MaxWidth(720).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, cat(c).GitRemoteLabel()).FontSize(12).Width(80)
			ui.Select(c.Key("gp-remote"), &s.pushRemote, names).Grow(1)
		})
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, cat(c).GitBranchLabel()).FontSize(12).Width(80)
			ui.Text(c, firstNonEmpty(s.pushBranch, branch)).FontSize(13).TextColor(t.Text)
		})
		pushBtn := ui.PrimaryButton(c.Key("gp-push"), cat(c).GitPushAction())
		pushBtn.Label(cat(c).GitPushActionLabel())
		if pushBtn.Clicked() {
			s.gitPush(root, s.pushRemote, s.pushBranch)
		}
		s.viewActionOrError(c)
	})
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
				ui.Row(c.Key("gmc-"+f)).PaddingX(8).PaddingY(4).Gap(8).AlignItems(ui.Center).Children(func() {
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
	ui.Row(c.Key(keyPrefix+file)).PaddingX(8).PaddingY(4).Gap(8).AlignItems(ui.Center).Children(func() {
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
// upstream, which the backend reports back through upstream_set.
func (s *Shell) gitPush(root, remote, branch string) {
	svc := s.svc
	go func() {
		_, err := s.B.raw("git_push", map[string]any{"path": root, "remote": remote, "branch": branch})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = "push: " + err.Error()
		} else {
			s.actionMsg = catMsg("pushed")
			s.pushUpstreamSet = true
		}
		delete(s.cache, "pushinfo:"+root)
		s.mu.Unlock()
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
	default:
		return key
	}
}

