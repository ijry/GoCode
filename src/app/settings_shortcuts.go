package app

// settings_shortcuts.go mirrors upstream src/app/settings/shortcuts +
// components/settings/shortcut-settings.tsx. The custom chords persist beside
// the saved servers (~/.gocode/shortcuts.json), the way upstream keeps them
// in localStorage. A binding arms a capture pass over mygo's window
// shortcuts: while armed, every candidate chord records instead of running
// its action (the action handlers are not registered on that frame).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"gocode/locales"
)

// shortcutDefinition is one reassignable action.
type shortcutDefinition struct {
	id    string
	title func(locales.Messages) string
	desc  func(locales.Messages) string
}

// shortcutDefinitions is the subset of upstream's SHORTCUT_DEFINITIONS this
// build wires. Actions with no surface here are left out rather than listed
// as dead bindings: file tabs, zoom (the native window has no page zoom) and
// the custom-style escape hatch.
var shortcutDefinitions = []shortcutDefinition{
	{"toggle_search", locales.Messages.SetShortcutToggleSearch, locales.Messages.SetShortcutToggleSearchHint},
	{"toggle_sidebar", locales.Messages.SetShortcutToggleSidebar, locales.Messages.SetShortcutToggleSidebarHint},
	{"toggle_terminal", locales.Messages.SetShortcutToggleTerminal, locales.Messages.SetShortcutToggleTerminalHint},
	{"new_terminal_tab", locales.Messages.SetShortcutNewTerminalTab, locales.Messages.SetShortcutNewTerminalTabHint},
	{"close_current_terminal_tab", locales.Messages.SetShortcutCloseTerminalTab, locales.Messages.SetShortcutCloseTerminalTabHint},
	{"toggle_aux_panel", locales.Messages.SetShortcutToggleAux, locales.Messages.SetShortcutToggleAuxHint},
	{"new_conversation", locales.Messages.SetShortcutNewConversation, locales.Messages.SetShortcutNewConversationHint},
	{"open_folder", locales.Messages.SetShortcutOpenFolder, locales.Messages.SetShortcutOpenFolderHint},
	{"open_settings", locales.Messages.SetShortcutOpenSettings, locales.Messages.SetShortcutOpenSettingsHint},
	{"close_current_tab", locales.Messages.SetShortcutCloseTab, locales.Messages.SetShortcutCloseTabHint},
	{"reopen_last_closed_tab", locales.Messages.SetShortcutReopenTab, locales.Messages.SetShortcutReopenTabHint},
	{"next_tab", locales.Messages.SetShortcutNextTab, locales.Messages.SetShortcutNextTabHint},
	{"prev_tab", locales.Messages.SetShortcutPrevTab, locales.Messages.SetShortcutPrevTabHint},
	{"switch_tab_1", locales.Messages.SetShortcutSwitchTab1, locales.Messages.SetShortcutSwitchTabHint},
	{"switch_tab_2", locales.Messages.SetShortcutSwitchTab2, locales.Messages.SetShortcutSwitchTabHint},
	{"switch_tab_3", locales.Messages.SetShortcutSwitchTab3, locales.Messages.SetShortcutSwitchTabHint},
	{"switch_tab_4", locales.Messages.SetShortcutSwitchTab4, locales.Messages.SetShortcutSwitchTabHint},
	{"switch_tab_5", locales.Messages.SetShortcutSwitchTab5, locales.Messages.SetShortcutSwitchTabHint},
	{"switch_tab_6", locales.Messages.SetShortcutSwitchTab6, locales.Messages.SetShortcutSwitchTabHint},
	{"switch_tab_7", locales.Messages.SetShortcutSwitchTab7, locales.Messages.SetShortcutSwitchTabHint},
	{"switch_tab_8", locales.Messages.SetShortcutSwitchTab8, locales.Messages.SetShortcutSwitchTabHint},
	{"switch_tab_9", locales.Messages.SetShortcutSwitchTab9, locales.Messages.SetShortcutSwitchTabHint},
	{"send_message", locales.Messages.SetShortcutSendMessage, locales.Messages.SetShortcutSendMessageHint},
	{"newline_in_message", locales.Messages.SetShortcutNewlineMessage, locales.Messages.SetShortcutNewlineMessageHint},
}

