package app

// settings_science.go mirrors upstream src/app/settings/science: the bundled
// science skill packs, installable per agent (science_list). This build ships
// none, so the list is empty by design — the same honest state as Experts.

import (
	"github.com/egoist/mygo/ui"

	"gocode/locales"
)

func (s *Shell) viewSettingsScience(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	ui.Row(c.Key("sc-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.NavScience()).FontSize(22).Bold()
		ui.Spacer(c)
		iconButton(c, "sc-refresh", iconRefresh, m.ActionRefresh(), func() {
			s.invalidate("science-list")
		})
	})
	ui.Text(c, m.SetScienceHint()).FontSize(12).TextColor(t.TextMuted)
	s.scienceBody(c, m)
}

// scienceBody is the science pack list without its page header — shared with
// the Skill Packs hub's Science tab.
func (s *Shell) scienceBody(c *ui.Context, m locales.Messages) {
	t := c.Theme()
	if s.errFor("science-list") != "" {
		s.errorState(c, "science-list", nil)
		return
	}
	list := asList(s.ensure("science-list", "science_list", nil))
	if list == nil {
		loadingState(c, "")
		return
	}
	if len(list) == 0 {
		emptyState(c, iconInfo, m.SetNoSciencePacks(), "")
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(6).Children(func() {
			for i, p := range list {
				id := str(p, "id")
				name := firstNonEmpty(str(p, "name"), str(p, "display_name"), id)
				ui.Column(c.Key(itoa("sc", i))).Gap(2).Padding(10).Background(t.Surface).Radius(8).Children(func() {
					ui.Text(c, name).Bold()
					if desc := str(p, "description"); desc != "" {
						ui.Text(c, desc).FontSize(12).TextColor(t.TextMuted).MaxLines(3).Wrap()
					}
				})
			}
		})
	})
}
