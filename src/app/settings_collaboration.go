package app

// settings_collaboration.go mirrors upstream src/app/settings/collaboration +
// components/settings/{collaboration,delegation,agent-tools}-settings.tsx:
// the delegation kill switch and its two valves, and the gocode-mcp tool
// groups an agent gets inside a conversation. Controls are mygo's native ones
// (Switch, StepSlider, TextInput); saves write only the groups that moved.

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"gocode/locales"
)

// collabForm is the delegation form as loaded from (and saved to)
// get_delegation_settings.
type collabForm struct {
	enabled  bool
	depth    float64
	cacheMB  string
	defaults map[string]any
}

// agentToolValues is one field per switch, flattened across the six backend
// groups the panel writes.
type agentToolValues struct {
	feedback    bool
	question    bool
	sessionInfo bool
	browser     bool
	browserEval bool
	computer    bool
	automations bool
	workTasks   bool
}

func (v agentToolValues) equal(o agentToolValues) bool { return v == o }

func (s *Shell) viewSettingsCollaboration(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	ui.Text(c, m.NavCollaboration()).FontSize(22).Bold()
	ui.Text(c, m.SetCollabHint()).FontSize(12).TextColor(t.TextMuted).Wrap()
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(20).Children(func() {
			s.viewDelegationSection(c)
			s.viewAgentToolsSection(c)
		})
	})
}

// ─── Delegation ────────────────────────────────────────────────────────────

func (s *Shell) viewDelegationSection(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	ui.Column(c.Key("cb-deleg")).Gap(4).Children(func() {
		ui.Text(c, m.SetCollabDelegationTitle()).FontSize(15).Bold()
		ui.Text(c, m.SetCollabDelegationDesc()).FontSize(11).TextColor(t.TextMuted).Wrap()
	})
	s.mu.Lock()
	if s.collabReseed {
		s.collabReseed, s.collabLoaded = false, false
	}
	s.mu.Unlock()
	raw := s.ensure("delegation", "get_delegation_settings", nil)
	if !s.collabLoaded {
		if raw == nil {
			if s.errFor("delegation") != "" {
				s.errorState(c, "delegation", nil)
			} else {
				loadingState(c, "")
			}
			return
		}
		s.seedDelegation(raw)
	}

	ui.Tabs(c.Key("cb-tabs"), &s.collabTab, m.SetCollabTabGeneral(), m.SetCollabTabAgentDefaults())

	ui.Column(c.Key("cb-card")).Gap(12).Padding(12).Background(t.Surface).Radius(8).Children(func() {
		if s.collabTab == 0 {
			s.delegationGeneralRows(c)
		} else {
			s.delegationAgentDefaultsRows(c)
		}
	})

	ui.Row(c.Key("cb-savebar")).Gap(10).AlignItems(ui.Center).Children(func() {
		if ui.PrimaryButton(c.Key("cb-save"), m.ActionSave()).Disabled(!s.collabDirty()).Clicked() {
			s.saveDelegation(c.Locale())
		}
		if s.collabDirty() {
			ui.Text(c, m.SetUnsaved()).FontSize(11).TextColor(t.Warning)
		}
	})
}

func (s *Shell) seedDelegation(raw json.RawMessage) {
	d := asObj(raw)
	if d == nil {
		return
	}
	cache := intVal(d, "completed_cache_max_mb")
	if cache == 0 {
		cache = 512
	}
	depth := float64(intVal(d, "depth_limit"))
	if depth < 1 {
		depth = 1
	}
	if depth > 8 {
		depth = 8
	}
	defaults := map[string]any{}
	if v, ok := d["agent_defaults"].(map[string]any); ok {
		defaults = v
	}
	s.collabEnabled = truthy(d["enabled"])
	s.collabDepth = depth
	s.collabCacheMB = fmt.Sprintf("%d", cache)
	s.collabDefaults = defaults
	s.collabBase = collabForm{
		enabled:  s.collabEnabled,
		depth:    s.collabDepth,
		cacheMB:  s.collabCacheMB,
		defaults: defaults,
	}
	s.collabLoaded = true
}