// shortcutInputActions may be bound without a modifier (plain Enter and
// Shift+Enter by default), as upstream's INPUT_SHORTCUT_IDS are.
var shortcutInputActions = map[string]bool{
	"send_message":       true,
	"newline_in_message": true,
}

// canShareShortcut reports upstream's SHARED_SHORTCUT_PAIRS: the conversation
// and terminal versions of new-tab and close-tab never apply to the same
// surface, so one chord may serve both.
func canShareShortcut(a, b string) bool {
	return a == "new_terminal_tab" && b == "new_conversation" ||
		a == "new_conversation" && b == "new_terminal_tab" ||
		a == "close_current_terminal_tab" && b == "close_current_tab" ||
		a == "close_current_tab" && b == "close_current_terminal_tab"
}

// defaultShortcuts mirrors upstream's DEFAULT_SHORTCUTS where the action exists
// in this build.
var defaultShortcuts = map[string]string{
	"toggle_search":              "mod+k",
	"toggle_sidebar":             "mod+b",
	"toggle_terminal":            "mod+j",
	"new_terminal_tab":           "mod+t",
	"close_current_terminal_tab": "mod+w",
	"toggle_aux_panel":           "mod+e",
	"new_conversation":           "mod+t",
	"open_folder":                "mod+o",
	"open_settings":              "mod+,",
	"close_current_tab":          "mod+w",
	"reopen_last_closed_tab":     "mod+shift+t",
	"next_tab":                   "mod+tab",
	"prev_tab":                   "mod+shift+tab",
	"switch_tab_1":               "mod+1",
	"switch_tab_2":               "mod+2",
	"switch_tab_3":               "mod+3",
	"switch_tab_4":               "mod+4",
	"switch_tab_5":               "mod+5",
	"switch_tab_6":               "mod+6",
	"switch_tab_7":               "mod+7",
	"switch_tab_8":               "mod+8",
	"switch_tab_9":               "mod+9",
	"send_message":               "enter",
	"newline_in_message":         "shift+enter",
}

// shortcutsFile is ~/.gocode/shortcuts.json (honoring GOCODE_DATA_DIR,
// like servers.json).
func shortcutsFile() string {
	dir := os.Getenv("GOCODE_DATA_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".gocode")
	}
	return filepath.Join(dir, "shortcuts.json")
}

func defaultShortcutMap() map[string]string {
	m := make(map[string]string, len(defaultShortcuts))
	for k, v := range defaultShortcuts {
		m[k] = v
	}
	return m
}

// loadShortcuts starts from the defaults and overlays every valid saved
// binding; unknown keys and unparsable chords are ignored.
func loadShortcuts() map[string]string {
	out := defaultShortcutMap()
	data, err := os.ReadFile(shortcutsFile())
	if err != nil {
		return out
	}
	var saved map[string]string
	if json.Unmarshal(data, &saved) != nil {
		return out
	}
	for id, chord := range saved {
		if _, ok := defaultShortcuts[id]; !ok {
			continue
		}
		// An empty string is the persisted "unassigned" state, not a missing
		// key: reseeding the default would put the binding back on next write.
		if chord == "" {
			out[id] = ""
			continue
		}
		if _, _, ok := parseShortcut(chord); ok {
			out[id] = chord
		}
	}
	return out
}

