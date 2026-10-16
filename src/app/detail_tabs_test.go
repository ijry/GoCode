package app

// detail_tabs_test.go covers the workspace's tab regions: the main area's tabs
// (Chat | Terminal) and the right-hand aux panel's tabs (Session | Files |
// Changes | Commits), mirroring upstream's workspace skeleton.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestDetailTabsSwitch(t *testing.T) {
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.selectedFolder = "1"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["folders"] = json.RawMessage(`[{"id":1,"name":"demo","path":"/tmp/demo"}]`)
	s.cache["ft:/tmp/demo"] = json.RawMessage(`[{"kind":"file","name":"main.go","path":"main.go"}]`)

	tt := ui.NewTester(s.View, 1240, 820)
	// The main-area tabs and the aux panel's tabs are both present.
	for _, want := range []string{"Chat", "Terminal", "Session", "Files", "Changes", "Commits"} {
		if !tt.HasText(want) {
			t.Fatalf("tab strip missing %q; texts = %v", want, tt.Texts())
		}
	}
	// Switching the aux panel to Files renders the folder's files.
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
