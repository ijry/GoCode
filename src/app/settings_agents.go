package app

// settings_agents.go is the Settings → Agents page (UI_GAPS P1-8 depth): the
// built-in ACP catalog with on-demand version probes (busy + inline result /
// error feedback) and custom-agent registration with the same client-side
// validation the backend enforces. It also explains what "available" means, so
// the page is usable without reading upstream docs.
//
// Wire contract:
//   acp_list_agents                          → built-in AcpAgentInfo rows
//   acp_detect_agent_local_version {agentType} → version string (or null)
//   acp_fetch_agent_latest_release {agentType} → {version} (or null)
//   acp_list_custom_agents                   → custom rows incl. problem/launchable
//   acp_save_custom_agent {params:{registry_id,name,description,version,
//     distribution_kind,spec,skills_dir?}}    → empty
//   acp_delete_custom_agent {registryId}      → empty

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"gocode/locales"
)

// customAgentEditor is the add-custom-agent form's working state.
type customAgentEditor struct {
	registryID   string
	name         string
	description  string
	distribution string // "npx" | "uvx"
	pkg          string // npm / Python package
	version      string // optional (backend defaults to "custom")
	skillsDir    string // optional, absolute or ~/…
}

// viewSettingsAgents renders the built-in catalog, its check feedback, and the
// custom-agent section.
func (s *Shell) viewSettingsAgents(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	agents := asList(s.ensure("agents", "acp_list_agents", nil))

	ui.Row(c.Key("ag-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.NavAgents()).FontSize(22).Bold()
		ui.Spacer(c)
		iconButton(c, "ag-refresh", iconRefresh, m.ActionRefresh(), func() {
			s.invalidate("agents")
			s.invalidate("custom-agents")
		})
		if ui.PrimaryButton(c.Key("ag-add-custom"), m.SetAddCustomAgent()).Clicked() {
			s.startCustomAgent()
		}
	})
	ui.Text(c, m.SetAgentsHint()).FontSize(12).TextColor(t.TextMuted).Wrap()

	if s.caEditor != nil {
		s.viewCustomAgentEditor(c)
		return
	}

	if s.errFor("agents") != "" {
		s.errorState(c, "agents", nil)
		return
	}
	if agents == nil {
		loadingState(c, m.SetLoadingAgents())
		return
	}
	ui.Text(c, m.SetAgentsCount(len(agents))).FontSize(12).TextColor(t.TextMuted)
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(6).Children(func() {
			for i, a := range agents {
				s.viewAgentCard(c, itoa("ag", i), a)
			}
			ui.Divider(c)
			s.viewCustomAgents(c)
		})
	})
}

// viewAgentCard renders one built-in agent plus its last check outcome.
func (s *Shell) viewAgentCard(c *ui.Context, key string, a map[string]any) {
	t := c.Theme()
	m := cat(c)
	name := firstNonEmpty(str(a, "name"), str(a, "agent_type"))
	at := str(a, "agent_type")
	desc := str(a, "description")
	available := asBool(a["available"])
	s.mu.Lock()
	ver := s.agentVer[at]
	latest := s.agentLatest[at]
	note := s.agentCheckNote[at]
	checkErr := s.agentCheckErr[at]
	checking := s.agentChecking == at
	busy := s.agentChecking != ""
	s.mu.Unlock()

	ui.Column(c.Key(key)).Gap(2).Padding(10).Background(t.Surface).Radius(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, name).Bold()
			ui.Text(c, at).FontSize(11).TextColor(t.TextMuted)
			ui.Spacer(c)
			if ver != "" {
				ui.Text(c, "v"+ver).FontSize(11).TextColor(t.Success)
			} else if available {
				ui.Text(c, m.SetAgentAvailable()).FontSize(11).TextColor(t.Success)
			} else {
				ui.Text(c, m.SetAgentNotInstalled()).FontSize(11).TextColor(t.TextMuted)
			}
			label := m.SetCheck()
			if checking {
				label = m.SetChecking()
			}
			btn := ui.Button(c.Key(key+"-chk"), label)
			btn.Disabled(busy)
			if btn.Clicked() {
				s.checkAgentVersion(c, name, at)
			}
		})
		if latest != "" {
			ui.Text(c, m.SetUpdateAvailable(latest)).FontSize(11).TextColor(t.Warning)
		}
		if note != "" {
			ui.Text(c, note).FontSize(11).TextColor(t.Warning)
		}
		if checkErr != "" {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconAlert).Width(12).Height(12).TextColor(t.Danger)
				ui.Text(c, checkErr).FontSize(11).TextColor(t.Danger).Wrap()
			})
		}
		if desc != "" {
			ui.Text(c, desc).FontSize(12).TextColor(t.TextMuted).MaxLines(3).Wrap()
		}
	})
}

