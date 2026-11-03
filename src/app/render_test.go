package app

// render_test.go renders the conversation detail headlessly (mygo's Tester
// draws frames in memory, no window) and asserts the visual structure upstream's
// transcript is made of: the assistant prose bubble, the tool-call card with
// its status badge, the conversation header and the composer dock.
//
// Set MYGO_UI_PNG to a directory to also write the rendered frames there, for
// looking at by eye.

import (
	"encoding/json"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"gocode/locales"
)

// conversationShellForTest builds a Shell with the conversation detail already
// selected and a live connection carrying a short turn, with every backend
// response pre-seeded so the view renders without touching the network.
func conversationShellForTest(t *testing.T) *Shell {
	t.Helper()
	b := newBackendForTest(t)
	s := New(b)
	// A pre-set stream keeps ensureStream from dialing, and lets the test push
	// the ACP events the transcript is built from.
	s.stream = newStreamForTest()
	s.selectedFolder = "1"
	s.selectedConv = "1"
	s.connByConv["1"] = "c1"
	// The empty-state tip is picked at random per shell (UnixNano % len(tips)),
	// so pin it: a rotating tip makes every substring assertion ("Send" is
	// inside "Sending while a turn is running queues the message.") flaky.
	s.welcomeTip = 0

	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["folders"] = json.RawMessage(`[{"id":1,"name":"demo","path":"/tmp/demo"}]`)
	s.cache["convs:1"] = json.RawMessage(`[{"id":1,"title":"Fix the parser","agent_type":"claude_code","status":"active"}]`)
	s.cache["turns:1"] = json.RawMessage(`{"turns":[{"role":"user","blocks":[{"type":"text","text":"Fix the parser in ` + "`main.go`" + `."}]}]}`)
	s.cache["agents"] = json.RawMessage(`[{"agent_type":"claude_code"}]`)
	s.cache["snapshot:c1"] = json.RawMessage(`{"connection_id":"c1","status":"prompting","active_tool_calls":[]}`)

	push := func(ev map[string]any) {
		ev["connection_id"] = "c1"
		s.stream.handle(&wsFrame{Type: "event", Envelope: ev})
	}
	push(map[string]any{"seq": float64(1), "type": "content_delta", "text": "Let me look at the parser first."})
	push(map[string]any{"seq": float64(2), "type": "tool_call", "tool_call_id": "t1", "title": "Read main.go", "kind": "read", "status": "running"})
	push(map[string]any{"seq": float64(3), "type": "tool_call_update", "tool_call_id": "t1", "status": "success"})
	return s
}

