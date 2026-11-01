package app

// import_sessions_test.go covers the import picker's rendering: it groups the
// discovered sessions by folder, shows every agent that produced one, and toggles
// a session's selection from the row itself.

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// importScanAllAgents is a scan result spanning the agents the backend can now
// enumerate — the listers were added one at a time, so this is what a real machine
// looks like once all of them contribute.
const importScanAllAgents = `{
  "importable_count": 6,
  "folders": [
    {
      "path": "/repos/demo",
      "name": "demo",
      "exists_in_codeg": false,
      "agent_types": ["claude_code", "codex", "cursor"],
      "sessions": [
        {"agent_type": "claude_code", "external_id": "c1", "title": "修复导出按钮",
         "model": "claude-sonnet-4", "git_branch": "main", "message_count": 24,
         "started_at": "2026-10-17T13:50:00Z", "status": "new"},
        {"agent_type": "codex", "external_id": "x1", "title": "重构导入流程",
         "model": "gpt-5-codex", "message_count": 41,
         "started_at": "2026-10-16T09:12:00Z", "status": "new"},
        {"agent_type": "cursor", "external_id": "cur1", "title": "补测试",
         "model": "claude-4.5-sonnet", "git_branch": "feat/tests", "message_count": 12,
         "started_at": "2026-10-15T18:03:00Z", "status": "imported"}
      ]
    },
    {
      "path": "/repos/other",
      "name": "other",
      "exists_in_codeg": true,
      "agent_types": ["deepseek", "code_buddy", "antigravity"],
      "sessions": [
        {"agent_type": "deepseek", "external_id": "d1", "title": "模型与配置问答",
         "model": "deepseek-v4-flash", "message_count": 3,
         "started_at": "2026-10-14T11:20:00Z", "status": "new"},
        {"agent_type": "code_buddy", "external_id": "cb1", "title": "查看项目进度与规划下一步",
         "model": "Deepseek-V4.1-Flash", "message_count": 3145,
         "started_at": "2026-10-12T14:11:00Z", "status": "new"},
        {"agent_type": "antigravity", "external_id": "ag1", "title": "修一下导出按钮",
         "model": "Gemini 3.7 Flash", "message_count": 8,
         "started_at": "2026-10-11T08:00:00Z", "status": "new"}
      ]
    }
  ]
}`

func TestImportPickerListsEveryAgent(t *testing.T) {
	s := importShellForTest(t, importScanAllAgents)
	tt := ui.NewTester(s.View, 1240, 860)
	tt.Frame()

	// Both folder groups, and one row per agent — including the ones whose listers
	// were added last.
	for _, want := range []string{
		"demo", "/repos/demo", "claude_code", "codex", "cursor",
		"other", "deepseek", "code_buddy", "antigravity",
		"修复导出按钮", "重构导入流程", "查看项目进度与规划下一步",
	} {
		if !tt.HasText(want) {
			t.Fatalf("picker missing %q; texts = %v", want, tt.Texts())
		}
	}

	// A row toggles its own selection.
	key := importSelKey("cursor", "cur1")
	if s.importSel[key] {
		t.Fatalf("row started selected")
	}
	if err := tt.Click("补测试"); err != nil {
		t.Fatalf("click row: %v", err)
	}
	tt.Frame()
	if !s.importSel[key] {
		t.Fatalf("clicking the row did not select it")
	}
	dumpFrame(t, tt, "import-picker-agents")
}
