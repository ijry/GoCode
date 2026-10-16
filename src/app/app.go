// Package app is the mygo native-UI layer of YoYoCode: the Go analog of the
// upstream Next.js page tree under src/app. Each upstream page maps to one
// view function in a like-named file here (workspace.go, settings.go, pet.go,
// …) so the structure tracks upstream for easy follow-on diffs.
//
// All data comes from the in-process upstream backend over loopback HTTP, through
// the exact same /api contract mcode-app uses — the UI never reaches into the
// DB directly.
package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
)

// Backend is a thin loopback client for the in-process upstream server.
type Backend struct {
	URL   string
	Token string
}

func (b *Backend) raw(cmd string, body map[string]any) (json.RawMessage, error) {
	if body == nil {
		body = map[string]any{}
	}
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, b.URL+"/api/"+cmd, bytes.NewReader(buf))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+b.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, newAPIError(cmd, data)
	}
	return json.RawMessage(data), nil
}

// apiError is a failed backend command. The backend returns a structured body
// ({"code","message","detail"?}); we parse it so the UI shows the human message
// rather than a raw JSON dump.
type apiError struct {
	cmd, body     string
	code, message string
	detail        string
}

func newAPIError(cmd string, body []byte) *apiError {
	e := &apiError{cmd: cmd, body: string(body)}
	var p struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if json.Unmarshal(body, &p) == nil {
		e.code, e.message, e.detail = p.Code, p.Message, p.Detail
	}
	return e
}

// Error returns the human-readable message (never the raw JSON envelope).
func (e *apiError) Error() string {
	if e.message == "" {
		return e.cmd + ": " + e.body
	}
	if e.detail != "" {
		return e.message + " — " + e.detail
	}
	return e.message
}

// hint is an actionable next step for known error codes ("" when none).
func (e *apiError) hint() string {
	switch e.code {
	case "dependency_missing":
		return "The agent runtime could not start. Open Settings → Agents to install it, then retry."
	case "network_error":
		return "Network error — check your connection, then retry."
	case "config_missing":
		return "Connect the matching forge account in Settings → Version Control, then retry."
	case "auth_failed":
		return "The forge token was rejected. Re-connect the account in Settings → Version Control."
	default:
		return ""
	}
}

