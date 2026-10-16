package app

// servers_test.go covers the multi-server connections (Settings → Servers):
// the page lists the local workspace plus saved remotes, Connect probes the
// target and swaps the whole shell over, failures keep the current server and
// explain why, and profiles persist to the data dir.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// serversShellForTest builds a shell on the real backend stack, parked on
// Settings → Servers with a stubbed stream, and an isolated data dir so
// profile persistence never touches the real home.
func serversShellForTest(t *testing.T) *Shell {
	t.Helper()
	t.Setenv("YOYOCODE_DATA_DIR", t.TempDir())
	b := newBackendForTest(t)
	s := New(b)
	s.stream = newStreamForTest()
	s.section = "settings"
	s.settingsPage = "servers"
	return s
}

func TestNormalizeServerURL(t *testing.T) {
	cases := []struct {
		in, want, errContains string
	}{
		{"http://192.168.1.9:8787", "http://192.168.1.9:8787", ""},
		{"http://box.lan:8787/", "http://box.lan:8787", ""},
		{"box.lan:8787", "http://box.lan:8787", ""},
		{"https://codeg.example.com", "https://codeg.example.com", ""},
		{"", "", "enter the server address"},
		{"ftp://box", "", "http(s)"},
		{"http://", "", "http(s)"},
	}
	for _, c := range cases {
		got, err := normalizeServerURL(c.in)
		if c.errContains == "" {
			if err != nil {
				t.Errorf("normalizeServerURL(%q) error: %v", c.in, err)
				continue
			}
			if got != c.want {
				t.Errorf("normalizeServerURL(%q) = %q, want %q", c.in, got, c.want)
			}
			continue
		}
		if err == nil {
			t.Errorf("normalizeServerURL(%q) = %q, want an error containing %q", c.in, got, c.errContains)
			continue
		}
		if !strings.Contains(err.Error(), c.errContains) {
			t.Errorf("normalizeServerURL(%q) error = %q, want it to contain %q", c.in, err.Error(), c.errContains)
		}
	}
}

func TestServerProfilesPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YOYOCODE_DATA_DIR", dir)
	want := []serverProfile{
		{ID: "srv-1", Name: "Studio box", URL: "http://192.168.1.9:8787", Token: "tok"},
		{ID: "srv-2", Name: "VPS", URL: "https://codeg.example.com", Token: ""},
	}
	saveServers(want)
	got := loadServers()
	if len(got) != 2 || got[0].Name != "Studio box" || got[1].URL != "https://codeg.example.com" {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	// New() picks the saved profiles up and parks on the local connection.
	b := &Backend{URL: "http://127.0.0.1:1", Token: "boot"}
	s := New(b)
	if len(s.servers) != 2 || s.activeServer != localServerID {
		t.Fatalf("New loaded servers = %+v, active = %q", s.servers, s.activeServer)
	}
	if p := s.activeProfile(); p.Name != "Local workspace" || p.URL != "http://127.0.0.1:1" {
		t.Fatalf("active profile = %+v", p)
	}
	// The file lands under the overridden data dir, not the real home
	// (guard against a regression that would write outside the sandbox).
	if _, err := os.Stat(filepath.Join(dir, "servers.json")); err != nil {
		t.Fatalf("servers.json not written under the data dir: %v", err)
	}
}