func saveShortcuts(m map[string]string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := shortcutsFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// shortcutKeysByName is the recordable key set: letters, digits, punctuation
// and the named keys upstream's recorder accepts. Escape is deliberately
// absent — it cancels recording.
var shortcutKeysByName = map[string]ui.Key{
	"a": ui.KeyA, "b": ui.KeyB, "c": ui.KeyC, "d": ui.KeyD, "e": ui.KeyE,
	"f": ui.KeyF, "g": ui.KeyG, "h": ui.KeyH, "i": ui.KeyI, "j": ui.KeyJ,
	"k": ui.KeyK, "l": ui.KeyL, "m": ui.KeyM, "n": ui.KeyN, "o": ui.KeyO,
	"p": ui.KeyP, "q": ui.KeyQ, "r": ui.KeyR, "s": ui.KeyS, "t": ui.KeyT,
	"u": ui.KeyU, "v": ui.KeyV, "w": ui.KeyW, "x": ui.KeyX, "y": ui.KeyY,
	"z": ui.KeyZ,
	"0": ui.Key0, "1": ui.Key1, "2": ui.Key2, "3": ui.Key3, "4": ui.Key4,
	"5": ui.Key5, "6": ui.Key6, "7": ui.Key7, "8": ui.Key8, "9": ui.Key9,
	"-": ui.KeyMinus, "=": ui.KeyEqual, ",": ui.KeyComma, ".": ui.KeyPeriod,
	"/": ui.KeySlash, ";": ui.KeySemicolon, "'": ui.KeyQuote,
	"[": ui.KeyBracketLeft, "]": ui.KeyBracketRight, "\\": ui.KeyBackslash,
	"`":   ui.KeyBackquote,
	"tab": ui.KeyTab, "enter": ui.KeyEnter, "space": ui.KeySpace,
	"left": ui.KeyLeft, "right": ui.KeyRight, "up": ui.KeyUp, "down": ui.KeyDown,
	"home": ui.KeyHome, "end": ui.KeyEnd, "pageup": ui.KeyPageUp, "pagedown": ui.KeyPageDown,
	"f1": ui.KeyF1, "f2": ui.KeyF2, "f3": ui.KeyF3, "f4": ui.KeyF4,
	"f5": ui.KeyF5, "f6": ui.KeyF6, "f7": ui.KeyF7, "f8": ui.KeyF8,
	"f9": ui.KeyF9, "f10": ui.KeyF10, "f11": ui.KeyF11, "f12": ui.KeyF12,
}

// shortcutModCombos are the recordable modifier sets: the platform's shortcut
// modifier alone, plus Shift and/or Alt. Plain keys are not recordable here —
// none of the wired actions is an input action.
var shortcutModCombos = []ui.Modifiers{
	ui.Cmd,
	ui.Cmd | ui.Shift,
	ui.Cmd | ui.Alt,
	ui.Cmd | ui.Shift | ui.Alt,
}

// parseShortcut parses a canonical chord ("mod+shift+k", or a bare "enter" for
// the input actions) into modifiers + key.
func parseShortcut(chord string) (ui.Modifiers, ui.Key, bool) {
	chord = strings.TrimSpace(chord)
	if chord == "" {
		return 0, 0, false
	}
	parts := strings.Split(chord, "+")
	var mods ui.Modifiers
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(p) {
		case "mod":
			mods |= ui.Cmd
		case "shift":
			mods |= ui.Shift
		case "alt":
			mods |= ui.Alt
		case "ctrl":
			mods |= ui.Ctrl
		default:
			return 0, 0, false
		}
	}
	key, ok := shortcutKeysByName[strings.ToLower(parts[len(parts)-1])]
	if !ok {
		return 0, 0, false
	}
	return mods, key, true
}

// chordString renders modifiers + key in the persisted canonical form.
func chordString(mods ui.Modifiers, key ui.Key) (string, bool) {
	name, ok := shortcutKeyName(key)
	if !ok {
		return "", false
	}
	var parts []string
	if mods&ui.Cmd != 0 {
		parts = append(parts, "mod")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "alt")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "shift")
	}
	return strings.Join(append(parts, name), "+"), true
}

// textKeyMatches reports whether a composer key event is the chord. Other
// modifier bits the event may carry are ignored.
func textKeyMatches(k ui.TextKey, chord string) bool {
	mods, key, ok := parseShortcut(chord)
	if !ok {
		return false
	}
	return k.Key == key && k.Mods&(ui.Cmd|ui.Ctrl|ui.Shift|ui.Alt) == mods
}

func shortcutKeyName(key ui.Key) (string, bool) {
	for name, k := range shortcutKeysByName {
		if k == key {
			return name, true
		}
	}
	return "", false
}

