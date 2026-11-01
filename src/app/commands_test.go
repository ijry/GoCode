package app

// commands_test.go covers the folder-command runner (workbench route
// "commands"): the folderless hint, the empty + populated list, create, and
// bootstrap-from-package.json.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// commandsShellForTest builds a Shell with the commands workbench active and a
// pre-seeded folder so folderPathByID resolves (the panel is folder-scoped).
func commandsShellForTest(t *testing.T, root string) *Shell {
	t.Helper()
	b := newBackendForTest(t)
	s := New(b)
	s.stream = newStreamForTest()
	s.workbench = "commands"
	s.selectedFolder = "1"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["folders"] = json.RawMessage(`[{"id":1,"path":` + jsonQuote(root) + `}]`)
	return s
}

// jsonQuote is a tiny JSON string encoder for test seeds.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestCommandsNoFolder shows the folderless hint when no folder is selected.
func TestCommandsNoFolder(t *testing.T) {
	b := newBackendForTest(t)
	s := New(b)
	s.stream = newStreamForTest()
	s.workbench = "commands"
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Select a folder to manage its commands.") {
		t.Errorf("folderless hint missing; texts = %v", tt.Texts())
	}
}

// TestCommandsRendersEmpty shows the new-command form and the honest empty state.
func TestCommandsRendersEmpty(t *testing.T) {
	dir := t.TempDir()
	s := commandsShellForTest(t, dir)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{
		"Folder commands",
		"New command",
		"Create",
		"Bootstrap from package.json",
		"No commands yet.",
	} {
		if !tt.HasText(want) {
			t.Errorf("commands frame missing %q; texts = %v", want, tt.Texts())
		}
	}
}

// settleCommands lets the initial list fetch land so a later mutation's
// cache-delete isn't clobbered by the still-in-flight first fetch writing a
// stale empty result (the list only refetches when its cache key is absent).
func settleCommands(t *testing.T, s *Shell, tt *ui.Tester) {
	t.Helper()
	key := folderCommandKey(s.selectedFolder)
	for i := 0; i < 80; i++ {
		tt.Frame()
		s.mu.Lock()
		_, ok := s.cache[key]
		s.mu.Unlock()
		if ok {
			return
		}
	}
	t.Fatalf("commands list never settled; texts = %v", tt.Texts())
}

// TestCommandsCreate adds a command through the real backend and waits for the
// row to appear in the list.
func TestCommandsCreate(t *testing.T) {
	dir := t.TempDir()
	s := commandsShellForTest(t, dir)
	tt := ui.NewTester(s.View, 1240, 820)
	settleCommands(t, s, tt)
	s.cmdNewName = "lint"
	s.cmdNewCmd = "pnpm lint"
	tt.Frame()
	if err := tt.Click("Create"); err != nil {
		t.Fatalf("click Create: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("lint") }) {
		t.Fatalf("created command never appeared; texts = %v", tt.Texts())
	}
}

// TestCommandsDelete removes a created command and waits for its row to leave.
func TestCommandsDelete(t *testing.T) {
	dir := t.TempDir()
	s := commandsShellForTest(t, dir)
	tt := ui.NewTester(s.View, 1240, 820)
	settleCommands(t, s, tt)
	s.cmdNewName = "build"
	s.cmdNewCmd = "pnpm build"
	tt.Frame()
	if err := tt.Click("Create"); err != nil {
		t.Fatalf("click Create: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("build") }) {
		t.Fatalf("created command never appeared; texts = %v", tt.Texts())
	}
	if err := tt.Click("Delete"); err != nil {
		t.Fatalf("click Delete: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return !tt.HasText("build") }) {
		t.Fatalf("deleted command row never left; texts = %v", tt.Texts())
	}
}

// TestCommandsBootstrap imports package.json scripts through the real backend.
func TestCommandsBootstrap(t *testing.T) {
	dir := t.TempDir()
	pkg := `{"name":"x","scripts":{"dev":"vite","build":"vite build","test":"vitest"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	s := commandsShellForTest(t, dir)
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Bootstrap from package.json"); err != nil {
		t.Fatalf("click Bootstrap: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("Imported 3 scripts") }) {
		t.Fatalf("bootstrap result never appeared; texts = %v", tt.Texts())
	}
}