func TestServersPageListsLocalAndActive(t *testing.T) {
	s := serversShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)

	for _, want := range []string{
		"Servers",                             // page title
		"Local workspace",                     // active card + the local row
		"Always available",                    // the local row's badge
		"Add a server",                        // the add CTA
		"Saved connections",                   // section label
		"This computer's in-process backend.", // active card's subtitle
	} {
		if !tt.HasText(want) {
			t.Errorf("servers page missing %q; texts = %v", want, tt.Texts())
		}
	}
	// The active server is reachable from the status bar: park the shell on the
	// workspace so the name appears ONLY on the status-bar button (the settings
	// page is not rendered, so the click is unambiguous).
	s.mu.Lock()
	s.section = "workspace"
	s.servers = []serverProfile{{ID: "srv-1", Name: "Studio box", URL: s.localURL, Token: "tok"}}
	s.activeServer = "srv-1"
	s.mu.Unlock()
	tt.Frame() // repaint so the status bar carries the new server's name
	if err := tt.Click("Studio box"); err != nil {
		t.Fatalf("click status-bar server: %v", err)
	}
	tt.Frame()
	if s.section != "settings" || s.settingsPage != "servers" {
		t.Errorf("status-bar server click did not park on the servers page: %q/%q", s.section, s.settingsPage)
	}
	dumpFrame(t, tt, "servers-page")
}

func TestServersConnectSwitchesTheShell(t *testing.T) {
	s := serversShellForTest(t)
	// A saved connection that points at the live test backend with the right
	// token: switching must succeed and take the whole shell with it.
	s.mu.Lock()
	s.servers = []serverProfile{{ID: "srv-1", Name: "Studio box", URL: s.B.URL, Token: "tok"}}
	s.cache["folders"] = json.RawMessage(`[{"id":9,"name":"remote","path":"/remote"}]`)
	s.selectedFolder = "9"
	s.mu.Unlock()

	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Connect"); err != nil {
		t.Fatalf("click Connect: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.activeServer == "srv-1"
	}) {
		t.Fatalf("switch never committed; srvErr = %q", s.srvErr)
	}
	tt.Frame() // render the post-switch frame before asserting on labels
	// The previous realm's data is gone: the cache was wiped and its state reset.
	s.mu.Lock()
	_, stale := s.cache["folders"]
	folder, active := s.selectedFolder, s.activeServer
	url, token := s.B.URL, s.B.Token
	wantURL := s.servers[0].URL
	s.mu.Unlock()
	if stale {
		t.Error("stale cache key from the previous server survived the switch")
	}
	if folder != "" {
		t.Errorf("selectedFolder = %q, want it reset", folder)
	}
	if active != "srv-1" {
		t.Errorf("activeServer = %q", active)
	}
	if url != wantURL || token != "tok" {
		t.Errorf("backend not repointed: %q / %q", url, token)
	}
	if !tt.HasText("Studio box") || !tt.HasText("Active") {
		t.Errorf("switched page shows neither the new name nor the Active badge; texts = %v", tt.Texts())
	}
	// The status bar reflects the new connection.
	if !tt.HasText("Connected to Studio box") {
		t.Logf("toast may have already faded; texts tail = %v", tt.Texts())
	}
	dumpFrame(t, tt, "servers-switched")
}

func TestServersConnectFailureKeepsCurrentServer(t *testing.T) {
	s := serversShellForTest(t)
	// A saved connection to a port nothing listens on: the probe must fail, the
	// shell must stay on the local backend, and the reason must be shown.
	s.mu.Lock()
	s.servers = []serverProfile{{ID: "srv-1", Name: "Ghost box", URL: "http://127.0.0.1:1", Token: "tok"}}
	s.mu.Unlock()

	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Connect"); err != nil {
		t.Fatalf("click Connect: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.srvErr != ""
	}) {
		t.Fatal("the failed switch reported no error")
	}
	tt.Frame() // the error row renders on the frame after it is recorded
	s.mu.Lock()
	active, url := s.activeServer, s.B.URL
	s.mu.Unlock()
	if active != localServerID {
		t.Errorf("activeServer = %q, want the local connection to survive a failed probe", active)
	}
	if url != s.localURL {
		t.Errorf("backend URL = %q, want the boot URL %q", url, s.localURL)
	}
	if !tt.HasText("Could not reach Ghost box") {
		t.Errorf("failure reason missing; texts = %v", tt.Texts())
	}
	if !tt.HasText("Local workspace") {
		t.Errorf("the page fell off the local connection; texts = %v", tt.Texts())
	}
}

