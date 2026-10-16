package app

// automations.go is the Automations workbench (UI_GAPS P1-7), mirroring
// upstream src/components/automations/automations-page.tsx: a selectable list
// beside a detail pane (schedule facts, prompt, run history) with per-row
// quick actions (run now / enable-disable / edit / delete), a create/edit
// editor form, and an onboarding empty state.
//
// Wire contract (src-go/web/handlers/automation.go):
//   automation_list            → [{id,name,enabled,trigger_kind,cron,timezone,
//                                 next_run_at,agent_type,root_folder_id,isolation,
//                                 branch,is_remote_branch,config,last_run_at,
//                                 last_run_status,unseen_failures,…}]
//   automation_get   {id}
//   automation_runs  {automationId,limit?} → [{id,status,trigger,started_at,
//                                 ended_at,conversation_id,error,summary,…}]
//   automation_create{draft:{name,enabled,trigger_kind,cron,timezone,agent_type,
//                                 isolation,is_remote_branch,config:{display_text}}}
//   automation_update{id,draft:{…}}
//   automation_set_enabled {id,enabled}   automation_delete {id}
//   automation_run_now {automationId}     automation_cancel_run {runId}

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

// automationEditor holds the create/edit form's working state. id == "" means
// create; otherwise it is the automation being edited.
type automationEditor struct {
	id        string
	name      string
	prompt    string
	trigger   string // schedule | manual
	cron      string
	tz        string
	agent     string
	isolation string
	enabled   bool
}

// viewAutomations renders the Automations workbench: header, then a two-column
// list | detail (or editor) split.
func (s *Shell) viewAutomations(c *ui.Context) {
	t := c.Theme()
	list := asList(s.ensure("automations", "automation_list", nil))

	ui.Row(c.Key("au-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Automations").FontSize(22).Bold()
		ui.Spacer(c)
		iconButton(c, "au-refresh", iconRefresh, "Refresh", func() {
			s.invalidate("automations")
			if s.autoSel != "" {
				s.invalidate("auto-runs:" + s.autoSel)
			}
		})
		if ui.PrimaryButton(c.Key("au-new"), "New automation").Clicked() {
			s.startNewAutomation()
		}
	})
	ui.Text(c, "Scheduled and event-driven agent runs.").FontSize(12).TextColor(t.TextMuted)

	if list == nil {
		ui.Text(c, "Loading…").TextColor(t.TextMuted)
		return
	}
	if len(list) == 0 && s.autoEditor == nil {
		s.viewAutomationsEmpty(c)
		return
	}

	cur := s.selectedAutomation(list)
	curID := ""
	if cur != nil {
		curID = fmt.Sprintf("%v", cur["id"])
	}
	ui.Row(c.Key("au-body")).Grow(1).Gap(12).Children(func() {
		// Left: the list.
		ui.Column(c.Key("au-list")).Width(250).FillHeight().Gap(4).Children(func() {
			if len(list) == 0 {
				ui.Text(c, "No automations.").FontSize(12).TextColor(t.TextMuted)
				return
			}
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					for _, a := range list {
						s.automationRow(c, a, curID)
					}
				})
			})
		})
		ui.Divider(c)
		// Right: the editor (when open) or the detail of the current row.
		ui.Column(c.Key("au-detail")).Grow(1).FillHeight().Children(func() {
			if s.autoEditor != nil {
				s.viewAutomationEditor(c)
				return
			}
			if cur == nil {
				ui.Text(c, "Select an automation").TextColor(t.TextMuted)
				return
			}
			s.viewAutomationDetail(c, cur)
		})
	})
}

