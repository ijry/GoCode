package app

// commands_test.go covers the folder-command runner (workbench route
// "commands"): the folderless hint, the empty + populated list, create, and
// bootstrap-from-package.json.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// TestCommandsReorder drives a row's Move up: the new order persists through
// reorder_folder_commands (only the non-first row carries the button, so the
// click is unambiguous with two rows).
func TestCommandsReorder(t *testing.T) {
	dir := t.TempDir()
	s := commandsShellForTest(t, dir)
	tt := ui.NewTester(s.View, 1240, 820)
	settleCommands(t, s, tt)
	for _, c := range [][2]string{{"first", "echo one"}, {"second", "echo two"}} {
		s.cmdNewName, s.cmdNewCmd = c[0], c[1]
		tt.Frame()
		if err := tt.Click("Create"); err != nil {
			t.Fatalf("click Create: %v", err)
		}
		if !waitUntil(t, tt, func() bool { return tt.HasText(c[0]) }) {
			t.Fatalf("created command %q never appeared; texts = %v", c[0], tt.Texts())
		}
	}
	order := func() string {
		raw, err := s.B.raw("list_folder_commands", map[string]any{"folderId": numOrString(s.selectedFolder)})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		names := []string{}
		for _, m := range asList(raw) {
			names = append(names, fmt.Sprintf("%v", m["name"]))
		}
		return strings.Join(names, ",")
	}
	if got := order(); got != "first,second" {
		t.Fatalf("initial order = %q, want first,second", got)
	}
	if err := tt.Click("Move up"); err != nil {
		t.Fatalf("click Move up: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return order() == "second,first" }) {
		t.Fatalf("reorder did not persist; order = %q actionMsg = %q", order(), s.actionMsg)
	}
	// The swapped row now sits first and has no Move up of its own.
	if !waitUntil(t, tt, func() bool { return countText(tt.Texts(), "Move up") == 1 }) {
		t.Fatalf("move buttons wrong after reorder; texts = %v", tt.Texts())
	}
}
