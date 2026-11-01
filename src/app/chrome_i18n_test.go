package app

// chrome_i18n_test.go verifies the workspace chrome (aux tabs + files/changes
// panels) follows the window language after migrating its strings to locales.

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestWorkspaceChromeLocalizes renders the aux panel in Chinese and asserts the
// tab labels and the Changes empty state are translated.
func TestWorkspaceChromeLocalizes(t *testing.T) {
	s, _ := gitTestRepo(t, "")
	tt := ui.NewTester(s.View, 1240, 820)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	for _, want := range []string{"会话", "文件", "改动", "提交"} {
		if !tt.HasText(want) {
			t.Errorf("zh chrome missing %q; texts = %v", want, tt.Texts())
		}
	}
	// The Changes tab's empty state is translated too (wait for git_status).
	s.auxTab = "changes"
	if !waitUntil(t, tt, func() bool { return tt.HasText("没有改动") }) {
		t.Errorf("zh changes empty state missing; texts = %v", tt.Texts())
	}
}
