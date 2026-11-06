package app

// project_boot.go mirrors upstream src/app/project-boot: a full-screen project
// bootstrap window that scaffolds a shadcn or HyperFrames project and wires the
// HyperFrames agent skills. Its backend (project_boot_*) is ported, so this is
// the real panel: a project-type switch, the per-type scaffold form, a Create
// that shells out to the matching runner, and a skills detect/install section.

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"gocode/locales"
)

const (
	pbTypeShadcn      = "shadcn"
	pbTypeHyperframes = "hyperframes"
	pbSkillsKey       = "pb-skills"
)

// pbPackageManagers are the runners the backend maps to a fetch-and-run CLI.
var pbPackageManagers = []string{"npm", "pnpm", "yarn", "bun"}

// viewProjectBoot renders the project-bootstrap window.
func (s *Shell) viewProjectBoot(c *ui.Context) {
	t := c.Theme()
	// Header: back, title, refresh.
	ui.Row(c.Key("pb-head")).Gap(8).AlignItems(ui.Center).Children(func() {
		iconButton(c, "pb-back", iconBack, cat(c).GitBack(), func() { s.workbench = "" })
		ui.Text(c, cat(c).RouteProjectBoot()).FontSize(18).Bold()
		ui.Spacer(c)
		iconButton(c, "pb-refresh", iconRefresh, cat(c).ActionRefresh(), func() {
			s.pbResult = ""
			s.errorMsg = ""
			s.invalidate(pbSkillsKey)
		})
	})

	// Project-type segmented control.
	ui.Text(c, cat(c).PbProjectType()).FontSize(11).Bold().TextColor(t.TextMuted)
	ui.Row(c.Key("pb-types")).Gap(8).Children(func() {
		for _, ty := range []string{pbTypeShadcn, pbTypeHyperframes} {
			label := cat(c).PbShadcn()
			if ty == pbTypeHyperframes {
				label = cat(c).PbHyperframes()
			}
			btn := ui.Button(c.Key("pb-type-"+ty), label)
			if s.pbType == ty {
				btn.Background(t.SurfacePressed)
			}
			if btn.Clicked() {
				s.pbType = ty
			}
		}
	})
	ui.Divider(c)

	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(12).MaxWidth(720).Children(func() {
			if s.pbType == pbTypeShadcn {
				s.viewProjectBootShadcn(c)
			} else {
				s.viewProjectBootHyperframes(c)
			}
			s.viewProjectBootSkills(c)
		})
	})

	if s.pbResult != "" {
		ui.Text(c, cat(c).PbCreated(s.pbResult)).FontSize(12).Bold().TextColor(t.Success)
	}
	s.viewActionOrError(c)
}

// pbFormField renders a labelled form row with the given body builder.
func (s *Shell) pbFormField(c *ui.Context, key, label string, body func()) {
	ui.Column(c.Key(key)).Gap(4).Children(func() {
		ui.Text(c, label).FontSize(12).Bold()
		body()
	})
}

// pbTargetDirField is the shared target-directory row (text + "Choose…" hook).
func (s *Shell) pbTargetDirField(c *ui.Context) {
	s.pbFormField(c, "pb-target", cat(c).PbTargetDir(), func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, firstNonEmpty(s.pbTargetDir, "—")).FontSize(12).
				TextColor(c.Theme().TextMuted).Grow(1).MaxLines(1)
			if ui.Button(c.Key("pb-choose"), cat(c).PbChooseDir()).Clicked() {
				s.pickPaths(cat(c).PbChooseDir(), true, false, func(picked []string) {
					s.mu.Lock()
					s.pbTargetDir = picked[0]
					s.mu.Unlock()
				})
			}
		})
	})
}

// ─── shadcn form ─────────────────────────────────────────────────────────────