func (s *Shell) collabDirty() bool {
	if !s.collabLoaded {
		return false
	}
	return s.collabBase.enabled != s.collabEnabled ||
		s.collabBase.depth != s.collabDepth ||
		s.collabBase.cacheMB != s.collabCacheMB ||
		!reflect.DeepEqual(s.collabBase.defaults, s.collabDefaults)
}

func (s *Shell) delegationGeneralRows(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	ui.Row(c.Key("cb-en")).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, m.SetCollabEnable()).Bold()
			ui.Text(c, m.SetCollabEnableHint()).FontSize(11).TextColor(t.TextMuted).Wrap()
		})
		ui.Switch(c.Key("cb-en-sw"), &s.collabEnabled)
	})
	ui.Row(c.Key("cb-depth")).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, m.SetCollabDepth()).Bold()
			ui.Text(c, m.SetCollabDepthHint()).FontSize(11).TextColor(t.TextMuted).Wrap()
		})
		ui.Textf(c, "%d", int(s.collabDepth+0.5)).Bold().Width(20)
		ui.StepSlider(c.Key("cb-depth-sl"), &s.collabDepth, 1, 8, 1).Width(200).Disabled(!s.collabEnabled)
	})
	ui.Row(c.Key("cb-cache")).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, m.SetCollabCache()).Bold()
			ui.Text(c, m.SetCollabCacheHint()).FontSize(11).TextColor(t.TextMuted).Wrap()
		})
		ui.TextInput(c.Key("cb-cache-in"), &s.collabCacheMB).Width(110).Disabled(!s.collabEnabled)
	})
}

func (s *Shell) delegationAgentDefaultsRows(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	if len(s.collabDefaults) == 0 {
		ui.Text(c, m.SetCollabDefaultsEmpty()).FontSize(12).TextColor(t.TextMuted).Wrap()
		return
	}
	keys := make([]string, 0, len(s.collabDefaults))
	for k := range s.collabDefaults {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, agent := range keys {
		agent := agent
		entry, _ := s.collabDefaults[agent].(map[string]any)
		mode := str(entry, "mode")
		ui.Row(c.Key("cb-ad-" + agent)).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, agent).Bold()
				line := m.SetCollabDefaultsMode(firstNonEmpty(mode, m.SetCollabDefaultsInherit()))
				if cfgMap, ok := entry["config_values"].(map[string]any); ok && len(cfgMap) > 0 {
					line += fmt.Sprintf(" · %d config", len(cfgMap))
				}
				ui.Text(c, line).FontSize(11).TextColor(t.TextMuted)
			})
			if ui.Button(c.Key("cb-ad-rm-"+agent), m.SetCollabRemoveOverride()).Disabled(!s.collabEnabled).Clicked() {
				s.removeDelegationOverride(agent)
			}
		})
	}
}

func (s *Shell) removeDelegationOverride(agent string) {
	next := make(map[string]any, len(s.collabDefaults))
	for k, v := range s.collabDefaults {
		if k != agent {
			next[k] = v
		}
	}
	s.collabDefaults = next
}

func (s *Shell) saveDelegation(loc string) {
	if !s.collabLoaded {
		return
	}
	cache := 512
	if v := strings.TrimSpace(s.collabCacheMB); v != "" {
		if n, err := parseNonNegativeInt(v); err == nil {
			cache = n
		}
	}
	depth := int(s.collabDepth + 0.5)
	if depth < 1 {
		depth = 1
	}
	if depth > 8 {
		depth = 8
	}
	payload := map[string]any{
		"enabled":                s.collabEnabled,
		"depth_limit":            depth,
		"completed_cache_max_mb": cache,
		"agent_defaults":         s.collabDefaults,
	}
	svc := s.svc
	go func() {
		_, err := s.B.raw("set_delegation_settings", map[string]any{"settings": payload})
		s.mu.Lock()
		if err == nil {
			s.collabReseed = true // re-seed from the server's applied values
			delete(s.cache, "delegation")
		}
		s.mu.Unlock()
		if err != nil {
			s.notifyErr(locales.In(loc).SetSaveFailed(err.Error()))
		} else {
			s.notify(locales.In(loc).SetSaved())
		}
		svc.Invalidate()
	}()
}

