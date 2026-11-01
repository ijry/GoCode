package app

// import_sessions.go mirrors upstream src/app/import-sessions: a full-screen
// window for importing conversations discovered in other agents' local stores
// (Claude Code so far). Its backend (scan_importable_sessions +
// import_selected_sessions) is ported, so this is the real panel: it scans,
// groups the discovered sessions by the folder they ran in, marks each New /
// Imported / Deleted, and imports the chosen ones.
//
// The picker's checkboxes are click-to-toggle rows rather than mygo's Checkbox
// (which binds to a *bool): the session set is dynamic, and a map value is not
// addressable, so the selection lives in s.importSel and the row flips it.

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"gocode/locales"
)

const importScanKey = "import-scan"

// importSelKey builds the selection map key for one session.
func importSelKey(agent, ext string) string { return agent + "\x00" + ext }

// viewImportSessions renders the import picker: a scan summary, then each
// discovered folder with its sessions and a per-session import checkbox.
func (s *Shell) viewImportSessions(c *ui.Context) {
	t := c.Theme()
	// Header: back, title, refresh.
	ui.Row(c.Key("imp-head")).Gap(8).AlignItems(ui.Center).Children(func() {
		iconButton(c, "imp-back", iconBack, cat(c).GitBack(), func() { s.workbench = "" })
		ui.Text(c, cat(c).RouteImportSessions()).FontSize(18).Bold()
		ui.Spacer(c)
		iconButton(c, "imp-refresh", iconRefresh, cat(c).ActionRefresh(), func() {
			s.invalidate(importScanKey)
			s.importSel = map[string]bool{}
			s.importResult = ""
		})
	})

	raw := s.ensure(importScanKey, "scan_importable_sessions", nil)
	if e := s.errFor(importScanKey); e != "" {
		s.errorState(c, importScanKey, nil)
		return
	}
	if raw == nil {
		loadingState(c, "")
		return
	}
	scan := asObj(raw)
	folders := asListVal(scan["folders"])
	importable := int(intVal(scan, "importable_count"))

	// Summary line.
	ui.Text(c, cat(c).ImportSummary(importable, len(folders))).FontSize(12).TextColor(t.TextMuted)
	ui.Divider(c)
	if len(folders) == 0 {
		emptyState(c, iconInfo, cat(c).ImportNoSessions(), "")
		s.viewImportResult(c)
		s.viewActionOrError(c)
		return
	}

	// Count the checked sessions so the import action can enable/disable.
	selected := 0
	for _, v := range s.importSel {
		if v {
			selected++
		}
	}

	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(14).Children(func() {
			for _, f := range folders {
				s.viewImportFolder(c, f)
			}
		})
	})

	// Action bar: import the checked sessions.
	ui.Row(c.Key("imp-actions")).Gap(10).AlignItems(ui.Center).Children(func() {
		btn := ui.PrimaryButton(c.Key("imp-import"), cat(c).ImportSelected())
		btn.Disabled(selected == 0)
		btn.Label(cat(c).ImportSelected())
		if btn.Clicked() {
			s.importChecked(c)
		}
		if selected > 0 {
			ui.Text(c, cat(c).ImportSelectedCount(selected)).FontSize(11).TextColor(t.TextMuted)
		}
	})
	s.viewImportResult(c)
	s.viewActionOrError(c)
}

// viewImportResult shows the last import's outcome summary, if any. Kept separate
// so it renders in both the populated and the empty (post-import) branches.
func (s *Shell) viewImportResult(c *ui.Context) {
	if s.importResult == "" {
		return
	}
	t := c.Theme()
	ui.Text(c, cat(c).ImportResultTitle()).FontSize(12).Bold().TextColor(t.Success)
	ui.Text(c, s.importResult).FontSize(12).TextColor(t.Success)
}

// viewImportFolder renders one discovered folder: its name, agent types, a
// "select all" / "import all in folder" pair, and the session rows.
func (s *Shell) viewImportFolder(c *ui.Context, f map[string]any) {
	t := c.Theme()
	path := str(f, "path")
	name := firstNonEmpty(str(f, "name"), path)
	agents := asStringList(f["agent_types"])
	sessions := asListVal(f["sessions"])
	ui.Column(c.Key("impf-" + path)).Gap(6).Padding(10).Background(t.Surface).Radius(8).Children(func() {
		ui.Row(c.Key("impf-head-" + path)).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, name).FontSize(14).Bold()
			if len(agents) > 0 {
				ui.Text(c, strings.Join(agents, ", ")).FontSize(11).TextColor(t.TextMuted)
			}
			ui.Spacer(c)
			if ui.Button(c.Key("impf-all-"+path), cat(c).ImportSelectAll()).Clicked() {
				for _, ss := range sessions {
					s.importSel[importSelKey(str(ss, "agent_type"), str(ss, "external_id"))] = true
				}
			}
			if ui.Button(c.Key("impf-imp-"+path), cat(c).ImportImportAll()).Clicked() {
				s.importFolderSessions(c, sessions)
			}
		})
		if path != "" {
			ui.Text(c, cat(c).ImportInFolder(path)).FontSize(11).TextColor(t.TextMuted)
		}
		ui.Column(c).Gap(1).Children(func() {
			for _, ss := range sessions {
				s.viewImportSessionRow(c, ss)
			}
		})
	})
}