// dumpFrame writes a rendered frame when MYGO_UI_PNG names a directory.
func dumpFrame(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("MYGO_UI_PNG")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestConversationDetailRenders(t *testing.T) {
	s := conversationShellForTest(t)
	// NewTester runs the view in strict mode: a duplicate element key panics,
	// so this also guards key uniqueness across the detail surface.
	tt := ui.NewTester(s.View, 1240, 820)

	for _, want := range []string{
		"Fix the parser",            // conversation header
		"Let me look at the parser", // assistant bubble
		"Read main.go",              // tool card title
		"Completed",                 // tool status badge
		"Send",                      // composer action
	} {
		if !tt.HasText(want) {
			t.Errorf("frame is missing %q; texts = %v", want, tt.Texts())
		}
	}
	// Folders live behind the Folders segment now (not stacked by default).
	if err := tt.Click("Folders"); err != nil {
		t.Fatalf("click Folders: %v", err)
	}
	tt.Frame()
	if !tt.HasText("demo") {
		t.Errorf("folders segment missing the folder; texts = %v", tt.Texts())
	}
	if err := tt.Click("Recent"); err != nil {
		t.Fatalf("click Recent: %v", err)
	}
	tt.Frame()
	dumpFrame(t, tt, "conversation-light")
	lightBg := tt.Image().RGBAAt(700, 420)

	// Dark is driven by the backend's appearance mode, which the view re-applies
	// every frame, so flip the seeded response rather than the tester's theme.
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"dark"}`)
	tt.Frame()
	if !tt.HasText("Let me look at the parser") || !tt.HasText("Completed") {
		t.Errorf("dark frame lost content; texts = %v", tt.Texts())
	}
	darkBg := tt.Image().RGBAAt(700, 420)
	t.Logf("mode=%q bg light=%v dark=%v", s.appearanceMode, lightBg, darkBg)
	if lightBg == darkBg {
		t.Errorf("dark mode did not repaint the canvas: %v", lightBg)
	}
	dumpFrame(t, tt, "conversation-dark")
}

// TestToolCardExpands drives the tool card's header click and asserts the body
// (the tool's detail line) appears — the collapsible behaviour upstream's Tool
// shell has.
func TestToolCardExpands(t *testing.T) {
	s := conversationShellForTest(t)
	// Give the call an output so the expanded body has something to show.
	s.cache["snapshot:c1"] = json.RawMessage(`{"connection_id":"c1","status":"prompting","active_tool_calls":[]}`)
	tt := ui.NewTester(s.View, 1240, 820)

	// A settled success starts collapsed, so the body is not in the frame yet.
	if tt.HasText("read") {
		t.Fatalf("collapsed card should hide its detail line; texts = %v", tt.Texts())
	}
	if err := tt.Click("Read main.go"); err != nil {
		t.Fatalf("click tool header: %v", err)
	}
	tt.Frame()
	if !tt.HasText("read") {
		t.Fatalf("expanded card should show its detail line; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "conversation-tool-open")
}

// TestZZRenderTerminalPanel renders the bottom terminal panel, both expanded
// (the tab strip over the real emulator) and collapsed (header only), so the
// layout can be eyeballed against upstream's bottom terminal bar.
func TestZZRenderTerminalPanel(t *testing.T) {
	if os.Getenv("MYGO_UI_PNG") == "" {
		t.Skip("set MYGO_UI_PNG to render")
	}
	s := terminalShellForTest(t, "t1")
	s.termTabs[0].Term, _ = newTestTerm(t, "$ ls\r\nsrc  go.mod  README.md\r\n$ echo hello\r\nhello\r\n$ ")
	addTerminalTabForTest(t, s, "t2", "Terminal 2", "$ git status\r\nOn branch main\r\n")
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Terminal") {
		t.Fatalf("panel header missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("New terminal tab") {
		t.Fatalf("tab strip missing its new-tab affordance; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "terminal-panel")
	// Collapsed variant: header stays, body hides.
	s.termOpen = false
	tt.Frame()
	dumpFrame(t, tt, "terminal-panel-collapsed")
}

// TestZZRenderImportSessions renders the import picker with a seeded scan result
// (a folder + a new session) so the list and selection affordances can be eyeballed.
func TestZZRenderImportSessions(t *testing.T) {
	if os.Getenv("MYGO_UI_PNG") == "" {
		t.Skip("set MYGO_UI_PNG to render")
	}
	s := importShellForTest(t, importScanFixture)
	s.importSel[importSelKey("claude-code", "abc")] = true
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Import sessions") {
		t.Fatalf("import frame missing header; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "import-sessions")
}

// TestZZRenderProjectBoot renders the project-boot window (shadcn form + skills).
func TestZZRenderProjectBoot(t *testing.T) {
	if os.Getenv("MYGO_UI_PNG") == "" {
		t.Skip("set MYGO_UI_PNG to render")
	}
	s := projectBootShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Project boot") {
		t.Fatalf("project-boot frame missing header; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "project-boot")
	// HyperFrames variant.
	if err := tt.Click("HyperFrames project"); err != nil {
		t.Fatalf("click HyperFrames: %v", err)
	}
	tt.Frame()
	dumpFrame(t, tt, "project-boot-hyperframes")
}

// TestZZRenderChromeZh renders the workspace chrome in Chinese (aux tabs +
// changes panel) to eyeball the localization.
func TestZZRenderChromeZh(t *testing.T) {
	if os.Getenv("MYGO_UI_PNG") == "" {
		t.Skip("set MYGO_UI_PNG to render")
	}
	s, _ := gitTestRepo(t, "")
	s.auxTab = "changes"
	tt := ui.NewTester(s.View, 1240, 820)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	dumpFrame(t, tt, "workspace-zh")
}

// TestZZRenderWorkspaceFolders renders the Workspace folders panel with two
// seeded links (one ok, one broken) and a dry-run plan awaiting confirmation,
// so the status colours, repair affordance, and preview hints can be eyeballed.
func TestZZRenderWorkspaceFolders(t *testing.T) {
	if os.Getenv("MYGO_UI_PNG") == "" {
		t.Skip("set MYGO_UI_PNG to render")
	}
	s, _, _, _ := wfShellForTest(t)
	s.cache[wfCacheKey(s.selectedFolder)] = json.RawMessage(`[{"id":1,"name":"shared","targetPath":"/home/u/shared","status":"ok"},{"id":2,"name":"vendor","targetPath":"/mnt/vendor","status":"broken"}]`)
	s.wfPlan = &wfLinkPlan{
		targetPath: "/home/u/src/proj", baseName: "proj", name: "proj-2",
		renamed: true, collides: true,
	}
	s.wfName = "proj-2"
	s.wfUploadPath = "/home/u/notes.md"
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Workspace folders") {
		t.Fatalf("workspace-folders frame missing header; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "workspace-folders")
}

// TestZZRenderBrowserPanel renders the Browser panel in its degraded (bridge-off)
// state.
func TestZZRenderBrowserPanel(t *testing.T) {
	if os.Getenv("MYGO_UI_PNG") == "" {
		t.Skip("set MYGO_UI_PNG to render")
	}
	s := browserShellForTest(t)
	s.browserURL = "http://localhost:3000"
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Browser") {
		t.Fatalf("browser panel missing header; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "browser-panel")
}

// TestZZRenderModelProviders renders Settings → Model providers with two seeded rows.
func TestZZRenderModelProviders(t *testing.T) {
	if os.Getenv("MYGO_UI_PNG") == "" {
		t.Skip("set MYGO_UI_PNG to render")
	}
	s, b := mpShellForTest(t)
	for _, p := range []map[string]any{
		{"name": "Acme", "apiUrl": "https://api.acme.test", "apiKey": "sk-1234567890abcdef", "agentType": "claude_code", "model": "acme-large"},
		{"name": "Beta", "apiUrl": "https://api.beta.test", "apiKey": "sk-zyxwvutsrqponmlk", "agentType": "codex"},
	} {
		if _, err := b.raw("create_model_provider", p); err != nil {
			t.Fatalf("seed provider: %v", err)
		}
	}
	raw, _ := b.raw("list_model_providers", map[string]any{})
	s.cache[mpCacheKey] = raw
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Model providers") {
		t.Fatalf("providers frame missing header; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "model-providers")
}

// TestZZRenderTokenUsage renders the token-usage dashboard from a populated report.
func TestZZRenderTokenUsage(t *testing.T) {
	if os.Getenv("MYGO_UI_PNG") == "" {
		t.Skip("set MYGO_UI_PNG to render")
	}
	s := usageShellForTest(t, usageReportFixture)
	s.cache["usage-facets"] = json.RawMessage(usageFacetsRichFixture)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Token usage") {
		t.Fatalf("usage frame missing header; texts = %v", tt.Texts())
	}
	// Show the multi-select checklist open over the dashboard.
	if err := tt.Click("Folder"); err != nil {
		t.Fatalf("open Folder filter: %v", err)
	}
	tt.Frame()
	dumpFrame(t, tt, "token-usage")
}

// TestZZRenderGitRemotes renders the Remotes page with two seeded remotes.
func TestZZRenderGitRemotes(t *testing.T) {
	if os.Getenv("MYGO_UI_PNG") == "" {
		t.Skip("set MYGO_UI_PNG to render")
	}
	s, repo := gitTestRepo(t, gitRouteRemotes)
	for _, r := range [][2]string{{"origin", "https://github.com/acme/app.git"}, {"upstream", "https://github.com/up/app.git"}} {
		if out, err := exec.Command("git", "-C", repo, "remote", "add", r[0], r[1]).CombinedOutput(); err != nil {
			t.Fatalf("git remote add: %v (%s)", err, out)
		}
	}
	s.invalidate(gitRemotesKey(repo))
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Remotes") {
		t.Fatalf("remotes frame missing header; texts = %v", tt.Texts())
	}
	waitUntil(t, tt, func() bool { return tt.HasText("https://github.com/acme/app.git") })
	dumpFrame(t, tt, "git-remotes")
}

// TestZZRenderClone renders the clone form with a filled URL/target.
func TestZZRenderClone(t *testing.T) {
	if os.Getenv("MYGO_UI_PNG") == "" {
		t.Skip("set MYGO_UI_PNG to render")
	}
	b := newBackendForTest(t)
	s := New(b)
	s.stream = newStreamForTest()
	s.workbench = "clone"
	s.cache["appearance-mode"] = []byte(`{"mode":"light"}`)
	s.cloneURL = "https://github.com/acme/app.git"
	s.cloneTarget = "/home/u/projects/app"
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Clone repository") {
		t.Fatalf("clone frame missing header; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "clone")
}

// TestZZRenderCommands renders the folder-command runner with two seeded commands
// so the list rows and Run/Edit/Delete affordances can be eyeballed.
func TestZZRenderCommands(t *testing.T) {
	if os.Getenv("MYGO_UI_PNG") == "" {
		t.Skip("set MYGO_UI_PNG to render")
	}
	s := commandsShellForTest(t, t.TempDir())
	s.cache[folderCommandKey("1")] = json.RawMessage(`[{"id":1,"folder_id":1,"name":"dev","command":"pnpm dev","sort_order":0},{"id":2,"folder_id":1,"name":"build","command":"pnpm build","sort_order":1}]`)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Folder commands") {
		t.Fatalf("commands frame missing header; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "commands")
}

// TestConversationLocalizes renders the conversation detail in Chinese and
// asserts the chrome (header actions, tool status badge, composer footer and
// the working hint) follows the window language — the strings that stayed
// English while every other panel spoke both languages.
func TestConversationLocalizes(t *testing.T) {
	s := conversationShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	for _, want := range []string{
		"重命名",         // header action
		"置顶",          // header action
		"删除",          // header action
		"已完成",         // tool status badge (Fold up from toolStatusLabel)
		"agent 正在处理…", // composer hint (status prompting)
		"停止",          // composer footer
		"发送",          // composer footer
	} {
		if !tt.HasText(want) {
			t.Errorf("zh conversation missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "conversation-zh")

	// The tool status label itself moves with the locale.
	if got := toolStatusLabel(locales.In("zh-Hans"), "success"); got != "已完成" {
		t.Errorf("toolStatusLabel(zh, success) = %q", got)
	}
	if got := toolStatusLabel(locales.In("zh-Hans"), "failed"); got != "失败" {
		t.Errorf("toolStatusLabel(zh, failed) = %q", got)
	}
}