// Shell is the root application state: which top-level section and settings
// sub-page are active, plus an async cache of backend responses keyed by an
// arbitrary string. The view never blocks on IO — it asks the cache for a key,
// which kicks off a background fetch and repaints when it lands.
type Shell struct {
	B *Backend

	section             string // workspace | settings | pet
	settingsPage        string // general | web-service | quick-messages | …
	selectedFolder      string
	selectedConv        string
	sidebarTab          string // workspace sidebar segmented tab: recent | pinned | folders
	sidebarSearch       string // workspace sidebar conversation search box (filters by title)
	composer            string
	composerAttachments []attachmentItem // staged in the composer, uploaded on send
	hostOpenFiles       func() []string  // Tauri host hook: open OS file dialog, return picked paths
	newAgent            string
	toolOpen            map[string]bool // conversation detail: tool card id → expanded
	copiedCode          map[string]bool // code block key → "Copied" label showing
	appearanceMode      string          // system | light | dark (from get_appearance_mode)
	actionMsg           string
	errorMsg            string // last failed action, shown as a banner ("" = none)
	errorHint           string // actionable next step for the error
	toastQueue          []ui.Toast
	renaming            bool
	renameText          string
	skillsAgent         string            // Settings → Skills: selected agent wire type
	skillsScope         string            // "global" | "project"
	qmEditor            *quickMsgEditor   // Settings → Quick Messages: open create/edit form
	qmErr               string            // Settings → Quick Messages: validation error
	qmConfirm           string            // Settings → Quick Messages: id awaiting delete confirm
	workbench           string            // "" (conversations) | automations | tasks | forge | canvas | files | …
	autoSel             string            // Automations: selected automation id ("" = first row)
	autoEditor          *automationEditor // Automations: open create/edit form (nil = closed)
	autoConfirm         string            // Automations: id awaiting delete confirmation ("" = none)
	taskSel             string            // To-dos: selected task id ("" = board)
	taskEditor          *taskEditor       // To-dos: open create/edit form (nil = closed)
	taskConfirm         string            // To-dos: id awaiting delete confirmation ("" = none)
	taskShowCanceled    bool              // To-dos: show canceled cards in the Done column
	taskShowArchived    bool              // To-dos: show archived cards
	canvasSel           string            // Canvas: open board id ("" = board list)
	canvasEditor        *canvasEditor     // Canvas: new/rename board form (nil = closed)
	canvasConfirm       string            // Canvas: board id awaiting delete confirmation ("" = none)
	canvasSearch        string            // Canvas: board search box
	forgeTab            string            // Repositories: issues | prs
	forgeState          string            // Repositories: open | closed | all
	forgeEditor         *forgeEditor      // Repositories: new-issue form (nil = closed)
	petTab              string            // Pet section: sessions | sprite
	fileDir             string            // Files route: current directory (workspace-relative)
	fileSel             string            // Files route: selected file (workspace-relative)
	diffSel             string            // Changes route: selected changed file (repo-relative)
	commitMsg           string            // Changes route: commit message input
	discardConfirm      string            // Changes route: file awaiting discard confirmation
	detailTab           string            // Workspace main area tab: chat | terminal
	auxTab              string            // Workspace aux panel tab: session | files | changes | commits
	commitSel           string            // Commits aux tab: selected commit hash ("" = none)
	termID              string            // Terminal route: live PTY id ("" = none started)
	termInput           string            // Terminal route: pending input line
	loginToken          string            // login gate: token input
	loginErr            string            // login gate: last probe error
	servers             []serverProfile   // saved remote connections (the local one is synthesized)
	activeServer        string            // localServerID | saved profile id
	localURL            string            // boot backend URL (the "local" connection's home)
	localToken          string            // boot backend token
	srvEditor           *serverEditor     // Settings → Servers: add/edit form (nil = closed)
	srvErr              string            // Settings → Servers: last add/switch failure (inline)
	srvConfirm          string            // Settings → Servers: profile id awaiting delete confirmation
	gitCustomPath       string            // Settings → Version Control: custom git path input
	gitTestOK           *bool             // Settings → Version Control: last test_git_path result (nil = untested)
	gitTestMsg          string            // Settings → Version Control: why the last test failed
	vcTesting           string            // Settings → Version Control: account id being validated
	vcRotate            string            // Settings → Version Control: account id whose token form is open
	vcRotateToken       string            // Settings → Version Control: the new token typed in it
	vcRemove            string            // Settings → Version Control: account id awaiting removal confirmation
	vcAdd               *vcAddForm        // Settings → Version Control: add-account form (nil = closed)
	paletteOpen         bool              // ⌘K command palette: open
	paletteQuery        string            // ⌘K command palette: the filter query
	paletteSel          int               // ⌘K command palette: the highlighted row
	epoch               uint64            // bumped on every backend switch; stale fetches are dropped
	newSkillID          string            // Settings → Skills: new custom-skill id input
	agentVer            map[string]string // Settings → Agents: last detected installed version
	agentLatest         map[string]string // Settings → Agents: latest available (when newer)

	stream     *Stream
	connByConv map[string]string // conversation id → live ACP connection id

	// transcriptList is the conversation detail's virtual list (mygo's
	// ui.List): only the rows in view are built, so a very long conversation
	// scrolls as one of a few. FollowEnd is the stick-to-bottom guard — it
	// keeps a streaming turn's end in view while the user is at it, and stops
	// as soon as they scroll back into history (see viewTranscript). It is
	// UI-thread state.
	transcriptList ui.ListState

	// md is the shared Markdown renderer state (mygo's ui/markdown): one per app,
	// given to every transcript bubble. It keeps each message's parse between
	// frames and streams a half-written reply, so a token costs the same at the
	// end of a long reply as at its start.
	md markdown.Options

	// toolDiff holds one editor.DiffState per diff a tool card shows (keyed by
	// the card's key, and per sub-edit for multi_edit). The state keeps the
	// collapsed-context expansions and the scroll position across frames, so it
	// has to outlive the view function that first builds it.
	toolDiff map[string]*editor.DiffState

	mu       sync.Mutex
	toastMu  sync.Mutex // guards toastQueue only; notify is often called with mu held
	svc      ui.Services
	cache    map[string]json.RawMessage
	inflight map[string]bool
	errs     map[string]string
	errHints map[string]string // cache key → actionable hint for its failed fetch
}

