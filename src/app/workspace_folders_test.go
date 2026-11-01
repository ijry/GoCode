package app

// workspace_folders_test.go covers the Workspace folders panel: the render, a
// real folder link create/remove (junctions on Windows), and a real multipart
// upload into the workspace.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// wfShellForTest registers a temp workspace folder and returns the shell, the
// workspace root, and a separate target dir that can be linked in.
func wfShellForTest(t *testing.T) (*Shell, *Backend, string, string) {
	t.Helper()
	b := newBackendForTest(t)
	root := t.TempDir()
	target := t.TempDir()
	fraw, err := b.raw("add_folder_to_history", map[string]any{"path": root})
	if err != nil {
		t.Fatalf("add_folder: %v", err)
	}
	var folder map[string]any
	_ = json.Unmarshal(fraw, &folder)
	fid := fmt.Sprintf("%v", folder["id"])
	s := New(b)
	s.stream = newStreamForTest()
	s.workbench = "workspace-folders"
	s.selectedFolder = fid
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["folders"] = json.RawMessage(fmt.Sprintf(`[{"id":%s,"name":"root","path":%s}]`, fid, jsonQuote(root)))
	s.cache[wfCacheKey(fid)] = json.RawMessage(`[]`)
	return s, b, root, target
}

// TestWorkspaceFoldersRenders shows the link + upload forms and the empty state.
func TestWorkspaceFoldersRenders(t *testing.T) {
	s, _, _, _ := wfShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{
		"Workspace folders",
		"Link a folder",
		"Add link",
		"No linked folders yet.",
		"Upload a file",
		"Choose file…",
	} {
		if !tt.HasText(want) {
			t.Errorf("workspace-folders frame missing %q; texts = %v", want, tt.Texts())
		}
	}
}

// TestWorkspaceFoldersCreateLink previews a real directory, confirms the plan,
// and waits for its row.
func TestWorkspaceFoldersCreateLink(t *testing.T) {
	s, _, root, target := wfShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	s.wfTarget = target
	s.wfName = "shared"
	tt.Frame()
	if err := tt.Click("Add link"); err != nil {
		t.Fatalf("click Add link: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("Ready to link") }) {
		t.Fatalf("preview never appeared; texts = %v", tt.Texts())
	}
	if !tt.HasText("Keep links out of git status") {
		t.Errorf("preview missing the git-exclude toggle; texts = %v", tt.Texts())
	}
	if err := tt.Click("Link 1 folder(s)"); err != nil {
		t.Fatalf("click Link 1 folder(s): %v", err)
	}
	link := filepath.Join(root, "shared")
	if !waitUntil(t, tt, func() bool {
		_, err := os.Lstat(link)
		return err == nil
	}) {
		t.Fatalf("link not created on disk")
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("shared") }) {
		t.Fatalf("created link never appeared; texts = %v", tt.Texts())
	}
}