// shortcutLabel is how a chord reads in the settings page: ⌘⇧K on macOS,
// Ctrl+Shift+K elsewhere.
func shortcutLabel(chord string) string {
	mods, key, ok := parseShortcut(chord)
	if !ok {
		return chord
	}
	name, _ := shortcutKeyName(key)
	keyLabel := shortcutKeyLabel(name)
	if runtime.GOOS != "darwin" {
		// modKeyLabel already ends in '+', so the modifiers concatenate.
		var b strings.Builder
		if mods&ui.Cmd != 0 {
			b.WriteString(modKeyLabel())
		}
		if mods&ui.Alt != 0 {
			b.WriteString("Alt+")
		}
		if mods&ui.Shift != 0 {
			b.WriteString("Shift+")
		}
		b.WriteString(keyLabel)
		return b.String()
	}
	var b strings.Builder
	if mods&ui.Cmd != 0 {
		b.WriteString("⌘")
	}
	if mods&ui.Alt != 0 {
		b.WriteString("⌥")
	}
	if mods&ui.Shift != 0 {
		b.WriteString("⇧")
	}
	if mods&ui.Ctrl != 0 {
		b.WriteString("⌃")
	}
	b.WriteString(keyLabel)
	return b.String()
}

func shortcutKeyLabel(name string) string {
	switch name {
	case "left":
		return "←"
	case "right":
		return "→"
	case "up":
		return "↑"
	case "down":
		return "↓"
	case "pageup":
		return "PgUp"
	case "pagedown":
		return "PgDn"
	case "space":
		return "Space"
	case "enter":
		return "Enter"
	case "tab":
		return "Tab"
	case "home":
		return "Home"
	case "end":
		return "End"
	default:
		return strings.ToUpper(name)
	}
}

// ─── the page ──────────────────────────────────────────────────────────────

func (s *Shell) viewSettingsShortcuts(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	ui.Row(c.Key("sc-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.NavShortcuts()).FontSize(22).Bold()
		ui.Spacer(c)
		if ui.Button(c.Key("sc-reset"), m.SetShortcutsReset()).Disabled(s.shortcutsAreDefault()).Clicked() {
			s.resetShortcuts(c.Locale())
		}
	})
	ui.Text(c, m.SetShortcutsHint()).FontSize(12).TextColor(t.TextMuted).Wrap()

	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c.Key("sc-card")).Gap(10).Padding(12).Background(t.Surface).Radius(8).Children(func() {
			for _, def := range shortcutDefinitions {
				def := def
				recording := s.shortcutRecording == def.id
				ui.Row(c.Key("sc-row-" + def.id)).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, def.title(m)).Bold()
						ui.Text(c, def.desc(m)).FontSize(11).TextColor(t.TextMuted).MaxLines(2).Wrap()
					})
					label := m.SetShortcutsUnassigned()
					if s.shortcuts[def.id] != "" {
						label = shortcutLabel(s.shortcuts[def.id])
					}
					if recording {
						label = m.SetShortcutsRecording()
					}
					if ui.Button(c.Key("sc-btn-"+def.id), label).Width(170).Clicked() {
						if recording {
							s.shortcutRecording = ""
						} else {
							s.shortcutRecording = def.id
						}
					}
					if s.shortcuts[def.id] != "" && !recording {
						iconButton(c, "sc-clear-"+def.id, iconX, m.SetShortcutsUnassignNamed(def.title(m)), func() {
							s.clearShortcut(def.id, c.Locale())
						})
					}
				})
			}
		})
	})
}

func (s *Shell) shortcutsAreDefault() bool {
	for id, def := range defaultShortcuts {
		if s.shortcuts[id] != def {
			return false
		}
	}
	return true
}

func (s *Shell) resetShortcuts(loc string) {
	s.shortcuts = defaultShortcutMap()
	s.shortcutRecording = ""
	if err := saveShortcuts(s.shortcuts); err != nil {
		s.notifyErr(locales.In(loc).SetShortcutsSaveFailed(err.Error()))
		return
	}
	s.notify(locales.In(loc).SetShortcutsResetDone())
}

