package app

// settings_browser.go mirrors upstream src/app/settings/browser
// (components/settings/browser-settings): the browser-tools toggles (DB-backed
// via get/set_browser_tools_settings) plus the loopback bridge status
// (browser_bridge_status). A compact subset of the upstream panel.

import (
	"github.com/egoist/mygo/ui"
	"gocode/locales"
)

func (s *Shell) viewSettingsBrowser(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	settings := asObj(s.ensure("browser-tools", "get_browser_tools_settings", nil))
	status := asObj(s.ensure("browser-bridge", "browser_bridge_status", nil))

	ui.Row(c.Key("br-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.NavBrowser()).FontSize(22).Bold()
		ui.Spacer(c)
		iconButton(c, "br-refresh", iconRefresh, m.ActionRefresh(), func() {
			s.invalidate("browser-tools")
			s.invalidate("browser-bridge")
		})
	})
	if s.errFor("browser-tools") != "" {
		s.errorState(c, "browser-tools", nil)
		return
	}
	ui.Text(c, m.SetBrowserTools()).FontSize(13).Bold()
	ui.Text(c, m.SetBrowserToolsHint()).FontSize(11).TextColor(t.TextMuted)
	if settings == nil {
		loadingState(c, "")
		return
	}
	enabled := truthy(settings["enabled"])
	evalOn := truthy(settings["eval"])
	s.browserToggleRow(c, "br-enabled", m.SetEnabledRow(), enabled, func() { s.setBrowserTools(c, !enabled, evalOn) })
	s.browserToggleRow(c, "br-eval", m.SetAllowEval(), evalOn, func() { s.setBrowserTools(c, enabled, !evalOn) })

	ui.Divider(c)
	ui.Text(c, m.SetBridge()).FontSize(13).Bold()
	if status == nil {
		loadingState(c, "")
		return
	}
	ui.Row(c).Gap(8).Children(func() {
		ui.Text(c, m.SetStatus()).TextColor(t.TextMuted)
		if truthy(status["enabled"]) {
			ui.Text(c, m.SetRunning()).Bold().TextColor(t.Success)
		} else {
			ui.Text(c, m.SetStopped()).Bold().TextColor(t.TextMuted)
		}
	})
	if ports, ok := status["ports"].([]any); ok && len(ports) > 0 {
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, m.SetPorts()).TextColor(t.TextMuted)
			ui.Textf(c, "%v", ports).Bold()
		})
	}
	if host := str(status, "public_host"); host != "" {
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, m.SetPublicHost()).TextColor(t.TextMuted)
			ui.Text(c, host).Bold()
		})
	}
}

// browserToggleRow renders a labelled on/off row with a toggle button.
func (s *Shell) browserToggleRow(c *ui.Context, key, label string, on bool, toggle func()) {
	t := c.Theme()
	m := cat(c)
	ui.Row(c.Key(key)).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).TextColor(t.TextMuted).Width(140)
		if on {
			ui.Text(c, m.SetOn()).Bold().TextColor(t.Success)
		} else {
			ui.Text(c, m.SetOff()).Bold().TextColor(t.TextMuted)
		}
		if ui.Button(c.Key(key+"-btn"), m.SetToggle()).Clicked() {
			toggle()
		}
	})
}

// setBrowserTools persists the browser-tools settings (set_browser_tools_settings).
func (s *Shell) setBrowserTools(c *ui.Context, enabled, evalOn bool) {
	svc := s.svc
	loc := c.Locale()
	go func() {
		_, err := s.B.raw("set_browser_tools_settings", map[string]any{
			"settings": map[string]any{"enabled": enabled, "eval": evalOn},
		})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = locales.In(loc).SetBrowserFailed(err.Error())
		} else {
			delete(s.cache, "browser-tools")
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}
