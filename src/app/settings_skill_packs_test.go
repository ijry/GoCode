package app

// settings_skill_packs_test.go covers the Settings → Skill Packs hub: the
// four native tabs over the shared pack bodies (Experts / Science / Office)
// plus the central-store custom skills, and the shared refresh/open controls.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSettingsSkillPacksHub(t *testing.T) {
	s := settingsShellForTest(t, "skill-packs")
	s.cache["experts-list"] = json.RawMessage(`[{"id":"ex1","name":"Code Reviewer"}]`)
	s.cache["science-list"] = json.RawMessage(`[]`)
	s.cache["officecli"] = json.RawMessage(`{"installed":true,"version":"1.2.3","path":"/usr/bin/officecli","runtime_error":null}`)
	s.cache["custom-skills"] = json.RawMessage(`[{"id":"my-skill","name":"My Skill","description":"Does things"}]`)
	tt := ui.NewTester(s.View, 1240, 820)

	for _, want := range []string{
		"Skill Packs", "Open central folder", "Experts", "Science", "Office Tools", "Custom",
		"Code Reviewer",
	} {
		if !tt.HasText(want) {
			t.Errorf("skill packs hub missing %q; texts = %v", want, tt.Texts())
		}
	}

	// The Custom tab shows the central-store list; the other tabs reuse the
	// standalone bodies (empty science state, office helper status).
	if err := tt.Click("Custom"); err != nil {
		t.Fatalf("click Custom tab: %v", err)
	}
	tt.Frame()
	if !tt.HasText("My Skill") || !tt.HasText("Does things") {
		t.Errorf("custom tab missing skill; texts = %v", tt.Texts())
	}

	s.skillPackTab = 1
	tt.Frame()
	if !tt.HasText("No science packs") {
		t.Errorf("science tab missing empty state; texts = %v", tt.Texts())
	}

	s.skillPackTab = 2
	tt.Frame()
	for _, want := range []string{"installed", "1.2.3"} {
		if !tt.HasText(want) {
			t.Errorf("office tab missing %q; texts = %v", want, tt.Texts())
		}
	}

	s.skillPackTab = 3
	tt.Frame()
	dumpFrame(t, tt, "settings-skill-packs")
}

func TestSettingsSkillPacksLocalize(t *testing.T) {
	s := settingsShellForTest(t, "skill-packs")
	s.cache["experts-list"] = json.RawMessage(`[]`)
	s.cache["science-list"] = json.RawMessage(`[]`)
	s.cache["officecli"] = json.RawMessage(`{"installed":false,"version":null,"path":null,"runtime_error":null}`)
	s.cache["custom-skills"] = json.RawMessage(`[]`)
	tt := ui.NewTester(s.View, 1240, 820)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	for _, want := range []string{"技能包", "打开中央文件夹", "专家", "科研", "Office 工具", "自定义"} {
		if !tt.HasText(want) {
			t.Errorf("zh skill packs hub missing %q; texts = %v", want, tt.Texts())
		}
	}
}