// checkAgentVersion probes the local version and the latest release on demand.
// The row shows a busy label while it runs; the outcome lands as a note, an
// error line, and a toast.
func (s *Shell) checkAgentVersion(c *ui.Context, name, agentType string) {
	loc := c.Locale()
	s.mu.Lock()
	if s.agentChecking != "" {
		s.mu.Unlock()
		return
	}
	s.agentChecking = agentType
	delete(s.agentCheckNote, agentType)
	delete(s.agentCheckErr, agentType)
	s.mu.Unlock()

	go func() {
		raw, err := s.B.raw("acp_detect_agent_local_version", map[string]any{"agentType": agentType})
		if err != nil {
			s.mu.Lock()
			s.agentChecking = ""
			s.agentCheckErr[agentType] = locales.In(loc).SetAgentCheckFailed(err.Error())
			s.mu.Unlock()
			s.svc.Invalidate()
			return
		}
		var ver string
		_ = json.Unmarshal(raw, &ver)

		latest := ""
		if lraw, lerr := s.B.raw("acp_fetch_agent_latest_release", map[string]any{"agentType": agentType}); lerr == nil {
			var lm map[string]any
			if json.Unmarshal(lraw, &lm) == nil && lm != nil {
				latest, _ = lm["version"].(string)
			}
		}

		s.mu.Lock()
		s.agentChecking = ""
		s.agentVer[agentType] = ver
		if latest != "" {
			s.agentLatest[agentType] = latest
		} else {
			delete(s.agentLatest, agentType)
		}
		if ver == "" {
			s.agentCheckNote[agentType] = locales.In(loc).SetAgentNoLocalVersion()
		} else {
			delete(s.agentCheckNote, agentType)
		}
		s.mu.Unlock()
		s.notify(locales.In(loc).SetAgentChecked(name))
		s.svc.Invalidate()
	}()
}

// viewCustomAgents renders the registered custom agents below the catalog.
func (s *Shell) viewCustomAgents(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	list := asList(s.ensure("custom-agents", "acp_list_custom_agents", nil))

	ui.Row(c.Key("ca-head")).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.SetCustomAgentsTitle()).FontSize(15).Bold()
		ui.Spacer(c)
		iconButton(c, "ca-refresh", iconRefresh, m.ActionRefresh(), func() {
			s.invalidate("custom-agents")
		})
	})
	ui.Text(c, m.SetCustomAgentsHint()).FontSize(12).TextColor(t.TextMuted).Wrap()

	if s.errFor("custom-agents") != "" {
		s.errorState(c, "custom-agents", nil)
		return
	}
	if list == nil {
		loadingState(c, "")
		return
	}
	if len(list) == 0 {
		ui.Text(c, m.SetNoCustomAgents()).FontSize(12).TextColor(t.TextMuted)
		return
	}
	for i, ca := range list {
		s.viewCustomAgentRow(c, itoa("ca", i), ca)
	}
}

