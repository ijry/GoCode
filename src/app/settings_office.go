package app

// settings_office.go mirrors upstream src/app/settings/office-tools, which
// upstream REDIRECTS into the unified Skill Packs hub (tab=office). This build
// surfaces the officecli helper's detect status (officecli_detect) instead of
// redirecting, and points at Skill Packs for the packs themselves.

import "github.com/egoist/mygo/ui"

func (s *Shell) viewSettingsOfficeTools(c *ui.Context) {
	t := c.Theme()
	ui.Row(c.Key("of-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Office Tools").FontSize(22).Bold()
		ui.Spacer(c)
		iconButton(c, "of-refresh", iconRefresh, "Refresh", func() {
			s.invalidate("officecli")
		})
	})
	ui.Text(c, "Upstream merges this page into Skill Packs (Office tab); officecli is the bundled document helper.").
		FontSize(11).TextColor(t.TextMuted).Wrap()
	if s.errFor("officecli") != "" {
		s.errorState(c, "officecli", nil)
		return
	}
	info := asObj(s.ensure("officecli", "officecli_detect", nil))
	if info == nil {
		loadingState(c, "")
		return
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Status").TextColor(t.TextMuted)
		if truthy(info["installed"]) {
			ui.Text(c, "installed").Bold().TextColor(t.Success)
		} else {
			ui.Text(c, "not installed").Bold().TextColor(t.TextMuted)
		}
	})
	if v := str(info, "version"); v != "" {
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, "Version").TextColor(t.TextMuted)
			ui.Text(c, v).Bold()
		})
	}
	if e := str(info, "runtime_error"); e != "" {
		ui.Text(c, e).FontSize(12).TextColor(t.Warning).Wrap()
	}
}