// viewAutomationsEmpty is the onboarding empty state (upstream's gallery,
// reduced to a hint + the primary action).
func (s *Shell) viewAutomationsEmpty(c *ui.Context) {
	t := c.Theme()
	ui.Column(c.Key("au-empty")).Fill().Center().Gap(10).Children(func() {
		ui.Text(c, "No automations yet").FontSize(18).Bold()
		ui.Text(c, "Schedule an agent to run on a cron, or run it on demand.").
			FontSize(13).TextColor(t.TextMuted)
		if ui.PrimaryButton(c.Key("au-empty-new"), "New automation").Clicked() {
			s.startNewAutomation()
		}
	})
}

// automationRow is one list entry: a status dot, the name, and the next-run /
// last-run time, with the quick actions on its right-click menu.
func (s *Shell) automationRow(c *ui.Context, a map[string]any, curID string) {
	t := c.Theme()
	id := fmt.Sprintf("%v", a["id"])
	name := firstNonEmpty(str(a, "name"), "(untitled)")
	enabled := truthy(a["enabled"])
	status := str(a, "last_run_status")
	selected := id == curID

	row := ui.Row(c.Key("au-row-" + id)).PaddingX(8).PaddingY(6).Gap(8).Radius(6).AlignItems(ui.Center)
	if selected {
		row.Background(t.SurfacePressed)
	}
	row.Children(func() {
		ui.Box(c).Width(7).Height(7).Radius(4).Background(autoDotColor(t, enabled, status))
		ui.Text(c, name).Grow(1).MaxLines(1).TextColor(t.Text)
		if lbl := autoRowTime(a); lbl != "" {
			ui.Text(c, lbl).FontSize(11).TextColor(t.TextMuted)
		}
	})
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("Run now").Chosen() {
			s.automationRunNow(id)
		}
		if enabled {
			if m.Item("Disable").Chosen() {
				s.automationSetEnabled(id, false)
			}
		} else {
			if m.Item("Enable").Chosen() {
				s.automationSetEnabled(id, true)
			}
		}
		if m.Item("Edit").Chosen() {
			s.startEditAutomation(a)
		}
		m.Separator()
		if m.Item("Delete").Chosen() {
			s.autoSel = id
			s.autoEditor = nil
			s.autoConfirm = id
		}
	})
	if row.Clicked() {
		s.autoSel = id
		s.autoEditor = nil
		s.autoConfirm = ""
	}
}