// ─── Agent tools ───────────────────────────────────────────────────────────

// agentToolKeys are the six endpoints the switch matrix reads, with the cache
// key each response lives under.
var agentToolKeys = []string{
	"tools-feedback", "tools-question", "tools-session-info",
	"tools-browser", "tools-computer", "tools-chat-authoring",
}

var agentToolCmds = map[string]string{
	"tools-feedback":       "get_feedback_settings",
	"tools-question":       "get_question_settings",
	"tools-session-info":   "get_session_info_settings",
	"tools-browser":        "get_browser_tools_settings",
	"tools-computer":       "get_computer_tools_settings",
	"tools-chat-authoring": "get_chat_authoring_settings",
}

func (s *Shell) viewAgentToolsSection(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	s.mu.Lock()
	if s.toolsReseed {
		s.toolsReseed, s.toolsLoaded = false, false
	}
	s.mu.Unlock()
	ui.Column(c.Key("cb-tools-head")).Gap(4).Children(func() {
		// Right padding keeps the refresh icon clear of the scrollbar drawn over
		// the page's right edge.
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(0, 14, 0, 0).Children(func() {
			ui.Text(c, m.SetToolsTitle()).FontSize(15).Bold()
			ui.Spacer(c)
			iconButton(c, "cb-tools-refresh", iconRefresh, m.ActionRefresh(), func() {
				s.mu.Lock()
				s.toolsLoaded = false
				for _, k := range agentToolKeys {
					delete(s.cache, k)
				}
				s.mu.Unlock()
				s.svc.Invalidate()
			})
		})
		ui.Text(c, m.SetToolsDesc()).FontSize(11).TextColor(t.TextMuted).Wrap()
	})

	if !s.toolsLoaded {
		var raws []json.RawMessage
		settled := true
		var failures []string
		for _, key := range agentToolKeys {
			raw := s.ensure(key, agentToolCmds[key], nil)
			if raw == nil && s.errFor(key) == "" {
				settled = false
			}
			if raw == nil && s.errFor(key) != "" {
				failures = append(failures, s.errFor(key))
			}
			raws = append(raws, raw)
		}
		if !settled {
			loadingState(c, "")
			return
		}
		s.seedAgentTools(raws)
		if len(failures) > 0 {
			ui.Text(c, m.SetCollabLoadFailed(strings.Join(failures, "; "))).
				FontSize(11).TextColor(t.Warning).Wrap()
		}
	}

	if !s.tools.browser {
		s.tools.browserEval = false
	}
	ui.Column(c.Key("cb-tools-card")).Gap(12).Padding(12).Background(t.Surface).Radius(8).Children(func() {
		s.agentToolSwitchRow(c, "cb-t-fb", m.SetToolFeedback(), m.SetToolFeedbackHint(), &s.tools.feedback, true)
		s.agentToolSwitchRow(c, "cb-t-q", m.SetToolQuestion(), m.SetToolQuestionHint(), &s.tools.question, true)
		s.agentToolSwitchRow(c, "cb-t-si", m.SetToolSessionInfo(), m.SetToolSessionInfoHint(), &s.tools.sessionInfo, true)
		s.agentToolSwitchRow(c, "cb-t-br", m.SetToolBrowser(), m.SetToolBrowserHint(), &s.tools.browser, true)
		s.agentToolSwitchRow(c, "cb-t-br-ev", m.SetToolBrowserEval(), m.SetToolBrowserEvalHint(), &s.tools.browserEval, s.tools.browser)
		s.agentToolSwitchRow(c, "cb-t-cu", m.SetToolComputer(), m.SetToolComputerHint(), &s.tools.computer, true)
		s.agentToolSwitchRow(c, "cb-t-au", m.SetToolAutomations(), m.SetToolAutomationsHint(), &s.tools.automations, true)
		s.agentToolSwitchRow(c, "cb-t-wt", m.SetToolWorkTasks(), m.SetToolWorkTasksHint(), &s.tools.workTasks, true)
	})

	ui.Row(c.Key("cb-tools-savebar")).Gap(10).AlignItems(ui.Center).Children(func() {
		dirty := !s.tools.equal(s.toolsBase)
		if ui.PrimaryButton(c.Key("cb-tools-save"), m.ActionSave()).Disabled(!dirty).Clicked() {
			s.saveAgentTools(c.Locale())
		}
		if dirty {
			ui.Text(c, m.SetUnsaved()).FontSize(11).TextColor(t.Warning)
		}
	})
}