func (s *Shell) viewProjectBootShadcn(c *ui.Context) {
	s.pbFormField(c, "pb-name", cat(c).PbProjectName(), func() {
		ui.TextInput(c.Key("pb-name"), &s.pbName).Placeholder("my-app").FillWidth()
	})
	s.pbFormField(c, "pb-template", cat(c).PbTemplate(), func() {
		ui.TextInput(c.Key("pb-template"), &s.pbTemplate).Placeholder("default").FillWidth()
	})
	s.pbFormField(c, "pb-preset", cat(c).PbPresetCode(), func() {
		ui.TextInput(c.Key("pb-preset"), &s.pbPreset).Placeholder("neutral").FillWidth()
	})
	s.pbFormField(c, "pb-pkg", cat(c).PbPackageManager(), func() {
		ui.Select(c.Key("pb-pkg"), &s.pbPkg, pbPackageManagers).Grow(1)
	})
	s.pbTargetDirField(c)
	ui.Row(c.Key("pb-create-row")).Gap(10).AlignItems(ui.Center).Children(func() {
		btn := ui.PrimaryButton(c.Key("pb-create"), cat(c).PbCreate())
		btn.Disabled(strings.TrimSpace(s.pbName) == "")
		if btn.Clicked() {
			s.createShadcn()
		}
		if strings.TrimSpace(s.pbName) == "" {
			ui.Text(c, cat(c).PbNameRequired()).FontSize(11).TextColor(c.Theme().Danger)
		}
	})
}

// ─── hyperframes form ────────────────────────────────────────────────────────

func (s *Shell) viewProjectBootHyperframes(c *ui.Context) {
	s.pbFormField(c, "pb-hf-name", cat(c).PbProjectName(), func() {
		ui.TextInput(c.Key("pb-hf-name"), &s.pbName).Placeholder("my-app").FillWidth()
	})
	s.pbFormField(c, "pb-hf-example", cat(c).PbExample(), func() {
		ui.TextInput(c.Key("pb-hf-example"), &s.pbExample).Placeholder("blank").FillWidth()
	})
	s.pbFormField(c, "pb-hf-res", cat(c).PbResolution(), func() {
		ui.TextInput(c.Key("pb-hf-res"), &s.pbResolution).Placeholder("1920x1080").FillWidth()
	})
	s.pbFormField(c, "pb-hf-pkg", cat(c).PbPackageManager(), func() {
		ui.Select(c.Key("pb-hf-pkg"), &s.pbPkg, pbPackageManagers).Grow(1)
	})
	s.pbTargetDirField(c)
	ui.Row(c.Key("pb-hf-create-row")).Gap(10).AlignItems(ui.Center).Children(func() {
		btn := ui.PrimaryButton(c.Key("pb-hf-create"), cat(c).PbCreate())
		btn.Disabled(strings.TrimSpace(s.pbName) == "")
		if btn.Clicked() {
			s.createHyperframes()
		}
		if strings.TrimSpace(s.pbName) == "" {
			ui.Text(c, cat(c).PbNameRequired()).FontSize(11).TextColor(c.Theme().Danger)
		}
	})
}

// ─── skill install ──────────────────────────────────────────────────────────

