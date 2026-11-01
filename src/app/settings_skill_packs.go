package app

// settings_skill_packs.go mirrors upstream src/app/settings/skill-packs +
// components/settings/skill-packs-settings.tsx: one hub over the existing pack
// bodies (Experts / Science / Office) plus the central-store custom skills,
// with a shared refresh and a button that opens the central skills folder.

import (
	"encoding/json"

	"github.com/egoist/mygo/ui"

	"gocode/locales"
)

func (s *Shell) viewSettingsSkillPacks(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	// A folder path handed back by experts_open_central_dir opens through the
	// host's URL opener on this frame (goroutines cannot drive the host).
	s.mu.Lock()
	pending := s.pendingOpenPath
	s.pendingOpenPath = ""
	s.mu.Unlock()
	if pending != "" {
		openPathInFileManager(c, pending)
	}

	ui.Row(c.Key("sp-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.NavSkillPacks()).FontSize(22).Bold()
		ui.Spacer(c)
		if ui.Button(c.Key("sp-open"), m.SetSkillPacksOpenFolder()).Clicked() {
			s.openSkillsFolder(c.Locale())
		}
		iconButton(c, "sp-refresh", iconRefresh, m.ActionRefresh(), func() {
			for _, key := range []string{"experts-list", "science-list", "officecli", "custom-skills"} {
				s.invalidate(key)
			}
		})
	})
	ui.Text(c, m.SetSkillPacksAbout()).FontSize(12).TextColor(t.TextMuted).Wrap()

	ui.Tabs(c.Key("sp-tabs"), &s.skillPackTab,
		m.SetSkillPackExperts(), m.SetSkillPackScience(), m.SetSkillPackOffice(), m.SetSkillPackCustom())

	switch s.skillPackTab {
	case 1:
		s.scienceBody(c, m)
	case 2:
		s.officeToolsBody(c, m)
	case 3:
		s.customSkillsBody(c)
	default:
		s.expertsBody(c, m)
	}
}

// openSkillsFolder asks the backend to create (and return) the shared central
// store path, then hands it to the host opener.
func (s *Shell) openSkillsFolder(loc string) {
	svc := s.svc
	go func() {
		raw, err := s.B.raw("experts_open_central_dir", nil)
		if err != nil {
			s.notifyErr(locales.In(loc).SetSkillPacksOpenFailed(err.Error()))
			svc.Invalidate()
			return
		}
		var dir string
		_ = json.Unmarshal(raw, &dir)
		if dir == "" {
			return
		}
		s.mu.Lock()
		s.pendingOpenPath = dir
		s.mu.Unlock()
		svc.Invalidate()
	}()
}
