package app

// servers.go — 多远程 server 连接（上游的 RemoteWorkspaceConnection）。
//
// 上游把「一个连接 = 一个 realm」，每个 realm 一个独立窗口（remote-workspace-{id}）。
// 本构建是单窗口：连接列表 + 在同一窗口内切换 —— 切换即「换 backend + 全量失效缓存」，
// 并对在途请求打 epoch 标记，回来的迟到响应直接丢弃（上游注释里的
// "epoch-invalidate in-flight requests" 最小实现）。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"yoyocode/locales"
)

// serverProfile is a saved server connection: where it lives and the token to
// authenticate with. The in-process backend is never stored — it is synthesized
// from the boot Backend as the "local" connection.
type serverProfile struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	URL   string `json:"url"`
	Token string `json:"token"`
}

// localServerID is the synthetic id of the in-process backend connection.
const localServerID = "local"

// serverEditor is the add/edit form state (id "" = creating a new connection).
type serverEditor struct {
	id    string
	name  string
	url   string
	token string
	err   string
}

// serversFile returns the path of the saved-connections file, honoring the
// same YOYOCODE_DATA_DIR override the backend itself uses.
func serversFile() string {
	dir := os.Getenv("YOYOCODE_DATA_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".yoyocode")
	}
	return filepath.Join(dir, "servers.json")
}

// loadServers reads the saved remote connections (nil when none or unreadable).
func loadServers() []serverProfile {
	data, err := os.ReadFile(serversFile())
	if err != nil {
		return nil
	}
	var profiles []serverProfile
	if json.Unmarshal(data, &profiles) != nil {
		return nil
	}
	return profiles
}

// saveServers writes the saved remote connections (best effort: a read-only
// home still gets a working in-memory list for this session).
func saveServers(profiles []serverProfile) {
	if err := os.MkdirAll(filepath.Dir(serversFile()), 0o755); err != nil {
		return
	}
	buf, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(serversFile(), buf, 0o600)
}

// localProfile returns the always-available in-process connection.
func (s *Shell) localProfile() serverProfile {
	return serverProfile{ID: localServerID, Name: "Local workspace", URL: s.localURL, Token: s.localToken}
}

// serverDisplayName says a connection's name in the window's language: the
// local connection's is a catalog message, a saved remote's is what the user
// typed.
func (s *Shell) serverDisplayName(c *ui.Context, p serverProfile) string {
	if p.ID == localServerID {
		return cat(c).ServersLocalName()
	}
	return p.Name
}

// profileByID resolves a connection id to its profile (local included).
func (s *Shell) profileByID(id string) (serverProfile, bool) {
	if id == localServerID {
		return s.localProfile(), true
	}
	for _, p := range s.servers {
		if p.ID == id {
			return p, true
		}
	}
	return serverProfile{}, false
}

// activeProfile returns the connection the shell currently talks through.
func (s *Shell) activeProfile() serverProfile {
	if p, ok := s.profileByID(s.activeServer); ok {
		return p
	}
	return s.localProfile()
}

