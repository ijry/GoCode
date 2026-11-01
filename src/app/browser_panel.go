package app

// browser_panel.go — the Browser workbench panel (route "browser"). Upstream
// embeds a loopback dev server in a webview through its port bridge. This Go
// build does not run the bridge (browser_bridge_status reports enabled:false and
// browser_bridge_open returns config_missing), and mygo has no webview, so the
// panel is an honest degraded surface: an address bar that opens the URL in the
// system browser, a "open via bridge" attempt that surfaces the backend's
// config_missing message, the bridge status, and a shortcut to Settings → Browser.

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"gocode/locales"
)

// viewBrowserPanel renders the browser panel.
func (s *Shell) viewBrowserPanel(c *ui.Context) {
	t := c.Theme()
	status := asObj(s.ensure("browser-bridge", "browser_bridge_status", nil))

	ui.Row(c.Key("brp-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, cat(c).NavBrowser()).FontSize(18).Bold()
		ui.Spacer(c)
		iconButton(c, "brp-refresh", iconRefresh, cat(c).ActionRefresh(), func() {
			s.invalidate("browser-bridge")
		})
	})

	// Address bar.
	ui.Text(c, cat(c).BrowserAddress()).FontSize(12).TextColor(t.TextMuted)
	ui.Row(c.Key("brp-bar")).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.TextInput(c.Key("brp-url"), &s.browserURL).Placeholder(cat(c).BrowserAddressPlaceholder()).Grow(1)
		ext := ui.PrimaryButton(c.Key("brp-open"), cat(c).BrowserOpenExternal())
		ext.Disabled(strings.TrimSpace(s.browserURL) == "")
		if ext.Clicked() {
			s.openBrowserExternal(c)
		}
		if ui.Button(c.Key("brp-bridge"), cat(c).BrowserOpenBridge()).Clicked() {
			s.openBrowserBridge(c)
		}
	})

	ui.Divider(c)

	// Bridge status.
	ui.Text(c, cat(c).BrowserBridge()).Bold()
	if status == nil {
		loadingState(c, "")
	} else {
		enabled := truthy(status["enabled"])
		ui.Row(c.Key("brp-status")).Gap(8).AlignItems(ui.Center).Children(func() {
			if enabled {
				ui.Text(c, cat(c).BrowserRunning()).Bold().TextColor(t.Success)
			} else {
				ui.Text(c, cat(c).BrowserStopped()).Bold().TextColor(t.TextMuted)
			}
		})
		if ports, ok := status["ports"].([]any); ok && len(ports) > 0 {
			// Join the numbers: %v on a slice prints "[3000 3001]".
			list := make([]string, 0, len(ports))
			for _, p := range ports {
				list = append(list, fmt.Sprintf("%v", p))
			}
			ui.Text(c, cat(c).BrowserPorts(strings.Join(list, ", "))).FontSize(12).TextColor(t.TextMuted)
		}
		if host := str(status, "public_host"); host != "" {
			ui.Text(c, cat(c).BrowserPublicHost(host)).FontSize(12).TextColor(t.TextMuted)
		}
		if !enabled {
			ui.Text(c, cat(c).BrowserBridgeOffHint()).FontSize(12).TextColor(t.TextMuted).Wrap()
		}
	}

	// Shortcut to the settings page.
	if ui.Button(c.Key("brp-settings"), cat(c).BrowserOpenSettings()).Clicked() {
		s.section = "settings"
		s.settingsPage = "browser"
	}

	s.viewActionOrError(c)
}

// openBrowserExternal launches the URL in the OS browser (mygo Context.OpenURL).
func (s *Shell) openBrowserExternal(c *ui.Context) {
	url := strings.TrimSpace(s.browserURL)
	if url == "" {
		return
	}
	s.browserURL = url
	c.OpenURL(url)
	s.actionMsg = locales.In(c.Locale()).BrowserOpened()
}

// openBrowserBridge attempts the loopback bridge open; the disabled bridge
// answers config_missing, which surfaces as an error banner with its hint.
func (s *Shell) openBrowserBridge(c *ui.Context) {
	url := strings.TrimSpace(s.browserURL)
	if url == "" {
		return
	}
	loc := c.Locale()
	svc := s.svc
	go func() {
		_, err := s.B.raw("browser_bridge_open", map[string]any{"url": url})
		s.mu.Lock()
		if err != nil {
			s.errorMsg = err.Error()
			s.actionMsg = ""
		} else {
			s.errorMsg = ""
			s.actionMsg = locales.In(loc).BrowserBridgeOpened()
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}