// TestWorkspaceFoldersPreviewRejects rejects a target that is the workspace
// itself, keeps the reason until dismissed, and links nothing.
func TestWorkspaceFoldersPreviewRejects(t *testing.T) {
	s, _, root, _ := wfShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	s.wfTarget = root
	tt.Frame()
	if err := tt.Click("Add link"); err != nil {
		t.Fatalf("click Add link: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		return tt.HasText("This is the workspace folder itself")
	}) {
		t.Fatalf("rejection never appeared; texts = %v", tt.Texts())
	}
	if tt.HasText("Ready to link") {
		t.Errorf("rejected plan offered a confirm step")
	}
	if !tt.HasText("No linked folders yet.") {
		t.Errorf("rejected target produced a link row; texts = %v", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatalf("click Cancel: %v", err)
	}
	tt.Frame()
	if tt.HasText("This is the workspace folder itself") {
		t.Errorf("rejection row survived Cancel")
	}
}

// TestWorkspaceFoldersPreviewAlreadyLinked reports the existing link's name.
func TestWorkspaceFoldersPreviewAlreadyLinked(t *testing.T) {
	s, b, _, target := wfShellForTest(t)
	if _, err := b.raw("create_folder_links", map[string]any{
		"folderId": s.selectedFolder, "items": []any{map[string]any{"path": target, "name": "shared"}},
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	raw, _ := b.raw("list_folder_links", map[string]any{"folderId": s.selectedFolder})
	s.cache[wfCacheKey(s.selectedFolder)] = raw
	s.wfTarget = target
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Add link"); err != nil {
		t.Fatalf("click Add link: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("Already linked as shared") }) {
		t.Fatalf("already-linked rejection missing; texts = %v", tt.Texts())
	}
}

// TestWorkspaceFoldersPreviewRenames shows the dedupe hint and creates the
// suffixed name the preview picked.
func TestWorkspaceFoldersPreviewRenames(t *testing.T) {
	s, _, root, target := wfShellForTest(t)
	base := filepath.Base(target)
	if err := os.Mkdir(filepath.Join(root, base), 0o755); err != nil {
		t.Fatalf("occupy base name: %v", err)
	}
	s.wfTarget = target
	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if err := tt.Click("Add link"); err != nil {
		t.Fatalf("click Add link: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		return tt.HasText("already exists in this folder")
	}) {
		t.Fatalf("rename hint missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Link 1 folder(s)"); err != nil {
		t.Fatalf("click Link 1 folder(s): %v", err)
	}
	renamed := filepath.Join(root, base+"-2")
	if !waitUntil(t, tt, func() bool {
		_, err := os.Lstat(renamed)
		return err == nil
	}) {
		t.Fatalf("renamed link %q not created", renamed)
	}
}

// TestWorkspaceFoldersRepairLink recreates a link after its entry vanished.
func TestWorkspaceFoldersRepairLink(t *testing.T) {
	s, b, root, target := wfShellForTest(t)
	if _, err := b.raw("create_folder_links", map[string]any{
		"folderId": s.selectedFolder, "items": []any{map[string]any{"path": target, "name": "gone"}},
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	link := filepath.Join(root, "gone")
	if err := os.Remove(link); err != nil {
		t.Fatalf("remove link entry: %v", err)
	}
	raw, _ := b.raw("list_folder_links", map[string]any{"folderId": s.selectedFolder})
	s.cache[wfCacheKey(s.selectedFolder)] = raw
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("missing") {
		t.Fatalf("missing status not shown; texts = %v", tt.Texts())
	}
	if err := tt.Click("Recreate link"); err != nil {
		t.Fatalf("click Recreate link: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		_, err := os.Lstat(link)
		return err == nil
	}) {
		t.Fatalf("link was not recreated on disk")
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("link recreated") }) {
		t.Fatalf("repair result never appeared; texts = %v", tt.Texts())
	}
}

// TestWorkspaceFoldersRemoveLink removes a link through the two-step confirm.
func TestWorkspaceFoldersRemoveLink(t *testing.T) {
	s, b, _, target := wfShellForTest(t)
	if _, err := b.raw("create_folder_links", map[string]any{
		"folderId": s.selectedFolder, "items": []any{map[string]any{"path": target, "name": "gone"}},
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	raw, _ := b.raw("list_folder_links", map[string]any{"folderId": s.selectedFolder})
	s.cache[wfCacheKey(s.selectedFolder)] = raw
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("gone") {
		t.Fatalf("seeded link missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Remove"); err != nil {
		t.Fatalf("click Remove: %v", err)
	}
	tt.Frame()
	if err := tt.Click("Confirm remove"); err != nil {
		t.Fatalf("click Confirm remove: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return !tt.HasText("gone") }) {
		t.Fatalf("removed link row never left; texts = %v", tt.Texts())
	}
}

// TestWorkspaceUploadFile uploads a real local file over multipart.
func TestWorkspaceUploadFile(t *testing.T) {
	s, _, root, _ := wfShellForTest(t)
	local := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(local, []byte("hi there\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.wfUploadPath = local
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Upload"); err != nil {
		t.Fatalf("click Upload: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("uploaded") }) {
		t.Fatalf("upload result never appeared; texts = %v", tt.Texts())
	}
	if _, err := os.Stat(filepath.Join(root, "hello.txt")); err != nil {
		t.Errorf("uploaded file missing in workspace: %v", err)
	}
}