// switchServer validates a connection and, if it answers /api/health, repoints
// the whole shell at it. Failures keep the current server and explain why.
func (s *Shell) switchServer(id string) {
	s.mu.Lock()
	p, ok := s.profileByID(id)
	if !ok || p.ID == s.activeServer {
		s.mu.Unlock()
		return
	}
	s.srvErr = ""
	s.mu.Unlock()

	svc := s.svc
	go func() {
		ok, status, err := probeHealth(p.URL, p.Token)
		s.mu.Lock()
		switch {
		case err != nil:
			s.srvErr = locales.ServersSwitchFailed(p.Name, err.Error())
		case !ok && status == http.StatusUnauthorized:
			s.srvErr = locales.ServersTokenRejected(p.Name)
		case !ok:
			s.srvErr = fmt.Sprintf("%s answered %d to the health check.", p.Name, status)
		default:
			s.commitServerSwitch(p)
			s.srvEditor = nil
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// commitServerSwitch repoints the shell at p and clears everything scoped to
// the previous server. Caller holds s.mu.
func (s *Shell) commitServerSwitch(p serverProfile) {
	// A new epoch: in-flight fetches for the old backend land and are dropped.
	s.epoch++
	s.cache = map[string]json.RawMessage{}
	s.inflight = map[string]bool{}
	s.errs = map[string]string{}
	s.errHints = map[string]string{}
	// Nothing of the old realm's UI state carries over.
	s.toolOpen = map[string]bool{}
	s.copiedCode = map[string]bool{}
	s.connByConv = map[string]string{}
	s.selectedFolder, s.selectedConv = "", ""
	s.sidebarSearch, s.composer = "", ""
	s.composerAttachments = nil
	s.workbench = ""
	s.section = "settings" // the switch was started from Settings → Servers
	s.settingsPage = "servers"
	s.appearanceMode = "system" // refetched from the new backend
	s.actionMsg = ""
	s.errorMsg = ""
	s.errorHint = ""
	s.autoSel, s.taskSel, s.canvasSel = "", "", ""
	s.autoConfirm, s.taskConfirm, s.canvasConfirm = "", "", ""
	s.qmEditor, s.qmConfirm = nil, ""
	s.srvConfirm = ""
	s.loginErr = ""
	s.activeServer = p.ID
	s.B.URL, s.B.Token = p.URL, p.Token
	if s.stream != nil {
		s.stream.Close()
		s.stream = nil // ensureStream rebuilds against the new backend
	}
	s.notify("Connected to " + p.Name) // queue-only, safe inside s.mu
}

// saveServerEditor validates the add/edit form, persists the connection and
// switches to it. Validation failures keep the form open with an inline error.
func (s *Shell) saveServerEditor() {
	ed := s.srvEditor
	if ed == nil {
		return
	}
	ed.err = ""
	name := strings.TrimSpace(ed.name)
	norm, err := normalizeServerURL(ed.url)
	if err != nil {
		ed.err = err.Error()
		return
	}
	if name == "" {
		if u, perr := url.Parse(norm); perr == nil {
			name = u.Hostname()
		} else {
			name = norm
		}
	}
	creating := ed.id == ""
	var target string
	s.mu.Lock()
	if creating {
		target = fmt.Sprintf("srv-%d", time.Now().UnixNano())
		p := serverProfile{ID: target, Name: name, URL: norm, Token: strings.TrimSpace(ed.token)}
		s.servers = append(s.servers, p)
	} else {
		target = ed.id
		for i := range s.servers {
			if s.servers[i].ID == target {
				s.servers[i].Name, s.servers[i].URL, s.servers[i].Token = name, norm, strings.TrimSpace(ed.token)
			}
		}
	}
	saveServers(s.servers)
	s.srvEditor = nil
	s.mu.Unlock()
	s.switchServer(target)
}

// deleteServer drops a saved connection, falling back to the local workspace
// when the deleted one was active.
func (s *Shell) deleteServer(id string) {
	s.mu.Lock()
	kept := s.servers[:0]
	for _, p := range s.servers {
		if p.ID != id {
			kept = append(kept, p)
		}
	}
	s.servers = kept
	saveServers(s.servers)
	wasActive := s.activeServer == id
	s.srvConfirm = ""
	s.mu.Unlock()
	if wasActive {
		s.switchServer(localServerID)
	}
}

// persistActiveToken stores a token the login gate just validated into the
// active remote profile, so reconnecting later does not ask again.
func (s *Shell) persistActiveToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeServer == localServerID {
		return
	}
	for i := range s.servers {
		if s.servers[i].ID == s.activeServer {
			s.servers[i].Token = token
			saveServers(s.servers)
			return
		}
	}
}

// normalizeServerURL accepts shorthand hosts ("box.lan:8787") and returns a
// full http(s) base URL without a trailing slash.
func normalizeServerURL(raw string) (string, error) {
	u := strings.TrimSpace(raw)
	if u == "" {
		return "", fmt.Errorf("enter the server address")
	}
	if !strings.Contains(u, "://") {
		u = "http://" + u
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("enter a full http(s) address, e.g. http://192.168.1.9:8787")
	}
	return strings.TrimRight(u, "/"), nil
}

// viewSettingsServers renders Settings → Servers: the active connection, the
// saved remotes with connect/edit/delete, and the add form. This is the
// single-window stand-in for upstream's connection manager — no extra windows.
func (s *Shell) viewSettingsServers(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, cat(c).ServersTitle()).FontSize(22).Bold()
	})
	ui.Text(c, cat(c).ServersHelp()).FontSize(12).TextColor(t.TextMuted).Wrap()

	// Active connection.
	active := s.activeProfile()
	ui.Column(c.Key("srv-active")).Gap(2).Padding(10).Margin(8, 0, 0, 0).Background(t.Surface).Radius(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Width(8).Height(8).Radius(4).Background(t.Success)
			ui.Text(c, s.serverDisplayName(c, active)).Bold()
			ui.Spacer(c)
			ui.Text(c, cat(c).ServersActiveBadge()).FontSize(11).TextColor(t.Success)
		})
		ui.Text(c, active.URL).FontSize(12).TextColor(t.TextMuted).MaxLines(1)
		if active.ID == localServerID {
			ui.Text(c, cat(c).ServersLocalNote()).FontSize(11).TextColor(t.TextMuted)
		} else if active.Token == "" {
			ui.Text(c, cat(c).ServersNoTokenWarn()).FontSize(11).TextColor(t.Danger)
		} else {
			ui.Text(c, cat(c).ServersTokenSaved()).FontSize(11).TextColor(t.TextMuted)
		}
	})

	ui.Text(c, cat(c).ServersSavedConnections()).FontSize(13).Bold().Margin(12, 0, 0, 0)

	// The local connection, always present and not removable.
	s.serverRow(c, s.localProfile(), true)

	// Saved remotes.
	for _, p := range s.servers {
		s.serverRow(c, p, false)
	}

	if s.srvErr != "" {
		ui.Row(c.Key("srv-err")).Gap(6).AlignItems(ui.Center).Margin(8, 0, 0, 0).Children(func() {
			ui.Icon(c, iconAlert).Width(13).Height(13).TextColor(t.Danger)
			ui.Text(c, s.srvErr).FontSize(12).TextColor(t.Danger).Wrap()
		})
	}

	// Add / edit form.
	if ed := s.srvEditor; ed != nil {
		ui.Column(c.Key("srv-form")).Gap(8).Padding(12).Margin(8, 0, 0, 0).Background(t.Surface).Radius(8).Children(func() {
			title := cat(c).ServersAddForm()
			if ed.id != "" {
				title = cat(c).ServersEditForm()
			}
			ui.Text(c, title).FontSize(14).Bold()
			ui.Text(c, cat(c).ServersNameLabel()).FontSize(11).TextColor(t.TextMuted)
			ui.TextInput(c.Key("srv-name"), &ed.name).Placeholder(cat(c).ServersNamePlaceholder())
			ui.Text(c, cat(c).ServersAddressLabel()).FontSize(11).TextColor(t.TextMuted)
			ui.TextInput(c.Key("srv-url"), &ed.url).Placeholder(cat(c).ServersAddressPlaceholder())
			ui.Text(c, cat(c).ServersTokenLabel()).FontSize(11).TextColor(t.TextMuted)
			ui.TextInput(c.Key("srv-token"), &ed.token).Placeholder(cat(c).ServersTokenPlaceholder())
			ui.Text(c, cat(c).ServersTokenHelp()).FontSize(11).TextColor(t.TextMuted).Wrap()
			if ed.err != "" {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconAlert).Width(13).Height(13).TextColor(t.Danger)
					ui.Text(c, ed.err).FontSize(12).TextColor(t.Danger).Wrap()
				})
			}
			ui.Row(c).Gap(6).Children(func() {
				if ui.PrimaryButton(c.Key("srv-save"), cat(c).ServersSaveConnect()).Clicked() {
					s.saveServerEditor()
				}
				if ui.Button(c.Key("srv-cancel"), cat(c).ActionCancel()).Clicked() {
					s.mu.Lock()
					s.srvEditor = nil
					s.mu.Unlock()
				}
			})
		})
	} else if ui.Button(c.Key("srv-add"), cat(c).ActionAddServer()).Clicked() {
		s.mu.Lock()
		s.srvEditor = &serverEditor{}
		s.srvErr = ""
		s.mu.Unlock()
	}
}

