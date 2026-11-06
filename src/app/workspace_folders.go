package app

// workspace_folders.go — the Workspace folders panel (workbench route
// "workspace-folders"). It links external directories into the active folder as
// named links (list/preview/create/rename/repair/remove folder links), and
// uploads a local file into the workspace over the multipart
// upload_workspace_file route. Adding a link is a two-step dry run: preview
// reports rejections (inside the workspace, already linked, …) and the name the
// link would get, then the user confirms the create.
//
// Backend contract (handlers/folder_links.go + workspace_file_transfer.go):
// list_folder_links {folderId} → [{id,folderId,name,targetPath,status}];
// preview_folder_links {folderId,paths:[…]} → [{targetPath,baseName,name,
//   renamed,collidesWithExistingEntry,rejection,existingLinkName}];
// create_folder_links {folderId,items:[{path,name?}],gitExclude} → created;
// rename_folder_link {linkId,newName}; repair_folder_link {linkId};
// remove_folder_link {linkId};
// upload_workspace_file (multipart: root_path, relative_path?, file) → {path,name,size}.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"gocode/locales"
)

// wfCacheKey builds the cache key for a folder's link list.
func wfCacheKey(folderID string) string { return "folder-links:" + folderID }

// wfLinkPlan is one preview_folder_links dry run. rejection is empty for a
// usable plan; otherwise it carries the backend code (see wfRejectionLabel).
type wfLinkPlan struct {
	targetPath string
	baseName   string
	name       string
	renamed    bool
	collides   bool // renamed because the base name is taken by a real entry
	rejection  string
	existing   string // existing link's name for "already_linked"
}

// wfPlanFrom decodes one preview_folder_links entry.
func wfPlanFrom(m map[string]any) *wfLinkPlan {
	return &wfLinkPlan{
		targetPath: str(m, "targetPath"),
		baseName:   str(m, "baseName"),
		name:       str(m, "name"),
		renamed:    asBool(m["renamed"]),
		collides:   asBool(m["collidesWithExistingEntry"]),
		rejection:  str(m, "rejection"),
		existing:   str(m, "existingLinkName"),
	}
}

