package app

// palette.go — the ⌘K command palette (UI_GAPS P2): a modal that lists the
// app's commands, filters them by a typed query, and runs the chosen one. It is
// the keyboard-first way to reach navigation and a few actions without hunting
// the chrome. mygo's ui.Modal gives the dimmed backdrop, focus and Escape/click-
// outside dismissal; ui.TextInput's OnTextKey gives Enter / Up / Down / Escape.

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
)

// modKeyLabel is the platform's shortcut modifier as users write it: ⌘ on macOS,
// Ctrl+ elsewhere.
func modKeyLabel() string {
	if runtime.GOOS == "darwin" {
		return "⌘"
	}
	return "Ctrl+"
}

// paletteCommand is one entry in the palette.
type paletteCommand struct {
	id    string
	label string
	hint  string // the right-aligned group
	icon  *ui.SVG
	run   func()
}

// paletteCommands is the catalog, in the order shown when the query is empty.
func (s *Shell) paletteCommands(c *ui.Context) []paletteCommand {
	settings := cat(c).SectionSettings()
	return []paletteCommand{
		{"new-chat", cat(c).ActionNewChat(), cat(c).PaletteChat(), iconNewChat, func() {
			s.section = "workspace"
			s.workbench = ""
			s.createChatConversation(c.Locale())
		}},
		// Conversation exports: the header's two icons, reachable from ⌘K too.
		// Both need an open conversation, so they no-op without one.
		{"copy-markdown", cat(c).ActionCopyMarkdown(), cat(c).PaletteChat(), iconFile, func() {
			if s.selectedConv == "" {
				return
			}
			md := s.conversationMarkdown(cat(c), s.selectedConv)
			if strings.TrimSpace(md) == "" {
				return
			}
			c.WriteClipboard(md)
			s.notify(cat(c).ToastCopied())
		}},
		{"export-html", cat(c).ActionExportHTML(), cat(c).PaletteChat(), iconDownload, func() {
			if s.selectedConv == "" {
				return
			}
			s.exportHTML(c.Locale(), s.selectedConv)
		}},
		{"automations", cat(c).NavAutomations(), cat(c).PaletteWorkbench(), iconAutomations, func() {
			s.section = "workspace"
			s.workbench = "automations"
		}},
		{"tasks", cat(c).NavTodos(), cat(c).PaletteWorkbench(), iconTasks, func() {
			s.section = "workspace"
			s.workbench = "tasks"
		}},
		{"forge", cat(c).NavRepositories(), cat(c).PaletteWorkbench(), iconForge, func() {
			s.section = "workspace"
			s.workbench = "forge"
		}},
		{"canvas", cat(c).NavCanvas(), cat(c).PaletteWorkbench(), iconCanvas, func() {
			s.section = "workspace"
			s.workbench = "canvas"
		}},
		{"settings", settings, cat(c).PaletteGoto(), iconSettings, func() {
			s.section = "settings"
		}},
		{"pet", cat(c).ActiveSessions(), cat(c).PaletteGoto(), iconPet, func() {
			s.section = "pet"
		}},
		{"settings-agents", settings + ": " + cat(c).NavAgents(), cat(c).PaletteGoto(), iconSettings, func() {
			s.section, s.settingsPage = "settings", "agents"
		}},
		{"settings-version-control", settings + ": " + cat(c).NavVersionControl(), cat(c).PaletteGoto(), iconSettings, func() {
			s.section, s.settingsPage = "settings", "version-control"
		}},
		{"settings-servers", settings + ": " + cat(c).NavServers(), cat(c).PaletteGoto(), iconSettings, func() {
			s.section, s.settingsPage = "settings", "servers"
		}},
		{"theme-light", cat(c).NavAppearance() + ": " + cat(c).ThemeLight(), cat(c).PaletteTheme(), iconInfo, func() { s.setAppearanceMode(c, "light") }},
		{"theme-dark", cat(c).NavAppearance() + ": " + cat(c).ThemeDark(), cat(c).PaletteTheme(), iconInfo, func() { s.setAppearanceMode(c, "dark") }},
		{"theme-system", cat(c).NavAppearance() + ": " + cat(c).ThemeSystem(), cat(c).PaletteTheme(), iconInfo, func() { s.setAppearanceMode(c, "system") }},
	}
}

// conversationPaletteCommands lists the conversations whose title contains the
// query. ⌘K is the app's search affordance, and with fifteen agents a chat you
// cannot find by name is a chat you cannot reach — the catalog alone only
// searches commands.
//
// It searches the cached `all-convs` list, so a conversation in another folder
// is reachable too, and falls back to the open folder's list when that cache is
// cold. Cached rows only: the palette never triggers a fetch while typing.
func (s *Shell) conversationPaletteCommands() []paletteCommand {
	q := strings.ToLower(strings.TrimSpace(s.paletteQuery))
	if q == "" {
		return nil
	}
	rows := asList(s.peek("all-convs"))
	if len(rows) == 0 {
		if s.selectedFolder == "" {
			return nil
		}
		rows = asList(s.peek("convs:" + s.selectedFolder))
	}
	out := []paletteCommand{}
	for _, cv := range rows {
		if cv == nil {
			continue
		}
		title := str(cv, "title")
		if title == "" || !strings.Contains(strings.ToLower(title), q) {
			continue
		}
		out = append(out, paletteCommand{
			id:    "conv-" + fmt.Sprintf("%v", cv["id"]),
			label: title,
			hint:  str(cv, "agent_type"),
			icon:  iconChat,
			run:   func() { s.openConversationRow(cv) },
		})
		if len(out) >= 8 {
			break
		}
	}
	return out
}