// viewCustomAgentRow is one custom agent: identity, launcher, and delete with
// an inline confirmation.
func (s *Shell) viewCustomAgentRow(c *ui.Context, key string, ca map[string]any) {
	t := c.Theme()
	m := cat(c)
	id := str(ca, "registryId")
	name := firstNonEmpty(str(ca, "name"), id)
	kind := str(ca, "distributionKind")
	version := str(ca, "version")
	desc := str(ca, "description")
	problem := str(ca, "problem")
	launchable := asBool(ca["launchable"])

	ui.Column(c.Key(key)).Gap(3).Padding(10).Background(t.Surface).Radius(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, name).Bold()
			ui.Text(c, cat(c).AgentCustomBadge(id)).FontSize(11).TextColor(t.TextMuted)
			ui.Spacer(c)
			if kind != "" {
				ui.Text(c, kind).FontSize(11).TextColor(t.TextMuted)
			}
			if version != "" {
				verColor := t.Warning
				if launchable {
					verColor = t.Success
				}
				ui.Text(c, "v"+version).FontSize(11).TextColor(verColor)
			}
			iconButton(c, key+"-del", iconX, m.SetDeleteNamed(name), func() {
				s.caDeleteConfirm = id
			})
		})
		if desc != "" {
			ui.Text(c, desc).FontSize(12).TextColor(t.TextMuted).MaxLines(2).Wrap()
		}
		if problem != "" {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconAlert).Width(12).Height(12).TextColor(t.Warning)
				ui.Text(c, m.SetCustomAgentProblem(problem)).FontSize(11).TextColor(t.Warning).Wrap()
			})
		}
		if s.caDeleteConfirm == id {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, m.SetCustomAgentDeletePrompt(name)).FontSize(12).TextColor(t.Danger)
				if ui.PrimaryButton(c.Key(key+"-confirm-yes"), m.ActionDelete()).Clicked() {
					s.deleteCustomAgent(c, id)
				}
				if ui.Button(c.Key(key+"-confirm-no"), m.ActionCancel()).Clicked() {
					s.caDeleteConfirm = ""
				}
			})
		}
	})
}

// viewCustomAgentEditor renders the add form with inline validation.
func (s *Shell) viewCustomAgentEditor(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	e := s.caEditor
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c.Key("ca-form")).Gap(8).Children(func() {
			ui.Text(c, m.SetAddCustomAgent()).FontSize(18).Bold()
			ui.Text(c, m.SetCustomAgentsHint()).FontSize(12).TextColor(t.TextMuted).Wrap()

			ui.Text(c, m.SetCustomAgentId()).FontSize(12).TextColor(t.TextMuted)
			ui.TextInput(c.Key("ca-f-id"), &e.registryID).Placeholder("team-agent")
			ui.Text(c, m.SetCustomAgentIdHint()).FontSize(11).TextColor(t.TextMuted).Wrap()

			ui.Text(c, m.SetCustomAgentName()).FontSize(12).TextColor(t.TextMuted)
			ui.TextInput(c.Key("ca-f-name"), &e.name).Placeholder("Team Agent")

			ui.Text(c, m.SetCustomAgentDescription()).FontSize(12).TextColor(t.TextMuted)
			ui.TextInput(c.Key("ca-f-desc"), &e.description).Placeholder(m.SetCustomAgentDescription())

			ui.Row(c.Key("ca-f-dist-row")).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, m.SetCustomAgentDistribution()).FontSize(12).TextColor(t.TextMuted)
				ui.Select(c.Key("ca-f-dist"), &e.distribution, []string{"npx", "uvx"}).Width(120)
			})
			ui.Text(c, m.SetCustomAgentPackage()).FontSize(12).TextColor(t.TextMuted)
			ui.TextInput(c.Key("ca-f-pkg"), &e.pkg).Placeholder("@scope/agent")
			ui.Text(c, m.SetCustomAgentPackageHint()).FontSize(11).TextColor(t.TextMuted).Wrap()

			ui.Text(c, m.SetCustomAgentVersion()).FontSize(12).TextColor(t.TextMuted)
			ui.TextInput(c.Key("ca-f-ver"), &e.version).Placeholder("1.0.0")
			ui.Text(c, m.SetCustomAgentVersionHint()).FontSize(11).TextColor(t.TextMuted).Wrap()

			ui.Text(c, m.SetCustomAgentSkillsDir()).FontSize(12).TextColor(t.TextMuted)
			ui.TextInput(c.Key("ca-f-skills"), &e.skillsDir).Placeholder("~/agents/team/skills")
			ui.Text(c, m.SetCustomAgentSkillsDirHint()).FontSize(11).TextColor(t.TextMuted).Wrap()

			if s.caErr != "" {
				ui.Row(c.Key("ca-form-err")).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconAlert).Width(13).Height(13).TextColor(t.Danger)
					ui.Text(c, s.caErr).FontSize(12).TextColor(t.Danger).Wrap()
				})
			}
			ui.Row(c).Gap(6).Children(func() {
				if ui.PrimaryButton(c.Key("ca-f-save"), m.SetSaveCustomAgent()).Clicked() {
					s.saveCustomAgent(c)
				}
				if ui.Button(c.Key("ca-f-cancel"), m.ActionCancel()).Clicked() {
					s.caEditor = nil
					s.caErr = ""
				}
			})
		})
	})
}

