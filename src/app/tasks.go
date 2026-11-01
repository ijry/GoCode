package app

// tasks.go is the To-dos workbench (UI_GAPS P1-7), mirroring upstream
// src/components/tasks/ (board-columns.ts + task-actions.ts + task-card.tsx):
// a four-column board (To do / In progress / Needs attention / Done), a card
// context menu whose actions depend on the task's status, a create/edit form,
// a detail view with the events timeline, and an empty state.
//
// Wire contract (src-go/web/handlers/work_task.go):
//   work_task_list  {folderId?} → [{id,folder_id,title,config,status,agent_type,
//                     work_branch,base_branch,files_changed,additions,deletions,
//                     verdict,result_summary,failure_reason,last_error,
//                     conversation_id,archived_at,scheduled_at,updated_at,…}]
//   work_task_events{taskId}    → [{id,kind,actor,payload,created_at}]
//   work_task_create{draft:{folderId,title,config:{display_text,agent_type}}}
//   work_task_update{id,draft:{title,config}}
//   work_task_start|retry|requeue|cancel|complete|merge {id}
//   work_task_archive {id,archived}     work_task_delete {id}
//   work_task_start_all {folderId?}

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"gocode/locales"
)

// taskEditor holds the create/edit form's working state. id == "" means create.
type taskEditor struct {
	id     string
	title  string
	prompt string
	agent  string
}

// taskColumns are the four board columns, in board order (upstream
// BOARD_COLUMN_IDS). The labels come from the catalogs, so the column headers
// follow the window's language.
func taskColumns(m locales.Messages) []struct{ id, label string } {
	return []struct{ id, label string }{
		{"todo", m.TkColTodo()},
		{"inProgress", m.TkColInProgress()},
		{"attention", m.TkColAttention()},
		{"done", m.TkColDone()},
	}
}

// taskColumnForStatus maps a DB status to its board column (upstream
// columnForStatus — the single source of truth for the mapping).
func taskColumnForStatus(status string) string {
	switch status {
	case "todo", "queued":
		return "todo"
	case "preparing", "running":
		return "inProgress"
	case "awaiting_input", "review", "merging", "failed":
		return "attention"
	case "done", "canceled":
		return "done"
	}
	return "todo"
}

// viewTasks renders the To-dos workbench: header + toggles, then the board, the
// create/edit form, or the selected task's detail.
func (s *Shell) viewTasks(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	list := asList(s.ensure(s.tasksKey(), "work_task_list", s.tasksBody()))

	ui.Row(c.Key("tk-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.NavTodos()).FontSize(22).Bold()
		ui.Spacer(c)
		iconButton(c, "tk-refresh", iconRefresh, m.ActionRefresh(), func() {
			s.invalidate(s.tasksKey())
		})
		if ui.Button(c.Key("tk-startall"), m.TkStartAll()).Clicked() {
			s.taskStartAll(c)
		}
		if ui.PrimaryButton(c.Key("tk-new"), m.TkNew()).Clicked() {
			s.startNewTask()
		}
	})
	ui.Text(c, m.TkHint()).FontSize(12).TextColor(t.TextMuted)

	if s.taskEditor != nil {
		s.viewTaskEditor(c)
		return
	}
	if s.taskSel != "" {
		if tk := s.taskByID(list, s.taskSel); tk != nil {
			s.viewTaskDetail(c, tk)
			return
		}
		s.taskSel = ""
	}

	if list == nil {
		loadingState(c, "")
		return
	}
	ui.Row(c.Key("tk-toggles")).Gap(14).AlignItems(ui.Center).Children(func() {
		ui.Checkbox(c.Key("tk-show-canceled"), &s.taskShowCanceled, m.TkShowCanceled())
		ui.Checkbox(c.Key("tk-show-archived"), &s.taskShowArchived, m.TkShowArchived())
	})
	if len(list) == 0 {
		s.viewTasksEmpty(c)
		return
	}

	visible := s.visibleTasks(list)
	ui.Row(c.Key("tk-board")).Grow(1).Gap(10).Children(func() {
		for _, col := range taskColumns(m) {
			tasks := tasksInColumn(visible, col.id)
			col := col
			ui.Column(c.Key("tk-col-" + col.id)).Grow(1).FillHeight().Gap(6).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, col.label).FontSize(12).Bold().TextColor(t.TextMuted)
					ui.Textf(c, "%d", len(tasks)).FontSize(11).TextColor(t.TextMuted)
				})
				ui.Scroll(c).Grow(1).Children(func() {
					ui.Column(c).Gap(6).Children(func() {
						if len(tasks) == 0 {
							ui.Text(c, "—").FontSize(11).TextColor(t.TextMuted)
							return
						}
						for _, tk := range tasks {
							s.taskCard(c, tk)
						}
					})
				})
			})
		}
	})
}

