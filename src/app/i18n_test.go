package app

// i18n_test.go covers the catalogs and the language section: the messages say
// themselves in both languages with the plural rules of each, the picker lists
// every language the app is written in (named in itself), and the window
// follows a language change at once — nav, status bar and pages alike.
//
// The window's language is switched with the Tester's own SetLocale, which
// sets the engine's language the view reads (c.Locale) — the one mygo keeps in
// step with mygo.App.SetLocale in a running app. mygo.App.SetLocale itself
// queues its work on the main thread's loop, which a headless test never runs.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"yoyocode/locales"
)

func TestCatalogsSayBothLanguages(t *testing.T) {
	// The app is written in English; before any language is chosen the
	// catalogs say their source language's messages.
	if got := locales.SectionSettings(); got != "Settings" {
		t.Errorf("SectionSettings() = %q, want the source language's", got)
	}
	if got := locales.AppTitle(); got != "YoYoCode" {
		t.Errorf("AppTitle() = %q", got)
	}
	// Plural rules are the language's: English has one/other, Chinese one.
	en := locales.In("en")
	if got := en.SidebarChatsCount(0); got != "No chats" {
		t.Errorf("SidebarChatsCount(0) = %q, want %q", got, "No chats")
	}
	if got := en.SidebarChatsCount(1); got != "1 chat" {
		t.Errorf("SidebarChatsCount(1) = %q, want %q", got, "1 chat")
	}
	if got := en.SidebarChatsCount(3); got != "3 chats" {
		t.Errorf("SidebarChatsCount(3) = %q, want %q", got, "3 chats")
	}
	zh := locales.In("zh-Hans")
	if got := zh.SidebarChatsCount(3); got != "3 个会话" {
		t.Errorf("zh SidebarChatsCount(3) = %q, want %q", got, "3 个会话")
	}
	if got := zh.SectionSettings(); got != "设置" {
		t.Errorf("zh SectionSettings() = %q, want %q", got, "设置")
	}
	// A message with placeholders fills them.
	if got := zh.ServersSwitchFailed("Studio box", "连不上"); got != "无法连接到 Studio box：连不上" {
		t.Errorf("zh ServersSwitchFailed = %q", got)
	}
	// The app ships the languages of its catalogs, its own first.
	langs := mygo.App.Languages()
	if len(langs) < 2 || langs[0] != "en" {
		t.Fatalf("Languages() = %v, want en first then the translations", langs)
	}
	found := false
	for _, l := range langs {
		if l == "zh-Hans" {
			found = true
		}
	}
	if !found {
		t.Errorf("Languages() = %v, want zh-Hans among them", langs)
	}
}

// TestLanguagePickerSwitchesTheWindow drives Settings → General's language
// section: the picker lists the languages named in themselves, and switching
// the window's language re-says the nav, the status bar and the pages at once.
func TestLanguagePickerSwitchesTheWindow(t *testing.T) {
	b := newBackendForTest(t)
	s := New(b)
	s.stream = newStreamForTest()
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.section = "settings"
	s.settingsPage = "general"

	tt := ui.NewTester(s.View, 1240, 820)
	// English first: the picker shows both languages, each named in itself.
	for _, want := range []string{"Language", "English", "简体中文"} {
		if !tt.HasText(want) {
			t.Fatalf("language section missing %q; texts = %v", want, tt.Texts())
		}
	}

	// Switch the window to Chinese — everything re-says itself.
	tt.SetLocale("zh-Hans")
	for _, want := range []string{"语言", "简体中文", "设置", "服务器", "工作台"} {
		if !tt.HasText(want) {
			t.Errorf("after switching to zh-Hans the frame is missing %q; texts = %v", want, tt.Texts())
		}
	}
	// A page opened through the nav follows too: Servers says 已保存的连接,
	// and the local connection's name is the catalog's.
	if err := tt.Click("服务器"); err != nil {
		t.Fatalf("click the Servers nav item: %v", err)
	}
	tt.Frame()
	for _, want := range []string{"已保存的连接", "本地工作区", "添加服务器"} {
		if !tt.HasText(want) {
			t.Errorf("the Servers page did not follow the language; missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "settings-servers-zh")

	// Back to the source language, the way "Follow system language" does: the
	// page on screen says its English messages again, and so does the nav.
	tt.SetLocale("")
	tt.Frame()
	for _, want := range []string{"Saved connections", "Add a server", "Local workspace"} {
		if !tt.HasText(want) {
			t.Errorf("returning to the source language failed; missing %q; texts = %v", want, tt.Texts())
		}
	}
	if err := tt.Click("General"); err != nil {
		t.Fatalf("click the General nav item: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Language") {
		t.Errorf("the nav did not return to the source language; texts = %v", tt.Texts())
	}
}