// viewWorkspaceFolders renders the folder links + upload forms.
func (s *Shell) viewWorkspaceFolders(c *ui.Context) {
	t := c.Theme()
	root := s.folderPathByID(s.selectedFolder)
	if s.selectedFolder == "" || root == "" {
		emptyState(c, iconInfo, cat(c).WfNoFolder(), "")
		return
	}
	fid := numOrString(s.selectedFolder)
	links := asList(s.ensure(wfCacheKey(s.selectedFolder), "list_folder_links", map[string]any{"folderId": fid}))

	ui.Row(c.Key("wf-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, cat(c).WfTitle()).FontSize(18).Bold()
		ui.Spacer(c)
		iconButton(c, "wf-refresh", iconRefresh, cat(c).ActionRefresh(), func() {
			s.invalidate(wfCacheKey(s.selectedFolder))
		})
	})

	// Add-link form.
	ui.Column(c.Key("wf-add")).Gap(6).FillWidth().Children(func() {
		ui.Text(c, cat(c).WfAdd()).Bold()
		ui.Row(c.Key("wf-add-r1")).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("wf-target"), &s.wfTarget).Placeholder(cat(c).WfTarget()).Grow(1)
			if ui.Button(c.Key("wf-choose"), cat(c).WfChoose()).Clicked() {
				s.pickPaths(cat(c).WfChoose(), true, false, func(picked []string) {
					s.mu.Lock()
					s.wfTarget = picked[0]
					s.mu.Unlock()
				})
			}
		})
		ui.Row(c.Key("wf-add-r2")).Gap(8).AlignItems(ui.Center).Children(func() {
			if s.wfPlan == nil {
				ui.TextInput(c.Key("wf-name"), &s.wfName).Placeholder(cat(c).WfName()).Width(220)
			}
			btn := ui.PrimaryButton(c.Key("wf-add-btn"), cat(c).WfAddAction())
			btn.Disabled(strings.TrimSpace(s.wfTarget) == "" || s.wfPlan != nil)
			if btn.Clicked() {
				s.previewFolderLink(c)
			}
			// Batch mode: stack several targets, then dry-run them in one call
			// (the backend dedupes names across the batch).
			if ui.Button(c.Key("wf-add-batch"), cat(c).WfAddToBatch()).Clicked() {
				if t := strings.TrimSpace(s.wfTarget); t != "" {
					s.wfBatchPaths = append(s.wfBatchPaths, t)
					s.wfTarget = ""
				}
			}
			if len(s.wfBatchPaths) > 0 && s.wfBatchPlan == nil {
				if ui.Button(c.Key("wf-batch-preview"), cat(c).WfPreviewBatch()).Clicked() {
					s.previewFolderLinksBatch(c)
				}
			}
		})
		// Pending targets as removable chips.
		for i, p := range s.wfBatchPaths {
			chip := ui.Row(c.Key(fmt.Sprintf("wf-batch-chip-%d", i))).Padding(2, 8, 2, 8).
				Radius(6).Gap(5).AlignItems(ui.Center).Background(t.SurfaceHover)
			chip.Children(func() {
				ui.Text(c, p).FontSize(11).MaxLines(1)
				x := ui.Row(c.Key(fmt.Sprintf("wf-batch-x-%d", i))).Width(14).Height(14).
					AlignItems(ui.Center).Justify(ui.Center).Label(cat(c).ActionDelete())
				x.Children(func() {
					ui.Icon(c, iconX).Width(9).Height(9).TextColor(t.TextMuted)
				})
				if x.Clicked() {
					s.mu.Lock()
					s.wfBatchPaths = append(s.wfBatchPaths[:i:i], s.wfBatchPaths[i+1:]...)
					s.mu.Unlock()
				}
			})
		}
	})

	// Dry-run result: a usable plan waits for confirmation; a rejection keeps
	// the reason visible until dismissed. The batch's plans list in one card.
	if s.wfPlan != nil {
		s.viewFolderLinkPlan(c, root)
	}
	if len(s.wfBatchPlan) > 0 {
		s.viewFolderLinkBatchPlan(c, root)
	}

	// Link list.
	if len(links) == 0 {
		emptyState(c, iconInfo, cat(c).WfEmpty(), "")
	} else {
		ui.Column(c.Key("wf-list")).Gap(8).FillWidth().Children(func() {
			for _, l := range links {
				id := fmt.Sprint(intOf(l["id"]))
				if s.wfEditID == id {
					s.viewFolderLinkEdit(c, id)
				} else {
					s.viewFolderLinkRow(c, id, l)
				}
			}
		})
	}

	// Upload section: a multi-select picker queues files; one file keeps the
	// optional relative subpath, several land under their base names.
	ui.Divider(c)
	ui.Text(c, cat(c).WfUpload()).Bold()
	ui.Row(c.Key("wf-up-r1")).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.Button(c.Key("wf-up-pick"), cat(c).WfUploadPick()).Clicked() {
			s.pickPaths(cat(c).WfUploadPick(), false, true, func(picked []string) {
				s.mu.Lock()
				s.wfUploadPaths = append(s.wfUploadPaths, picked...)
				s.mu.Unlock()
			})
		}
		for i, p := range s.wfUploadPaths {
			chip := ui.Row(c.Key(fmt.Sprintf("wf-up-chip-%d", i))).Padding(2, 8, 2, 8).
				Radius(6).Gap(5).AlignItems(ui.Center).Background(t.SurfaceHover)
			chip.Children(func() {
				ui.Text(c, filepath.Base(p)).FontSize(11).MaxLines(1)
				x := ui.Row(c.Key(fmt.Sprintf("wf-up-x-%d", i))).Width(14).Height(14).
					AlignItems(ui.Center).Justify(ui.Center).Label(cat(c).ActionDelete())
				x.Children(func() {
					ui.Icon(c, iconX).Width(9).Height(9).TextColor(t.TextMuted)
				})
				if x.Clicked() {
					s.mu.Lock()
					s.wfUploadPaths = append(s.wfUploadPaths[:i:i], s.wfUploadPaths[i+1:]...)
					s.mu.Unlock()
				}
			})
		}
	})
	ui.Row(c.Key("wf-up-r2")).Gap(8).AlignItems(ui.Center).Children(func() {
		n := len(s.wfUploadPaths)
		if n == 0 && strings.TrimSpace(s.wfUploadPath) != "" {
			n = 1 // the single-file flow (tests, and a pick that predates the queue)
		}
		if n == 1 {
			ui.TextInput(c.Key("wf-up-rel"), &s.wfUploadRel).Placeholder(cat(c).WfUploadRel()).Grow(1)
		}
		btn := ui.PrimaryButton(c.Key("wf-up-go"), cat(c).WfUploadAction())
		if n > 1 {
			btn.Label(cat(c).WfUploadMany(itoa("", n)))
		}
		btn.Disabled(n == 0)
		if btn.Clicked() {
			s.uploadWorkspaceFiles(c, root)
		}
	})

	s.viewActionOrError(c)
}