// viewTasksEmpty is the onboarding empty state.
func (s *Shell) viewTasksEmpty(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	ui.Column(c.Key("tk-empty")).Fill().Center().Gap(10).Children(func() {
		ui.Text(c, m.TkEmptyTitle()).FontSize(18).Bold()
		ui.Text(c, m.TkEmptyHint()).
			FontSize(13).TextColor(t.TextMuted)
		if ui.PrimaryButton(c.Key("tk-empty-new"), m.TkNew()).Clicked() {
			s.startNewTask()
		}
	})
}

// taskCard is one board card: title, status chip and a meta line, with the
// status-dependent actions on its right-click menu.
func (s *Shell) taskCard(c *ui.Context, tk map[string]any) {
	t := c.Theme()
	m := cat(c)
	id := fmt.Sprintf("%v", tk["id"])
	title := firstNonEmpty(str(tk, "title"), m.Untitled())
	status := str(tk, "status")
	agent := str(tk, "agent_type")
	branch := str(tk, "work_branch")
	files := intOf(tk["files_changed"])

	card := ui.Column(c.Key("tk-card-" + id)).Gap(4).Padding(8).Background(t.Surface).Radius(8)
	card.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).Grow(1).MaxLines(2).TextColor(t.Text)
			taskStatusChip(c, "tk-chip-"+id, status, m)
		})
		meta := agent
		if branch != "" {
			meta += sepIf(meta) + branch
		}
		if files > 0 {
			meta += sepIf(meta) + m.TkFilesMeta(files, intOf(tk["additions"]), intOf(tk["deletions"]))
		}
		if meta != "" {
			ui.Text(c, meta).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
		}
		if isArchived(tk) {
			ui.Text(c, m.TkArchived()).FontSize(10).TextColor(t.TextMuted)
		}
	})
	card.ContextMenu(func(mn *ui.Menu) { s.taskMenu(c, mn, m, tk) })
	if card.Clicked() {
		s.taskSel = id
		s.taskEditor = nil
		s.taskConfirm = ""
	}
}

// taskMenu fills the card's context menu with the actions the task's status
// allows (upstream buildTaskActions).
func (s *Shell) taskMenu(c *ui.Context, mn *ui.Menu, m locales.Messages, tk map[string]any) {
	id := fmt.Sprintf("%v", tk["id"])
	status := str(tk, "status")
	if isArchived(tk) {
		if mn.Item(m.TkUnarchive()).Chosen() {
			s.taskArchive(c, id, false)
		}
		return
	}
	switch status {
	case "todo":
		if mn.Item(m.TkStart()).Chosen() {
			s.taskStart(c, id)
		}
		if mn.Item(m.ActionEdit()).Chosen() {
			s.startEditTask(tk)
		}
	case "queued", "preparing", "running", "awaiting_input":
		if mn.Item(m.ActionCancel()).Chosen() {
			s.taskCancel(c, id)
		}
	case "review":
		// Nothing changed ⟹ nothing to merge: accept outright; else merge.
		if intOf(tk["files_changed"]) == 0 {
			if mn.Item(m.TkComplete()).Chosen() {
				s.taskComplete(c, id)
			}
		} else {
			if mn.Item(m.TkMerge()).Chosen() {
				s.taskMerge(c, id)
			}
		}
	case "merging":
		// A merge is in flight — no action.
	case "failed":
		if mn.Item(m.TkRetry()).Chosen() {
			s.taskRetry(c, id)
		}
		if mn.Item(m.TkArchive()).Chosen() {
			s.taskArchive(c, id, true)
		}
	case "done":
		if mn.Item(m.TkArchive()).Chosen() {
			s.taskArchive(c, id, true)
		}
	case "canceled":
		if mn.Item(m.TkRequeue()).Chosen() {
			s.taskRequeue(c, id)
		}
		if mn.Item(m.TkArchive()).Chosen() {
			s.taskArchive(c, id, true)
		}
	}
	mn.Separator()
	if mn.Item(m.ActionDelete()).Chosen() {
		s.taskSel = id
		s.taskEditor = nil
		s.taskConfirm = id
	}
}