// viewAutomationDetail renders the selected automation's facts, prompt, run
// history and actions.
func (s *Shell) viewAutomationDetail(c *ui.Context, a map[string]any) {
	t := c.Theme()
	id := fmt.Sprintf("%v", a["id"])
	name := firstNonEmpty(str(a, "name"), "(untitled)")
	enabled := truthy(a["enabled"])
	status := str(a, "last_run_status")
	cfg := asObjVal(a["config"])
	prompt := str(cfg, "display_text")
	trigger := str(a, "trigger_kind")
	cron := str(a, "cron")
	agent := str(a, "agent_type")
	isolation := str(a, "isolation")
	nextRun := str(a, "next_run_at")
	lastRun := str(a, "last_run_at")

	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c.Key("au-detail-col")).Gap(14).Children(func() {
			// Title + status chip + enable/disable.
			ui.Row(c.Key("au-detail-head")).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, name).FontSize(18).Bold()
				autoStatusChip(c, "au-status", status)
				ui.Spacer(c)
				toggle := "Disable"
				if !enabled {
					toggle = "Enable"
				}
				if ui.Button(c.Key("au-toggle"), toggle).Clicked() {
					s.automationSetEnabled(id, !enabled)
				}
			})

			// Actions row, or the inline delete confirmation.
			if s.autoConfirm == id {
				ui.Row(c.Key("au-confirm")).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Delete this automation?").TextColor(t.Danger)
					if ui.PrimaryButton(c.Key("au-confirm-yes"), "Delete").Clicked() {
						s.autoConfirm = ""
						s.automationDelete(id)
					}
					if ui.Button(c.Key("au-confirm-no"), "Cancel").Clicked() {
						s.autoConfirm = ""
					}
				})
			} else {
				ui.Row(c.Key("au-actions")).Gap(6).Children(func() {
					if ui.PrimaryButton(c.Key("au-run"), "Run now").Clicked() {
						s.automationRunNow(id)
					}
					if ui.Button(c.Key("au-edit"), "Edit").Clicked() {
						s.startEditAutomation(a)
					}
					if ui.Button(c.Key("au-delete"), "Delete").Clicked() {
						s.autoConfirm = id
					}
				})
			}

			// Schedule facts.
			ui.Column(c.Key("au-facts")).Gap(4).Children(func() {
				if trigger == "schedule" && cron != "" {
					ui.Textf(c, "Trigger: every cron %s (%s)", cron, firstNonEmpty(str(a, "timezone"), "UTC")).FontSize(13).TextColor(t.TextMuted)
					ui.Text(c, cat(c).AutoNextRun(firstNonEmpty(autoAbsTime(nextRun, c.Locale()), "—"))).FontSize(13).TextColor(t.TextMuted)
				} else {
					ui.Text(c, "Trigger: manual").FontSize(13).TextColor(t.TextMuted)
				}
				ui.Textf(c, "Agent: %s", firstNonEmpty(agent, "—")).FontSize(13).TextColor(t.TextMuted)
				ui.Textf(c, "Isolation: %s", firstNonEmpty(isolation, "—")).FontSize(13).TextColor(t.TextMuted)
				ui.Text(c, cat(c).AutoLastRun(firstNonEmpty(autoAbsTime(lastRun, c.Locale()), cat(c).AutoNever()))).FontSize(13).TextColor(t.TextMuted)
			})

			// Prompt.
			ui.Text(c, "PROMPT").FontSize(10).Bold().TextColor(t.TextMuted)
			ui.Text(c, firstNonEmpty(prompt, "—")).FontSize(13).TextColor(t.Text).Wrap()

			// Run history.
			ui.Text(c, "RUN HISTORY").FontSize(10).Bold().TextColor(t.TextMuted)
			runs := asList(s.ensure("auto-runs:"+id, "automation_runs", map[string]any{"automationId": numOrString(id)}))
			if runs == nil {
				ui.Text(c, "Loading…").FontSize(12).TextColor(t.TextMuted)
			} else if len(runs) == 0 {
				ui.Text(c, "No runs yet.").FontSize(12).TextColor(t.TextMuted)
			} else {
				ui.Column(c.Key("au-runs")).Gap(4).Children(func() {
					for i, r := range runs {
						s.automationRunRow(c, r, itoa("aur", i), id)
					}
				})
			}
		})
	})
}

// automationRunRow renders one run-history entry: status, started time, an
// optional duration, the error/summary line, and a Cancel action while running.
// autoID is the parent automation, so a cancel can refresh its run list.
func (s *Shell) automationRunRow(c *ui.Context, r map[string]any, key, autoID string) {
	t := c.Theme()
	runID := fmt.Sprintf("%v", r["id"])
	status := str(r, "status")
	started := str(r, "started_at")
	ended := str(r, "ended_at")
	ui.Row(c.Key(key)).Gap(8).AlignItems(ui.Center).Children(func() {
		autoStatusChip(c, key+"-chip", status)
		line := autoAbsTime(started, c.Locale())
		if d := autoDuration(started, ended); d != "" {
			line += " · " + d
		}
		ui.Text(c, line).FontSize(12).TextColor(t.TextMuted).Grow(1).MaxLines(1)
		if status == "running" {
			if ui.Button(c.Key(key+"-cancel"), "Cancel").Clicked() {
				s.automationCancelRun(autoID, runID)
			}
		}
	})
	if msg := firstNonEmpty(str(r, "error"), str(r, "summary")); msg != "" {
		col := t.TextMuted
		if str(r, "error") != "" {
			col = t.Danger
		}
		ui.Text(c, msg).FontSize(11).TextColor(col).MaxLines(1)
	}
}