// viewFolderLinkPlan renders the dry-run result. A usable plan waits for the
// user to confirm (and lets them edit the name); a rejected target shows why.
func (s *Shell) viewFolderLinkPlan(c *ui.Context, root string) {
	t := c.Theme()
	p := s.wfPlan
	card := ui.Column(c.Key("wf-plan")).Gap(6).FillWidth().Padding(10).Radius(8).
		Background(t.Surface)
	card.Children(func() {
		if p.rejection != "" {
			ui.Row(c.Key("wf-plan-reject")).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconAlert).Width(15).Height(15).TextColor(t.Warning)
				ui.Column(c.Key("wf-plan-reject-meta")).Gap(2).Grow(1).Children(func() {
					ui.Text(c, p.targetPath).FontSize(12).Bold().MaxLines(1)
					ui.Text(c, wfRejectionLabel(c, p)).FontSize(12).TextColor(t.Warning)
				})
				if ui.Button(c.Key("wf-plan-dismiss"), cat(c).WfCancel()).Clicked() {
					s.wfPlan = nil
					s.wfName = ""
				}
			})
			return
		}
		ui.Text(c, cat(c).WfPreviewTitle()).FontSize(12).Bold()
		ui.Row(c.Key("wf-plan-name-row")).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("wf-plan-name"), &s.wfName).Grow(1)
			if ui.Button(c.Key("wf-plan-discard"), cat(c).WfDiscard()).Clicked() {
				s.wfPlan = nil
				s.wfName = ""
				return
			}
			btn := ui.PrimaryButton(c.Key("wf-plan-create"), cat(c).WfLinkCount(1))
			btn.Disabled(strings.TrimSpace(s.wfName) == "")
			if btn.Clicked() {
				s.createFolderLink(c, root)
			}
		})
		ui.Text(c, p.targetPath).FontSize(12).TextColor(t.TextMuted).MaxLines(1)
		if p.renamed {
			hint := cat(c).WfRenamedForDuplicate(p.baseName)
			if p.collides {
				hint = cat(c).WfRenamedForExistingEntry(p.baseName)
			}
			ui.Text(c, hint).FontSize(12).TextColor(t.Warning)
		} else {
			ui.Text(c, cat(c).WfWillAppearAs(p.name)).FontSize(12).TextColor(t.TextMuted)
		}
		ui.Checkbox(c.Key("wf-git-exclude"), &s.wfGitExclude, cat(c).WfGitExclude())
	})
}

// wfRejectionLabel localizes a preview rejection code.
func wfRejectionLabel(c *ui.Context, p *wfLinkPlan) string {
	switch p.rejection {
	case "not_found":
		return cat(c).WfRejectNotFound()
	case "not_a_directory":
		return cat(c).WfRejectNotADirectory()
	case "same_as_root":
		return cat(c).WfRejectSameAsRoot()
	case "ancestor_of_root":
		return cat(c).WfRejectAncestorOfRoot()
	case "inside_root":
		return cat(c).WfRejectInsideRoot()
	case "already_linked":
		if p.existing != "" {
			return cat(c).WfRejectAlreadyLinkedAs(p.existing)
		}
		return cat(c).WfRejectAlreadyLinked()
	case "name_unavailable":
		return cat(c).WfRejectNameUnavailable()
	}
	return p.rejection
}