// viewTaskDetail renders the selected task's facts, actions and events timeline.
func (s *Shell) viewTaskDetail(c *ui.Context, tk map[string]any) {
	t := c.Theme()
	m := cat(c)
	id := fmt.Sprintf("%v", tk["id"])
	status := str(tk, "status")
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c.Key("tk-detail")).Gap(12).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				iconButton(c, "tk-back", iconBack, m.TkBack(), func() {
					s.taskSel = ""
					s.taskConfirm = ""
				})
			})
			ui.Row(c.Key("tk-detail-head")).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, firstNonEmpty(str(tk, "title"), m.Untitled())).FontSize(18).Bold()
				taskStatusChip(c, "tk-detail-chip", status, m)
				ui.Spacer(c)
				if isArchived(tk) {
					ui.Text(c, m.TkArchived()).FontSize(11).TextColor(t.TextMuted)
				}
			})

			// Actions, or the inline delete confirmation.
			if s.taskConfirm == id {
				ui.Row(c.Key("tk-detail-confirm")).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, m.TkConfirmDelete()).TextColor(t.Danger)
					if ui.PrimaryButton(c.Key("tk-confirm-yes"), m.ActionDelete()).Clicked() {
						s.taskConfirm = ""
						s.taskDelete(c, id)
					}
					if ui.Button(c.Key("tk-confirm-no"), m.ActionCancel()).Clicked() {
						s.taskConfirm = ""
					}
				})
			} else {
				s.taskActionRow(c, tk)
			}

			// Facts.
			ui.Column(c.Key("tk-facts")).Gap(4).Children(func() {
				ui.Text(c, m.AuAgentLine(firstNonEmpty(str(tk, "agent_type"), "—"))).FontSize(13).TextColor(t.TextMuted)
				if b := str(tk, "work_branch"); b != "" {
					ui.Text(c, m.TkBranchLine(b, firstNonEmpty(str(tk, "base_branch"), "—"))).FontSize(13).TextColor(t.TextMuted)
				}
				if n := intOf(tk["files_changed"]); n > 0 {
					ui.Text(c, m.TkChangesLine(n, intOf(tk["additions"]), intOf(tk["deletions"]))).FontSize(13).TextColor(t.TextMuted)
				}
				if v := str(tk, "verdict"); v != "" {
					ui.Text(c, m.TkVerdictLine(v)).FontSize(13).TextColor(t.TextMuted)
				}
				if rs := str(tk, "result_summary"); rs != "" {
					ui.Text(c, m.TkSummaryLine(rs)).FontSize(13).TextColor(t.TextMuted).Wrap()
				}
				if fe := firstNonEmpty(str(tk, "failure_reason"), str(tk, "last_error")); fe != "" {
					ui.Text(c, m.TkErrorLine(fe)).FontSize(13).TextColor(t.Danger).Wrap()
				}
				ui.Text(c, m.TasksUpdated(firstNonEmpty(autoAbsTime(str(tk, "updated_at"), c.Locale()), "—"))).FontSize(13).TextColor(t.TextMuted)
			})

			// Events timeline.
			ui.Text(c, m.TkActivity()).FontSize(10).Bold().TextColor(t.TextMuted)
			evs := asList(s.ensure("task-events:"+id, "work_task_events", map[string]any{"taskId": numOrString(id)}))
			if evs == nil {
				loadingState(c, "")
			} else if len(evs) == 0 {
				ui.Text(c, m.TkNoActivity()).FontSize(12).TextColor(t.TextMuted)
			} else {
				ui.Column(c.Key("tk-events")).Gap(3).Children(func() {
					for _, ev := range evs {
						ui.Text(c, m.TkEventLine(
							firstNonEmpty(str(ev, "kind"), m.TkEventKindFallback()),
							firstNonEmpty(str(ev, "actor"), m.TkEventActorFallback()),
							firstNonEmpty(autoAbsTime(str(ev, "created_at"), c.Locale()), "—"))).
							FontSize(12).TextColor(t.TextMuted)
					}
				})
			}
		})
	})
}