func (s *Shell) seedAgentTools(raws []json.RawMessage) {
	v := agentToolValues{
		// The two read-only lookups ship on; feedback and the writers ship off.
		question:    true,
		sessionInfo: true,
	}
	if len(raws) == 6 {
		if d := asObj(raws[0]); d != nil {
			v.feedback = truthy(d["enabled"])
		}
		if d := asObj(raws[1]); d != nil {
			v.question = truthy(d["enabled"])
		}
		if d := asObj(raws[2]); d != nil {
			v.sessionInfo = truthy(d["enabled"])
		}
		if d := asObj(raws[3]); d != nil {
			v.browser = truthy(d["enabled"])
			v.browserEval = truthy(d["eval"]) && v.browser
		}
		if d := asObj(raws[4]); d != nil {
			v.computer = truthy(d["enabled"])
		}
		if d := asObj(raws[5]); d != nil {
			v.automations = truthy(d["automations_enabled"])
			v.workTasks = truthy(d["work_tasks_enabled"])
		}
	}
	s.tools = v
	s.toolsBase = v
	s.toolsLoaded = true
}

func (s *Shell) agentToolSwitchRow(c *ui.Context, key, title, hint string, val *bool, available bool) {
	t := c.Theme()
	ui.Row(c.Key(key)).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, title).Bold()
			ui.Text(c, hint).FontSize(11).TextColor(t.TextMuted).Wrap()
		})
		ui.Switch(c.Key(key+"-sw"), val).Disabled(!available)
	})
}

func (s *Shell) saveAgentTools(loc string) {
	v, base := s.tools, s.toolsBase
	svc := s.svc
	go func() {
		var failures []string
		write := func(cmd string, body map[string]any) {
			if _, err := s.B.raw(cmd, body); err != nil {
				failures = append(failures, err.Error())
			}
		}
		if v.feedback != base.feedback {
			write("set_feedback_settings", map[string]any{"settings": map[string]any{"enabled": v.feedback}})
		}
		if v.question != base.question {
			write("set_question_settings", map[string]any{"settings": map[string]any{"enabled": v.question}})
		}
		if v.sessionInfo != base.sessionInfo {
			write("set_session_info_settings", map[string]any{"settings": map[string]any{"enabled": v.sessionInfo}})
		}
		if v.browser != base.browser || v.browserEval != base.browserEval {
			write("set_browser_tools_settings", map[string]any{"settings": map[string]any{
				"enabled": v.browser, "eval": v.browserEval && v.browser,
			}})
		}
		if v.computer != base.computer {
			write("set_computer_tools_enabled", map[string]any{"enabled": v.computer})
		}
		if v.automations != base.automations || v.workTasks != base.workTasks {
			write("set_chat_authoring_settings", map[string]any{"settings": map[string]any{
				"automations_enabled": v.automations, "work_tasks_enabled": v.workTasks,
			}})
		}
		s.mu.Lock()
		if len(failures) == 0 {
			s.toolsReseed = true
		}
		for _, k := range agentToolKeys {
			delete(s.cache, k)
		}
		s.mu.Unlock()
		if len(failures) > 0 {
			s.notifyErr(locales.In(loc).SetSaveFailed(strings.Join(failures, "; ")))
		} else {
			s.notify(locales.In(loc).SetSaved())
		}
		svc.Invalidate()
	}()
}

// parseNonNegativeInt parses a cache-budget field; negative input is clamped.
func parseNonNegativeInt(v string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return 0, err
	}
	if n < 0 {
		n = 0
	}
	return n, nil
}