// viewProjectBootSkills lists the HyperFrames-capable agents with their
// installed state and lets the user install the selected ones.
func (s *Shell) viewProjectBootSkills(c *ui.Context) {
	t := c.Theme()
	ui.Column(c.Key("pb-skills")).Gap(6).Padding(10).Background(t.Surface).Radius(8).Children(func() {
		ui.Text(c, cat(c).PbDetectSkills()).FontSize(13).Bold()
		ui.Text(c, cat(c).PbSkillsHint()).FontSize(11).TextColor(t.TextMuted).Wrap()
		raw := s.ensure(pbSkillsKey, "detect_hyperframes_skills", nil)
		if e := s.errFor(pbSkillsKey); e != "" {
			s.errorState(c, pbSkillsKey, nil)
			return
		}
		if raw == nil {
			loadingState(c, "")
			return
		}
		agents := asList(raw)
		ui.Column(c).Gap(1).Children(func() {
			for _, a := range agents {
				agent := str(a, "agent")
				installed := asBool(a["installed"])
				sel := s.pbSkillAgents[agent]
				row := ui.Row(c.Key("pb-skill-" + agent)).PaddingX(8).PaddingY(5).Gap(8).AlignItems(ui.Center)
				if sel {
					row.Background(t.SurfacePressed)
				}
				row.Children(func() {
					if sel {
						ui.Icon(c, iconCheck).Width(14).Height(14).TextColor(t.Success)
					} else {
						ui.Icon(c, iconCircle).Width(14).Height(14).TextColor(t.TextMuted)
					}
					ui.Text(c, agent).FontSize(13).TextColor(t.Text).Grow(1)
					if installed {
						ui.Text(c, cat(c).PbInstalled()).FontSize(11).TextColor(t.Success)
					} else {
						ui.Text(c, cat(c).PbNotInstalled()).FontSize(11).TextColor(t.TextMuted)
					}
				})
				if row.Clicked() {
					s.pbSkillAgents[agent] = !sel
				}
			}
		})
		chosen := make([]string, 0)
		for a, on := range s.pbSkillAgents {
			if on {
				chosen = append(chosen, a)
			}
		}
		ui.Row(c.Key("pb-skills-actions")).Gap(10).AlignItems(ui.Center).Children(func() {
			btn := ui.PrimaryButton(c.Key("pb-install"), cat(c).PbInstallSkills())
			btn.Disabled(len(chosen) == 0)
			if btn.Clicked() {
				s.installSkills(c, chosen)
			}
		})
	})
}

// ─── create / install actions ───────────────────────────────────────────────

// createShadcn scaffolds a shadcn project (create_shadcn_project).
func (s *Shell) createShadcn() {
	name := strings.TrimSpace(s.pbName)
	if name == "" {
		s.mu.Lock()
		s.errorMsg = locales.PbNameRequired()
		s.mu.Unlock()
		return
	}
	svc := s.svc
	body := map[string]any{
		"projectName":    name,
		"template":       strings.TrimSpace(s.pbTemplate),
		"presetCode":     strings.TrimSpace(s.pbPreset),
		"packageManager": s.pbPkg,
		"targetDir":      strings.TrimSpace(s.pbTargetDir),
	}
	go func() {
		raw, err := s.B.raw("create_shadcn_project", body)
		s.mu.Lock()
		if err != nil {
			s.errorMsg = "create_shadcn_project: " + err.Error()
			s.pbResult = ""
		} else {
			s.errorMsg = ""
			s.pbResult = decodeString(raw)
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// createHyperframes scaffolds a HyperFrames project (create_hyperframes_project).
func (s *Shell) createHyperframes() {
	name := strings.TrimSpace(s.pbName)
	if name == "" {
		s.mu.Lock()
		s.errorMsg = locales.PbNameRequired()
		s.mu.Unlock()
		return
	}
	svc := s.svc
	body := map[string]any{
		"projectName":    name,
		"example":        strings.TrimSpace(s.pbExample),
		"resolution":     strings.TrimSpace(s.pbResolution),
		"packageManager": s.pbPkg,
		"targetDir":      strings.TrimSpace(s.pbTargetDir),
	}
	go func() {
		raw, err := s.B.raw("create_hyperframes_project", body)
		s.mu.Lock()
		if err != nil {
			s.errorMsg = "create_hyperframes_project: " + err.Error()
			s.pbResult = ""
		} else {
			s.errorMsg = ""
			s.pbResult = decodeString(raw)
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// installSkills installs the HyperFrames skills for the chosen agents.
func (s *Shell) installSkills(c *ui.Context, agents []string) {
	if len(agents) == 0 {
		return
	}
	svc := s.svc
	loc := c.Locale()
	go func() {
		_, err := s.B.raw("install_hyperframes_skills", map[string]any{"agents": agents})
		s.mu.Lock()
		if err != nil {
			s.errorMsg = locales.In(loc).PbInstallFailed(err.Error())
		} else {
			s.errorMsg = ""
			s.actionMsg = locales.In(loc).PbSkillsInstalled(strings.Join(agents, ", "))
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}