// taskActionRow renders the status-appropriate primary + secondary actions.
func (s *Shell) taskActionRow(c *ui.Context, tk map[string]any) {
	m := cat(c)
	id := fmt.Sprintf("%v", tk["id"])
	status := str(tk, "status")
	archived := isArchived(tk)
	ui.Row(c.Key("tk-actions")).Gap(6).Wrap().Children(func() {
		if archived {
			if ui.PrimaryButton(c.Key("tk-act-unarchive"), m.TkUnarchive()).Clicked() {
				s.taskArchive(c, id, false)
			}
			return
		}
		switch status {
		case "todo":
			if ui.PrimaryButton(c.Key("tk-act-start"), m.TkStart()).Clicked() {
				s.taskStart(c, id)
			}
			if ui.Button(c.Key("tk-act-edit"), m.ActionEdit()).Clicked() {
				s.startEditTask(tk)
			}
		case "queued", "preparing", "running", "awaiting_input":
			if ui.PrimaryButton(c.Key("tk-act-cancel"), m.ActionCancel()).Clicked() {
				s.taskCancel(c, id)
			}
		case "review":
			if intOf(tk["files_changed"]) == 0 {
				if ui.PrimaryButton(c.Key("tk-act-complete"), m.TkComplete()).Clicked() {
					s.taskComplete(c, id)
				}
			} else if ui.PrimaryButton(c.Key("tk-act-merge"), m.TkMerge()).Clicked() {
				s.taskMerge(c, id)
			}
		case "merging":
			ui.Text(c, m.TkMerging()).FontSize(12).TextColor(c.Theme().TextMuted)
		case "failed":
			if ui.PrimaryButton(c.Key("tk-act-retry"), m.TkRetry()).Clicked() {
				s.taskRetry(c, id)
			}
			if ui.Button(c.Key("tk-act-archive"), m.TkArchive()).Clicked() {
				s.taskArchive(c, id, true)
			}
		case "done":
			if ui.PrimaryButton(c.Key("tk-act-archive2"), m.TkArchive()).Clicked() {
				s.taskArchive(c, id, true)
			}
		case "canceled":
			if ui.PrimaryButton(c.Key("tk-act-requeue"), m.TkRequeue()).Clicked() {
				s.taskRequeue(c, id)
			}
			if ui.Button(c.Key("tk-act-archive3"), m.TkArchive()).Clicked() {
				s.taskArchive(c, id, true)
			}
		}
		if ui.Button(c.Key("tk-act-delete"), m.ActionDelete()).Clicked() {
			s.taskConfirm = id
		}
	})
}

// viewTaskEditor renders the create/edit form.
func (s *Shell) viewTaskEditor(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	e := s.taskEditor
	if e == nil {
		return
	}
	title := m.TkNewTitle()
	if e.id != "" {
		title = m.TkEditTitle()
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c.Key("tk-form")).Gap(10).Children(func() {
			ui.Text(c, title).FontSize(18).Bold()
			if s.selectedFolder == "" && e.id == "" {
				ui.Text(c, m.TkSelectFolderFirst()).FontSize(12).TextColor(t.Danger)
			}
			ui.Text(c, m.SetTitle()).FontSize(12).TextColor(t.TextMuted)
			ui.TextInput(c.Key("tk-f-title"), &e.title).Placeholder(m.TkTitlePlaceholder())
			ui.Text(c, m.AuPrompt()).FontSize(12).TextColor(t.TextMuted)
			ui.TextArea(c.Key("tk-f-prompt"), &e.prompt).Placeholder(m.AuPromptPlaceholder()).MinHeight(80)
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, m.AuxAgent()).FontSize(12).TextColor(t.TextMuted)
				ui.Select(c.Key("tk-f-agent"), &e.agent, s.agentOptions())
			})
			ui.Row(c).Gap(6).Children(func() {
				saveDisabled := e.id == "" && s.selectedFolder == ""
				if ui.PrimaryButton(c.Key("tk-f-save"), m.ActionSave()).Disabled(saveDisabled).Clicked() {
					s.taskSave(c)
				}
				if ui.Button(c.Key("tk-f-cancel"), m.ActionCancel()).Clicked() {
					s.taskEditor = nil
				}
			})
		})
	})
}

