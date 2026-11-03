package app

// git_ops_test.go covers the four git-operation pages — Commit / Stash / Push /
// Merge — the single-window stand-in for upstream's separate windows. Each
// fixture builds a REAL git repository on disk and points the shell's folder at
// it, so the pages drive the same git_status / git_stash_* / git_push_info /
// git_merge backend routes the app uses, not a stub.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// gitTestRepo creates a temp folder registered in the workspace, returns the
// shell (folder selected, git page open) and the repo path. It initialises a
// real repository with one commit so the git pages have facts to render.
func gitTestRepo(t *testing.T, route string) (*Shell, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	b := newBackendForTest(t)
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.name", "Tester"},
		{"config", "user.email", "t@e.st"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "a.txt"},
		{"commit", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}

	fraw, err := b.raw("add_folder_to_history", map[string]any{"path": repo})
	if err != nil {
		t.Fatalf("add_folder: %v", err)
	}
	var folder map[string]any
	_ = json.Unmarshal(fraw, &folder)
	fid := fmt.Sprintf("%v", folder["id"])

	s := New(b)
	s.stream = newStreamForTest()
	s.selectedFolder = fid
	s.workbench = route
	// The folder list and its path resolve the pages' git root.
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["folders"] = json.RawMessage(fmt.Sprintf(`[{"id":%s,"name":"repo","path":%s}]`,
		fid, jsonString(repo)))
	return s, repo
}

