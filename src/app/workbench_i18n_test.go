package app

// workbench_i18n_test.go verifies the workbench panels (Automations, Canvas,
// Repositories, Pet) follow the window language after migrating their chrome
// and toasts to the catalogs.

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestWorkbenchLocalize renders each panel in Chinese and asserts the chrome
// (titles, hints, empty states, actions) is translated.
func TestWorkbenchLocalize(t *testing.T) {
	// Automations: the header hint and the detail pane's actions.
	s, _ := automationsShellForTest(t, true)
	tt := ui.NewTester(s.View, 1240, 820)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	for _, want := range []string{"按计划或事件驱动的 agent 运行。", "立即运行", "上次运行：从未"} {
		if !tt.HasText(want) {
			t.Errorf("zh automations missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "workbench-zh-automations")

	// Canvas: the hint and the search placeholder.
	s2, _, _ := canvasShellForTest(t, "demo")
	tt2 := ui.NewTester(s2.View, 1240, 820)
	tt2.SetLocale("zh-Hans")
	tt2.Frame()
	if !tt2.HasText("由会话、笔记与终端组成的空间画板。") {
		t.Errorf("zh canvas missing hint; texts = %v", tt2.Texts())
	}

	// Repositories: the pick-a-folder notice.
	s3, _ := forgeShellForTest(t)
	s3.selectedFolder = ""
	tt3 := ui.NewTester(s3.View, 1240, 820)
	tt3.SetLocale("zh-Hans")
	tt3.Frame()
	if !tt3.HasText("选择一个文件夹") {
		t.Errorf("zh forge missing folder prompt; texts = %v", tt3.Texts())
	}

	// Pet: the empty state.
	s4 := petShellForTest(t, `{"running_count":0,"waiting_count":0,"error_count":0,"sessions":[]}`)
	tt4 := ui.NewTester(s4.View, 900, 600)
	tt4.SetLocale("zh-Hans")
	tt4.Frame()
	for _, want := range []string{"活动会话", "暂无活动会话"} {
		if !tt4.HasText(want) {
			t.Errorf("zh pet missing %q; texts = %v", want, tt4.Texts())
		}
	}

	// To-dos: the board columns and the detail facts.
	s5, _ := tasksShellForTest(t, `[{"id":1,"folder_id":1,"title":"Add retry","status":"todo","agent_type":"claude_code","config":{},"archived_at":null,"updated_at":"2026-10-15T10:00:00Z","files_changed":0}]`)
	tt5 := ui.NewTester(s5.View, 1240, 820)
	tt5.SetLocale("zh-Hans")
	tt5.Frame()
	for _, want := range []string{"待办", "需关注", "已完成"} {
		if !tt5.HasText(want) {
			t.Errorf("zh to-dos missing column %q; texts = %v", want, tt5.Texts())
		}
	}
	dumpFrame(t, tt5, "workbench-zh-todos")

	// Import sessions: the action bar and the per-folder affordances.
	s6 := importShellForTest(t, importScanFixture)
	tt6 := ui.NewTester(s6.View, 1240, 820)
	tt6.SetLocale("zh-Hans")
	tt6.Frame()
	for _, want := range []string{"导入选中", "全选", "导入整个文件夹"} {
		if !tt6.HasText(want) {
			t.Errorf("zh import page missing %q; texts = %v", want, tt6.Texts())
		}
	}
}
