package app

// git_branches_depth_test.go covers the Branches page's deeper slice: the
// prefix-grouped local tree, rebase, the worktree lifecycle (create / switch /
// remove / force escalation / delete-branch variant), and remote checkout.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// runGitT runs git in repo and fails the test on a non-zero exit.
func runGitT(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestGitBranchTreeGroupsAndRebase checks slash-delimited branches render as a
// collapsible prefix tree and that Rebase drives git_rebase.
func TestGitBranchTreeGroupsAndRebase(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteBranches)
	runGitT(t, repo, "branch", "feature/a")
	runGitT(t, repo, "branch", "feature/b")
	runGitT(t, repo, "branch", "release/1.0")

	tt := ui.NewTester(s.View, 1240, 900)
	if !waitUntil(t, tt, func() bool {
		return tt.HasText("feature/") && tt.HasText("release/")
	}) {
		t.Fatalf("branch groups missing; texts = %v", tt.Texts())
	}
	if !hasExactText(tt, "a") || !hasExactText(tt, "b") {
		t.Fatalf("grouped leaves missing; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "git-branches-depth")

	// Collapse "feature/": its leaves disappear until the group is reopened.
	if err := tt.Click("feature/"); err != nil {
		t.Fatalf("click group: %v", err)
	}
	tt.Frame()
	if hasExactText(tt, "a") || hasExactText(tt, "b") {
		t.Fatalf("leaves still visible after collapse; texts = %v", tt.Texts())
	}
	if err := tt.Click("feature/"); err != nil {
		t.Fatalf("reopen group: %v", err)
	}
	tt.Frame()
	if !hasExactText(tt, "a") {
		t.Fatalf("leaves missing after expand; texts = %v", tt.Texts())
	}

	// The first leaf's Rebase (feature/a) is up to date with the current branch;
	// the action line proves git_rebase ran successfully.
	if err := tt.Click("Rebase"); err != nil {
		t.Fatalf("click Rebase: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return s.actionMsg == "rebased" }) {
		t.Fatalf("rebase never completed; actionMsg = %q", s.actionMsg)
	}
}

// hasExactText reports whether any rendered text equals want.
func hasExactText(tt *ui.Tester, want string) bool {
	for _, txt := range tt.Texts() {
		if txt == want {
			return true
		}
	}
	return false
}

// TestGitWorktreeCreateSwitchRemove drives the worktree form: create registers
// the checkout as a folder and opens it, Switch navigates back to it, and a
// two-click Remove takes the checkout away while keeping its branch.
func TestGitWorktreeCreateSwitchRemove(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteBranches)
	source := s.selectedFolder
	wtPath := filepath.Join(t.TempDir(), "wt-checkout")

	tt := ui.NewTester(s.View, 1240, 900)
	if !waitUntil(t, tt, func() bool { return tt.HasText("Create worktree") }) {
		t.Fatalf("worktree form missing; texts = %v", tt.Texts())
	}
	s.worktreeBranchName = "wt-checkout"
	s.worktreePath = wtPath
	tt.Frame()
	if err := tt.Click("Create worktree"); err != nil {
		t.Fatalf("click Create worktree: %v", err)
	}
	if !waitUntilFor(t, tt, 30*time.Second, func() bool {
		return s.workbench == "" && s.selectedFolder != "" && s.selectedFolder != source
	}) {
		t.Fatalf("worktree folder never opened; selected = %q workbench = %q",
			s.selectedFolder, s.workbench)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree directory missing: %v", err)
	}
	if got := runGitT(t, repo, "branch", "--list", "wt-checkout"); got == "" {
		t.Fatal("worktree branch missing")
	}
	openFolder := s.selectedFolder
	if !waitUntil(t, tt, func() bool {
		return filepath.Clean(s.folderPathByID(openFolder)) == filepath.Clean(wtPath)
	}) {
		t.Fatalf("selected folder path = %q, want %q", s.folderPathByID(openFolder), wtPath)
	}

	// Back to the root repo's branch page: Switch resolves and reopens the
	// worktree folder.
	s.selectedFolder, s.selectedConv, s.workbench = source, "", gitRouteBranches
	s.invalidate("allbranches:" + repo)
	s.invalidate("gitbranch:" + repo)
	if !waitUntil(t, tt, func() bool { return tt.HasText("Remove worktree") }) {
		t.Fatalf("worktree row missing; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "git-worktrees")
	if err := tt.Click("Switch"); err != nil {
		t.Fatalf("click worktree Switch: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return s.selectedFolder == openFolder }) {
		t.Fatalf("switch left selectedFolder = %q, want %q", s.selectedFolder, openFolder)
	}

	// Remove asks twice; the branch survives the checkout's removal.
	s.selectedFolder, s.selectedConv, s.workbench = source, "", gitRouteBranches
	s.invalidate("allbranches:" + repo)
	s.invalidate("gitbranch:" + repo)
	if !waitUntil(t, tt, func() bool { return tt.HasText("Remove worktree") }) {
		t.Fatalf("worktree row missing before remove; texts = %v", tt.Texts())
	}
	if err := tt.Click("Remove worktree"); err != nil {
		t.Fatalf("click Remove worktree: %v", err)
	}
	tt.Frame()
	if err := tt.Click("Remove worktree"); err != nil {
		t.Fatalf("confirm Remove worktree: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		_, err := os.Stat(wtPath)
		return os.IsNotExist(err)
	}) {
		t.Fatalf("worktree directory still present; actionMsg = %q", s.actionMsg)
	}
	if got := runGitT(t, repo, "branch", "--list", "wt-checkout"); got == "" {
		t.Fatal("removing the checkout also deleted its branch")
	}
}

// TestGitWorktreeRemoveForceAndDeleteBranch covers the two escalation paths:
// a dirty worktree refuses plain removal and offers Force remove, and the
// "Remove + delete branch" variant also drops the branch.
func TestGitWorktreeRemoveForceAndDeleteBranch(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteBranches)
	wtPath := filepath.Join(t.TempDir(), "wt-force")
	runGitT(t, repo, "worktree", "add", "-b", "wt-force", wtPath)
	if err := os.WriteFile(filepath.Join(wtPath, "dirty.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tt := ui.NewTester(s.View, 1240, 900)
	if !waitUntil(t, tt, func() bool { return tt.HasText("Remove worktree") }) {
		t.Fatalf("worktree row missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Remove worktree"); err != nil {
		t.Fatalf("click Remove worktree: %v", err)
	}
	tt.Frame()
	if err := tt.Click("Remove worktree"); err != nil {
		t.Fatalf("confirm Remove worktree: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("Force remove") }) {
		t.Fatalf("force escalation missing; actionMsg = %q texts = %v", s.actionMsg, tt.Texts())
	}
	if err := tt.Click("Force remove"); err != nil {
		t.Fatalf("click Force remove: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		_, err := os.Stat(wtPath)
		return os.IsNotExist(err)
	}) {
		t.Fatalf("force remove left the directory; actionMsg = %q", s.actionMsg)
	}
	if got := runGitT(t, repo, "branch", "--list", "wt-force"); got == "" {
		t.Fatal("force removal deleted the branch it was told to keep")
	}

	// Recreate the checkout (branch is free again), then take both.
	wtPath2 := filepath.Join(t.TempDir(), "wt-force2")
	runGitT(t, repo, "worktree", "add", wtPath2, "wt-force")
	s.invalidate("allbranches:" + repo)
	s.invalidate("gitbranch:" + repo)
	if !waitUntil(t, tt, func() bool { return tt.HasText("Remove + delete branch") }) {
		t.Fatalf("delete-branch variant missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Remove + delete branch"); err != nil {
		t.Fatalf("click Remove + delete branch: %v", err)
	}
	tt.Frame()
	if s.worktreeRemove != "wt-force" || !s.worktreeRemoveBranch {
		t.Fatalf("confirm state = (%q,%v), want wt-force with branch",
			s.worktreeRemove, s.worktreeRemoveBranch)
	}
	if _, err := os.Stat(wtPath2); err != nil {
		t.Fatalf("second worktree missing before confirm: %v (actionMsg = %q)", err, s.actionMsg)
	}
	if err := tt.Click("Remove + delete branch"); err != nil {
		t.Fatalf("confirm Remove + delete branch: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		out, err := exec.Command("git", "-C", repo, "branch", "--list", "wt-force").Output()
		return err == nil && strings.TrimSpace(string(out)) == ""
	}) {
		t.Fatalf("branch survived Remove + delete branch; actionMsg = %q", s.actionMsg)
	}
	if !waitUntil(t, tt, func() bool {
		_, err := os.Stat(wtPath2)
		return os.IsNotExist(err)
	}) {
		t.Fatalf("worktree+branch removal left the directory; actionMsg = %q", s.actionMsg)
	}
}

// TestGitRemoteBranchCheckout checks a remote-only branch out as a tracking
// branch in the root working tree.
func TestGitRemoteBranchCheckout(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteBranches)
	runGitT(t, repo, "remote", "add", "origin", repo)
	sha := runGitT(t, repo, "rev-parse", "HEAD")
	runGitT(t, repo, "update-ref", "refs/remotes/origin/remote-only", sha)

	tt := ui.NewTester(s.View, 1240, 900)
	if !waitUntil(t, tt, func() bool { return tt.HasText("origin/remote-only") }) {
		t.Fatalf("remote branch missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Checkout"); err != nil {
		t.Fatalf("click Checkout: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		out, err := exec.Command("git", "-C", repo, "rev-parse", "--abbrev-ref", "HEAD").Output()
		return err == nil && strings.TrimSpace(string(out)) == "remote-only"
	}) {
		t.Fatalf("remote branch never checked out; actionMsg = %q", s.actionMsg)
	}
}
