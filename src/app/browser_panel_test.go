package app

// browser_panel_test.go covers the Browser workbench panel: the degraded-state
// render (bridge stopped + hint + address bar) and the bridge-open error path.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

// browserShellForTest opens the browser panel with a seeded bridge status.
func browserShellForTest(t *testing.T) *Shell {
	t.Helper()
	b := newBackendForTest(t)
	s := New(b)
	s.stream = newStreamForTest()
	s.workbench = "browser"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["browser-bridge"] = json.RawMessage(`{"enabled":false,"ports":[],"public_host":null,"host_pattern":null}`)
	return s
}

// TestBrowserPanelRenders shows the address bar, the stopped bridge and the
// honest hint about the bridge being disabled.
func TestBrowserPanelRenders(t *testing.T) {
	s := browserShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{
		"Browser",
		"Address",
		"Open in browser",
		"Open via bridge",
		"Bridge",
		"stopped",
		"The port bridge is not enabled in this build",
		"Open Settings → Browser",
	} {
		if !tt.HasText(want) {
			t.Errorf("browser panel missing %q; texts = %v", want, tt.Texts())
		}
	}
}

// TestBrowserOpenBridgeShowsError drives the bridge-open attempt and waits for
// the backend's config_missing message to surface.
func TestBrowserOpenBridgeShowsError(t *testing.T) {
	s := browserShellForTest(t)
	s.browserURL = "http://localhost:3000"
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Open via bridge"); err != nil {
		t.Fatalf("click Open via bridge: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("port bridge is not enabled") }) {
		t.Fatalf("bridge error never surfaced; texts = %v", tt.Texts())
	}
}

// TestBrowserOpenDisabledWithoutURL leaves the address empty and asserts the
// external-open button is present but nothing is reported as opened.
func TestBrowserOpenDisabledWithoutURL(t *testing.T) {
	s := browserShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Open in browser") {
		t.Fatalf("external-open button missing; texts = %v", tt.Texts())
	}
	if s.actionMsg != "" {
		t.Errorf("actionMsg = %q, want empty with no URL", s.actionMsg)
	}
}