// New builds the root shell.
func New(b *Backend) *Shell {
	return &Shell{
		B:              b,
		section:        "workspace",
		settingsPage:   "general",
		appearanceMode: "system",
		sidebarTab:     "recent",
		cache:          map[string]json.RawMessage{},
		inflight:       map[string]bool{},
		errs:           map[string]string{},
		errHints:       map[string]string{},
		connByConv:     map[string]string{},
		toolOpen:       map[string]bool{},
		agentVer:       map[string]string{},
		agentLatest:    map[string]string{},
		// The in-process backend is always connection #0 ("local"); saved
		// remotes come from ~/.yoyocode/servers.json.
		localURL:     b.URL,
		localToken:   b.Token,
		servers:      loadServers(),
		activeServer: localServerID,
	}
}

// ensure returns the cached raw response for key, kicking off a one-shot
// background fetch (cmd+body) the first time and whenever the cache was
// invalidated. Returns nil while the fetch is in flight.
func (s *Shell) ensure(key, cmd string, body map[string]any) json.RawMessage {
	s.mu.Lock()
	if raw, ok := s.cache[key]; ok {
		s.mu.Unlock()
		return raw
	}
	if s.inflight[key] {
		s.mu.Unlock()
		return nil
	}
	s.inflight[key] = true
	epoch := s.epoch // stamped so a server switch can drop this response
	svc := s.svc
	s.mu.Unlock()

	go func() {
		raw, err := s.B.raw(cmd, body)
		s.mu.Lock()
		s.inflight[key] = false
		if epoch != s.epoch {
			// The user switched servers while this was in flight: the response
			// belongs to the previous backend, so drop it (the fresh cache the
			// switch installed is never polluted with the old realm's data).
			s.mu.Unlock()
			svc.Invalidate()
			return
		}
		if err != nil {
			s.errs[key] = err.Error()
			if ae, ok := err.(*apiError); ok {
				s.errHints[key] = ae.hint()
			}
		} else {
			s.cache[key] = raw
			delete(s.errs, key)
			delete(s.errHints, key)
		}
		s.mu.Unlock()
		svc.Invalidate() // repaint on the UI thread; safe from any goroutine
	}()
	return nil
}

// invalidate drops a cached key so the next ensure refetches it.
func (s *Shell) invalidate(key string) {
	s.mu.Lock()
	delete(s.cache, key)
	s.mu.Unlock()
}

func (s *Shell) errFor(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.errs[key]
}

// errHintFor returns the actionable hint stored for a failed cached fetch.
func (s *Shell) errHintFor(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.errHints[key]
}

// list / obj decode a cached raw response; nil until the fetch lands.
func asList(raw json.RawMessage) []map[string]any {
	if raw == nil {
		return nil
	}
	var v []map[string]any
	_ = json.Unmarshal(raw, &v)
	return v
}

