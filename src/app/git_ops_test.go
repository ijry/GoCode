package app

// git_ops_test.go covers the four git-operation pages — Commit / Stash / Push /
// Merge — the single-window stand-in for upstream's separate windows. Each
// fixture builds a REAL git repository on disk and points the shell's folder at
// it, so the pages drive the same git_status / git_stash_* / git_push_info /
// git_merge backend routes the app uses, not a stub.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