// clearShortcut unbinds an action. The empty string is persisted so the next
// launch keeps it unassigned.
func (s *Shell) clearShortcut(action, loc string) {
	s.shortcuts[action] = ""
	s.shortcutRecording = ""
	m := locales.In(loc)
	if err := saveShortcuts(s.shortcuts); err != nil {
		s.notifyErr(m.SetShortcutsSaveFailed(err.Error()))
		return
	}
	s.notify(m.SetShortcutsCleared())
}

// assignShortcut stores the recorded chord unless another action already uses
// it. A conflict keeps the recorder armed so the user can try another chord.
func (s *Shell) assignShortcut(action string, mods ui.Modifiers, key ui.Key, loc string) {
	chord, ok := chordString(mods, key)
	if !ok {
		return
	}
	m := locales.In(loc)
	for _, def := range shortcutDefinitions {
		if def.id == action || s.shortcuts[def.id] == "" || canShareShortcut(def.id, action) {
			continue
		}
		if s.shortcuts[def.id] == chord {
			s.notifyErr(m.SetShortcutsConflict(def.title(m)))
			return
		}
	}
	s.shortcuts[action] = chord
	s.shortcutRecording = ""
	if err := saveShortcuts(s.shortcuts); err != nil {
		s.notifyErr(m.SetShortcutsSaveFailed(err.Error()))
		return
	}
	s.notify(m.SetShortcutsUpdated())
}

// installShortcuts registers the window's shortcut handlers. While a binding
// is being recorded it registers the capture pass instead, so no action fires
// from the keys the user is trying out.
func (s *Shell) installShortcuts(c *ui.Context, loc string) {
	if action := s.shortcutRecording; action != "" {
		c.OnShortcut(0, ui.KeyEscape, func() { s.shortcutRecording = "" })
		combos := shortcutModCombos
		if shortcutInputActions[action] {
			combos = append(append([]ui.Modifiers(nil), combos...), 0, ui.Shift, ui.Alt, ui.Shift|ui.Alt)
		}
		for _, mods := range combos {
			for _, key := range shortcutKeysByName {
				mods, key := mods, key
				c.OnShortcut(mods, key, func() { s.assignShortcut(action, mods, key, loc) })
			}
		}
		return
	}
	s.bindAction(c, "toggle_search", func() {
		s.paletteOpen = true
		s.paletteQuery = ""
		s.paletteSel = 0
	})
	s.bindAction(c, "toggle_sidebar", func() { s.sidebarHidden = !s.sidebarHidden })
	s.bindAction(c, "toggle_terminal", func() { s.termOpen = !s.termOpen })
	s.bindNewTabPair(c, loc)
	s.bindCloseTabPair(c)
	s.bindAction(c, "toggle_aux_panel", func() { s.auxHidden = !s.auxHidden })
	s.bindAction(c, "open_folder", func() { s.openFolder(loc) })
	s.bindAction(c, "open_settings", func() { s.section = "settings" })
	s.bindAction(c, "reopen_last_closed_tab", func() { s.reopenClosedTab() })
	s.bindAction(c, "next_tab", func() { s.stepConversationTab(1) })
	s.bindAction(c, "prev_tab", func() { s.stepConversationTab(-1) })
	for i := 1; i <= 9; i++ {
		i := i
		s.bindAction(c, "switch_tab_"+strconv.Itoa(i), func() { s.focusTabIndex(i - 1) })
	}
}

// bindNewTabPair registers new_terminal_tab and new_conversation. The two
// share a default chord, and mygo hands a chord to one window action, so a
// shared chord gets one handler that picks the surface the way upstream's
// capture-phase terminal listener does: the terminal takes it while the
// pointer or focus is in it, the conversation strip otherwise. Chords the
// user split apart keep their own meanings.
func (s *Shell) bindNewTabPair(c *ui.Context, loc string) {
	shared := s.shortcuts["new_conversation"]
	if shared != "" && shared == s.shortcuts["new_terminal_tab"] {
		if mods, key, ok := parseShortcut(shared); ok {
			c.OnShortcut(mods, key, func() { s.newTabByRegion(loc) })
		}
		return
	}
	s.bindAction(c, "new_terminal_tab", func() {
		if root := s.terminalNewTabRoot(); root != "" {
			s.spawnTerminal(root)
		}
	})
	s.bindAction(c, "new_conversation", func() { s.newConversationShortcut(loc) })
}

