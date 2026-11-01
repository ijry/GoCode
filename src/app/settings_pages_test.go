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

// TestSettingsPagesLocalize renders the migrated settings pages in Chinese and
// asserts the page chrome (titles, empty states, status rows) follows the
// window language.
func TestSettingsPagesLocalize(t *testing.T) {
	// Browser: toggles + bridge status.
	s := settingsShellForTest(t, "browser")
	s.cache["browser-tools"] = json.RawMessage(`{"enabled":true,"eval":false}`)
	s.cache["browser-bridge"] = json.RawMessage(`{"enabled":false,"ports":[],"public_host":null,"host_pattern":null}`)
	tt := ui.NewTester(s.View, 1240, 820)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	for _, want := range []string{"浏览器工具", "已启用", "允许 eval", "桥接", "已停止"} {
		if !tt.HasText(want) {
			t.Errorf("zh browser page missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "settings-zh-browser")

	// Science: honest empty state.
	s2 := settingsShellForTest(t, "science")
	s2.cache["science-list"] = json.RawMessage(`[]`)
	tt2 := ui.NewTester(s2.View, 1240, 820)
	tt2.SetLocale("zh-Hans")
	tt2.Frame()
	if !tt2.HasText("本版本没有科学技能包。") {
		t.Errorf("zh science page missing empty state; texts = %v", tt2.Texts())
	}

	// Office tools: detect status row.
	s3 := settingsShellForTest(t, "office-tools")
	s3.cache["officecli"] = json.RawMessage(`{"installed":false,"version":null,"path":null,"runtime_error":null}`)
	tt3 := ui.NewTester(s3.View, 1240, 820)
	tt3.SetLocale("zh-Hans")
	tt3.Frame()
	if !tt3.HasText("未安装") {
		t.Errorf("zh office page missing status; texts = %v", tt3.Texts())
	}

	// Quick messages: the "no snippets yet" empty state.
	s4 := settingsShellForTest(t, "quick-messages")
	s4.cache["quick-messages"] = json.RawMessage(`[]`)
	tt4 := ui.NewTester(s4.View, 1240, 820)
	tt4.SetLocale("zh-Hans")
	tt4.Frame()
	for _, want := range []string{"暂无快捷消息", "新建消息"} {
		if !tt4.HasText(want) {
			t.Errorf("zh quick-messages page missing %q; texts = %v", want, tt4.Texts())
		}
	}

	// MCP: the scanned-server count and rescan action.
	s5 := settingsShellForTest(t, "mcp")
	s5.cache["mcp-scan"] = json.RawMessage(`{"servers":[{"id":"s1","type":"stdio","command":"npx -y srv"}],"warnings":[]}`)
	tt5 := ui.NewTester(s5.View, 1240, 820)
	tt5.SetLocale("zh-Hans")
	tt5.Frame()
	if !tt5.HasText("1 个本地 MCP 服务器") || !tt5.HasText("重新扫描") {
		t.Errorf("zh mcp page missing chrome; texts = %v", tt5.Texts())
	}

	// System: the info key/value rows.
	s6 := settingsShellForTest(t, "system")
	s6.cache["home-dir"] = json.RawMessage(`"/home/demo"`)
	tt6 := ui.NewTester(s6.View, 1240, 820)
	tt6.SetLocale("zh-Hans")
	tt6.Frame()
	for _, want := range []string{"主目录", "后端", "构建"} {
		if !tt6.HasText(want) {
			t.Errorf("zh system page missing %q; texts = %v", want, tt6.Texts())
		}
	}

	// Skills: the agent/scope pickers over the seeded agent.
	s7 := settingsShellForTest(t, "skills")
	s7.cache["agents"] = json.RawMessage(`[{"agent_type":"claude_code","name":"Claude Code"}]`)
	tt7 := ui.NewTester(s7.View, 1240, 820)
	tt7.SetLocale("zh-Hans")
	tt7.Frame()
	if !tt7.HasText("该 agent 不支持在此管理技能。") && !tt7.HasText("范围内共") {
		if !tt7.HasText("GoCode 在所选 agent 自有技能库中可见的技能。") {
			t.Errorf("zh skills page missing chrome; texts = %v", tt7.Texts())
		}
	}

	// Version control: the git card and the forge accounts card.
	s8 := settingsShellForTest(t, "version-control")
	s8.cache["git-detect"] = json.RawMessage(`{"installed":true,"version":"2.45.1","path":"/usr/bin/git"}`)
	s8.cache["git-settings"] = json.RawMessage(`{"custom_path":""}`)
	s8.cache["gh-accounts"] = json.RawMessage(`{"accounts":[]}`)
	tt8 := ui.NewTester(s8.View, 1240, 820)
	tt8.SetLocale("zh-Hans")
	tt8.Frame()
	for _, want := range []string{"自定义 git 路径", "暂无 forge 账号", "添加账号"} {
		if !tt8.HasText(want) {
			t.Errorf("zh version-control page missing %q; texts = %v", want, tt8.Texts())
		}
	}
	dumpFrame(t, tt8, "settings-zh-version-control")
}
