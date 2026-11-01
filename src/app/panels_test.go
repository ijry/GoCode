package app

// panels_test.go covers the two previously-placeholder full-screen workbench
// routes whose backends are now ported: import-sessions (scan + selective import)
// and project-boot (shadcn / HyperFrames scaffold + skill install).

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

// importShellForTest builds a Shell with the import-sessions workbench active and
// a pre-seeded scan result, so the picker renders without touching the network.
func importShellForTest(t *testing.T, scan string) *Shell {
	t.Helper()
	b := newBackendForTest(t)
	s := New(b)
	s.stream = newStreamForTest()
	s.workbench = "import-sessions"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache[importScanKey] = json.RawMessage(scan)
	return s
}

const importScanFixture = `{"folders":[{"path":"/home/u/proj","name":"proj","exists_in_codeg":false,"folder_id":null,"agent_types":["claude-code"],"sessions":[{"external_id":"abc","agent_type":"claude-code","title":"My chat","started_at":"2024-01-01T00:00:00Z","ended_at":null,"message_count":5,"model":"claude-opus","git_branch":"main","status":"new"}]}],"no_folder_count":0,"total_sessions":1,"importable_count":1}`

// TestImportSessionsRenders shows the scan summary, the folder, its session and
// the per-folder + global import affordances.
func TestImportSessionsRenders(t *testing.T) {
	s := importShellForTest(t, importScanFixture)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{
		"Import sessions",
		"proj",            // folder name
		"My chat",         // session title
		"New",             // status badge
		"Import selected", // global action
		"Select all",      // per-folder select
		"Import all in folder",
	} {
		if !tt.HasText(want) {
			t.Errorf("import frame missing %q; texts = %v", want, tt.Texts())
		}
	}
}

// TestImportSessionsEmpty shows the honest empty state when nothing is discoverable.
func TestImportSessionsEmpty(t *testing.T) {
	s := importShellForTest(t, `{"folders":[],"no_folder_count":0,"total_sessions":0,"importable_count":0}`)
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("No importable sessions found.") {
		t.Errorf("empty import state missing; texts = %v", tt.Texts())
	}
}

// TestImportSelectAllSelects toggles every session in a folder when "Select all".
func TestImportSelectAllSelects(t *testing.T) {
	s := importShellForTest(t, importScanFixture)
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Select all"); err != nil {
		t.Fatalf("click Select all: %v", err)
	}
	tt.Frame()
	key := importSelKey("claude-code", "abc")
	if !s.importSel[key] {
		t.Errorf("Select all did not select %q; importSel = %v", key, s.importSel)
	}
}

// TestImportSelectedImports drives the checked import through the real backend
// and waits for the outcome line to land.
func TestImportSelectedImports(t *testing.T) {
	s := importShellForTest(t, importScanFixture)
	tt := ui.NewTester(s.View, 1240, 820)
	s.importSel[importSelKey("claude-code", "abc")] = true
	tt.Frame()
	if err := tt.Click("Import selected"); err != nil {
		t.Fatalf("click Import selected: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("imported") }) {
		t.Fatalf("import result never appeared; texts = %v", tt.Texts())
	}
}

// projectBootShellForTest builds a Shell with the project-boot workbench active and
// a pre-seeded skills-detection result.
func projectBootShellForTest(t *testing.T) *Shell {
	t.Helper()
	b := newBackendForTest(t)
	s := New(b)
	s.stream = newStreamForTest()
	s.workbench = "project-boot"
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache[pbSkillsKey] = json.RawMessage(`[{"agent":"claude-code","installed":false},{"agent":"codex","installed":false}]`)
	return s
}

// TestProjectBootRenders shows the type switch, the shadcn form, and the skills
// section with its agents.
func TestProjectBootRenders(t *testing.T) {
	s := projectBootShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{
		"Project boot",
		"Shadcn project",
		"HyperFrames project",
		"Project name",
		"Template",
		"Package manager",
		"Target directory",
		"Create project",
		"Detect HyperFrames skills",
		"claude-code",
		"Install skills",
	} {
		if !tt.HasText(want) {
			t.Errorf("project-boot frame missing %q; texts = %v", want, tt.Texts())
		}
	}
}

// TestProjectBootSwitchToHyperframes swaps the form to the HyperFrames fields.
func TestProjectBootSwitchToHyperframes(t *testing.T) {
	s := projectBootShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("HyperFrames project"); err != nil {
		t.Fatalf("click HyperFrames: %v", err)
	}
	tt.Frame()
	for _, want := range []string{"Example", "Resolution"} {
		if !tt.HasText(want) {
			t.Errorf("hyperframes form missing %q; texts = %v", want, tt.Texts())
		}
	}
}

// TestProjectBootCreateRequiresTemplate drives Create with a name but no template
// and asserts the backend's validation surfaces as an error banner.
func TestProjectBootCreateRequiresTemplate(t *testing.T) {
	s := projectBootShellForTest(t)
	s.pbName = "demo"
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Create project"); err != nil {
		t.Fatalf("click Create: %v", err)
	}
	if !waitUntil(t, tt, func() bool { return tt.HasText("Template is required") }) {
		t.Fatalf("create error never surfaced; texts = %v", tt.Texts())
	}
}
