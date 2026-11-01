package app

// settings_office.go mirrors upstream src/app/settings/office-tools, which
// upstream REDIRECTS into the unified Skill Packs hub (tab=office). This build
// surfaces the officecli helper's detect status (officecli_detect) instead of
// redirecting, and points at Skill Packs for the packs themselves.

import (
	"github.com/egoist/mygo/ui"

	"gocode/locales"
)

func (s *Shell) viewSettingsOfficeTools(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	ui.Row(c.Key("of-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.NavOfficeTools()).FontSize(22).Bold()
		ui.Spacer(c)
		iconButton(c, "of-refresh", iconRefresh, m.ActionRefresh(), func() {
			s.invalidate("officecli")
		})
	})
	ui.Text(c, m.SetOfficeToolsHint()).
		FontSize(11).TextColor(t.TextMuted).Wrap()
	s.officeToolsBody(c, m)
}

// officeToolsBody is the officecli status without its page header — shared
// with the Skill Packs hub's Office tab.
func (s *Shell) officeToolsBody(c *ui.Context, m locales.Messages) {
	t := c.Theme()
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
		ui.Text(c, m.SetStatus()).TextColor(t.TextMuted)
		if truthy(info["installed"]) {
			ui.Text(c, m.SetInstalled()).Bold().TextColor(t.Success)
		} else {
			ui.Text(c, m.SetAgentNotInstalled()).Bold().TextColor(t.TextMuted)
		}
	})
	if v := str(info, "version"); v != "" {
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, m.SetVersion()).TextColor(t.TextMuted)
			ui.Text(c, v).Bold()
		})
	}
	if e := str(info, "runtime_error"); e != "" {
		ui.Text(c, e).FontSize(12).TextColor(t.Warning).Wrap()
	}
}
