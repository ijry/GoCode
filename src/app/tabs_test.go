package app

// tabs_test.go covers the conversation tab strip (upstream's TabBar /
// tab-store): the open set hydrates from list_opened_tabs, a tab switch
// focuses its conversation, the close verbs work, and the persisted set is
// written back through save_opened_tabs — all driven against the REAL backend
// stack (like integration_test.go), so the CAS save really round-trips.
//
// The right-hand aux panel's tabs (Session | Files | Changes | Commits) are
// covered here too, since both strips are part of the same workspace skeleton.

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// tabsShellForTest opens a folder with two real conversations, persists them
// as the open set THROUGH THE BACKEND (save_opened_tabs, the versioned CAS
// pair), and returns a Shell that hydrates its strip from list_opened_tabs on
// the first frame. The sidebar is left on the Folders segment so a tab's title
// is not shadowed by the identical sidebar row text — a click on a title then
// unambiguously targets the strip.
func tabsShellForTest(t *testing.T) (*Shell, string, string, string) {
	t.Helper()
	b := newBackendForTest(t)

	fraw, err := b.raw("add_folder_to_history", map[string]any{"path": t.TempDir()})
	if err != nil {
		t.Fatalf("add_folder: %v", err)
	}
	var folder map[string]any
	_ = json.Unmarshal(fraw, &folder)
	fid := fmt.Sprintf("%v", folder["id"])

	titles := []string{"Fix the parser", "Refactor auth"}
	ids := make([]string, 0, len(titles))
	for _, title := range titles {
		craw, err := b.raw("create_conversation", map[string]any{"folderId": folder["id"], "agentType": "claude_code"})
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
		ids = append(ids, fmt.Sprintf("%v", int64(id)))
	}

	// Persist the two-conversation open set the way a previous session would
	// have, so hydration reads it back through the real endpoint.
	if _, err := b.raw("save_opened_tabs", map[string]any{
		"expectedVersion": 0,
		"items": []map[string]any{
			{"folder_id": folder["id"], "conversation_id": ids[0], "agent_type": "claude_code", "position": 0, "is_active": true, "is_pinned": false},
			{"folder_id": folder["id"], "conversation_id": ids[1], "agent_type": "claude_code", "position": 1, "is_active": false, "is_pinned": false},
		},
	}); err != nil {
		t.Fatalf("save_opened_tabs: %v", err)
	}

	s := New(b)
	s.stream = newStreamForTest() // keep ensureStream from dialing
	s.selectedFolder = fid
	s.sidebarTab = "folders" // keep a tab title unambiguous in the tester
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["folders"] = json.RawMessage(`[{"id":1,"name":"demo","path":"/tmp/demo"}]`)
	s.cache["agents"] = json.RawMessage(`[{"agent_type":"claude_code"}]`)
	return s, fid, ids[0], ids[1]
}

// TestConversationTabsHydrateAndSwitch hydrates the strip from the persisted
// set, checks both titles render, and switches the active tab by clicking the
// other one — which also moves the selected conversation.
func TestConversationTabsHydrateAndSwitch(t *testing.T) {
	s, _, first, second := tabsShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)

	// Both tabs render, decorated with their live titles.
	for _, want := range []string{"Fix the parser", "Refactor auth"} {
		if !tt.HasText(want) {
			t.Fatalf("tab strip missing %q; texts = %v", want, tt.Texts())
		}
	}
	if s.selectedConv != first {
		t.Fatalf("selectedConv = %q, want the active tab %q", s.selectedConv, first)
	}
	// The terminal toggle rides the strip's tail.
	if !tt.HasText("Terminal") {
		t.Errorf("terminal toggle missing from the strip; texts = %v", tt.Texts())
	}

	// Click the second tab: it becomes the selected conversation.
	if err := tt.Click("Refactor auth"); err != nil {
		t.Fatalf("click second tab: %v", err)
	}
	tt.Frame()
	if s.selectedConv != second {
		t.Fatalf("after switch, selectedConv = %q, want %q", s.selectedConv, second)
	}
	dumpFrame(t, tt, "conv-tabs")
}