// ── selection / helpers ──

func (s *Shell) tasksKey() string {
	if s.selectedFolder != "" {
		return "tasks:" + s.selectedFolder
	}
	return "tasks:all"
}

func (s *Shell) tasksBody() map[string]any {
	if s.selectedFolder != "" {
		return map[string]any{"folderId": numOrString(s.selectedFolder)}
	}
	return nil
}

func (s *Shell) taskByID(list []map[string]any, id string) map[string]any {
	for _, tk := range list {
		if fmt.Sprintf("%v", tk["id"]) == id {
			return tk
		}
	}
	return nil
}

// visibleTasks applies the show-canceled / show-archived toggles (upstream
// groupTasksByColumn's filters).
func (s *Shell) visibleTasks(list []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, tk := range list {
		if str(tk, "status") == "canceled" && !s.taskShowCanceled {
			continue
		}
		if isArchived(tk) && !s.taskShowArchived {
			continue
		}
		out = append(out, tk)
	}
	return out
}

func tasksInColumn(list []map[string]any, col string) []map[string]any {
	out := make([]map[string]any, 0)
	for _, tk := range list {
		if taskColumnForStatus(str(tk, "status")) == col {
			out = append(out, tk)
		}
	}
	return out
}

// isArchived reports whether a task row carries a non-empty archived_at.
func isArchived(tk map[string]any) bool {
	v := tk["archived_at"]
	if v == nil {
		return false
	}
	if s, ok := v.(string); ok {
		return s != ""
	}
	return true
}

// sepIf returns " · " when s is non-empty, so meta joins never double up.
func sepIf(s string) string {
	if s == "" {
		return ""
	}
	return " · "
}

// ── mutations ──

func (s *Shell) startNewTask() {
	agent := s.newAgent
	if agent == "" {
		if opts := s.agentOptions(); len(opts) > 0 {
			agent = opts[0]
		}
	}
	s.taskEditor = &taskEditor{agent: agent}
	s.taskSel = ""
	s.taskConfirm = ""
}

func (s *Shell) startEditTask(tk map[string]any) {
	cfg := asObjVal(tk["config"])
	s.taskEditor = &taskEditor{
		id:     fmt.Sprintf("%v", tk["id"]),
		title:  str(tk, "title"),
		prompt: str(cfg, "display_text"),
		agent:  str(tk, "agent_type"),
	}
	s.taskConfirm = ""
}

// taskSave creates or updates the edited to-do off the UI thread.
func (s *Shell) taskSave(c *ui.Context) {
	e := s.taskEditor
	if e == nil {
		return
	}
	cfg := map[string]any{"display_text": e.prompt, "agent_type": e.agent}
	id := e.id
	folderID := numOrString(s.selectedFolder)
	svc := s.svc
	loc := c.Locale()
	go func() {
		var err error
		if id == "" {
			_, err = s.B.raw("work_task_create", map[string]any{"draft": map[string]any{
				"folderId": folderID, "title": e.title, "config": cfg,
			}})
		} else {
			_, err = s.B.raw("work_task_update", map[string]any{"id": numOrString(id), "draft": map[string]any{
				"title": e.title, "config": cfg,
			}})
		}
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).TkSaveFailed(err.Error()))
		} else {
			s.notify(locales.In(loc).TkSaved())
			s.taskEditor = nil
		}
		delete(s.cache, s.tasksKey())
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// taskTransition runs a {id}-only lifecycle command and refreshes the board.
func (s *Shell) taskTransition(c *ui.Context, cmd, id string) {
	svc := s.svc
	loc := c.Locale()
	go func() {
		_, err := s.B.raw(cmd, map[string]any{"id": numOrString(id)})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).TkTransitionFailed(err.Error()))
		} else {
			s.notify(locales.In(loc).TkUpdated())
		}
		delete(s.cache, s.tasksKey())
		delete(s.cache, "task-events:"+id)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