// ── mutations ──

func (s *Shell) startCustomAgent() {
	s.caEditor = &customAgentEditor{distribution: "npx"}
	s.caErr = ""
	s.caDeleteConfirm = ""
}

// validCustomAgentID mirrors the backend's id rule: ≤64 chars, no leading dot,
// only letters/digits/dot/dash/underscore.
func validCustomAgentID(id string) bool {
	if id == "" || len(id) > 64 || strings.HasPrefix(id, ".") {
		return false
	}
	for _, r := range id {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.'
		if !ok {
			return false
		}
	}
	return true
}

// validCustomVersion mirrors isSafePathComponent(version): ≤128 chars, no
// leading dot, only letters/digits/dot/dash/underscore.
func validCustomVersion(v string) bool {
	if v == "" || len(v) > 128 || strings.HasPrefix(v, ".") {
		return false
	}
	for _, r := range v {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.'
		if !ok {
			return false
		}
	}
	return true
}

// validateCustomAgent catches what the backend would reject, before the round
// trip, so the form can point at the offending field.
func (s *Shell) validateCustomAgent(c *ui.Context) bool {
	m := cat(c)
	e := s.caEditor
	if e == nil {
		return false
	}
	if strings.TrimSpace(e.registryID) == "" {
		s.caErr = m.SetCustomAgentIdRequired()
		return false
	}
	if !validCustomAgentID(strings.TrimSpace(e.registryID)) {
		s.caErr = m.SetCustomAgentIdInvalid()
		return false
	}
	if strings.TrimSpace(e.name) == "" {
		s.caErr = m.SetCustomAgentNameRequired()
		return false
	}
	if strings.TrimSpace(e.pkg) == "" {
		s.caErr = m.SetCustomAgentPackageRequired()
		return false
	}
	if v := strings.TrimSpace(e.version); v != "" && !validCustomVersion(v) {
		s.caErr = m.SetCustomAgentVersionInvalid()
		return false
	}
	if d := strings.TrimSpace(e.skillsDir); d != "" && !strings.HasPrefix(d, "~") && !filepath.IsAbs(d) {
		s.caErr = m.SetCustomAgentSkillsAbs()
		return false
	}
	s.caErr = ""
	return true
}

func (s *Shell) saveCustomAgent(c *ui.Context) {
	if !s.validateCustomAgent(c) {
		return
	}
	e := s.caEditor
	id := strings.TrimSpace(e.registryID)
	name := strings.TrimSpace(e.name)
	kind := e.distribution
	if kind != "uvx" {
		kind = "npx"
	}
	params := map[string]any{
		"registry_id":       id,
		"name":              name,
		"description":       strings.TrimSpace(e.description),
		"version":           strings.TrimSpace(e.version),
		"distribution_kind": kind,
		"spec":              map[string]any{kind: map[string]any{"package": strings.TrimSpace(e.pkg)}},
	}
	if d := strings.TrimSpace(e.skillsDir); d != "" {
		params["skills_dir"] = d
	}
	loc := c.Locale()
	s.caErr = ""
	svc := s.svc
	go func() {
		_, err := s.B.raw("acp_save_custom_agent", map[string]any{"params": params})
		s.mu.Lock()
		if err != nil {
			s.caErr = locales.In(loc).SetSaveFailed(err.Error())
		} else {
			s.notify(locales.In(loc).SetCustomAgentSaved(name))
			s.caEditor = nil
			s.caErr = ""
			delete(s.cache, "custom-agents")
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

func (s *Shell) deleteCustomAgent(c *ui.Context, id string) {
	loc := c.Locale()
	s.caDeleteConfirm = ""
	svc := s.svc
	go func() {
		_, err := s.B.raw("acp_delete_custom_agent", map[string]any{"registryId": id})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).SetCustomAgentDeleteFailed(err.Error()))
		} else {
			s.notify(locales.In(loc).SetCustomAgentDeleted())
			delete(s.cache, "custom-agents")
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}