// filteredPaletteCommands narrows the catalog by the query (case-insensitive
// substring on the label).
func (s *Shell) filteredPaletteCommands(c *ui.Context) []paletteCommand {
	// Conversations come first: when you type a chat's name, that chat is what
	// you are looking for.
	all := append(s.conversationPaletteCommands(), s.paletteCommands(c)...)
	q := strings.ToLower(strings.TrimSpace(s.paletteQuery))
	if q == "" {
		return all
	}
	out := make([]paletteCommand, 0, len(all))
	for _, cmd := range all {
		if strings.Contains(strings.ToLower(cmd.label), q) {
			out = append(out, cmd)
		}
	}
	return out
}

// viewCommandPalette renders the ⌘K modal while s.paletteOpen is true. ui.Modal
// dims the window, focuses the dialog, and sets s.paletteOpen to false on
// Escape or a click outside.
func (s *Shell) viewCommandPalette(c *ui.Context) {
	if !s.paletteOpen {
		return
	}
	t := c.Theme()
	cmds := s.filteredPaletteCommands(c)
	if s.paletteSel >= len(cmds) {
		s.paletteSel = len(cmds) - 1
	}
	if s.paletteSel < 0 {
		s.paletteSel = 0
	}
	ui.Modal(c, &s.paletteOpen, func() {
		ui.Column(c.Key("palette")).Width(560).MaxHeight(440).Background(t.Background).
			Border(1, t.Text.Alpha(0.2)).Radius(12).Children(func() {
			ui.Row(c).Padding(12, 14, 10, 14).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconSearch).Width(15).Height(15).TextColor(t.TextMuted)
				ui.TextInput(c.Key("palette-input"), &s.paletteQuery).
					Placeholder(cat(c).PalettePlaceholder()).Grow(1).AutoFocus().
					OnTextKey(func(k ui.TextKey) bool {
						switch {
						case k.Key == ui.KeyEnter && k.Mods == 0:
							s.runPaletteSelection(c)
							return true
						case k.Key == ui.KeyDown:
							s.paletteSel++
							return true
						case k.Key == ui.KeyUp:
							s.paletteSel--
							return true
						case k.Key == ui.KeyEscape:
							s.paletteOpen = false
							return true
						}
						return false
					})
			})
			ui.Divider(c)
			ui.Scroll(c).Grow(1).MaxHeight(360).Children(func() {
				ui.Column(c).Gap(1).Padding(6).Children(func() {
					if len(cmds) == 0 {
						ui.Text(c, cat(c).PaletteNoMatches()).FontSize(12).TextColor(t.TextMuted).Padding(8)
						return
					}
					for i, cmd := range cmds {
						s.paletteRow(c, i, cmd)
					}
				})
			})
		})
	})
}

// paletteRow is one command row: highlighted when selected, run on click.
func (s *Shell) paletteRow(c *ui.Context, i int, cmd paletteCommand) {
	t := c.Theme()
	bg := t.Background
	if i == s.paletteSel {
		bg = t.SurfaceHover
	}
	row := ui.Row(c.Key("palette-row-" + cmd.id)).FillWidth().PaddingX(10).PaddingY(8).Gap(10).
		AlignItems(ui.Center).Radius(6).Background(bg).Label(cmd.label)
	row.Children(func() {
		if cmd.icon != nil {
			ui.Icon(c, cmd.icon).Width(15).Height(15).TextColor(t.TextMuted)
		}
		ui.Text(c, cmd.label).FontSize(13).Grow(1)
		if cmd.hint != "" {
			ui.Text(c, cmd.hint).FontSize(11).TextColor(t.TextMuted)
		}
	})
	if row.Clicked() {
		s.runPaletteCommand(cmd)
	}
}

// runPaletteSelection runs the highlighted command (Enter in the palette).
func (s *Shell) runPaletteSelection(c *ui.Context) {
	cmds := s.filteredPaletteCommands(c)
	if s.paletteSel < 0 || s.paletteSel >= len(cmds) {
		return
	}
	s.runPaletteCommand(cmds[s.paletteSel])
}

// runPaletteCommand runs a command and closes the palette.
func (s *Shell) runPaletteCommand(cmd paletteCommand) {
	s.paletteOpen = false
	s.paletteQuery = ""
	s.paletteSel = 0
	if cmd.run != nil {
		cmd.run()
	}
}

// stopActiveTurn cancels the in-flight turn of the selected conversation (Esc).
// It does nothing when no turn is running, so Esc stays quiet otherwise.
func (s *Shell) stopActiveTurn(loc string) {
	s.mu.Lock()
	connID := s.connByConv[s.selectedConv]
	raw := s.cache["snapshot:"+connID]
	s.mu.Unlock()
	if connID == "" || str(asObj(raw), "status") != "prompting" {
		return
	}
	s.cancelTurn(loc, connID)
}
