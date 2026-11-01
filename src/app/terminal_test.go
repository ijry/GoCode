package app

// terminal_test.go covers the terminal panel: the start prompt, the live
// snapshot with its output, and the no-folder hint. Snapshot responses are
// pre-seeded so the view renders without spawning a real PTY.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func terminalShellForTest(t *testing.T, termID string) *Shell {
	t.Helper()
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.workbench = ""
	s.termOpen = true
	s.selectedFolder = "1"
	// Pin the random empty-state tip (see conversationShellForTest).
	s.welcomeTip = 0
	if termID != "" {
		s.termTabs = []termTab{{
			ID: termID, Title: "Terminal 1", FolderID: "1", WorkingDir: "/tmp/demo",
		}}
		s.termActive = termID
		s.termCounter = 1
	}
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["folders"] = json.RawMessage(`[{"id":1,"name":"demo","path":"/tmp/demo"}]`)
	return s
}

func TestTerminalPanelShowsSnapshot(t *testing.T) {
	s := terminalShellForTest(t, "t1")
	s.cache["term:t1"] = json.RawMessage(`{"alive":true,"data":"$ echo hi\nhi\n","seq":5}`)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{"Terminal", "$ echo hi", "Send"} {
		if !tt.HasText(want) {
			t.Errorf("terminal panel missing %q; texts = %v", want, tt.Texts())
		}
	}
}

func TestTerminalPanelStartPrompt(t *testing.T) {
	s := terminalShellForTest(t, "")
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Start terminal") {
		t.Errorf("start prompt missing; texts = %v", tt.Texts())
	}
}

func TestTerminalPanelNoFolder(t *testing.T) {
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.termOpen = true
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Select a folder") {
		t.Errorf("no-folder hint missing; texts = %v", tt.Texts())
	}
}

// TestTerminalPanelCollapsedByDefault checks the panel header (the toggle) is
// always present, but the body is hidden until expanded — matching upstream's
// collapsible bottom bar.
func TestTerminalPanelCollapsedByDefault(t *testing.T) {
	s := terminalShellForTest(t, "t1")
	s.termOpen = false
	s.cache["term:t1"] = json.RawMessage(`{"alive":true,"data":"$ echo hi\nhi\n","seq":5}`)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Terminal") {
		t.Errorf("panel header missing; texts = %v", tt.Texts())
	}
	if countText(tt.Texts(), "Send") > 0 {
		t.Errorf("terminal body should be collapsed by default; texts = %v", tt.Texts())
	}
}

// TestTerminalPanelToggleExpands verifies clicking the header toggle reveals the
// terminal body.
func TestTerminalPanelToggleExpands(t *testing.T) {
	s := terminalShellForTest(t, "t1")
	s.termOpen = false
	s.cache["term:t1"] = json.RawMessage(`{"alive":true,"data":"$ echo hi\nhi\n","seq":5}`)
	tt := ui.NewTester(s.View, 1240, 820)
	if countText(tt.Texts(), "Send") > 0 {
		t.Fatalf("body should start collapsed; texts = %v", tt.Texts())
	}
	tt.Click("Toggle terminal panel")
	if !waitUntil(t, tt, func() bool { return countText(tt.Texts(), "Send") > 0 }) {
		t.Errorf("body did not expand after toggle; texts = %v", tt.Texts())
	}
}

// addTerminalTabForTest appends a second tab and its seeded snapshot without
// spawning a process.
func addTerminalTabForTest(s *Shell, id, title, data string) {
	s.termTabs = append(s.termTabs, termTab{
		ID: id, Title: title, FolderID: "1", WorkingDir: "/tmp/demo",
	})
	s.cache["term:"+id] = json.RawMessage(`{"alive":true,"data":"` + data + `","seq":9}`)
}

