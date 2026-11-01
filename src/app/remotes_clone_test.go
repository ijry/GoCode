package app

// remotes_clone_test.go covers the Remotes git page (list/add/remove) and the
// Clone repository page. Both drive real git against temp repositories.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// settleCache pumps frames (yielding to the fetch goroutine) until a cache key
// is present, so a later mutation's cache-delete isn't clobbered by the
// still-in-flight initial fetch.
func settleCache(t *testing.T, s *Shell, tt *ui.Tester, key string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		tt.Frame()
		s.mu.Lock()
		_, ok := s.cache[key]
		s.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cache %q never settled; err=%q texts = %v", key, s.errFor(key), tt.Texts())
}

// TestGitRemotesEmpty shows the add form and the empty state on a repo with no
// remotes.
func TestGitRemotesEmpty(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteRemotes)
	tt := ui.NewTester(s.View, 1240, 820)
	settleCache(t, s, tt, gitRemotesKey(repo))
	for _, want := range []string{
		"Remotes",
		"Add remote",
		"Add",
		"No git remotes configured",
		"Add a remote (for example origin)",
	} {
		if !tt.HasText(want) {
			t.Errorf("remotes empty frame missing %q; texts = %v", want, tt.Texts())
		}
	}
}

// TestGitRemotesAdd adds a remote and waits for its row to appear.
func TestGitRemotesAdd(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteRemotes)
	tt := ui.NewTester(s.View, 1240, 820)
	settleCache(t, s, tt, gitRemotesKey(repo))
	s.remoteNewName = "origin"
	s.remoteNewURL = "https://example.com/x.git"
	tt.Frame()
	if err := tt.Click("Add"); err != nil {
		t.Fatalf("click Add: %v", err)
	}
	// The URL only appears in a real remote row (the empty-state hint says
	// "for example origin", so "origin" alone is ambiguous).
	if !waitUntil(t, tt, func() bool { return tt.HasText("https://example.com/x.git") }) {
		t.Fatalf("added remote never appeared; texts = %v", tt.Texts())
	}
}

// TestGitRemotesRemove removes a remote through the two-step confirm.
func TestGitRemotesRemove(t *testing.T) {
	s, repo := gitTestRepo(t, gitRouteRemotes)
	// Seed one remote directly so the remove path is the only thing under test.
	if out, err := exec.Command("git", "-C", repo, "remote", "add", "origin", "https://example.com/x.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v (%s)", err, out)
	}
	tt := ui.NewTester(s.View, 1240, 820)
	settleCache(t, s, tt, gitRemotesKey(repo))
	if !tt.HasText("https://example.com/x.git") {
		t.Fatalf("seeded remote missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Remove"); err != nil {
		t.Fatalf("click Remove: %v", err)
	}
	tt.Frame()
	if err := tt.Click("Confirm remove"); err != nil {
		t.Fatalf("click Confirm remove: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return !tt.HasText("https://example.com/x.git") }) {
		t.Fatalf("removed remote row never left; texts = %v", tt.Texts())
	}
}

// TestClonePageRenders shows the clone form.
func TestClonePageRenders(t *testing.T) {
	b := newBackendForTest(t)
	s := New(b)
	s.stream = newStreamForTest()
	s.workbench = "clone"
	s.cache["appearance-mode"] = []byte(`{"mode":"light"}`)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{
		"Clone repository",
		"Repository URL",
		"Target directory",
		"Clone",
	} {
		if !tt.HasText(want) {
			t.Errorf("clone frame missing %q; texts = %v", want, tt.Texts())
		}
	}
}

// TestCloneRepository drives a real local clone and waits for the success line.
func TestCloneRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	// Source repo with one commit.
	src := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.name", "Tester"},
		{"config", "user.email", "t@e.st"},
		{"config", "commit.gpgsign", "false"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", src}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "a.txt"}, {"commit", "-m", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", src}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}

	target := filepath.Join(t.TempDir(), "cloned")
	b := newBackendForTest(t)
	s := New(b)
	s.stream = newStreamForTest()
	s.workbench = "clone"
	s.cache["appearance-mode"] = []byte(`{"mode":"light"}`)
	s.cloneURL = src
	s.cloneTarget = target
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Clone"); err != nil {
		t.Fatalf("click Clone: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("Cloned to") }) {
		t.Fatalf("clone result never appeared; texts = %v", tt.Texts())
	}
	if _, err := os.Stat(filepath.Join(target, "a.txt")); err != nil {
		t.Errorf("cloned file missing: %v", err)
	}
}
