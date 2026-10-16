package app

// settings_pages_test.go covers the settings sub-pages that read real backend
// data (browser) and the honest stub pages (science, office-tools). Responses
// are pre-seeded so the views render without touching the network.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func settingsShellForTest(t *testing.T, page string) *Shell {
	t.Helper()
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.section = "settings"
	s.settingsPage = page
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	return s
}

func TestSettingsBrowserPage(t *testing.T) {
	s := settingsShellForTest(t, "browser")
	s.cache["browser-tools"] = json.RawMessage(`{"enabled":true,"eval":false}`)
	s.cache["browser-bridge"] = json.RawMessage(`{"enabled":false,"ports":[],"public_host":null,"host_pattern":null}`)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{"Browser", "Browser tools", "Enabled", "Allow eval", "Bridge", "stopped"} {
		if !tt.HasText(want) {
			t.Errorf("browser page missing %q; texts = %v", want, tt.Texts())
		}
	}
	// The enabled toggle reflects the seeded settings.
	if !tt.HasText("on") || !tt.HasText("off") {
		t.Errorf("toggle states missing; texts = %v", tt.Texts())
	}
}

func TestSettingsSciencePage(t *testing.T) {
	s := settingsShellForTest(t, "science")
	s.cache["science-list"] = json.RawMessage(`[]`)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{"Science", "No science packs"} {
		if !tt.HasText(want) {
			t.Errorf("science page missing %q; texts = %v", want, tt.Texts())
		}
	}
}

func TestSettingsOfficeToolsPage(t *testing.T) {
	s := settingsShellForTest(t, "office-tools")
	s.cache["officecli"] = json.RawMessage(`{"installed":false,"version":null,"path":null,"runtime_error":null}`)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{"Office Tools", "not installed"} {
		if !tt.HasText(want) {
			t.Errorf("office page missing %q; texts = %v", want, tt.Texts())
		}
	}
}

// TestSettingsVersionControlPage exercises the version-control page with a
// detected git and one GitHub account: the Git card shows the version, the
// custom-path field is present, and the forge accounts card groups the account
// under its provider with its scopes and saved-token state.
func TestSettingsVersionControlPage(t *testing.T) {
	s := settingsShellForTest(t, "version-control")
	s.cache["git-detect"] = json.RawMessage(`{"installed":true,"version":"2.45.1","path":"/usr/bin/git"}`)
	s.cache["git-settings"] = json.RawMessage(`{"custom_path":""}`)
	s.cache["gh-accounts"] = json.RawMessage(`{"accounts":[{"id":"acct-1","provider":"github","server_url":"https://github.com","username":"octocat","scopes":["repo"]}]}`)
	s.cache["acct-token:acct-1"] = json.RawMessage(`"ghp_exampletoken"`)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{
		"Git", "git 2.45.1", "Custom git path",
		"Forge accounts", "GitHub", "octocat", "Scopes: repo",
		"Token saved.", "Test", "Delete", "Add account",
	} {
		if !tt.HasText(want) {
			t.Errorf("version-control page missing %q; texts = %v", want, tt.Texts())
		}
	}
}

// TestSettingsVersionControlEmpty covers the two honest states the page must
// render without a backend: no git detected (the alert branch) and no forge
// accounts yet (the empty branch).
func TestSettingsVersionControlEmpty(t *testing.T) {
	s := settingsShellForTest(t, "version-control")
	s.cache["git-detect"] = json.RawMessage(`{"installed":false}`)
	s.cache["git-settings"] = json.RawMessage(`{"custom_path":""}`)
	s.cache["gh-accounts"] = json.RawMessage(`{"accounts":[]}`)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{
		"Git", "No git found", "Custom git path",
		"Forge accounts", "No forge accounts yet.", "Add account",
	} {
		if !tt.HasText(want) {
			t.Errorf("version-control empty page missing %q; texts = %v", want, tt.Texts())
		}
	}
}