func (s *Shell) taskStart(c *ui.Context, id string)   { s.taskTransition(c, "work_task_start", id) }
func (s *Shell) taskRetry(c *ui.Context, id string)   { s.taskTransition(c, "work_task_retry", id) }
func (s *Shell) taskRequeue(c *ui.Context, id string) { s.taskTransition(c, "work_task_requeue", id) }
func (s *Shell) taskCancel(c *ui.Context, id string)  { s.taskTransition(c, "work_task_cancel", id) }
func (s *Shell) taskComplete(c *ui.Context, id string) {
	s.taskTransition(c, "work_task_complete", id)
}
func (s *Shell) taskMerge(c *ui.Context, id string) { s.taskTransition(c, "work_task_merge", id) }

func (s *Shell) taskArchive(c *ui.Context, id string, archived bool) {
	svc := s.svc
	loc := c.Locale()
	go func() {
		_, err := s.B.raw("work_task_archive", map[string]any{"id": numOrString(id), "archived": archived})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).TkArchiveFailed(err.Error()))
		} else {
			s.notify(locales.In(loc).TkUpdated())
		}
		delete(s.cache, s.tasksKey())
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

func (s *Shell) taskDelete(c *ui.Context, id string) {
	svc := s.svc
	loc := c.Locale()
	go func() {
		_, err := s.B.raw("work_task_delete", map[string]any{"id": numOrString(id)})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).TkDeleteFailed(err.Error()))
		} else {
			s.notify(locales.In(loc).TkDeleted())
			if s.taskSel == id {
				s.taskSel = ""
			}
		}
		delete(s.cache, s.tasksKey())
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

func (s *Shell) taskStartAll(c *ui.Context) {
	svc := s.svc
	loc := c.Locale()
	body := map[string]any{}
	if s.selectedFolder != "" {
		body["folderId"] = numOrString(s.selectedFolder)
	}
	go func() {
		_, err := s.B.raw("work_task_start_all", body)
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).TkStartAllFailed(err.Error()))
		} else {
			s.notify(locales.In(loc).TkStartedAll())
		}
		delete(s.cache, s.tasksKey())
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// ── status chip ──

// taskStatusChip draws a tinted status pill for a work-task status.
func taskStatusChip(c *ui.Context, key, status string, m locales.Messages) {
	if status == "" {
		return
	}
	t := c.Theme()
	col := t.TextMuted
	switch status {
	case "preparing", "running":
		col = t.Warning
	case "awaiting_input":
		col = t.Warning
	case "review", "merging":
		col = t.Accent
	case "failed":
		col = t.Danger
	case "done":
		col = t.Success
	}
	ui.Row(c.Key(key)).PaddingX(8).PaddingY(2).Radius(10).Background(col.Alpha(0.12)).Children(func() {
		ui.Text(c, taskStatusLabel(m, status)).FontSize(11).TextColor(col)
	})
}

func taskStatusLabel(m locales.Messages, status string) string {
	switch status {
	case "todo":
		return m.TkStatusToDo()
	case "queued":
		return m.TkStatusQueued()
	case "preparing":
		return m.TkStatusPreparing()
	case "running":
		return m.TkStatusRunning()
	case "awaiting_input":
		return m.TkStatusAwaitingInput()
	case "review":
		return m.TkStatusReview()
	case "merging":
		return m.TkStatusMerging()
	case "failed":
		return m.TkStatusFailed()
	case "done":
		return m.TkStatusDone()
	case "canceled":
		return m.TkStatusCanceled()
	}
	return status
}