// viewFolderLinkRow renders one link: name + target + status + Recreate /
// Rename / Remove.
func (s *Shell) viewFolderLinkRow(c *ui.Context, id string, l map[string]any) {
	t := c.Theme()
	name := str(l, "name")
	status := str(l, "status")
	col := t.TextMuted
	switch status {
	case "ok":
		col = t.Success
	case "conflicted":
		col = t.Danger
	case "missing", "broken":
		col = t.Warning
	}
	ui.Row(c.Key("wf-row-" + id)).Gap(10).AlignItems(ui.Center).PaddingY(6).Children(func() {
		ui.Column(c.Key("wf-meta-" + id)).Gap(2).Grow(1).Children(func() {
			ui.Text(c, name).FontSize(13).Bold()
			ui.Text(c, str(l, "targetPath")).FontSize(12).TextColor(t.TextMuted).MaxLines(1)
		})
		ui.Text(c, wfStatusLabel(c, status)).FontSize(11).TextColor(col)
		// Recreate only makes sense when the entry itself is gone or dangling;
		// a name taken by a real directory has to be fixed by hand.
		if status == "missing" || status == "broken" {
			iconButton(c, "wf-repair-"+id, iconRefresh, cat(c).WfRepair(), func() {
				s.repairFolderLink(c, id)
			})
		}
		if ui.Button(c.Key("wf-ren-"+id), cat(c).WfRename()).Clicked() {
			s.wfEditID = id
			s.wfEditName = name
		}
		if s.wfRemoveConfirm == id {
			ui.Text(c, cat(c).WfRemovePrompt(name)).FontSize(12).TextColor(t.Danger)
			if ui.Button(c.Key("wf-confirm-"+id), cat(c).WfConfirmRemove()).Clicked() {
				s.wfRemoveConfirm = ""
				s.removeFolderLink(c, id)
			}
			if ui.Button(c.Key("wf-cancel-"+id), cat(c).WfCancel()).Clicked() {
				s.wfRemoveConfirm = ""
			}
		} else {
			iconButton(c, "wf-del-"+id, iconX, cat(c).WfRemove(), func() { s.wfRemoveConfirm = id })
		}
	})
	ui.Divider(c)
}

// viewFolderLinkEdit renders the inline rename form.
func (s *Shell) viewFolderLinkEdit(c *ui.Context, id string) {
	ui.Row(c.Key("wf-edit-" + id)).Gap(8).AlignItems(ui.Center).PaddingY(6).Children(func() {
		ui.TextInput(c.Key("wf-edit-name-"+id), &s.wfEditName).Placeholder(cat(c).WfName()).Grow(1)
		if ui.Button(c.Key("wf-save-"+id), cat(c).WfSave()).Clicked() {
			s.renameFolderLink(c, id)
		}
		if ui.Button(c.Key("wf-edit-cancel-"+id), cat(c).WfCancel()).Clicked() {
			s.wfEditID = ""
			s.wfEditName = ""
		}
	})
	ui.Divider(c)
}

// wfStatusLabel localizes a link status code.
func wfStatusLabel(c *ui.Context, status string) string {
	switch status {
	case "ok":
		return cat(c).WfStatusOk()
	case "missing":
		return cat(c).WfStatusMissing()
	case "broken":
		return cat(c).WfStatusBroken()
	case "conflicted":
		return cat(c).WfStatusConflicted()
	}
	return status
}