// serverRow renders one connection: identity, status and its actions. The local
// row is informational only; remote rows offer Connect / Edit / Delete (with an
// inline confirmation, never a silent delete).
func (s *Shell) serverRow(c *ui.Context, p serverProfile, local bool) {
	t := c.Theme()
	selected := p.ID == s.activeServer
	row := ui.Row(c.Key("srv-" + p.ID)).PaddingX(8).PaddingY(5).Gap(8).Radius(6).AlignItems(ui.Center)
	if selected {
		row.Background(t.SurfacePressed)
	}
	row.Children(func() {
		ui.Box(c).Width(8).Height(8).Radius(4).Background(func() ui.Color {
			if selected {
				return t.Success
			}
			return t.TextMuted
		}())
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			ui.Text(c, s.serverDisplayName(c, p)).MaxLines(1)
			ui.Text(c, p.URL).FontSize(10).MaxLines(1).TextColor(t.TextMuted)
		})
		if local {
			ui.Text(c, cat(c).ServersLocalBadge()).FontSize(11).TextColor(t.TextMuted)
			return
		}
		if selected {
			ui.Text(c, cat(c).ServersActiveRowBadge()).FontSize(11).TextColor(t.Success)
		}
		if !selected && ui.Button(c.Key("srv-connect-"+p.ID), cat(c).ActionConnect()).Clicked() {
			s.switchServer(p.ID)
		}
		if ui.Button(c.Key("srv-edit-"+p.ID), cat(c).ActionEdit()).Clicked() {
			s.mu.Lock()
			s.srvEditor = &serverEditor{id: p.ID, name: p.Name, url: p.URL, token: p.Token}
			s.srvErr = ""
			s.mu.Unlock()
		}
		if s.srvConfirm == p.ID {
			ui.Text(c, cat(c).ActionDelete()+"?").FontSize(11).TextColor(t.Danger)
			if ui.Button(c.Key("srv-rm-ok-"+p.ID), cat(c).ActionRemove()).Clicked() {
				s.deleteServer(p.ID)
			}
			if ui.Button(c.Key("srv-rm-no-"+p.ID), cat(c).ActionKeep()).Clicked() {
				s.mu.Lock()
				s.srvConfirm = ""
				s.mu.Unlock()
			}
		} else if ui.Button(c.Key("srv-del-"+p.ID), cat(c).ActionDelete()).Clicked() {
			s.mu.Lock()
			s.srvConfirm = p.ID
			s.mu.Unlock()
		}
	})
}