// asListVal converts an already-decoded value (e.g. a nested array inside an
// object response) into a slice of objects, skipping non-object elements.
func asListVal(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, el := range arr {
		if m, ok := el.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func asObj(raw json.RawMessage) map[string]any {
	if raw == nil {
		return nil
	}
	var v map[string]any
	_ = json.Unmarshal(raw, &v)
	return v
}

// decodeString decodes a cached raw response that is a bare JSON string
// (e.g. get_home_directory); "" until the fetch lands or on any mismatch.
func decodeString(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var v string
	_ = json.Unmarshal(raw, &v)
	return v
}

func str(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

// intVal reads a numeric (JSON float64) field, defaulting to 0.
func intVal(m map[string]any, k string) int64 {
	if m == nil {
		return 0
	}
	switch v := m[k].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}

// asBool reads a boolean field, defaulting to false.
func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

// View is the root view. There is deliberately NO outer navigation rail: the
// leftmost column IS the workspace sidebar (upstream's IA — its left column is
// the conversation list, and Settings / the pet are separate routes there).
// The single-window build surfaces those two on the bottom chrome instead.
func (s *Shell) View(c *ui.Context) {
	// Unauthenticated: show the login gate (upstream src/app/login). The desktop
	// build always boots with an in-process token, so — exactly like upstream's
	// `isDesktop()` skip — the gate never shows there; it is the surface for a
	// tokenless (remote/web) boot.
	if s.B.Token == "" {
		s.svc = c.Services()
		c.SetTheme(resolveTheme(s.appearanceMode))
		s.viewLogin(c)
		return
	}
	// Resolve the appearance mode from the backend (cached one-shot fetch;
	// defaults to system=light until it arrives) and apply the matching theme.
	if raw := s.ensure("appearance-mode", "get_appearance_mode", nil); raw != nil {
		if m := asObj(raw); m != nil {
			if mode := str(m, "mode"); mode != "" {
				s.appearanceMode = mode
			}
		}
	}
	c.SetTheme(resolveTheme(s.appearanceMode)) // upstream neutral light/dark palette
	s.svc = c.Services()
	s.ensureStream()
	// Window-global shortcuts (UI_GAPS P2): ⌘K opens the command palette
	// wherever the focus is; Esc stops the running turn while the palette is
	// closed (the palette owns Esc while it is open).
	if c.Shortcut(ui.Cmd, ui.KeyK) {
		s.paletteOpen = true
		s.paletteQuery = ""
		s.paletteSel = 0
	}
	if !s.paletteOpen && c.Shortcut(0, ui.KeyEscape) {
		s.stopActiveTurn()
	}
	// Mutation outcomes (queued by goroutines) surface as toasts, the way
	// mygo's own apps report them.
	s.drainToasts(c)
	// Window = content row on top of a slim status bar (upstream's bottom chrome).
	ui.Column(c).Fill().Children(func() {
		// Computer-use strip, pinned above the content while anything is shared
		// (a no-op on a backend that refuses computer use).
		s.viewComputerIndicator(c)
		ui.Row(c.Key("body")).Grow(1).Children(func() {
			ui.Column(c.Key("page")).Grow(1).FillHeight().Children(func() {
				switch s.section {
				case "settings":
					s.viewSettings(c)
				case "pet":
					s.viewPet(c)
				default:
					s.viewWorkspace(c)
				}
			})
		})
		ui.Divider(c)
		s.viewStatusBar(c)
	})
	// The toast viewport overlays the window; build it after the content so its
	// Bottom anchoring sits above the status bar.
	s.viewToasts(c)
	// The ⌘K command palette overlays everything, built last.
	s.viewCommandPalette(c)
}

// viewStatusBar is the slim bottom chrome: a liveness dot + backend address on
// the left, the section switcher and product/version on the right — modeled on
// upstream's status bar. The section switcher is the single-window stand-in for
// the separate settings / pet routes upstream opens.
func (s *Shell) viewStatusBar(c *ui.Context) {
	t := c.Theme()
	connected := s.stream != nil && s.stream.isReady()
	dot := t.TextMuted
	label := cat(c).StatusConnecting()
	if connected {
		dot = t.Success
		label = cat(c).StatusConnected()
	}
	ui.Row(c.Key("statusbar")).Background(t.Surface).PaddingX(12).PaddingY(5).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "\u25CF").FontSize(9).TextColor(dot)
		ui.Text(c, label).FontSize(11).TextColor(t.TextMuted)
		// Active server: a button because the whole workspace can be swapped to
		// another one from Settings → Servers (single window, no second window).
		srv := s.activeProfile()
		if ui.Button(c.Key("sb-server"), s.serverDisplayName(c, srv)).Clicked() {
			s.section = "settings"
			s.settingsPage = "servers"
		}
		ui.Text(c, s.B.URL).FontSize(11).TextColor(t.TextMuted)
		if ui.Button(c.Key("sb-palette"), modKeyLabel()+"K").Clicked() {
			s.paletteOpen = true
			s.paletteQuery = ""
			s.paletteSel = 0
		}
		ui.Spacer(c)
		// The two full-screen routes upstream opens as SEPARATE windows (never
		// sidebar rows) are listed here in the single-window build.
		for _, item := range []struct{ route, label string }{
			{"import-sessions", cat(c).RouteImportSessions()},
			{"project-boot", cat(c).RouteProjectBoot()},
		} {
			active := s.section == "workspace" && s.workbench == item.route
			var clicked bool
			if active {
				clicked = ui.PrimaryButton(c.Key("sb-route-"+item.route), item.label).Clicked()
			} else {
				clicked = ui.Button(c.Key("sb-route-"+item.route), item.label).Clicked()
			}
			if clicked {
				s.section = "workspace"
				s.workbench = item.route
			}
		}
		ui.Divider(c)
		// Section switcher: the single-window stand-in for the separate
		// settings / pet routes upstream opens.
		for _, item := range []struct{ id, label string }{
			{"workspace", cat(c).SectionWorkspace()},
			{"settings", cat(c).SectionSettings()},
			{"pet", cat(c).SectionPet()},
		} {
			active := s.section == item.id
			var clicked bool
			if active {
				clicked = ui.PrimaryButton(c.Key("sb-"+item.id), item.label).Clicked()
			} else {
				clicked = ui.Button(c.Key("sb-"+item.id), item.label).Clicked()
			}
			if clicked {
				s.section = item.id
			}
		}
		ui.Divider(c)
		s.viewStatusMcp(c)
		s.viewStatusTasks(c)
		s.viewStatusUpdate(c)
		ui.Text(c, "YoYoCode v0.1.0").FontSize(11).TextColor(t.TextMuted)
	})
}

