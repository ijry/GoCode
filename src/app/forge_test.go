package app

// forge_test.go covers the Repositories workbench (UI_GAPS P1-7): the no-folder
// and no-remote states, the "connect an account" state (which navigates to
// Settings → Version Control), and — with the forge caches seeded — the
// connected issue list plus the new-issue form.

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

// forgeShellForTest opens a folder (no git remote on a temp dir) and returns a
// Shell on the Repositories route.
func forgeShellForTest(t *testing.T) (*Shell, string) {
	t.Helper()
	b := newBackendForTest(t)
	fraw, err := b.raw("add_folder_to_history", map[string]any{"path": t.TempDir()})
	if err != nil {
		t.Fatalf("add_folder: %v", err)
	}
	var folder map[string]any
	_ = json.Unmarshal(fraw, &folder)
	fid := fmt.Sprintf("%v", folder["id"])

	s := New(b)
	s.stream = newStreamForTest()
	s.workbench = "forge"
	s.selectedFolder = fid
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	return s, fid
}

// supportedRemote is a seeded folder_forge_remote response for github.
const supportedRemote = `{"server_host":"github.com","owner_repo":"octo/repo","remote_url":"https://github.com/octo/repo.git","provider":"github","supported":true}`

// TestForgeNoFolder shows the pick-a-folder hint.
func TestForgeNoFolder(t *testing.T) {
	s, _ := forgeShellForTest(t)
	s.selectedFolder = ""
	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText("Repositories") || !tt.HasText("Select a folder") {
		t.Fatalf("no-folder state wrong; texts = %v", tt.Texts())
	}
}

// TestForgeNoRemote shows the no-remote notice for a folder without a git origin.
func TestForgeNoRemote(t *testing.T) {
	s, _ := forgeShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	if !waitText(t, tt, "No forge remote", true) {
		t.Fatalf("no-remote notice missing; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "forge-no-remote")
}

// TestForgeConnectState seeds a supported remote, lets the account-less identity
// fetch fail, and checks the connect state + the Settings navigation.
func TestForgeConnectState(t *testing.T) {
	s, fid := forgeShellForTest(t)
	s.cache["forge-remote:"+fid] = json.RawMessage(supportedRemote)
	tt := ui.NewTester(s.View, 1240, 820)
	// The identity fetch has no account → the connect state appears.
	if !waitText(t, tt, "Connect a GitHub account", true) {
		t.Fatalf("connect state missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("octo/repo") {
		t.Errorf("remote summary missing; texts = %v", tt.Texts())
	}
	if err := tt.Click("Open Version Control"); err != nil {
		t.Fatalf("click Open Version Control: %v", err)
	}
	tt.Frame()
	if s.section != "settings" || s.settingsPage != "version-control" {
		t.Fatalf("navigation = %s/%s, want settings/version-control", s.section, s.settingsPage)
	}
	dumpFrame(t, tt, "forge-connect")
}

// TestForgeConnectedList seeds remote + identity + issues and checks the list,
// then opens the new-issue form.
func TestForgeConnectedList(t *testing.T) {
	s, fid := forgeShellForTest(t)
	s.cache["forge-remote:"+fid] = json.RawMessage(supportedRemote)
	s.cache["forge-identity:"+fid] = json.RawMessage(`{"username":"octocat","avatar_url":null}`)
	s.cache["forge-issues:"+fid+":issues:open"] = json.RawMessage(`{"rows":[
		{"number":7,"title":"Fix the widget","state":"open","labels":[{"name":"bug","color":"ff0000"}],"author":"octocat","comments":2,"is_pr":false},
		{"number":8,"title":"Add docs","state":"open","labels":[],"author":"hubot","comments":0,"is_pr":false}
	],"page":1,"per_page":20,"has_next":false}`)

	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{"Signed in as octocat", "Issues", "Pull requests", "#7", "Fix the widget", "by octocat", "2 comment(s)", "bug", "Close"} {
		if !tt.HasText(want) {
			t.Fatalf("issue list missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "forge-issues")
	// New issue opens the form.
	if err := tt.Click("New issue"); err != nil {
		t.Fatalf("click New issue: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Create") || !tt.HasText("Body") {
		t.Fatalf("new-issue form not shown; texts = %v", tt.Texts())
	}
}