func (s *Shell) newTabByRegion(loc string) {
	if s.terminalTakesShortcut() {
		if root := s.folderPathByID(s.selectedFolder); root != "" {
			s.spawnTerminal(root)
		}
		return
	}
	s.newConversationShortcut(loc)
}

func (s *Shell) newConversationShortcut(loc string) {
	s.section = "workspace"
	s.workbench = ""
	s.createChatConversation(loc)
}

// bindCloseTabPair registers close_current_terminal_tab and
// close_current_tab with the same shared-chord routing as the new-tab pair.
func (s *Shell) bindCloseTabPair(c *ui.Context) {
	shared := s.shortcuts["close_current_tab"]
	if shared != "" && shared == s.shortcuts["close_current_terminal_tab"] {
		if mods, key, ok := parseShortcut(shared); ok {
			c.OnShortcut(mods, key, func() { s.closeTabByRegion() })
		}
		return
	}
	s.bindAction(c, "close_current_terminal_tab", func() {
		if s.terminalTakesClose() {
			s.closeTerminalTab(s.termActive)
		}
	})
	s.bindAction(c, "close_current_tab", func() { s.closeConversationShortcut() })
}

func (s *Shell) closeTabByRegion() {
	if s.terminalTakesShortcut() {
		// No conversation closes while the terminal owns the chord, even
		// with no terminal tab left to close.
		if s.termActive != "" {
			s.closeTerminalTab(s.termActive)
		}
		return
	}
	s.closeConversationShortcut()
}

func (s *Shell) closeConversationShortcut() {
	if (s.section == "" || s.section == "workspace") && s.workbench == "" && s.selectedConv != "" {
		s.closeConversationTab(s.selectedConv)
	}
}

// terminalTakesShortcut reports upstream's routing rule: the terminal's own
// tab shortcuts only apply while the panel is open and the pointer or the line
// input is in it. Elsewhere the chord belongs to the conversation strip.
func (s *Shell) terminalTakesShortcut() bool {
	return s.termOpen && (s.termHovered || s.termInputFocused)
}

// terminalNewTabRoot is the folder a new terminal tab would open in, or ""
// when there is no folder or the terminal is not the shortcut's target.
func (s *Shell) terminalNewTabRoot() string {
	if !s.terminalTakesShortcut() {
		return ""
	}
	return s.folderPathByID(s.selectedFolder)
}

// terminalTakesClose is true when close_current_terminal_tab applies: the
// terminal has the shortcut and a tab to close.
func (s *Shell) terminalTakesClose() bool {
	return s.terminalTakesShortcut() && s.termActive != ""
}

func (s *Shell) bindAction(c *ui.Context, id string, fn func()) {
	chord, ok := s.shortcuts[id]
	if !ok {
		return
	}
	mods, key, ok := parseShortcut(chord)
	if !ok {
		return
	}
	c.OnShortcut(mods, key, fn)
}

// stepConversationTab focuses the next (+1) or previous (-1) open tab, wrapping.
func (s *Shell) stepConversationTab(delta int) {
	s.mu.Lock()
	n := len(s.openTabs)
	if n == 0 {
		s.mu.Unlock()
		return
	}
	i := s.tabIndexOf(s.selectedConv)
	if i < 0 {
		i = 0
	} else {
		i = (i + delta + n) % n
	}
	id := s.openTabs[i].convID
	s.mu.Unlock()
	s.focusConversationTab(id)
}

// focusTabIndex focuses the tab at index i when it exists.
func (s *Shell) focusTabIndex(i int) {
	s.mu.Lock()
	if i < 0 || i >= len(s.openTabs) {
		s.mu.Unlock()
		return
	}
	id := s.openTabs[i].convID
	s.mu.Unlock()
	s.focusConversationTab(id)
}

// openPathInFileManager hands a local path to the OS through the host's URL
// opener (file:///…). Used by the Skill Packs hub's folder button.
func openPathInFileManager(c *ui.Context, path string) {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	c.OpenURL("file://" + path)
}