// previewFolderLink runs the backend dry run for the typed target. Nothing is
// linked until the user confirms the returned plan.
func (s *Shell) previewFolderLink(c *ui.Context) {
	loc := c.Locale()
	target := strings.TrimSpace(s.wfTarget)
	if target == "" {
		s.mu.Lock()
		s.errorMsg = locales.In(loc).WfTargetRequired()
		s.mu.Unlock()
		return
	}
	fid := numOrString(s.selectedFolder)
	// A pending plan's name belongs to its own target; a new preview starts
	// from the form's name only when no plan is waiting.
	typedName := strings.TrimSpace(s.wfName)
	if s.wfPlan != nil {
		typedName = ""
	}
	s.wfTarget, s.wfName, s.wfPlan = "", "", nil
	svc := s.svc
	go func() {
		raw, err := s.B.raw("preview_folder_links", map[string]any{
			"folderId": fid, "paths": []any{target},
		})
		s.mu.Lock()
		if err != nil {
			s.errorMsg = err.Error()
		} else {
			s.errorMsg = ""
			if plans := asList(raw); len(plans) > 0 {
				p := wfPlanFrom(plans[0])
				s.wfPlan = p
				if p.rejection == "" {
					if typedName != "" {
						s.wfName = typedName
					} else {
						s.wfName = p.name
					}
				}
			}
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// createFolderLink links the confirmed preview's target under its (possibly
// edited) name.
func (s *Shell) createFolderLink(c *ui.Context, root string) {
	loc := c.Locale()
	p := s.wfPlan
	if p == nil || p.rejection != "" {
		return
	}
	name := strings.TrimSpace(s.wfName)
	if name == "" {
		return
	}
	fid := numOrString(s.selectedFolder)
	folder := s.selectedFolder
	gitExclude := s.wfGitExclude
	s.wfPlan, s.wfTarget, s.wfName = nil, "", ""
	svc := s.svc
	go func() {
		_, err := s.B.raw("create_folder_links", map[string]any{
			"folderId":   fid,
			"items":      []any{map[string]any{"path": p.targetPath, "name": name}},
			"gitExclude": gitExclude,
		})
		s.mu.Lock()
		if err != nil {
			s.errorMsg = err.Error()
		} else {
			s.errorMsg = ""
			s.actionMsg = locales.In(loc).WfLinked()
			delete(s.cache, wfCacheKey(folder))
			delete(s.cache, "ft:"+root)
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// repairFolderLink recreates a missing/broken link at its recorded name.
func (s *Shell) repairFolderLink(c *ui.Context, id string) {
	loc := c.Locale()
	folder := s.selectedFolder
	svc := s.svc
	go func() {
		_, err := s.B.raw("repair_folder_link", map[string]any{"linkId": numOrString(id)})
		s.mu.Lock()
		if err != nil {
			s.errorMsg = err.Error()
		} else {
			s.errorMsg = ""
			s.actionMsg = locales.In(loc).WfRepaired()
			delete(s.cache, wfCacheKey(folder))
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// renameFolderLink renames a link.
func (s *Shell) renameFolderLink(c *ui.Context, id string) {
	loc := c.Locale()
	name := strings.TrimSpace(s.wfEditName)
	if name == "" {
		return
	}
	folder := s.selectedFolder
	s.wfEditID, s.wfEditName = "", ""
	svc := s.svc
	go func() {
		_, err := s.B.raw("rename_folder_link", map[string]any{"linkId": numOrString(id), "newName": name})
		s.mu.Lock()
		if err != nil {
			s.errorMsg = err.Error()
		} else {
			s.errorMsg = ""
			s.actionMsg = locales.In(loc).WfRenamed()
			delete(s.cache, wfCacheKey(folder))
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// removeFolderLink removes a link.
func (s *Shell) removeFolderLink(c *ui.Context, id string) {
	loc := c.Locale()
	folder := s.selectedFolder
	svc := s.svc
	go func() {
		_, err := s.B.raw("remove_folder_link", map[string]any{"linkId": numOrString(id)})
		s.mu.Lock()
		if err != nil {
			s.errorMsg = err.Error()
		} else {
			s.errorMsg = ""
			s.actionMsg = locales.In(loc).WfRemoved()
			delete(s.cache, wfCacheKey(folder))
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// uploadWorkspaceFile uploads the picked local file into the workspace root.
func (s *Shell) uploadWorkspaceFiles(c *ui.Context, root string) {
	loc := c.Locale()
	paths := append([]string(nil), s.wfUploadPaths...)
	if len(paths) == 0 && strings.TrimSpace(s.wfUploadPath) != "" {
		paths = []string{strings.TrimSpace(s.wfUploadPath)}
	}
	if len(paths) == 0 {
		s.mu.Lock()
		s.errorMsg = locales.In(loc).WfUploadNoFile()
		s.mu.Unlock()
		return
	}
	rel := strings.TrimSpace(s.wfUploadRel)
	svc := s.svc
	go func() {
		var firstErr error
		var lastPath string
		ok := 0
		for i, path := range paths {
			fields := map[string]string{"root_path": root}
			if len(paths) == 1 && rel != "" {
				fields["relative_path"] = rel
			}
			raw, err := s.B.upload("upload_workspace_file", fields, "file", path)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			ok++
			lastPath = path
			if i == len(paths)-1 {
				var res map[string]any
				_ = json.Unmarshal(raw, &res)
			}
		}
		s.mu.Lock()
		if firstErr != nil {
			s.errorMsg = firstErr.Error()
		} else {
			s.errorMsg = ""
			if ok == 1 {
				s.actionMsg = locales.In(loc).WfUploaded(lastPath)
			} else {
				s.actionMsg = locales.In(loc).WfUploadedMany(itoa("", ok))
			}
			s.wfUploadPaths = nil
			s.wfUploadRel = ""
		}
		s.mu.Unlock()
		s.refreshWorkspaceTree(root)
		svc.Invalidate()
	}()
}

// previewFolderLinksBatch dry-runs every pending target in one call.
func (s *Shell) previewFolderLinksBatch(c *ui.Context) {
	fid := numOrString(s.selectedFolder)
	paths := append([]string(nil), s.wfBatchPaths...)
	if len(paths) == 0 {
		return
	}
	svc := s.svc
	go func() {
		raw, err := s.B.raw("preview_folder_links", map[string]any{
			"folderId": fid, "paths": paths,
		})
		s.mu.Lock()
		if err != nil {
			s.errorMsg = err.Error()
		} else {
			s.errorMsg = ""
			s.wfBatchPlan = nil
			for _, p := range asList(raw) {
				s.wfBatchPlan = append(s.wfBatchPlan, wfPlanFrom(p))
			}
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// viewFolderLinkBatchPlan lists the batch's dry-run plans: each target with
// its assigned name or its rejection, and one confirm that links every valid
// plan in a single create_folder_links call.
func (s *Shell) viewFolderLinkBatchPlan(c *ui.Context, root string) {
	t := c.Theme()
	valid := 0
	ui.Column(c.Key("wf-batch-plan")).Gap(4).FillWidth().Padding(10).Radius(8).
		Background(t.Surface).Children(func() {
		ui.Text(c, cat(c).WfBatchReady()).FontSize(12).Bold()
		for _, p := range s.wfBatchPlan {
			if p.rejection == "" {
				valid++
			}
			ui.Row(c.Key("wf-bp-" + p.targetPath)).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, p.targetPath).FontSize(11).Grow(1).MaxLines(1).TextColor(t.Text)
				if p.rejection != "" {
					ui.Text(c, p.rejection).FontSize(11).TextColor(t.Warning).MaxLines(1)
				} else {
					ui.Text(c, p.name).FontSize(11).TextColor(t.Success)
				}
			})
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			btn := ui.PrimaryButton(c.Key("wf-bp-go"), cat(c).WfBatchLink(itoa("", valid)))
			btn.Disabled(valid == 0)
			if btn.Clicked() {
				s.linkFolderLinksBatch(c, root)
			}
			if ui.Button(c.Key("wf-bp-clear"), cat(c).ActionCancel()).Clicked() {
				s.wfBatchPaths, s.wfBatchPlan = nil, nil
			}
		})
	})
}

// linkFolderLinksBatch creates every valid plan in one call (invalid ones are
// skipped; the backend deduped their names at preview time).
func (s *Shell) linkFolderLinksBatch(c *ui.Context, root string) {
	loc := c.Locale()
	fid := numOrString(s.selectedFolder)
	items := []any{}
	for _, p := range s.wfBatchPlan {
		if p.rejection == "" {
			items = append(items, map[string]any{"path": p.targetPath, "name": p.name})
		}
	}
	if len(items) == 0 {
		return
	}
	svc := s.svc
	go func() {
		_, err := s.B.raw("create_folder_links", map[string]any{
			"folderId": fid, "items": items, "gitExclude": s.wfGitExclude,
		})
		s.mu.Lock()
		if err != nil {
			s.errorMsg = err.Error()
		} else {
			s.errorMsg = ""
			s.actionMsg = locales.In(loc).WfBatchLinked(itoa("", len(items)))
			s.wfBatchPaths, s.wfBatchPlan = nil, nil
		}
		s.refreshWorkspaceTree(root)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// refreshWorkspaceTree drops the folder-link and file-tree caches a link or
// upload mutation touched. Call with s.mu held.
func (s *Shell) refreshWorkspaceTree(root string) {
	delete(s.cache, "folder-links:"+s.selectedFolder)
	delete(s.cache, "ft:"+root)
}

// uploadWorkspaceFile is the single-file upload (kept for the tests that set
// wfUploadPath directly).
func (s *Shell) uploadWorkspaceFile(c *ui.Context, root string) {
	loc := c.Locale()
	path := strings.TrimSpace(s.wfUploadPath)
	if path == "" {
		s.mu.Lock()
		s.errorMsg = locales.In(loc).WfUploadNoFile()
		s.mu.Unlock()
		return
	}
	fields := map[string]string{"root_path": root}
	if rel := strings.TrimSpace(s.wfUploadRel); rel != "" {
		fields["relative_path"] = rel
	}
	svc := s.svc
	go func() {
		raw, err := s.B.upload("upload_workspace_file", fields, "file", path)
		s.mu.Lock()
		if err != nil {
			s.errorMsg = err.Error()
		} else {
			s.errorMsg = ""
			var res map[string]any
			_ = json.Unmarshal(raw, &res)
			rel := ""
			if v, ok := res["path"].(string); ok {
				rel = v
			}
			s.actionMsg = locales.In(loc).WfUploaded(rel)
			s.wfUploadPath, s.wfUploadRel = "", ""
			delete(s.cache, "ft:"+root)
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}
