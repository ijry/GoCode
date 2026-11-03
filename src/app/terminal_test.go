package app

// terminal_test.go covers the terminal panel: the real-emulator body (mygo's
// terminal plugin bridged to the backend PTY), the start prompt, the tab strip
// and its close verbs. Chrome assertions read the tab strip; output assertions
// read the emulator's Text() — its cells are painted, not ui.Text, so tt.Texts
// never contains program output.

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// testTermConn is a ptyConn stand-in for tests that need a real emulator
// without a backend PTY: pushes are manual, writes are dropped.
type testTermConn struct {
	ch   chan []byte
	dead bool
}

func newTestTermConn() *testTermConn {
	return &testTermConn{ch: make(chan []byte, 64)}
}

func (c *testTermConn) Read(p []byte) (int, error) {
	if c.dead {
		return 0, io.EOF
	}
	b, ok := <-c.ch
	if !ok {
		return 0, io.EOF
	}
	return copy(p, b), nil
}

func (c *testTermConn) Write(p []byte) (int, error) { return len(p), nil }
func (c *testTermConn) Close() error                { return nil }

// feed delivers VT chunks to the emulator (as the firehose would).
func (c *testTermConn) feed(chunks ...string) {
	for _, ch := range chunks {
		c.ch <- []byte(ch)
	}
}

// newTestTerm builds a real plugin terminal fed the given chunks. It skips
// when the terminal library is unavailable in this environment.
func newTestTerm(t *testing.T, chunks ...string) (*terminal.Terminal, *testTermConn) {
	t.Helper()
	conn := newTestTermConn()
	term, err := terminal.New(terminal.Options{Conn: conn})
	if err != nil {
		t.Skipf("terminal plugin unavailable: %v", err)
	}
	conn.feed(chunks...)
	return term, conn
}

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
		term, _ := newTestTerm(t)
		s.termTabs = []termTab{{
			ID: termID, Title: "Terminal 1", FolderID: "1", WorkingDir: "/tmp/demo", Term: term,
		}}
		s.termActive = termID
		s.termCounter = 1
	}
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["folders"] = json.RawMessage(`[{"id":1,"name":"demo","path":"/tmp/demo"}]`)
	return s
}

