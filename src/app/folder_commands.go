package app

// folder_commands.go — the per-folder "command runner" panel (workbench route
// "commands"). It lists the slash commands scoped to the active folder
// (folder_command rows seeded by the user or bootstrapped from package.json),
// supports create / edit / delete CRUD, and "Run" streams the command into the
// shared bottom terminal panel.
//
// Backend contract (handlers/folder_commands.go): list_folder_commands /
// create_folder_command / update_folder_command / delete_folder_command /
// bootstrap_folder_commands_from_package_json, all keyed by folderId.

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"gocode/locales"
)

// folderCommandKey builds the cache + ensure key for a folder's command list.
func folderCommandKey(folderID string) string { return "folder-commands:" + folderID }

// viewFolderCommands is the workbench route "commands": manage the active
// folder's runnable commands.
func (s *Shell) viewFolderCommands(c *ui.Context) {
	t := c.Theme()
	root := s.folderPathByID(s.selectedFolder)
	if s.selectedFolder == "" || root == "" {
		emptyState(c, iconInfo, cat(c).CommandsNoFolder(), "")
		return
	}
	fid := numOrString(s.selectedFolder)
	cmds := asList(s.ensure(folderCommandKey(s.selectedFolder), "list_folder_commands", map[string]any{"folderId": fid}))

	ui.Text(c, cat(c).CommandsTitle()).FontSize(18).Bold()

	// New-command form.
	ui.Column(c.Key("cmd-new")).Gap(6).FillWidth().Children(func() {
		ui.Text(c, cat(c).CommandsNew()).Bold()
		ui.TextInput(c.Key("cmd-new-name"), &s.cmdNewName).Placeholder(cat(c).CommandsName()).FillWidth()
		ui.TextInput(c.Key("cmd-new-cmd"), &s.cmdNewCmd).Placeholder(cat(c).CommandsCommand()).FillWidth()
		ui.Row(c.Key("cmd-new-actions")).Gap(10).AlignItems(ui.Center).Children(func() {
			btn := ui.PrimaryButton(c.Key("cmd-create"), cat(c).CommandsCreate())
			btn.Disabled(strings.TrimSpace(s.cmdNewName) == "" || strings.TrimSpace(s.cmdNewCmd) == "")
			if btn.Clicked() {
				s.createFolderCommand(c)
			}
		})
	})

	// Bootstrap from package.json + the success line.
	ui.Row(c.Key("cmd-bootstrap-row")).Gap(10).AlignItems(ui.Center).Children(func() {
		if ui.Button(c.Key("cmd-bootstrap"), cat(c).CommandsBootstrap()).Clicked() {
			s.bootstrapFolderCommands(c, root)
		}
	})
	if s.cmdResult != "" {
		ui.Text(c, s.cmdResult).FontSize(12).TextColor(t.Success)
	}

	// List.
	if len(cmds) == 0 {
		emptyState(c, iconInfo, cat(c).CommandsEmpty(), "")
	} else {
		ui.Column(c.Key("cmd-list")).Gap(8).FillWidth().Children(func() {
			for i, m := range cmds {
				key := fmt.Sprint(intOf(m["id"]))
				if s.cmdEditID == key {
					s.viewCommandEditRow(c, key, m)
				} else {
					s.viewCommandRow(c, key, m, i > 0, i < len(cmds)-1)
				}
			}
		})
	}
	s.viewActionOrError(c)
}

// viewCommandRow renders one command: name + command + Run / Edit / Up / Down /
// Delete. Up/Down persist the new order through reorder_folder_commands (the
// list sorts by sort_order), matching upstream's reorder.
func (s *Shell) viewCommandRow(c *ui.Context, key string, m map[string]any, canUp, canDown bool) {
	t := c.Theme()
	ui.Row(c.Key("cmd-row-" + key)).Gap(10).AlignItems(ui.Center).PaddingY(6).Children(func() {
		ui.Column(c.Key("cmd-meta-" + key)).Gap(2).Grow(1).Children(func() {
			ui.Text(c, str(m, "name")).FontSize(13).Bold()
			ui.Text(c, str(m, "command")).FontSize(12).TextColor(t.TextMuted).Wrap()
		})
		// Run streams the command into the bottom terminal panel.
		if ui.Button(c.Key("cmd-run-"+key), cat(c).CommandsRun()).Clicked() {
			s.runFolderCommand(firstNonEmpty(str(m, "name"), cat(c).CommandsRun()), str(m, "command"))
		}
		if canUp {
			iconButton(c, "cmd-up-"+key, iconChevron, cat(c).CommandsMoveUp(), func() {
				s.moveFolderCommand(c, key, -1)
			})
		}
		if canDown {
			iconButton(c, "cmd-down-"+key, iconChevronD, cat(c).CommandsMoveDown(), func() {
				s.moveFolderCommand(c, key, 1)
			})
		}
		if ui.Button(c.Key("cmd-edit-"+key), cat(c).CommandsEdit()).Clicked() {
			s.cmdEditID = key
			s.cmdEditName = str(m, "name")
			s.cmdEditCmd = str(m, "command")
		}
		iconButton(c, "cmd-del-"+key, iconX, cat(c).CommandsDelete(), func() {
			s.deleteFolderCommand(key)
		})
	})
	ui.Divider(c)
}