// viewImportSessionRow renders one session as a click-to-toggle selection row: a
// checkbox glyph, the title + metadata, and its New / Imported / Deleted badge.
func (s *Shell) viewImportSessionRow(c *ui.Context, sess map[string]any) {
	t := c.Theme()
	agent := str(sess, "agent_type")
	ext := str(sess, "external_id")
	key := importSelKey(agent, ext)
	checked := s.importSel[key]
	title := firstNonEmpty(str(sess, "title"), ext)
	status := str(sess, "status")
	meta := strings.TrimSpace(strings.Join([]string{
		str(sess, "model"),
		str(sess, "git_branch"),
		cat(c).ImportMsgCount(int(intVal(sess, "message_count"))),
		shortDate(str(sess, "started_at")),
	}, " · "))

	row := ui.Row(c.Key("imp-row-" + key)).PaddingX(8).PaddingY(6).Gap(8).AlignItems(ui.Center)
	if checked {
		row.Background(t.SurfacePressed)
	}
	row.Children(func() {
		if checked {
			ui.Icon(c, iconCheck).Width(15).Height(15).TextColor(t.Success)
		} else {
			ui.Icon(c, iconCircle).Width(15).Height(15).TextColor(t.TextMuted)
		}
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			ui.Text(c, title).FontSize(13).TextColor(t.Text).MaxLines(1)
			if meta != "" {
				ui.Text(c, meta).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
			}
		})
		importStatusBadge(c, status)
	})
	if row.Clicked() {
		s.importSel[key] = !checked
	}
}

// importStatusBadge renders the New / Imported / Deleted status in its color.
func importStatusBadge(c *ui.Context, status string) {
	var color ui.Color
	label := status
	switch status {
	case "new":
		color = c.Theme().Success
		label = cat(c).ImportStatusNew()
	case "imported":
		color = c.Theme().TextMuted
		label = cat(c).ImportStatusImported()
	case "deleted":
		color = c.Theme().Danger
		label = cat(c).ImportStatusDeleted()
	}
	ui.Text(c, label).FontSize(11).Bold().TextColor(color)
}

// ─── import actions ────────────────────────────────────────────────────────

// importChecked gathers the checked sessions and imports them.
func (s *Shell) importChecked(c *ui.Context) {
	keys := make([]map[string]any, 0)
	for k, v := range s.importSel {
		if !v {
			continue
		}
		parts := strings.SplitN(k, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		keys = append(keys, map[string]any{"agentType": parts[0], "externalId": parts[1]})
	}
	s.importSessions(c, keys)
}

// importFolderSessions imports every session in a folder (the "import all" path).
func (s *Shell) importFolderSessions(c *ui.Context, sessions []map[string]any) {
	sels := make([]map[string]any, 0, len(sessions))
	for _, ss := range sessions {
		sels = append(sels, map[string]any{
			"agentType":  str(ss, "agent_type"),
			"externalId": str(ss, "external_id"),
		})
	}
	s.importSessions(c, sels)
}

// importSessions runs import_selected_sessions off the UI thread and reports the
// outcome; the scan list is left in place (Refresh re-scans to update statuses).
func (s *Shell) importSessions(c *ui.Context, sels []map[string]any) {
	if len(sels) == 0 {
		return
	}
	svc := s.svc
	loc := c.Locale()
	go func() {
		raw, err := s.B.raw("import_selected_sessions", map[string]any{"selections": sels})
		s.mu.Lock()
		if err != nil {
			s.errorMsg = locales.In(loc).ImportFailed(err.Error())
			s.importResult = ""
		} else {
			s.errorMsg = ""
			m := asObj(raw)
			s.importResult = locales.In(loc).ImportSummaryCounts(
				int(intVal(m, "imported")), int(intVal(m, "updated")),
				int(intVal(m, "restored")), int(intVal(m, "skipped")))
		}
		// Keep the seeded scan list in view; the user re-scans with Refresh to
		// see updated per-session statuses.
		s.mu.Unlock()
		svc.Invalidate()
	}()
}