func TestTerminalPanelShowsOutput(t *testing.T) {
	s := terminalShellForTest(t, "")
	term, _ := newTestTerm(t, "$ echo hi\r\nhi\r\n")
	s.termTabs = []termTab{{ID: "t1", Title: "Terminal 1", FolderID: "1", WorkingDir: "/tmp/demo", Term: term}}
	s.termActive = "t1"
	s.termCounter = 1
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{"Terminal 1", "New terminal tab"} {
		if !tt.HasText(want) {
			t.Errorf("terminal panel missing %q; texts = %v", want, tt.Texts())
		}
	}
	// The emulator owns input now: the old line-input row is gone, and the
	// program output lives in the emulator's cells (asserted via Text()).
	if countText(tt.Texts(), "Send") > 0 {
		t.Errorf("line input should be gone; texts = %v", tt.Texts())
	}
	if !strings.Contains(term.Text(), "hi") {
		t.Errorf("emulator missing output; text = %q", term.Text())
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
// always present, but the tab strip is hidden until expanded — matching
// upstream's collapsible bottom bar.
func TestTerminalPanelCollapsedByDefault(t *testing.T) {
	s := terminalShellForTest(t, "t1")
	s.termOpen = false
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Terminal") {
		t.Errorf("panel header missing; texts = %v", tt.Texts())
	}
	if countText(tt.Texts(), "New terminal tab") > 0 {
		t.Errorf("terminal body should be collapsed by default; texts = %v", tt.Texts())
	}
}

// TestTerminalPanelToggleExpands verifies clicking the header toggle reveals
// the tab strip and the emulator body.
func TestTerminalPanelToggleExpands(t *testing.T) {
	s := terminalShellForTest(t, "t1")
	s.termOpen = false
	tt := ui.NewTester(s.View, 1240, 820)
	if countText(tt.Texts(), "New terminal tab") > 0 {
		t.Fatalf("body should start collapsed; texts = %v", tt.Texts())
	}
	tt.Click("Toggle terminal panel")
	if !waitUntil(t, tt, func() bool { return countText(tt.Texts(), "New terminal tab") > 0 }) {
		t.Errorf("body did not expand after toggle; texts = %v", tt.Texts())
	}
}

// addTerminalTabForTest appends a second tab with a real emulator fed data.
func addTerminalTabForTest(t *testing.T, s *Shell, id, title, data string) {
	t.Helper()
	term, _ := newTestTerm(t, data)
	s.termTabs = append(s.termTabs, termTab{
		ID: id, Title: title, FolderID: "1", WorkingDir: "/tmp/demo", Term: term,
	})
}

// TestTerminalTabsSwitchAndClose covers the tab strip: both tabs render,
// clicking a title focuses that tab, and its close affordance removes it and
// falls back to the neighbor.
func TestTerminalTabsSwitchAndClose(t *testing.T) {
	s := terminalShellForTest(t, "t1")
	addTerminalTabForTest(t, s, "t2", "Terminal 2", "beta")
	tt := ui.NewTester(s.View, 1240, 820)

	for _, want := range []string{"Terminal 1", "Terminal 2", "New terminal tab"} {
		if !tt.HasText(want) {
			t.Fatalf("terminal tab bar missing %q; texts = %v", want, tt.Texts())
		}
	}

	if err := tt.Click("Terminal 2"); err != nil {
		t.Fatalf("click second tab: %v", err)
	}
	tt.Frame()
	s.mu.Lock()
	active := s.termActive
	s.mu.Unlock()
	if active != "t2" {
		t.Fatalf("second tab did not become active: %q", active)
	}

	if err := tt.Click("Close terminal tab: Terminal 2"); err != nil {
		t.Fatalf("close second tab: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return !tt.HasText("Terminal 2") }) {
		t.Fatalf("closing the active tab did not remove it; texts = %v", tt.Texts())
	}
	s.mu.Lock()
	active, count := s.termActive, len(s.termTabs)
	s.mu.Unlock()
	if active != "t1" || count != 1 {
		t.Fatalf("after close active=%q tabs=%d, want t1/1", active, count)
	}
}

// TestTerminalTabRename drives upstream's right-click Rename verb; a renamed
// tab stops following the program's OSC titles.
func TestTerminalTabRename(t *testing.T) {
	s := terminalShellForTest(t, "t1")
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
	title, renamed := s.termTabs[0].Title, s.termTabs[0].Renamed
	s.mu.Unlock()
	if title != "dev server" || !renamed {
		t.Fatalf("tab title = %q renamed=%v, want dev server/true", title, renamed)
	}
}

// TestTerminalTabCloseOthersAndAll covers the remaining close verbs.
func TestTerminalTabCloseOthersAndAll(t *testing.T) {
	s := terminalShellForTest(t, "t1")
	addTerminalTabForTest(t, s, "t2", "Terminal 2", "beta")
	addTerminalTabForTest(t, s, "t3", "Terminal 3", "gamma")
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
	if !waitUntil(t, tt, func() bool { return !tt.HasText("Terminal 2") }) {
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
	tt := ui.NewTester(s.View, 1240, 820)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	for _, want := range []string{"新建终端标签", "关闭终端标签：Terminal 1"} {
		if !tt.HasText(want) {
			t.Errorf("zh terminal tab bar missing %q; texts = %v", want, tt.Texts())
		}
	}
}

// TestPtyConnPrimeOrdering checks the bridge's read path: snapshot bytes
// first, pre-prime chunks deduped by seq, live chunks in order, EOF at the
// end of the stream.
func TestPtyConnPrimeOrdering(t *testing.T) {
	conn := newPtyConn("t1", New(newBackendForTest(t)).B)
	defer conn.Close()
	conn.push("early", 1)
	conn.push("early", 1) // duplicate
	conn.prime("SNAP", 2)
	conn.push("late", 3)
	conn.push("late", 3) // duplicate
	conn.push("more", 4)
	conn.endOfStream()

	readAll := func() string {
		var sb strings.Builder
		buf := make([]byte, 256)
		for {
			n, err := conn.Read(buf)
			sb.Write(buf[:n])
			if err == io.EOF {
				return sb.String()
			}
			if err != nil {
				t.Fatalf("read: %v", err)
			}
		}
	}
	if got := readAll(); got != "SNAPlatemore" {
		t.Fatalf("stream = %q, want SNAPlatemore", got)
	}
	if _, err := conn.Read(make([]byte, 8)); err != io.EOF {
		t.Fatalf("read after EOF = %v, want io.EOF", err)
	}
}

// TestPtyConnEOFWithoutPrime: a bridge whose stream ends before any snapshot
// still reads cleanly to EOF (an exited PTY reaped before the attach).
func TestPtyConnEOFWithoutPrime(t *testing.T) {
	conn := newPtyConn("t2", New(newBackendForTest(t)).B)
	defer conn.Close()
	conn.endOfStream()
	if _, err := conn.Read(make([]byte, 8)); err != io.EOF {
		t.Fatalf("read = %v, want io.EOF", err)
	}
}

// TestTerminalBridgeE2E drives the whole chain against the real backend: a
// real PTY spawns in a temp folder, the bridge primes from the snapshot, the
// real firehose pushes output into the emulator, and keystrokes reach the
// shell through terminal_write.
func TestTerminalBridgeE2E(t *testing.T) {
	s := New(newBackendForTest(t))
	s.workbench = ""
	s.selectedFolder = "1"
	s.termOpen = true
	s.welcomeTip = 0
	dir := t.TempDir()
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["folders"] = json.RawMessage(`[{"id":1,"name":"demo","path":"` + strings.ReplaceAll(dir, `\`, `\\`) + `"}]`)

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame() // builds once so ensureStream dials the real firehose
	if !waitUntil(t, tt, func() bool { return s.stream.isReady() }) {
		t.Skipf("firehose not ready in this environment")
	}
	// The spawned shell holds its working directory (the temp dir), so every
	// PTY must be reaped before t.TempDir's cleanup runs.
	t.Cleanup(func() {
		s.closeAllTerminalTabs()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			raw, err := s.B.raw("terminal_list", nil)
			var live []any
			if err == nil {
				_ = json.Unmarshal(raw, &live)
				if len(live) == 0 {
					return
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
	})

	if err := tt.Click("Start terminal"); err != nil {
		t.Fatalf("start terminal: %v", err)
	}
	// The tab either gains an emulator or records the failure (a machine
	// without a usable PTY skips rather than fails).
	var term *terminal.Terminal
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		if len(s.termTabs) == 1 {
			if s.termTabs[0].Err != "" {
				errText := s.termTabs[0].Err
				s.mu.Unlock()
				t.Skipf("PTY unavailable in this environment: %s", errText)
			}
			term = s.termTabs[0].Term
		}
		s.mu.Unlock()
		if term != nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
		tt.Frame()
	}
	if term == nil {
		t.Fatalf("terminal never started; texts = %v", tt.Texts())
	}

	// Typing flows through the emulator: Send is exactly what a keypress does.
	term.Send([]byte("echo gocode_bridge_marker\r"))
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(term.Text(), "gocode_bridge_marker") {
			return
		}
		time.Sleep(25 * time.Millisecond)
		tt.Frame()
	}
	t.Fatalf("bridge did not deliver the round trip; text = %q", term.Text())
}
