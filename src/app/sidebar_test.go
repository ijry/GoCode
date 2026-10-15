package app

// sidebar_test.go covers UI_GAPS P1-6: the workspace sidebar's conversation
// search box and its right-click menu (Rename / Pin↔Unpin / Delete). The
// fixture drives the REAL backend stack (like integration_test.go) so the
// menu's Rename/Delete actions run against real rows, not a stub.

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// sidebarShellForTest opens a folder with three real conversations — the last
// one pinned (so it lives on the Pinned tab) — and returns a Shell whose
// sidebar cache is pre-seeded with the real list, so the first frame renders
// synchronously. It returns the two Recent titles and the pinned title.
func sidebarShellForTest(t *testing.T) (*Shell, []string, string) {
	t.Helper()
	b := newBackendForTest(t)

	fraw, err := b.raw("add_folder_to_history", map[string]any{"path": t.TempDir()})
	if err != nil {
		t.Fatalf("add_folder: %v", err)
	}
	var folder map[string]any
	_ = json.Unmarshal(fraw, &folder)
	folderID := folder["id"]

	recent := []string{"Fix the parser", "Refactor auth"}
	pinnedTitle := "Write tests"
	titles := append(append([]string{}, recent...), pinnedTitle)
	ids := make([]float64, 0, len(titles))
	for _, title := range titles {
		craw, err := b.raw("create_conversation", map[string]any{"folderId": folderID, "agentType": "claude_code"})
		if err != nil {
			t.Fatalf("create_conversation: %v", err)
		}
		var id float64
		if err := json.Unmarshal(craw, &id); err != nil {
			t.Fatalf("create_conversation returned %s, want a bare id: %v", craw, err)
		}
		if _, err := b.raw("update_conversation_title", map[string]any{"conversationId": id, "title": title}); err != nil {
			t.Fatalf("rename %q: %v", title, err)
		}
		ids = append(ids, id)
	}
	// Pin the last one so it moves to the Pinned tab and its menu offers Unpin.
	if _, err := b.raw("update_conversation_pinned", map[string]any{"conversationId": ids[len(ids)-1], "pinned": true}); err != nil {
		t.Fatalf("pin: %v", err)
	}

	s := New(b)
	s.stream = newStreamForTest() // keep ensureStream from dialing
	fid := fmt.Sprintf("%v", folderID)
	s.selectedFolder = fid
	listRaw, err := b.raw("list_conversations", map[string]any{"folderId": folderID})
	if err != nil {
		t.Fatalf("list_conversations: %v", err)
	}
	s.cache["convs:"+fid] = listRaw
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["folders"] = json.RawMessage(`[{"id":1,"name":"demo","path":"/tmp/demo"}]`)
	s.cache["agents"] = json.RawMessage(`[{"agent_type":"claude_code"}]`)
	return s, recent, pinnedTitle
}

// TestSidebarSearchFiltersConversations types a term and asserts the sidebar
// narrows to matching titles, the clear control restores the full list, and a
// no-match term shows the empty hint.
func TestSidebarSearchFiltersConversations(t *testing.T) {
	s, recent, pinnedTitle := sidebarShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, title := range recent {
		if !tt.HasText(title) {
			t.Fatalf("sidebar missing %q before search; texts = %v", title, tt.Texts())
		}
	}
	// The pinned conversation is not on the Recent tab.
	if tt.HasText(pinnedTitle) {
		t.Errorf("pinned %q leaked into Recent; texts = %v", pinnedTitle, tt.Texts())
	}

	// Narrow to the one title containing "parser".
	s.sidebarSearch = "parser"
	tt.Frame()
	if !tt.HasText("Fix the parser") {
		t.Errorf("search dropped the matching row; texts = %v", tt.Texts())
	}
	if tt.HasText("Refactor auth") {
		t.Errorf("search kept non-matching %q; texts = %v", "Refactor auth", tt.Texts())
	}

	// The ✕ clears the term and the whole list returns.
	if err := tt.Click("✕"); err != nil {
		t.Fatalf("click clear: %v", err)
	}
	tt.Frame()
	if s.sidebarSearch != "" {
		t.Errorf("clear left the term %q", s.sidebarSearch)
	}
	for _, title := range recent {
		if !tt.HasText(title) {
			t.Errorf("after clear, %q is missing; texts = %v", title, tt.Texts())
		}
	}

	// A term that matches nothing shows the empty hint, not a stale row.
	s.sidebarSearch = "zzz-nope"
	tt.Frame()
	if !tt.HasText("No matches") {
		t.Errorf("no-match hint missing; texts = %v", tt.Texts())
	}
	if tt.HasText("Fix the parser") {
		t.Errorf("a non-matching row survived; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "sidebar-search")
}

// TestSidebarContextMenuRename right-clicks a conversation, checks the menu
// items and the pin-state variant, and drives Rename into the header editor.
func TestSidebarContextMenuRename(t *testing.T) {
	s, _, pinnedTitle := sidebarShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)

	// An unpinned Recent row offers "Pin".
	if err := tt.RightClick("Fix the parser"); err != nil {
		t.Fatalf("right-click unpinned row: %v", err)
	}
	if got, want := tt.Menu(), []string{"Rename", "Pin", "-", "Delete"}; !slices.Equal(got, want) {
		t.Fatalf("menu %v, want %v", got, want)
	}
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatalf("choose Rename: %v", err)
	}
	if !s.renaming {
		t.Errorf("Rename did not open the inline editor")
	}
	if s.selectedConv == "" {
		t.Errorf("Rename did not select the row")
	}
	tt.Frame()
	if !tt.HasText("Save") {
		t.Errorf("rename editor missing its Save action; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "sidebar-menu-rename")

	// The pinned row (on the Pinned tab) offers "Unpin" instead.
	if err := tt.Click("Pinned"); err != nil {
		t.Fatalf("click Pinned: %v", err)
	}
	tt.Frame()
	if err := tt.RightClick(pinnedTitle); err != nil {
		t.Fatalf("right-click pinned row: %v", err)
	}
	if got, want := tt.Menu(), []string{"Rename", "Unpin", "-", "Delete"}; !slices.Equal(got, want) {
		t.Fatalf("pinned menu %v, want %v", got, want)
	}
}

// TestSidebarContextMenuDelete deletes a conversation through the right-click
// menu and asserts the row goes away while the others stay — proving the menu
// wires to the same delete the detail header uses.
func TestSidebarContextMenuDelete(t *testing.T) {
	s, _, _ := sidebarShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Fix the parser") {
		t.Fatalf("precondition: row not shown; texts = %v", tt.Texts())
	}
	if err := tt.RightClick("Fix the parser"); err != nil {
		t.Fatalf("right-click: %v", err)
	}
	if err := tt.ChooseMenuItem("Delete"); err != nil {
		t.Fatalf("choose Delete: %v", err)
	}
	// Delete runs off the UI thread and the list re-fetches; pump frames until
	// the list settles — the deleted row gone AND the surviving one back. (The
	// cache is cleared mid-flight, so a bare "row gone" check would pass on the
	// transient empty frame before the re-fetch lands.)
	settled := false
	for i := 0; i < 200; i++ {
		tt.Frame()
		if !tt.HasText("Fix the parser") && tt.HasText("Refactor auth") {
			settled = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !settled {
		t.Fatalf("list did not settle after delete; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "sidebar-menu-delete")
}