// TestConversationTabClose drives a tab's close affordance and asserts the
// neighbor takes over the focus and the persisted set shrinks.
func TestConversationTabClose(t *testing.T) {
	s, _, _, second := tabsShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)

	// Close the ACTIVE first tab; the second should take over.
	if err := tt.Click("Close tab"); err != nil {
		t.Fatalf("click close: %v", err)
	}
	tt.Frame()
	s.mu.Lock()
	n := len(s.openTabs)
	s.mu.Unlock()
	if n != 1 {
		t.Fatalf("after close, %d tabs open, want 1", n)
	}
	if s.selectedConv != second {
		t.Fatalf("after closing the active tab, selectedConv = %q, want %q", s.selectedConv, second)
	}
	if tt.HasText("Fix the parser") {
		t.Errorf("the closed tab is still rendered; texts = %v", tt.Texts())
	}
}

// TestConversationTabContextMenu checks the right-click close verbs against a
// tab, and that "Close Others" leaves exactly the clicked tab open.
func TestConversationTabCloseOthers(t *testing.T) {
	s, _, _, second := tabsShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)

	if err := tt.RightClick("Refactor auth"); err != nil {
		t.Fatalf("right-click a tab: %v", err)
	}
	menu := tt.Menu()
	if len(menu) == 0 {
		t.Fatalf("no context menu on a tab")
	}
	if err := tt.ChooseMenuItem("Close Others"); err != nil {
		t.Fatalf("choose Close Others: %v", err)
	}
	tt.Frame()
	s.mu.Lock()
	n := len(s.openTabs)
	s.mu.Unlock()
	if n != 1 {
		t.Fatalf("Close Others left %d tabs, want 1", n)
	}
	if s.selectedConv != second {
		t.Fatalf("Close Others selected %q, want %q", s.selectedConv, second)
	}
	if tt.HasText("Fix the parser") {
		t.Errorf("a non-kept tab survived Close Others; texts = %v", tt.Texts())
	}
}

// TestConversationTabsPersist asserts a tab switch is written back through
// save_opened_tabs: after the save settles, the backend's active tab is the one
// the user switched to.
func TestConversationTabsPersist(t *testing.T) {
	s, _, first, second := tabsShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)

	if err := tt.Click("Refactor auth"); err != nil {
		t.Fatalf("click second tab: %v", err)
	}
	// The save runs off the UI thread; pump until the backend reports the
	// second conversation as the active tab.
	raw, err := func() (json.RawMessage, error) {
		for i := 0; i < 200; i++ {
			tt.Frame()
			r, e := s.B.raw("list_opened_tabs", nil)
			if e != nil {
				return nil, e
			}
			if page := asObj(r); page != nil {
				for _, it := range asListVal(page["items"]) {
					if fmt.Sprintf("%v", it["conversation_id"]) == second && truthy(it["is_active"]) {
						return r, nil
					}
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		return nil, fmt.Errorf("save never settled")
	}()
	if err != nil {
		t.Fatalf("persisted active tab: %v (first=%s second=%s)", err, first, second)
	}
	page := asObj(raw)
	if got := len(asListVal(page["items"])); got != 2 {
		t.Fatalf("persisted %d tabs, want 2", got)
	}
}

// TestAuxPanelTabs covers the right-hand aux panel's four tabs and that
// switching to Files renders the folder's file tree.
func TestAuxPanelTabs(t *testing.T) {
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.selectedFolder = "1"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["folders"] = json.RawMessage(`[{"id":1,"name":"demo","path":"/tmp/demo"}]`)
	s.cache["ft:/tmp/demo"] = json.RawMessage(`[{"kind":"file","name":"main.go","path":"main.go"}]`)

	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{"Session", "Files", "Changes", "Commits"} {
		if !tt.HasText(want) {
			t.Fatalf("aux panel missing tab %q; texts = %v", want, tt.Texts())
		}
	}
	if err := tt.Click("Files"); err != nil {
		t.Fatalf("click Files tab: %v", err)
	}
	tt.Frame()
	if s.auxTab != "files" {
		t.Errorf("auxTab = %q, want files", s.auxTab)
	}
	if !tt.HasText("main.go") {
		t.Errorf("files tab content missing; texts = %v", tt.Texts())
	}
}