// viewAutomationEditor renders the create/edit form.
func (s *Shell) viewAutomationEditor(c *ui.Context) {
	t := c.Theme()
	e := s.autoEditor
	if e == nil {
		return
	}
	title := "New automation"
	if e.id != "" {
		title = "Edit automation"
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c.Key("au-form")).Gap(10).Children(func() {
			ui.Text(c, title).FontSize(18).Bold()
			ui.Text(c, "Name").FontSize(12).TextColor(t.TextMuted)
			ui.TextInput(c.Key("au-f-name"), &e.name).Placeholder("Nightly review")
			ui.Text(c, "Prompt").FontSize(12).TextColor(t.TextMuted)
			ui.TextArea(c.Key("au-f-prompt"), &e.prompt).Placeholder("What should the agent do?").MinHeight(80)
			ui.Row(c).Gap(10).Children(func() {
				ui.Column(c).Grow(1).Gap(4).Children(func() {
					ui.Text(c, "Trigger").FontSize(12).TextColor(t.TextMuted)
					ui.Select(c.Key("au-f-trigger"), &e.trigger, []string{"schedule", "manual"})
				})
				ui.Column(c).Grow(1).Gap(4).Children(func() {
					ui.Text(c, "Agent").FontSize(12).TextColor(t.TextMuted)
					ui.Select(c.Key("au-f-agent"), &e.agent, s.agentOptions())
				})
			})
			if e.trigger == "schedule" {
				ui.Row(c).Gap(10).Children(func() {
					ui.Column(c).Grow(1).Gap(4).Children(func() {
						ui.Text(c, "Cron (min hour dom mon dow)").FontSize(12).TextColor(t.TextMuted)
						ui.TextInput(c.Key("au-f-cron"), &e.cron).Placeholder("0 9 * * *")
					})
					ui.Column(c).Grow(1).Gap(4).Children(func() {
						ui.Text(c, "Timezone").FontSize(12).TextColor(t.TextMuted)
						ui.TextInput(c.Key("au-f-tz"), &e.tz).Placeholder("UTC")
					})
				})
			}
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, "Isolation").FontSize(12).TextColor(t.TextMuted)
				ui.Select(c.Key("au-f-isolation"), &e.isolation, []string{"worktree_per_run", "shared_in_root"})
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Switch(c.Key("au-f-enabled"), &e.enabled)
				ui.Text(c, "Enabled").FontSize(13).TextColor(t.Text)
			})
			ui.Row(c).Gap(6).Children(func() {
				if ui.PrimaryButton(c.Key("au-f-save"), "Save").Clicked() {
					s.automationSave()
				}
				if ui.Button(c.Key("au-f-cancel"), "Cancel").Clicked() {
					s.autoEditor = nil
				}
			})
		})
	})
}

// ── selection / mutations ──

// selectedAutomation returns the explicitly selected automation, else the
// first row (so the detail pane is never blank when rows exist).
func (s *Shell) selectedAutomation(list []map[string]any) map[string]any {
	if len(list) == 0 {
		return nil
	}
	if s.autoSel != "" {
		for _, a := range list {
			if fmt.Sprintf("%v", a["id"]) == s.autoSel {
				return a
			}
		}
	}
	return list[0]
}

// agentOptions lists the agent_type values for the editor's agent select.
func (s *Shell) agentOptions() []string {
	agents := asList(s.ensure("agents", "acp_list_agents", nil))
	opts := make([]string, 0, len(agents))
	for _, a := range agents {
		if at := str(a, "agent_type"); at != "" {
			opts = append(opts, at)
		}
	}
	if len(opts) == 0 {
		opts = []string{"claude_code"}
	}
	return opts
}