// viewStatusMcp is the yoyocode-mcp companion health indicator (upstream
// StatusBarMcp). Reflects get_codeg_mcp_service_status: green dot when running,
// muted otherwise (stopped until the companion + broker ship — see UI doc 点 3).
func (s *Shell) viewStatusMcp(c *ui.Context) {
	t := c.Theme()
	state := ""
	if m := asObj(s.ensure("status-mcp", "get_codeg_mcp_service_status", nil)); m != nil {
		state = str(m, "state")
	}
	dot := t.TextMuted
	if state == "running" {
		dot = t.Success
	}
	ui.Row(c.Key("sb-mcp")).Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "\u25CF").FontSize(8).TextColor(dot)
		ui.Text(c, "MCP").FontSize(11).TextColor(t.TextMuted)
	})
}

// viewStatusTasks is the open to-do count (upstream StatusBarTasks). Counts
// work tasks not yet done; clicking jumps to the To-dos workbench. Hidden when
// nothing is open.
func (s *Shell) viewStatusTasks(c *ui.Context) {
	open := 0
	for _, tk := range asList(s.ensure("status-worktasks", "work_task_list", nil)) {
		switch str(tk, "status") {
		case "done", "completed", "cancelled", "archived":
		default:
			open++
		}
	}
	if open == 0 {
		return
	}
	if ui.Button(c.Key("sb-tasks"), fmt.Sprintf("\u2611 %d", open)).Clicked() {
		s.section = "workspace"
		s.workbench = "tasks"
	}
}

// viewStatusUpdate is the update-available badge (upstream StatusBarUpdate).
// check_app_update reports a newer version only when one exists; this build does
// not self-update, so it stays hidden until update discovery is enabled.
func (s *Shell) viewStatusUpdate(c *ui.Context) {
	var upd any
	if m := asObj(s.ensure("status-appupdate", "check_app_update", nil)); m != nil {
		upd = m["update"]
	}
	if upd == nil {
		return
	}
	if ui.PrimaryButton(c.Key("sb-update"), "\u2191 Update").Clicked() {
		s.section = "settings"
		s.settingsPage = "general"
	}
}