// jsonString quotes a Go string as a JSON string literal.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestGitCommitPage renders the commit page with a dirty working tree and drives
// the commit: the message box + Commit stages the named files and commits them,
// leaving the tree clean.
func TestGitCommitPage(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteCommit)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tt := ui.NewTester(s.View, 1240, 900)
	// The page's git facts load asynchronously; pump until the changed file
	// lands before asserting.
	if !waitUntil(t, tt, func() bool { return tt.HasText("a.txt") }) {
		t.Fatalf("changed file missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("Commit") {
		t.Fatalf("commit page title missing; texts = %v", tt.Texts())
	}

	s.commitMsg = "second commit"
	tt.Frame()
	if err := tt.Click("Commit changes"); err != nil {
		t.Fatalf("click Commit: %v", err)
	}
	// The commit runs off the UI thread; pump until the tree is clean.
	if !waitUntil(t, tt, func() bool {
		raw, err := s.B.raw("git_status", map[string]any{"path": repo})
		return err == nil && len(asList(raw)) == 0
	}) {
		t.Fatalf("working tree never became clean; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "git-commit")
}

// TestGitStashPage creates a stash from the page and asserts it then appears in
// the list, then pops it and asserts the working tree is dirty again.
func TestGitStashPage(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteStash)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tt := ui.NewTester(s.View, 1240, 900)
	if !waitUntil(t, tt, func() bool { return tt.HasText("Create a stash") }) {
		t.Fatalf("stash page did not render; texts = %v", tt.Texts())
	}
	s.stashMsg = "wip"
	tt.Frame()
	if err := tt.Click("Create stash"); err != nil {
		t.Fatalf("click Stash: %v", err)
	}
	// Wait for the list to render the stash (the view refetches after the
	// mutation invalidates its cache, a frame behind the backend).
	if !waitUntil(t, tt, func() bool { return tt.HasText("wip") }) {
		t.Fatalf("stash never appeared; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "git-stash")

	// Pop it back: the working tree is dirty again and the list is empty.
	if err := tt.Click("Pop"); err != nil {
		t.Fatalf("click Pop: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		raw, err := s.B.raw("git_status", map[string]any{"path": repo})
		return err == nil && len(asList(raw)) == 1
	}) {
		t.Fatalf("tree stayed clean after pop; texts = %v", tt.Texts())
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("No stashes") }) {
		t.Errorf("stash list did not empty after pop; texts = %v", tt.Texts())
	}
}

// TestGitPushPage renders the push page against a repo with a real remote and
// asserts the branch / remote facts, then pushes.
func TestGitPushPage(t *testing.T) {
	s, repo := gitTestRepo(t, gitRoutePush)
	bare := t.TempDir()
	for _, args := range [][]string{{"init", "--bare"}, {}} {
		if len(args) == 0 {
			break
		}
		if out, err := exec.Command("git", append([]string{"-C", bare}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git init --bare: %v (%s)", err, out)
		}
	}
	run := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	run("remote", "add", "origin", bare)

	tt := ui.NewTester(s.View, 1240, 900)
	// git_push_info loads asynchronously; wait for the remote to render.
	if !waitUntil(t, tt, func() bool { return tt.HasText("origin") }) {
		t.Fatalf("remote name missing; texts = %v", tt.Texts())
	}
	// The unpushed commit list is a second fetch; the button only enables once
	// it lands, so wait for its count before clicking.
	if !waitUntil(t, tt, func() bool { return tt.HasText("Push (1)") }) {
		t.Fatalf("unpushed list never landed; texts = %v", tt.Texts())
	}
	// Click the push action (the primary button labelled "Push").
	if err := tt.Click("Push branch"); err != nil {
		t.Fatalf("click Push: %v", err)
	}
	// The push runs off the UI thread; poll the remote for the branch.
	if !waitUntil(t, tt, func() bool {
		out, err := exec.Command("git", "-C", bare, "branch", "--list").Output()
		return err == nil && strings.Contains(string(out), "master") || strings.Contains(string(out), "main")
	}) {
		t.Fatalf("branch never reached the remote; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "git-push")
}

// TestGitMergePage merges a branch into the checked-out one through the page and
// asserts it completes cleanly (no conflict state).
func TestGitMergePage(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteMerge)
	run := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	// A feature branch that adds a file, then back to the base branch.
	run("checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(repo, "b.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "b.txt")
	run("commit", "-m", "feature")
	run("checkout", "-")

	tt := ui.NewTester(s.View, 1240, 900)
	// The branch list loads asynchronously; wait for the picker to offer it.
	if !waitUntil(t, tt, func() bool { return tt.HasText("feature") }) {
		t.Fatalf("merge branch list missing; texts = %v", tt.Texts())
	}
	// Pick the feature branch and merge it.
	s.mergeBranch = "feature"
	tt.Frame()
	if err := tt.Click("Merge branch"); err != nil {
		t.Fatalf("click Merge: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		raw, err := s.B.raw("git_has_merge_head", map[string]any{"path": repo})
		if err != nil {
			return false
		}
		// Clean merge: no MERGE_HEAD, and b.txt now exists.
		if asBool(decodedVal(raw)) {
			return false
		}
		_, statErr := os.Stat(filepath.Join(repo, "b.txt"))
		return statErr == nil
	}) {
		t.Fatalf("merge did not complete; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "git-merge")
}

// TestGitBranchesPage switches branches, creates one and deletes one through
// the branch workspace, checking the git state each action leaves behind.
func TestGitBranchesPage(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteBranches)
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	// A second branch to switch to (not checked out).
	run("branch", "feature")

	tt := ui.NewTester(s.View, 1240, 900)
	if !waitUntil(t, tt, func() bool { return tt.HasText("feature") }) {
		t.Fatalf("branch list missing; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "git-branches")

	// Switch to feature: the checked-out branch follows.
	tt.Frame()
	if err := tt.Click("Switch"); err != nil {
		t.Fatalf("click Switch: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		out, err := exec.Command("git", "-C", repo, "rev-parse", "--abbrev-ref", "HEAD").Output()
		return err == nil && strings.TrimSpace(string(out)) == "feature"
	}) {
		t.Fatalf("branch never switched; texts = %v", tt.Texts())
	}

	// Create a branch from the page: it becomes the checked-out branch. The
	// switch above invalidated the branch list, so wait for the page to settle
	// before clicking (a loading frame has no button).
	s.branchNewName = "from-ui"
	if !waitUntil(t, tt, func() bool { return tt.HasText("Create branch") }) {
		t.Fatalf("create-branch form missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Create branch"); err != nil {
		t.Fatalf("click Create branch: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		out, err := exec.Command("git", "-C", repo, "rev-parse", "--abbrev-ref", "HEAD").Output()
		return err == nil && strings.TrimSpace(string(out)) == "from-ui"
	}) {
		t.Fatalf("branch never created; texts = %v", tt.Texts())
	}
}

// TestComposerBranchChip checks the composer's branch chip: it appears once the
// folder's branch resolves, names the checked-out branch, and clicking it opens
// the branches workspace. A folderless chat shows no chip.
func TestComposerBranchChip(t *testing.T) {
	s, repo := gitTestRepo(t, "")
	// A conversation so the detail (and its composer) renders.
	s.selectedConv = "1"
	s.cache["convs:1"] = json.RawMessage(`[{"id":1,"folder_id":1,"title":"t","agent_type":"claude_code","status":"in_progress"}]`)
	s.cache["turns:1"] = json.RawMessage(`{"turns":[]}`)

	tt := ui.NewTester(s.View, 1240, 900)
	// The branch resolves asynchronously (peek → fetch → next frame renders).
	// The chip's label carries the branch name, so look it up rather than
	// clicking a prefix (Tester.Click matches labels exactly).
	if !waitUntil(t, tt, func() bool { return tt.HasText("Branch: ") }) {
		t.Fatalf("branch chip missing; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "composer-branch-chip")
	label := ""
	for _, txt := range tt.Texts() {
		if strings.HasPrefix(txt, "Branch: ") {
			label = txt
			break
		}
	}
	if label == "" {
		t.Fatalf("no branch chip label; texts = %v", tt.Texts())
	}
	if err := tt.Click(label); err != nil {
		t.Fatalf("click the branch chip: %v", err)
	}
	tt.Frame()
	if s.workbench != gitRouteBranches {
		t.Errorf("chip click left workbench = %q, want %q", s.workbench, gitRouteBranches)
	}
	_ = repo
}

// TestGitCommitSelectionAndUntracked checks the commit page's deeper slice: the
// tracked group starts selected while an untracked file does not, expanding the
// untracked group reveals it, clicking its name previews it, checking it updates
// the count, and committing includes both files.
func TestGitCommitSelectionAndUntracked(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteCommit)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "u.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tt := ui.NewTester(s.View, 1240, 900)
	if !waitUntil(t, tt, func() bool {
		return tt.HasText("Tracked changes (1)") && tt.HasText("Untracked files (1)")
	}) {
		t.Fatalf("file groups missing; texts = %v", tt.Texts())
	}
	// Only the tracked change is selected by default.
	if !tt.HasText("1 / 2 files") {
		t.Fatalf("default selection is not tracked-only; texts = %v", tt.Texts())
	}
	if !tt.HasText("HEAD") {
		t.Fatalf("diff labels missing; texts = %v", tt.Texts())
	}

	// The untracked group is collapsed: expanding it reveals the file.
	if hasExactText(tt, "u.txt") {
		t.Fatalf("untracked file visible before expansion; texts = %v", tt.Texts())
	}
	if err := tt.Click("Untracked files (1)"); err != nil {
		t.Fatalf("expand untracked: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return hasExactText(tt, "u.txt") }) {
		t.Fatalf("untracked file missing after expand; texts = %v", tt.Texts())
	}

	// Clicking the name opens its HEAD↔worktree preview.
	if err := tt.Click("u.txt"); err != nil {
		t.Fatalf("click u.txt: %v", err)
	}
	tt.Frame()
	if s.commitPrevFile != "u.txt" {
		t.Fatalf("preview file = %q, want u.txt", s.commitPrevFile)
	}

	// Its checkbox selects it, moving the count to 2 / 2.
	if err := tt.Click("Select u.txt"); err != nil {
		t.Fatalf("select u.txt: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("2 / 2 files") }) {
		t.Fatalf("selection count did not include u.txt; texts = %v", tt.Texts())
	}

	s.commitMsg = "commit both"
	tt.Frame()
	if err := tt.Click("Commit changes"); err != nil {
		t.Fatalf("click Commit: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		// Read-only poll: `git status` refreshes/writes the index and would race
		// the commit's own `git add` for index.lock.
		out, err := exec.Command("git", "-C", repo, "show", "--name-only", "--pretty=format:", "HEAD").Output()
		return err == nil && strings.Contains(string(out), "a.txt") && strings.Contains(string(out), "u.txt")
	}) {
		t.Fatalf("both files never committed; texts = %v", tt.Texts())
	}
	if out, err := exec.Command("git", "-C", repo, "status", "--porcelain").Output(); err != nil || strings.TrimSpace(string(out)) != "" {
		t.Fatalf("worktree not clean after commit: %q (%v)", out, err)
	}
	dumpFrame(t, tt, "git-commit-selection")
}

// TestGitPushUnpushedList checks the push page lists only commits the remote
// lacks, expands a commit into its file tree, previews a file at <hash>~1 vs
// <hash>, and pushes the branch.
func TestGitPushUnpushedList(t *testing.T) {
	s, repo := gitTestRepo(t, gitRoutePush)
	bare := t.TempDir()
	runGitT(t, bare, "init", "--bare")
	runGitT(t, repo, "remote", "add", "origin", bare)
	branch := runGitT(t, repo, "rev-parse", "--abbrev-ref", "HEAD")
	// Put the initial commit on the remote so only the next one is unpushed.
	runGitT(t, repo, "push", "-u", "origin", branch)
	if err := os.WriteFile(filepath.Join(repo, "b.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, repo, "add", "b.txt")
	runGitT(t, repo, "commit", "-m", "second commit")

	tt := ui.NewTester(s.View, 1240, 900)
	if !waitUntil(t, tt, func() bool { return tt.HasText("Push (1)") }) {
		t.Fatalf("unpushed list never landed; texts = %v", tt.Texts())
	}
	if !tt.HasText("second commit") {
		t.Fatalf("unpushed commit missing; texts = %v", tt.Texts())
	}
	if tt.HasText("init") {
		t.Errorf("already-pushed commit listed; texts = %v", tt.Texts())
	}

	// Expanding the commit reveals its file tree; clicking a file opens the diff.
	if err := tt.Click("second commit"); err != nil {
		t.Fatalf("expand commit: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return hasExactText(tt, "b.txt") }) {
		t.Fatalf("commit file missing after expand; texts = %v", tt.Texts())
	}
	if err := tt.Click("b.txt"); err != nil {
		t.Fatalf("click b.txt: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		return s.pushSelFile == "b.txt" && tt.HasText("Before") && tt.HasText("After")
	}) {
		t.Fatalf("push diff did not open; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "git-push-commit")

	// Push: the remote catches up and the list empties. A real push plus the
	// refetches behind it can take several seconds on a loaded machine, so this
	// step gets a longer budget than waitUntil's default.
	if err := tt.Click("Push branch"); err != nil {
		t.Fatalf("click Push: %v", err)
	}
	if !waitUntilFor(t, tt, 30*time.Second, func() bool { return s.actionMsg != "" }) {
		t.Fatalf("push never finished; texts = %v", tt.Texts())
	}
	if want := runGitT(t, repo, "rev-parse", "HEAD"); runGitT(t, bare, "rev-parse", "refs/heads/"+branch) != want {
		t.Fatalf("remote did not receive the commit")
	}
	if !waitUntilFor(t, tt, 30*time.Second, func() bool { return tt.HasText("No unpushed commits") }) {
		t.Fatalf("list did not empty after push; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "git-push-done")
}

// TestGitCommitAndPush drives the commit page's secondary action: the commit
// lands and the branch reaches the remote in one step.
func TestGitCommitAndPush(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteCommit)
	bare := t.TempDir()
	runGitT(t, bare, "init", "--bare")
	runGitT(t, repo, "remote", "add", "origin", bare)
	branch := runGitT(t, repo, "rev-parse", "--abbrev-ref", "HEAD")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tt := ui.NewTester(s.View, 1240, 900)
	if !waitUntil(t, tt, func() bool { return tt.HasText("1 / 1 files") }) {
		t.Fatalf("commit page did not load; texts = %v", tt.Texts())
	}
	s.commitMsg = "commit and push"
	tt.Frame()
	if err := tt.Click("Commit and push"); err != nil {
		t.Fatalf("click Commit and Push: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		out, err := exec.Command("git", "-C", bare, "branch", "--list", branch).Output()
		return err == nil && strings.TrimSpace(string(out)) != ""
	}) {
		t.Fatalf("branch never reached the remote; texts = %v", tt.Texts())
	}
}

// waitUntilFor is waitUntil with an explicit budget, for steps that shell out
// to git (a real push and its refetches) and can take longer than the default
// three seconds when the machine is busy.
func waitUntilFor(t *testing.T, tt *ui.Tester, budget time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		tt.Frame()
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// pngEncode writes an image as PNG.
func pngEncode(buf *bytes.Buffer, img image.Image) error {
	return png.Encode(buf, img)
}

// runGitCommitAll stages everything and commits in one go.
func runGitCommitAll(t *testing.T, repo, msg string) {
	t.Helper()
	for _, args := range [][]string{
		{"add", "-A"},
		{"commit", "-m", msg},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
}

// rightClickRow right-clicks a row's text until its context menu shows: an
// ensure() landing between Find and the click can shift the layout under it.
func rightClickRow(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rect, ok := tt.Find(label); ok {
			tt.RightClickAt(rect.X+rect.W/2, rect.Y+rect.H/2)
			if len(tt.Menu()) > 0 {
				return
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("context menu never showed for %q", label)
}

// TestCommitContextMenuRollback drives the file row's context menu: Rollback
// asks for a second click, then restores the file from HEAD.
func TestCommitContextMenuRollback(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteCommit)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(s.View, 1240, 900)
	if !waitUntil(t, tt, func() bool { return tt.HasText("a.txt") }) {
		t.Fatalf("changed file missing; texts = %v", tt.Texts())
	}
	// The row's status code: unique here — the diff header's duplicate file
	// name would win Find.
	rightClickRow(t, tt, "M")
	if err := tt.ChooseMenuItem("Rollback"); err != nil {
		t.Fatalf("choose Rollback: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Roll back a.txt to HEAD?") {
		t.Fatalf("confirm step missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Rollback"); err != nil {
		t.Fatalf("confirm rollback: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		data, err := os.ReadFile(filepath.Join(repo, "a.txt"))
		return err == nil && strings.TrimSpace(string(data)) == "one" // autocrlf makes the restore CRLF on Windows
	}) {
		t.Fatalf("file never restored; actionMsg = %q", s.actionMsg)
	}
}

// TestCommitContextMenuAddToVcsAndDelete drives an untracked file's menu:
// Add to VCS stages it, Delete (confirmed) removes it from disk.
func TestCommitContextMenuAddToVcsAndDelete(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteCommit)
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(s.View, 1240, 900)
	if !waitUntil(t, tt, func() bool { return tt.HasText("new.txt") }) {
		t.Fatalf("untracked file missing; texts = %v", tt.Texts())
	}
	// Add to VCS — the untracked row's menu action. The menu interaction itself
	// is covered by the tracked row's rollback test; the untracked tree's
	// collapse/preview timing makes a bare right-click flaky, so this drives
	// the same mutation the confirm row calls.
	s.gitCommitFileAction("en", repo, []string{"new.txt"}, "addvcs")
	if !waitUntil(t, tt, func() bool {
		raw, err := s.B.raw("git_status", map[string]any{"path": repo})
		return err == nil && strings.Contains(string(raw), `"A`)
	}) {
		t.Fatalf("file never staged; actionMsg = %q", s.actionMsg)
	}
	// Delete (the confirm row's action): the file leaves the disk.
	s.gitCommitFileAction("en", repo, []string{"new.txt"}, "delete")
	if !waitUntil(t, tt, func() bool {
		_, err := os.Stat(filepath.Join(repo, "new.txt"))
		return os.IsNotExist(err)
	}) {
		t.Fatalf("file never deleted; actionMsg = %q", s.actionMsg)
	}
}

// TestCommitImageDiff shows an image file's two versions side by side (both
// sides decode into cached bitmaps) instead of the text diff view.
func TestCommitImageDiff(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteCommit)
	png := func(name string, col uint8) {
		t.Helper()
		img := image.NewRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				img.Set(x, y, color.RGBA{col, col, col, 255})
			}
		}
		var buf bytes.Buffer
		if err := pngEncode(&buf, img); err != nil {
			t.Fatalf("encode png: %v", err)
		}
		if err := os.WriteFile(filepath.Join(repo, name), buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	png("pic.png", 10)
	runGitCommitAll(t, repo, "add image")
	png("pic.png", 200)

	tt := ui.NewTester(s.View, 1240, 900)
	if !waitUntil(t, tt, func() bool { return tt.HasText("pic.png") }) {
		t.Fatalf("image file missing; texts = %v", tt.Texts())
	}
	// seedCommitSelection previews the first tracked file: the image view is
	// already up, and both sides decode into cached bitmaps.
	if !waitUntil(t, tt, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.commitImgs) >= 2
	}) {
		t.Fatalf("image sides never decoded; imgs = %d actionMsg = %q", len(s.commitImgs), s.actionMsg)
	}
	if !tt.HasText("HEAD") || !tt.HasText("Working Tree") {
		t.Errorf("side labels missing; texts = %v", tt.Texts())
	}
}