// viewCommandEditRow renders the inline edit form for one command.
func (s *Shell) viewCommandEditRow(c *ui.Context, key string, m map[string]any) {
	_ = m
	ui.Row(c.Key("cmd-edit-" + key)).Gap(8).AlignItems(ui.Center).PaddingY(6).Children(func() {
		ui.TextInput(c.Key("cmd-edit-name-"+key), &s.cmdEditName).Placeholder(cat(c).CommandsName()).Grow(1)
		ui.TextInput(c.Key("cmd-edit-cmd-"+key), &s.cmdEditCmd).Placeholder(cat(c).CommandsCommand()).Grow(1)
		if ui.Button(c.Key("cmd-edit-save-"+key), cat(c).CommandsSave()).Clicked() {
			s.updateFolderCommand(c, key)
		}
		if ui.Button(c.Key("cmd-edit-cancel-"+key), cat(c).CommandsCancel()).Clicked() {
			s.cmdEditID = ""
			s.cmdEditName, s.cmdEditCmd = "", ""
		}
	})
	ui.Divider(c)
}

// createFolderCommand validates and inserts a new command off the UI thread.
func (s *Shell) createFolderCommand(c *ui.Context) {
	loc := c.Locale()
	name := strings.TrimSpace(s.cmdNewName)
	cmd := strings.TrimSpace(s.cmdNewCmd)
	if name == "" {
		s.mu.Lock()
		s.errorMsg = locales.In(loc).CommandsNameRequired()
		s.mu.Unlock()
		return
	}
	if cmd == "" {
		s.mu.Lock()
		s.errorMsg = locales.In(loc).CommandsCommandRequired()
		s.mu.Unlock()
		return
	}
	fid := s.selectedFolder
	s.cmdNewName, s.cmdNewCmd = "", ""
	svc := s.svc
	go func() {
		_, err := s.B.raw("create_folder_command", map[string]any{
			"folderId": fid, "name": name, "command": cmd,
		})
		s.mu.Lock()
		s.cmdResult = ""
		if err != nil {
			s.errorMsg = err.Error()
		} else {
			s.errorMsg = ""
			s.cmdResult = locales.In(loc).CommandsCreate()
			delete(s.cache, folderCommandKey(fid))
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// updateFolderCommand saves an edited command.
func (s *Shell) updateFolderCommand(_ *ui.Context, key string) {
	name := strings.TrimSpace(s.cmdEditName)
	cmd := strings.TrimSpace(s.cmdEditCmd)
	if name == "" || cmd == "" {
		return
	}
	fid := s.selectedFolder
	s.cmdEditID, s.cmdEditName, s.cmdEditCmd = "", "", ""
	svc := s.svc
	go func() {
		_, err := s.B.raw("update_folder_command", map[string]any{
			"id": key, "name": name, "command": cmd,
		})
		s.mu.Lock()
		s.cmdResult = ""
		if err != nil {
			s.errorMsg = err.Error()
		} else {
			s.errorMsg = ""
			delete(s.cache, folderCommandKey(fid))
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// moveFolderCommand swaps a command with its neighbour and persists the new
// order (reorder_folder_commands takes the whole id list).
func (s *Shell) moveFolderCommand(c *ui.Context, key string, dir int) {
	list := asList(s.ensure(folderCommandKey(s.selectedFolder), "list_folder_commands", map[string]any{"folderId": numOrString(s.selectedFolder)}))
	idx := -1
	for i, m := range list {
		if fmt.Sprint(intOf(m["id"])) == key {
			idx = i
			break
		}
	}
	at := idx + dir
	if idx < 0 || at < 0 || at >= len(list) {
		return
	}
	list[idx], list[at] = list[at], list[idx]
	ids := make([]any, 0, len(list))
	for _, m := range list {
		ids = append(ids, numOrString(fmt.Sprint(intOf(m["id"]))))
	}
	svc := s.svc
	loc := c.Locale()
	fid := s.selectedFolder
	go func() {
		_, err := s.B.raw("reorder_folder_commands", map[string]any{
			"folderId": numOrString(fid), "ids": ids,
		})
		s.mu.Lock()
		s.cmdResult = ""
		if err != nil {
			s.errorMsg = locales.In(loc).CommandsReorderFailed(err.Error())
		} else {
			s.errorMsg = ""
			delete(s.cache, folderCommandKey(fid))
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// deleteFolderCommand removes a command.
func (s *Shell) deleteFolderCommand(key string) {
	fid := s.selectedFolder
	if s.cmdEditID == key {
		s.cmdEditID, s.cmdEditName, s.cmdEditCmd = "", "", ""
	}
	svc := s.svc
	go func() {
		_, err := s.B.raw("delete_folder_command", map[string]any{"id": key})
		s.mu.Lock()
		s.cmdResult = ""
		if err != nil {
			s.errorMsg = err.Error()
		} else {
			s.errorMsg = ""
			delete(s.cache, folderCommandKey(fid))
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// bootstrapFolderCommands imports the folder's package.json scripts as commands.
func (s *Shell) bootstrapFolderCommands(c *ui.Context, root string) {
	loc := c.Locale()
	fid := s.selectedFolder
	svc := s.svc
	go func() {
		raw, err := s.B.raw("bootstrap_folder_commands_from_package_json", map[string]any{
			"folderId": fid, "folderPath": root,
		})
		s.mu.Lock()
		s.cmdResult = ""
		if err != nil {
			s.errorMsg = err.Error()
		} else {
			s.errorMsg = ""
			list := asList(raw)
			delete(s.cache, folderCommandKey(fid))
			s.cmdResult = locales.In(loc).CommandsBootstrapped(len(list))
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// runFolderCommand opens a new terminal tab in the selected folder and starts
// the command there (upstream's createTerminalWithCommand), expanding the
// shared bottom panel so the run is visible.
func (s *Shell) runFolderCommand(title, cmd string) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return
	}
	root := s.folderPathByID(s.selectedFolder)
	if root == "" {
		return
	}
	s.newTerminalTab(title, root, cmd)
}
