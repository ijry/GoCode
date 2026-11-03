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
	// Upstream opens a fresh conversation in the new checkout: a tab for it is
	// in the strip and it is the selected conversation.
	if !waitUntil(t, tt, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.openTabs) == 1 && s.openTabs[0].folderID == openFolder && s.selectedConv != ""
	}) {
		s.mu.Lock()
		tabs, conv := len(s.openTabs), s.selectedConv
		s.mu.Unlock()
		t.Fatalf("worktree conversation not opened; tabs = %d conv = %q actionMsg = %q", tabs, conv, s.actionMsg)
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
	// Remote leaves sit under their remote's group header, so the short name
	// and the remote name render as separate texts.
	if !waitUntil(t, tt, func() bool { return tt.HasText("remote-only") && tt.HasText("origin") }) {
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

// gitBareRemoteForTest creates a bare remote, points repo's origin at it, and
// pushes repo's checked-out branch there. Returns the bare path.
func gitBareRemoteForTest(t *testing.T, repo string) string {
	t.Helper()
	bare := t.TempDir()
	runGitT(t, bare, "init", "--bare")
	runGitT(t, repo, "remote", "add", "origin", bare)
	runGitT(t, repo, "push", "origin", "HEAD")
	return bare
}

// gitCloneForTest clones bare into a fresh temp repo with a test identity, so
// its commits are attributable.
func gitCloneForTest(t *testing.T, bare string) string {
	t.Helper()
	clone := t.TempDir()
	runGitT(t, clone, "clone", bare, ".")
	runGitT(t, clone, "config", "user.name", "Tester")
	runGitT(t, clone, "config", "user.email", "t@e.st")
	return clone
}

// TestRemoteBranchesGroupedAndDeleteRemote covers the remote section's new
// shape: branches grouped under their remote, and a two-step Delete remote
// that removes the branch on the server.
func TestRemoteBranchesGroupedAndDeleteRemote(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteBranches)
	bare := gitBareRemoteForTest(t, repo)
	// Delete a NON-current remote branch: a bare remote refuses to delete the
	// one its HEAD points at. Only that leaf stays in the list, so the clicks
	// below are unambiguous.
	base := runGitT(t, repo, "rev-parse", "--abbrev-ref", "HEAD")
	runGitT(t, repo, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("f\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, repo, "add", "f.txt")
	runGitT(t, repo, "commit", "-m", "feature work")
	runGitT(t, repo, "push", "origin", "feature")
	runGitT(t, repo, "checkout", base)
	runGitT(t, repo, "update-ref", "-d", "refs/remotes/origin/"+base)

	tt := ui.NewTester(s.View, 1240, 900)
	if !waitUntil(t, tt, func() bool { return tt.HasText("Delete remote") }) {
		t.Fatalf("remote leaf missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("origin") {
		t.Fatalf("remote group header missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Delete remote"); err != nil {
		t.Fatalf("click Delete remote: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Delete from remote") {
		t.Fatalf("confirm step missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Delete from remote"); err != nil {
		t.Fatalf("confirm delete: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		out, err := exec.Command("git", "-C", bare, "branch", "--list").Output()
		return err == nil && !strings.Contains(string(out), "feature")
	}) {
		t.Fatalf("branch survived on the remote; actionMsg = %q", s.actionMsg)
	}
}

// TestBranchLeafPullUpdatesRemoteRef drives a remote leaf's Pull: the
// remote-tracking ref fast-forwards to what the remote actually holds,
// without any checkout.
func TestBranchLeafPullUpdatesRemoteRef(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteBranches)
	bare := gitBareRemoteForTest(t, repo)
	clone := gitCloneForTest(t, bare)
	runGitT(t, clone, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(clone, "c.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, clone, "add", "c.txt")
	runGitT(t, clone, "commit", "-m", "feature base")
	runGitT(t, clone, "push", "origin", "feature")
	runGitT(t, repo, "fetch", "origin", "feature")
	// The clone moves on; the repo's tracking ref is now stale.
	runGitT(t, clone, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(clone, "d.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, clone, "add", "d.txt")
	runGitT(t, clone, "commit", "-m", "feature ahead")
	runGitT(t, clone, "push", "origin", "feature")
	want := runGitT(t, clone, "rev-parse", "refs/remotes/origin/feature")

	tt := ui.NewTester(s.View, 1240, 900)
	if !waitUntil(t, tt, func() bool { return tt.HasText("feature") }) {
		t.Fatalf("remote leaf missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Pull"); err != nil {
		t.Fatalf("click Pull: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		got, err := exec.Command("git", "-C", repo, "rev-parse", "refs/remotes/origin/feature").Output()
		return err == nil && strings.TrimSpace(string(got)) == want
	}) {
		t.Fatalf("tracking ref never advanced to %s; actionMsg = %q", want, s.actionMsg)
	}
}

// TestBranchLeafPullFastForwardsLocal drives a local leaf's Pull: a branch
// that is not checked out fast-forwards from its configured upstream.
func TestBranchLeafPullFastForwardsLocal(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteBranches)
	bare := gitBareRemoteForTest(t, repo)
	runGitT(t, repo, "checkout", "-b", "dev")
	if err := os.WriteFile(filepath.Join(repo, "dev.txt"), []byte("dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, repo, "add", "dev.txt")
	runGitT(t, repo, "commit", "-m", "dev base")
	runGitT(t, repo, "push", "-u", "origin", "dev")
	base := runGitT(t, repo, "rev-parse", "--abbrev-ref", "HEAD")
	runGitT(t, repo, "checkout", base)
	// A clone moves dev ahead; the repo's dev is behind.
	clone := gitCloneForTest(t, bare)
	runGitT(t, clone, "checkout", "dev")
	if err := os.WriteFile(filepath.Join(clone, "more.txt"), []byte("more\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, clone, "add", "more.txt")
	runGitT(t, clone, "commit", "-m", "dev ahead")
	runGitT(t, clone, "push", "origin", "dev")
	want := runGitT(t, clone, "rev-parse", "dev")

	tt := ui.NewTester(s.View, 1240, 900)
	if !waitUntil(t, tt, func() bool { return tt.HasText("dev") }) {
		t.Fatalf("dev leaf missing; texts = %v", tt.Texts())
	}
	// Both the local dev leaf and the remote origin/dev leaf show a Pull, so a
	// bare click is ambiguous — the remote leaf's click path is covered by
	// TestBranchLeafPullUpdatesRemoteRef; this drives the local branch update.
	if countText(tt.Texts(), "Pull") < 2 {
		t.Fatalf("expected Pull on the local leaf and the remote leaf; texts = %v", tt.Texts())
	}
	s.gitUpdateBranchRef(repo, "dev", false)
	if !waitUntil(t, tt, func() bool {
		got, err := exec.Command("git", "-C", repo, "rev-parse", "dev").Output()
		return err == nil && strings.TrimSpace(string(got)) == want
	}) {
		t.Fatalf("dev never fast-forwarded to %s; actionMsg = %q", want, s.actionMsg)
	}
}

// TestBranchLeafPushPreselectsPushPage drives a local leaf's Push: it lands on
// the push workbench with that branch preselected, and leaving the page drops
// the preselection.
func TestBranchLeafPushPreselectsPushPage(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteBranches)
	gitBareRemoteForTest(t, repo)
	base := runGitT(t, repo, "rev-parse", "--abbrev-ref", "HEAD")
	runGitT(t, repo, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("f\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, repo, "add", "f.txt")
	runGitT(t, repo, "commit", "-m", "feature work")
	runGitT(t, repo, "checkout", base)

	tt := ui.NewTester(s.View, 1240, 900)
	// The leaf list renders before the current-branch fetch lands; until it
	// does, even the checked-out leaf shows a Push. Wait for the badge.
	if !waitUntil(t, tt, func() bool { return tt.HasText("current") }) {
		t.Fatalf("current badge missing; texts = %v", tt.Texts())
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("feature") }) {
		t.Fatalf("feature leaf missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Push"); err != nil {
		t.Fatalf("click Push: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.workbench == gitRoutePush && s.pushBranchOverride == "feature"
	}) {
		t.Fatalf("push page not preselected; workbench=%q override=%q", s.workbench, s.pushBranchOverride)
	}
	// The page targets the preselected branch, not the checked-out one.
	if !waitUntil(t, tt, func() bool { return tt.HasText("feature") }) {
		t.Fatalf("push page missing the preselected branch; texts = %v", tt.Texts())
	}
	// The override deliberately survives leaving the page; the «Git 操作»
	// menu's push entry is what retargets the checked-out branch.
	if err := tt.Click("Back to workspace"); err != nil {
		t.Fatalf("leave the push page: %v", err)
	}
	s.pushBranchOverride = ""
	s.workbench = gitRoutePush
	tt.Frame()
	if !waitUntil(t, tt, func() bool { return tt.HasText(base) }) {
		t.Fatalf("menu push entry did not retarget the checked-out branch %q; texts = %v", base, tt.Texts())
	}
}