func (s *Shell) startNewAutomation() {
	agent := s.newAgent
	if agent == "" {
		if opts := s.agentOptions(); len(opts) > 0 {
			agent = opts[0]
		}
	}
	s.autoEditor = &automationEditor{
		trigger:   "schedule",
		tz:        "UTC",
		agent:     agent,
		isolation: "worktree_per_run",
		enabled:   true,
	}
	s.autoConfirm = ""
}

func (s *Shell) startEditAutomation(a map[string]any) {
	cfg := asObjVal(a["config"])
	s.autoEditor = &automationEditor{
		id:        fmt.Sprintf("%v", a["id"]),
		name:      str(a, "name"),
		prompt:    str(cfg, "display_text"),
		trigger:   firstNonEmpty(str(a, "trigger_kind"), "schedule"),
		cron:      str(a, "cron"),
		tz:        firstNonEmpty(str(a, "timezone"), "UTC"),
		agent:     str(a, "agent_type"),
		isolation: firstNonEmpty(str(a, "isolation"), "worktree_per_run"),
		enabled:   truthy(a["enabled"]),
	}
	s.autoConfirm = ""
}

// automationSave creates or updates the edited automation off the UI thread.
func (s *Shell) automationSave() {
	e := s.autoEditor
	if e == nil {
		return
	}
	draft := map[string]any{
		"name":             strings.TrimSpace(e.name),
		"enabled":          e.enabled,
		"trigger_kind":     e.trigger,
		"cron":             e.cron,
		"timezone":         firstNonEmpty(e.tz, "UTC"),
		"agent_type":       e.agent,
		"isolation":        firstNonEmpty(e.isolation, "worktree_per_run"),
		"is_remote_branch": false,
		"config":           map[string]any{"display_text": e.prompt},
	}
	id := e.id
	svc := s.svc
	go func() {
		var err error
		if id == "" {
			_, err = s.B.raw("automation_create", map[string]any{"draft": draft})
		} else {
			_, err = s.B.raw("automation_update", map[string]any{"id": numOrString(id), "draft": draft})
		}
		s.mu.Lock()
		if err != nil {
			s.notify("save failed: " + err.Error())
		} else {
			s.notify("automation saved")
			s.autoEditor = nil
		}
		delete(s.cache, "automations")
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

func (s *Shell) automationSetEnabled(id string, on bool) {
	svc := s.svc
	go func() {
		_, err := s.B.raw("automation_set_enabled", map[string]any{"id": numOrString(id), "enabled": on})
		s.mu.Lock()
		if err != nil {
			s.notify("update failed: " + err.Error())
		} else if on {
			s.notify("automation enabled")
		} else {
			s.notify("automation disabled")
		}
		delete(s.cache, "automations")
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

func (s *Shell) automationRunNow(id string) {
	svc := s.svc
	go func() {
		_, err := s.B.raw("automation_run_now", map[string]any{"automationId": numOrString(id)})
		s.mu.Lock()
		if err != nil {
			s.notify("run failed: " + err.Error())
		} else {
			s.notify("run started")
		}
		delete(s.cache, "automations")
		delete(s.cache, "auto-runs:"+id)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

func (s *Shell) automationCancelRun(autoID, runID string) {
	svc := s.svc
	go func() {
		_, err := s.B.raw("automation_cancel_run", map[string]any{"runId": numOrString(runID)})
		s.mu.Lock()
		if err != nil {
			s.notify("cancel failed: " + err.Error())
		} else {
			s.notify("run cancelled")
		}
		delete(s.cache, "automations")
		delete(s.cache, "auto-runs:"+autoID)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

func (s *Shell) automationDelete(id string) {
	svc := s.svc
	go func() {
		_, err := s.B.raw("automation_delete", map[string]any{"id": numOrString(id)})
		s.mu.Lock()
		if err != nil {
			s.notify("delete failed: " + err.Error())
		} else {
			s.notify("automation deleted")
			if s.autoSel == id {
				s.autoSel = ""
			}
		}
		delete(s.cache, "automations")
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// ── small display helpers ──

// autoStatusChip draws a tinted status pill; empty status renders nothing.
func autoStatusChip(c *ui.Context, key, status string) {
	if status == "" {
		return
	}
	t := c.Theme()
	col := t.TextMuted
	switch status {
	case "running":
		col = t.Warning
	case "succeeded":
		col = t.Success
	case "failed":
		col = t.Danger
	}
	ui.Row(c.Key(key)).PaddingX(8).PaddingY(2).Radius(10).Background(col.Alpha(0.12)).Children(func() {
		ui.Text(c, autoStatusLabel(status)).FontSize(11).TextColor(col)
	})
}

func autoStatusLabel(status string) string {
	switch status {
	case "running":
		return "Running"
	case "succeeded":
		return "Succeeded"
	case "failed":
		return "Failed"
	case "cancelled":
		return "Cancelled"
	case "skipped":
		return "Skipped"
	}
	if status == "" {
		return "Idle"
	}
	return status
}

// autoDotColor is the row's status dot: muted when disabled, else tinted by the
// last run (green when it has never run).
func autoDotColor(t *ui.Theme, enabled bool, status string) ui.Color {
	if !enabled {
		return t.TextMuted.Alpha(0.4)
	}
	switch status {
	case "running":
		return t.Warning
	case "failed":
		return t.Danger
	case "cancelled", "skipped":
		return t.TextMuted
	}
	return t.Success
}

// autoRowTime is the row's trailing label: "in 3h" for an enabled scheduled
// automation, else the last-run relative time, else "".
func autoRowTime(a map[string]any) string {
	now := time.Now()
	if truthy(a["enabled"]) && str(a, "trigger_kind") == "schedule" {
		if next := str(a, "next_run_at"); next != "" {
			return "in " + relTimeFuture(next, now)
		}
	}
	if last := str(a, "last_run_at"); last != "" {
		return relTime(last, now)
	}
	return ""
}

// relTime formats a past timestamp as a compact relative label.
func relTime(iso string, now time.Time) string {
	ts, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return "—"
	}
	sec := int(now.Sub(ts).Seconds())
	if sec < 0 {
		sec = 0
	}
	switch {
	case sec < 45:
		return "now"
	case sec < 3600:
		return fmt.Sprintf("%dm", sec/60)
	case sec < 86400:
		return fmt.Sprintf("%dh", sec/3600)
	default:
		return fmt.Sprintf("%dd", sec/86400)
	}
}

// relTimeFuture formats a future timestamp as a compact relative label, floored
// at 1m so an imminent run never reads "0m".
func relTimeFuture(iso string, now time.Time) string {
	ts, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return "—"
	}
	sec := int(ts.Sub(now).Seconds())
	if sec < 0 {
		sec = 0
	}
	min := sec / 60
	if min < 1 {
		min = 1
	}
	switch {
	case min < 60:
		return fmt.Sprintf("%dm", min)
	case min < 1440:
		return fmt.Sprintf("%dh", min/60)
	default:
		return fmt.Sprintf("%dd", min/1440)
	}
}

// autoAbsTime formats a timestamp for the detail pane, or "" when absent.
func autoAbsTime(iso, locale string) string {
	if iso == "" {
		return ""
	}
	ts, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	if locale == "" {
		locale = "en"
	}
	return i18n.FormatDate(locale, ts.Local(), i18n.DateOptions{Date: i18n.ShortStyle, Time: i18n.ShortStyle})
}

// autoDuration is the human run length between two timestamps, or "".
func autoDuration(startIso, endIso string) string {
	if startIso == "" || endIso == "" {
		return ""
	}
	start, err1 := time.Parse(time.RFC3339, startIso)
	end, err2 := time.Parse(time.RFC3339, endIso)
	if err1 != nil || err2 != nil || end.Before(start) {
		return ""
	}
	sec := int(end.Sub(start).Seconds())
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	return fmt.Sprintf("%dm %ds", sec/60, sec%60)
}