// TestTerminalTabsSwitchAndClose covers the tab strip: both tabs render, clicking
// a title focuses that PTY's snapshot, and its close affordance removes it and
// falls back to the neighbor.
func TestTerminalTabsSwitchAndClose(t *testing.T) {
	s := terminalShellForTest(t, "t1")
	addTerminalTabForTest(s, "t2", "Terminal 2", "beta")
	s.cache["term:t1"] = json.RawMessage(`{"alive":true,"data":"alpha","seq":4}`)
	tt := ui.NewTester(s.View, 1240, 820)

	for _, want := range []string{"Terminal 1", "Terminal 2", "New terminal tab", "alpha"} {
		if !tt.HasText(want) {
			t.Fatalf("terminal tab bar missing %q; texts = %v", want, tt.Texts())
		}
	}
	if tt.HasText("beta") {
		t.Fatalf("inactive tab output should be hidden; texts = %v", tt.Texts())
	}

	if err := tt.Click("Terminal 2"); err != nil {
		t.Fatalf("click second tab: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("beta") }) {
		t.Fatalf("second tab did not become active; texts = %v", tt.Texts())
	}
	if tt.HasText("alpha") {
		t.Fatalf("first tab output should now be hidden; texts = %v", tt.Texts())
	}

	if err := tt.Click("Close terminal tab: Terminal 2"); err != nil {
		t.Fatalf("close second tab: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return !tt.HasText("Terminal 2") && tt.HasText("alpha") }) {
		t.Fatalf("closing the active tab did not focus its neighbor; texts = %v", tt.Texts())
	}
	s.mu.Lock()
	active, count := s.termActive, len(s.termTabs)
	s.mu.Unlock()
	if active != "t1" || count != 1 {
		t.Fatalf("after close active=%q tabs=%d, want t1/1", active, count)
	}
}

// TestTerminalTabRename drives upstream's right-click Rename verb.
func TestTerminalTabRename(t *testing.T) {
	s := terminalShellForTest(t, "t1")
	s.cache["term:t1"] = json.RawMessage(`{"alive":true,"data":"alpha","seq":4}`)
	tt := ui.NewTester(s.View, 1240, 820)

	if err := tt.RightClick("Terminal 1"); err != nil {
		t.Fatalf("right-click tab: %v", err)
	}
	for _, want := range []string{"Rename", "-", "Close", "Close Others", "Close All"} {
		found := false
		for _, item := range tt.Menu() {
			if item == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("context menu missing %q; menu = %v", want, tt.Menu())
		}
	}
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatalf("choose Rename: %v", err)
	}
	s.termRenameDraft = "dev server"
	if err := tt.Click("Save terminal tab name"); err != nil {
		t.Fatalf("save rename: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("dev server") }) {
		t.Fatalf("renamed tab title not shown; texts = %v", tt.Texts())
	}
	s.mu.Lock()
	title := s.termTabs[0].Title
	s.mu.Unlock()
	if title != "dev server" {
		t.Fatalf("tab title = %q, want dev server", title)
	}
}

// TestTerminalTabCloseOthersAndAll covers the remaining close verbs.
func TestTerminalTabCloseOthersAndAll(t *testing.T) {
	s := terminalShellForTest(t, "t1")
	addTerminalTabForTest(s, "t2", "Terminal 2", "beta")
	addTerminalTabForTest(s, "t3", "Terminal 3", "gamma")
	s.cache["term:t1"] = json.RawMessage(`{"alive":true,"data":"alpha","seq":4}`)
	tt := ui.NewTester(s.View, 1240, 820)

	if err := tt.RightClick("Terminal 2"); err != nil {
		t.Fatalf("right-click middle tab: %v", err)
	}
	if err := tt.ChooseMenuItem("Close Others"); err != nil {
		t.Fatalf("choose Close Others: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return !tt.HasText("Terminal 1") && !tt.HasText("Terminal 3") }) {
		t.Fatalf("Close Others left tabs behind; texts = %v", tt.Texts())
	}
	if !tt.HasText("Terminal 2") {
		t.Fatalf("Close Others dropped the kept tab; texts = %v", tt.Texts())
	}

	if err := tt.RightClick("Terminal 2"); err != nil {
		t.Fatalf("right-click last tab: %v", err)
	}
	if err := tt.ChooseMenuItem("Close All"); err != nil {
		t.Fatalf("choose Close All: %v", err)
	}
	// Park the pointer off the strip: a tab's close button carries the tooltip
	// "Close terminal tab: Terminal 2", and HasText is a substring match, so a
	// tooltip left showing would keep the name alive for this assertion.
	tt.Move(2, 2)
	if !waitUntil(t, tt, func() bool { return !tt.HasText("Terminal 2") && countText(tt.Texts(), "Send") == 0 }) {
		t.Fatalf("Close All did not empty the strip; texts = %v", tt.Texts())
	}
	s.mu.Lock()
	open, count := s.termOpen, len(s.termTabs)
	s.mu.Unlock()
	if open || count != 0 {
		t.Fatalf("after Close All open=%v tabs=%d, want false/0", open, count)
	}
	if !tt.HasText("Terminal · /tmp/demo") {
		t.Fatalf("collapsed panel header missing after Close All; texts = %v", tt.Texts())
	}
}

// TestTerminalTabKeepsCreationFolder verifies worktree context: a tab stays
// bound to the folder/worktree it was opened in when the workspace selection
// moves to another folder.
func TestTerminalTabKeepsCreationFolder(t *testing.T) {
	s := terminalShellForTest(t, "t1")
	s.cache["folders"] = json.RawMessage(`[
		{"id":1,"name":"demo","path":"/tmp/demo"},
		{"id":2,"name":"feature","path":"/tmp/feature"}
	]`)
	s.selectedFolder = "2"
	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()

	s.mu.Lock()
	folder, dir := s.termTabs[0].FolderID, s.termTabs[0].WorkingDir
	s.mu.Unlock()
	if folder != "1" || dir != "/tmp/demo" {
		t.Fatalf("tab context = folder %q dir %q, want 1//tmp/demo", folder, dir)
	}
}

// TestTerminalTabBarLocalizes checks the tab strip's zh-Hans chrome.
func TestTerminalTabBarLocalizes(t *testing.T) {
	s := terminalShellForTest(t, "t1")
	s.cache["term:t1"] = json.RawMessage(`{"alive":true,"data":"alpha","seq":4}`)
	tt := ui.NewTester(s.View, 1240, 820)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	for _, want := range []string{"新建终端标签", "关闭终端标签：Terminal 1"} {
		if !tt.HasText(want) {
			t.Errorf("zh terminal tab bar missing %q; texts = %v", want, tt.Texts())
		}
	}
}