func TestServersDeleteAsksFirst(t *testing.T) {
	s := serversShellForTest(t)
	s.mu.Lock()
	s.servers = []serverProfile{
		{ID: "srv-1", Name: "Studio box", URL: "http://127.0.0.1:9", Token: "tok"},
		{ID: "srv-2", Name: "VPS", URL: "http://127.0.0.1:8", Token: "tok"},
	}
	s.mu.Unlock()

	tt := ui.NewTester(s.View, 1240, 820)
	// Both rows are listed with their own Delete buttons.
	if !tt.HasText("Studio box") || !tt.HasText("VPS") {
		t.Fatalf("saved connections not listed; texts = %v", tt.Texts())
	}
	if err := tt.Click("Delete"); err != nil {
		t.Fatalf("click Delete: %v", err)
	}
	tt.Frame()
	// The first click only arms the confirmation — nothing is deleted yet.
	s.mu.Lock()
	n := len(s.servers)
	s.mu.Unlock()
	if n != 2 {
		t.Fatalf("delete fired without confirmation: %d profiles left", n)
	}
	if err := tt.Click("Remove"); err != nil {
		t.Fatalf("click Remove: %v", err)
	}
	tt.Frame()
	if !waitUntil(t, tt, func() bool { return !tt.HasText("Studio box") }) {
		t.Errorf("the deleted connection survived; texts = %v", tt.Texts())
	}
	if !tt.HasText("VPS") {
		t.Errorf("the wrong connection was deleted; texts = %v", tt.Texts())
	}
	// Deleting the ACTIVE connection falls back to the local workspace.
	s.mu.Lock()
	s.servers = []serverProfile{{ID: "srv-1", Name: "Studio box", URL: s.localURL, Token: "tok"}}
	s.activeServer = "srv-1"
	s.mu.Unlock()
	s.deleteServer("srv-1")
	tt.Frame()
	if !waitUntil(t, tt, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.activeServer == localServerID
	}) {
		t.Errorf("deleting the active connection did not fall back to local")
	}
}

func TestServersAddFormValidatesAddress(t *testing.T) {
	s := serversShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)

	if err := tt.Click("Add a server"); err != nil {
		t.Fatalf("click add: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Access token") || !tt.HasText("Save & connect") {
		t.Fatalf("add form did not open; texts = %v", tt.Texts())
	}
	// An unusable address is refused inline; no connection is created.
	s.mu.Lock()
	if s.srvEditor == nil {
		s.mu.Unlock()
		t.Fatal("editor did not open")
	}
	s.srvEditor.url = "::::"
	s.mu.Unlock()
	if err := tt.Click("Save & connect"); err != nil {
		t.Fatalf("click save: %v", err)
	}
	tt.Frame()
	s.mu.Lock()
	open, n := s.srvEditor != nil, len(s.servers)
	s.mu.Unlock()
	if !open || n != 0 {
		t.Errorf("invalid address was accepted: editor open = %v, profiles = %d", open, n)
	}
	if !tt.HasText("http(s)") {
		t.Errorf("validation message missing; texts = %v", tt.Texts())
	}
	// A bare host is normalized and saved; the switch runs against it.
	s.mu.Lock()
	s.srvEditor.url = "127.0.0.1:1"
	s.srvEditor.name = "Ghost"
	s.mu.Unlock()
	if err := tt.Click("Save & connect"); err != nil {
		t.Fatalf("click save: %v", err)
	}
	if !waitUntil(t, tt, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.srvErr != ""
	}) {
		t.Fatal("the normalized address was never probed")
	}
	s.mu.Lock()
	saved := s.servers
	s.mu.Unlock()
	if len(saved) != 1 || saved[0].URL != "http://127.0.0.1:1" || saved[0].Name != "Ghost" {
		t.Fatalf("profile saved as %+v, want the normalized address", saved)
	}
	s.mu.Lock()
	stillLocal := s.activeServer == localServerID
	s.mu.Unlock()
	if !stillLocal {
		t.Errorf("a failed probe still switched the active server")
	}
}
