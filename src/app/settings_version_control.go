package app

// settings_version_control.go — Settings → Version Control, the upstream
// version-control-settings page: the git binary (a custom path, tested before
// it is saved) and the forge accounts whose tokens the Repositories panel
// reads issues and pull requests with.
//
// Upstream groups the accounts by forge (GitHub / GitLab / Gitea — a declared
// provider decides it outright, an undeclared one falls back to the host) and
// lets each be tested, its token rotated in place, or removed. Validating a
// token goes to the forge's own "who am I" endpoint, which is why a bad token
// says so instead of failing later in the Repositories panel.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"gocode/locales"
)

// vcAddForm is the add-an-account form state.
type vcAddForm struct {
	provider string // github | gitlab | gitea
	host     string
	token    string
	err      string
}

// vcProviders are the forges the backend can validate a token for, in the
// order upstream lists them.
var vcProviders = []struct{ id, label, defaultHost string }{
	{"github", "GitHub", "https://github.com"},
	{"gitlab", "GitLab", "https://gitlab.com"},
	{"gitea", "Gitea", ""},
}

// vcProviderOf decides which section an account belongs in, as upstream does:
// a declared provider decides it outright; without one, the host decides, and
// an unknown host means Gitea (which covers Forgejo).
func vcProviderOf(a map[string]any) string {
	if p := str(a, "provider"); p != "" {
		return p
	}
	host := strings.ToLower(firstNonEmpty(str(a, "server_url"), str(a, "serverUrl"), str(a, "host")))
	switch {
	case strings.Contains(host, "gitlab"):
		return "gitlab"
	case strings.Contains(host, "github"):
		return "github"
	default:
		return "gitea"
	}
}

// vcValidateCmd maps a provider to the command that asks its "who am I"
// endpoint about a token.
func vcValidateCmd(provider string) string {
	switch provider {
	case "gitlab":
		return "validate_gitlab_token"
	case "gitea":
		return "validate_gitea_token"
	default:
		return "validate_github_token"
	}
}

func vcProviderLabel(provider string) string {
	for _, p := range vcProviders {
		if p.id == provider {
			return p.label
		}
	}
	return provider
}

// vcHostOf reads an account's host, whatever field it was stored under.
func vcHostOf(a map[string]any) string {
	return firstNonEmpty(str(a, "server_url"), str(a, "serverUrl"), str(a, "host"))
}

// vcAccountName is how a card titles itself: the label, else the username,
// else the id. The last-resort fallback is a view-time label, so a card that
// reaches for it without any identity falls back to the neutral wording.
func vcAccountName(a map[string]any) string {
	name := firstNonEmpty(str(a, "label"), str(a, "username"), str(a, "id"))
	if name == "" {
		return ""
	}
	return name
}

func (s *Shell) viewSettingsVersionControl(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	ui.Text(c, m.NavVersionControl()).FontSize(22).Bold()
	ui.Text(c, m.VcHint()).
		FontSize(12).TextColor(t.TextMuted).Wrap()

	s.viewGitBinaryCard(c)
	s.viewForgeAccountsCard(c)

	ui.Spacer(c).Grow(1)
}

// viewGitBinaryCard shows the detected git and edits the custom path: a path
// is tested before it is saved, exactly like upstream (test first, save on
// success), and an empty path means "the git found on PATH".
func (s *Shell) viewGitBinaryCard(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	detected := asObj(s.ensure("git-detect", "detect_git", nil))
	settings := asObj(s.ensure("git-settings", "get_git_settings", nil))

	ui.Text(c, m.SetGitTitle()).FontSize(14).Bold().Margin(10, 0, 0, 0)
	ui.Column(c.Key("vc-git")).Gap(8).Padding(12).Background(t.Surface).Radius(8).Children(func() {
		// Detection state.
		if detected == nil {
			ui.Text(c, m.VcDetecting()).FontSize(12).TextColor(t.TextMuted)
		} else if asBool(detected["installed"]) {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Width(8).Height(8).Radius(4).Background(t.Success)
				ui.Text(c, m.VcGitVersion(firstNonEmpty(str(detected, "version"), m.VcVersionUnknown()))).
					FontSize(12).TextColor(t.TextMuted)
				if p := str(detected, "path"); p != "" {
					ui.Text(c, p).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
				}
			})
		} else {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconAlert).Width(13).Height(13).TextColor(t.Danger)
				ui.Text(c, m.VcNoGit()).
					FontSize(12).TextColor(t.Danger).Wrap()
			})
		}

		// Custom path: prefilled from the saved settings once they land.
		if settings != nil && s.gitCustomPath == "" && s.gitTestOK == nil {
			s.gitCustomPath = firstNonEmpty(str(settings, "custom_path"), "")
		}
		ui.Text(c, m.VcCustomGitPath()).FontSize(11).TextColor(t.TextMuted)
		ui.Row(c).Gap(6).Children(func() {
			ui.TextInput(c.Key("vc-git-path"), &s.gitCustomPath).
				Placeholder(m.VcPathPlaceholder()).Grow(1)
			if ui.Button(c.Key("vc-git-test"), m.ActionTest()).Clicked() && strings.TrimSpace(s.gitCustomPath) != "" {
				s.testGitPath(c)
			}
			if ui.PrimaryButton(c.Key("vc-git-save"), m.ActionSave()).Clicked() {
				s.saveGitPath(c)
			}
		})
		ui.Text(c, m.VcCustomPathHint()).FontSize(11).TextColor(t.TextMuted).Wrap()
		if s.gitTestOK != nil {
			if *s.gitTestOK {
				ui.Row(c.Key("vc-git-test-ok")).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconCheck).Width(13).Height(13).TextColor(t.Success)
					ui.Text(c, m.VcPathWorks()).FontSize(12).TextColor(t.Success)
				})
			} else {
				ui.Row(c.Key("vc-git-test-err")).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconAlert).Width(13).Height(13).TextColor(t.Danger)
					ui.Text(c, firstNonEmpty(s.gitTestMsg, m.VcNotValidGit())).
						FontSize(12).TextColor(t.Danger).Wrap()
				})
			}
		}
	})
}

// testGitPath asks the backend to run the typed path's git --version.
func (s *Shell) testGitPath(c *ui.Context) {
	loc := c.Locale()
	path := strings.TrimSpace(s.gitCustomPath)
	if path == "" {
		return
	}
	svc := s.svc
	go func() {
		raw, err := s.B.raw("test_git_path", map[string]any{"path": path})
		s.mu.Lock()
		switch {
		case err != nil:
			ok := false
			s.gitTestOK = &ok
			s.gitTestMsg = err.Error()
		default:
			ok := asBool(asObj(raw)["installed"])
			s.gitTestOK = &ok
			if !ok {
				s.gitTestMsg = locales.In(loc).VcNotValidGit()
			}
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// saveGitPath tests a non-empty path first (upstream's order) and only then
// writes the settings.
func (s *Shell) saveGitPath(c *ui.Context) {
	loc := c.Locale()
	path := strings.TrimSpace(s.gitCustomPath)
	svc := s.svc
	go func() {
		if path != "" {
			raw, err := s.B.raw("test_git_path", map[string]any{"path": path})
			if err != nil {
				s.mu.Lock()
				ok := false
				s.gitTestOK = &ok
				s.gitTestMsg = err.Error()
				s.mu.Unlock()
				svc.Invalidate()
				s.notifyErr(locales.In(loc).VcSaveFailedGit(err.Error()))
				return
			}
			if !asBool(asObj(raw)["installed"]) {
				s.mu.Lock()
				ok := false
				s.gitTestOK = &ok
				s.gitTestMsg = locales.In(loc).VcNotValidGit()
				s.mu.Unlock()
				svc.Invalidate()
				s.notifyErr(locales.In(loc).VcSaveFailedGit(locales.In(loc).VcNotValidGit()))
				return
			}
		}
		_, err := s.B.raw("update_git_settings", map[string]any{
			"settings": map[string]any{"custom_path": path},
		})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = locales.In(loc).SetSaveFailed(err.Error())
		} else {
			s.gitTestOK = nil
			s.gitTestMsg = ""
		}
		s.mu.Unlock()
		s.invalidate("git-settings")
		s.invalidate("git-detect")
		if err != nil {
			s.notifyErr(locales.In(loc).VcSaveFailedGit(err.Error()))
		} else {
			s.notify(locales.In(loc).SetSaved())
		}
		svc.Invalidate()
	}()
}

// viewForgeAccountsCard lists the forge accounts grouped by provider, each
// testable, its token rotatable in place and removable, with an add form.
func (s *Shell) viewForgeAccountsCard(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	accts := asObj(s.ensure("gh-accounts", "get_github_accounts", nil))

	ui.Row(c.Key("vc-accts-head")).Gap(10).AlignItems(ui.Center).Margin(10, 0, 0, 0).Children(func() {
		ui.Text(c, m.VcForgeAccounts()).FontSize(14).Bold()
		ui.Spacer(c)
		if ui.Button(c.Key("vc-accts-refresh"), m.ActionRefresh()).Clicked() {
			s.invalidate("gh-accounts")
		}
		if ui.Button(c.Key("vc-add-open"), m.VcAddAccountBtn()).Clicked() {
			s.mu.Lock()
			s.vcAdd = &vcAddForm{provider: "github"}
			s.mu.Unlock()
		}
	})
	ui.Text(c, m.VcAccountsHint()).FontSize(11).TextColor(t.TextMuted).Wrap()

	var list []map[string]any
	if accts != nil {
		list = asListVal(accts["accounts"])
	}
	if len(list) == 0 {
		ui.Column(c.Key("vc-accts-empty")).Gap(4).Padding(12).Background(t.Surface).Radius(8).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconInfo).Width(14).Height(14).TextColor(t.TextMuted)
				ui.Text(c, m.VcNoAccounts()).FontSize(12).Bold()
			})
			ui.Text(c, m.VcNoAccountsHint()).FontSize(11).TextColor(t.TextMuted).Wrap()
		})
	} else {
		for _, p := range vcProviders {
			group := make([]map[string]any, 0, len(list))
			for _, a := range list {
				if vcProviderOf(a) == p.id {
					group = append(group, a)
				}
			}
			if len(group) == 0 {
				continue
			}
			ui.Text(c, p.label).FontSize(12).Bold().Margin(8, 0, 0, 0).TextColor(t.TextMuted)
			ui.Column(c.Key("vc-grp-" + p.id)).Gap(6).Children(func() {
				for _, a := range group {
					s.viewForgeAccountCard(c, a)
				}
			})
		}
	}

	if s.vcAdd != nil {
		s.viewForgeAddForm(c)
	}
}

// viewForgeAccountCard is one account: identity, its token's state, and the
// Test / Rotate / Remove actions. Removing asks first, inline.
func (s *Shell) viewForgeAccountCard(c *ui.Context, a map[string]any) {
	t := c.Theme()
	m := cat(c)
	id := str(a, "id")
	provider := vcProviderOf(a)
	host := vcHostOf(a)
	token := decodeString(s.ensure("acct-token:"+id, "get_account_token", map[string]any{"accountId": id}))

	ui.Column(c.Key("vc-acct-" + id)).Gap(4).Padding(12).Background(t.Surface).Radius(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Width(8).Height(8).Radius(4).Background(t.TextMuted)
			ui.Text(c, firstNonEmpty(vcAccountName(a), m.VcAccountFallback())).Bold().MaxLines(1)
			ui.Spacer(c)
			ui.Text(c, vcProviderLabel(provider)).FontSize(11).TextColor(t.TextMuted)
		})
		if host != "" {
			ui.Text(c, host).FontSize(12).TextColor(t.TextMuted).MaxLines(1)
		}
		if scs, ok := a["scopes"].([]any); ok && len(scs) > 0 {
			names := make([]string, 0, len(scs))
			for _, sc := range scs {
				switch v := sc.(type) {
				case string:
					names = append(names, v)
				case map[string]any:
					if n := str(v, "scope"); n != "" {
						names = append(names, n)
					}
				}
			}
			if len(names) > 0 {
				ui.Text(c, m.VcScopes(strings.Join(names, ", "))).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
			}
		}
		if token == "" {
			ui.Text(c, m.VcNoToken()).FontSize(11).TextColor(t.Danger)
		} else {
			ui.Text(c, m.VcTokenSaved()).FontSize(11).TextColor(t.TextMuted)
		}

		ui.Row(c).Gap(6).Children(func() {
			if s.vcTesting == id {
				ui.Text(c, m.VcChecking()).FontSize(11).TextColor(t.TextMuted)
			} else if ui.Button(c.Key("vc-test-"+id), m.ActionTest()).Disabled(token == "").Clicked() {
				s.testForgeToken(c, id, provider, host, token)
			}
			if ui.Button(c.Key("vc-rotate-"+id), m.VcRotateToken()).Clicked() {
				s.mu.Lock()
				s.vcRotate = id
				s.vcRotateToken = ""
				s.mu.Unlock()
			}
			if s.vcRemove == id {
				ui.Text(c, m.VcRemovePrompt()).FontSize(11).TextColor(t.Danger)
				if ui.Button(c.Key("vc-rm-ok-"+id), m.ActionRemove()).Clicked() {
					s.removeForgeAccount(c, id)
				}
				if ui.Button(c.Key("vc-rm-no-"+id), m.ActionKeep()).Clicked() {
					s.mu.Lock()
					s.vcRemove = ""
					s.mu.Unlock()
				}
			} else if ui.Button(c.Key("vc-del-"+id), m.ActionDelete()).Clicked() {
				s.mu.Lock()
				s.vcRemove = id
				s.mu.Unlock()
			}
		})

		// Token rotation: an inline form that saves in place, the way
		// upstream replaces a token without appending a second row.
		if s.vcRotate == id {
			ui.Column(c.Key("vc-rotate-" + id)).Gap(6).Padding(10).Background(t.Background).Radius(6).Children(func() {
				ui.Text(c, m.VcNewToken()).FontSize(11).TextColor(t.TextMuted)
				ui.TextInput(c.Key("vc-rotate-input-"+id), &s.vcRotateToken).Grow(1)
				ui.Row(c).Gap(6).Children(func() {
					if ui.PrimaryButton(c.Key("vc-rotate-save-"+id), m.VcSaveToken()).Disabled(s.vcRotateToken == "").Clicked() {
						s.saveForgeToken(c, id, provider, host, s.vcRotateToken, nil)
					}
					if ui.Button(c.Key("vc-rotate-cancel-"+id), m.ActionCancel()).Clicked() {
						s.mu.Lock()
						s.vcRotate = ""
						s.vcRotateToken = ""
						s.mu.Unlock()
					}
				})
			})
		}
	})
}

// viewForgeAddForm adds an account: the provider's host defaults per forge,
// the token is validated before anything is stored, and only a working token
// creates a row (a bad one says why, here, rather than in Repositories).
func (s *Shell) viewForgeAddForm(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	f := s.vcAdd
	ui.Column(c.Key("vc-add")).Gap(8).Padding(12).Margin(8, 0, 0, 0).Background(t.Surface).Radius(8).Children(func() {
		ui.Text(c, m.VcAddAccount()).FontSize(14).Bold()
		ui.Text(c, m.VcProvider()).FontSize(11).TextColor(t.TextMuted)
		ui.Row(c.Key("vc-add-provider")).Gap(6).Children(func() {
			for _, p := range vcProviders {
				active := f.provider == p.id
				var clicked bool
				if active {
					clicked = ui.PrimaryButton(c.Key("vc-add-p-"+p.id), p.label).Clicked()
				} else {
					clicked = ui.Button(c.Key("vc-add-p-"+p.id), p.label).Clicked()
				}
				if clicked {
					f.provider = p.id
				}
			}
		})
		ui.Text(c, m.VcHost()).FontSize(11).TextColor(t.TextMuted)
		ui.TextInput(c.Key("vc-add-host"), &f.host).
			Placeholder(firstNonEmpty(func() string {
				for _, p := range vcProviders {
					if p.id == f.provider {
						return p.defaultHost
					}
				}
				return ""
			}(), m.VcHostPlaceholder())).Grow(1)
		ui.Text(c, m.VcAccessToken()).FontSize(11).TextColor(t.TextMuted)
		ui.TextInput(c.Key("vc-add-token"), &f.token).Grow(1)
		ui.Text(c, m.VcTokenHint()).FontSize(11).TextColor(t.TextMuted).Wrap()
		if f.err != "" {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconAlert).Width(13).Height(13).TextColor(t.Danger)
				ui.Text(c, f.err).FontSize(12).TextColor(t.Danger).Wrap()
			})
		}
		ui.Row(c).Gap(6).Children(func() {
			if ui.PrimaryButton(c.Key("vc-add-save"), m.VcValidateAdd()).Disabled(f.token == "").Clicked() {
				s.addForgeAccount(c)
			}
			if ui.Button(c.Key("vc-add-cancel"), m.ActionCancel()).Clicked() {
				s.mu.Lock()
				s.vcAdd = nil
				s.mu.Unlock()
			}
		})
	})
}

// testForgeToken asks the forge who a token belongs to and reports it inline.
func (s *Shell) testForgeToken(c *ui.Context, id, provider, host, token string) {
	loc := c.Locale()
	if token == "" {
		return
	}
	s.mu.Lock()
	s.vcTesting = id
	s.mu.Unlock()
	svc := s.svc
	go func() {
		raw, err := s.B.raw(vcValidateCmd(provider), map[string]any{"serverUrl": host, "token": token})
		s.mu.Lock()
		s.vcTesting = ""
		switch {
		case err != nil:
			s.actionMsg = locales.In(loc).VcTestFailed(err.Error())
		case asBool(asObj(raw)["success"]):
			who := str(asObj(raw), "username")
			if who != "" {
				s.actionMsg = locales.In(loc).VcTokenWorksAs(who)
			} else {
				s.actionMsg = locales.In(loc).VcTokenWorks()
			}
		default:
			s.actionMsg = locales.In(loc).VcTestFailed(firstNonEmpty(str(asObj(raw), "message"), locales.In(loc).VcForgeRejected()))
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// saveForgeToken stores a token for an account (rotating it in place) after
// the forge has said the token works. onDone, when set, runs after the save —
// the add form uses it to append the new account.
func (s *Shell) saveForgeToken(c *ui.Context, id, provider, host, token string, onDone func(username string)) {
	loc := c.Locale()
	svc := s.svc
	go func() {
		raw, verr := s.B.raw(vcValidateCmd(provider), map[string]any{"serverUrl": host, "token": token})
		if verr != nil {
			s.mu.Lock()
			if s.vcAdd != nil {
				s.vcAdd.err = verr.Error()
			}
			s.actionMsg = locales.In(loc).VcSaveFailed(verr.Error())
			s.mu.Unlock()
			svc.Invalidate()
			return
		}
		if !asBool(asObj(raw)["success"]) {
			msg := firstNonEmpty(str(asObj(raw), "message"), locales.In(loc).VcForgeRejected())
			s.mu.Lock()
			if s.vcAdd != nil {
				s.vcAdd.err = msg
			}
			s.actionMsg = locales.In(loc).VcSaveFailed(msg)
			s.mu.Unlock()
			svc.Invalidate()
			return
		}
		username := str(asObj(raw), "username")
		if _, err := s.B.raw("save_account_token", map[string]any{"accountId": id, "token": token}); err != nil {
			s.mu.Lock()
			s.actionMsg = locales.In(loc).VcSaveFailed(err.Error())
			s.mu.Unlock()
			svc.Invalidate()
			return
		}
		s.mu.Lock()
		s.vcRotate = ""
		s.vcRotateToken = ""
		if s.vcAdd != nil {
			s.vcAdd = nil
		}
		s.actionMsg = locales.In(loc).VcTokenSaved()
		s.mu.Unlock()
		s.invalidate("acct-token:" + id)
		if onDone != nil {
			onDone(username)
		}
		svc.Invalidate()
	}()
}

// addForgeAccount validates the form's token, then appends the account and
// saves its token. A refused token keeps the form open with the reason.
func (s *Shell) addForgeAccount(c *ui.Context) {
	s.mu.Lock()
	f := s.vcAdd
	if f == nil {
		s.mu.Unlock()
		return
	}
	provider, host, token := f.provider, strings.TrimSpace(f.host), strings.TrimSpace(f.token)
	f.err = ""
	if token == "" {
		s.mu.Unlock()
		return
	}
	if host == "" {
		for _, p := range vcProviders {
			if p.id == provider && p.defaultHost != "" {
				host = p.defaultHost
			}
		}
	}
	s.mu.Unlock()

	id := fmt.Sprintf("acct-%d", time.Now().UnixNano())
	s.saveForgeToken(c, id, provider, host, token, func(username string) {
		s.appendForgeAccount(c, id, provider, host, username)
	})
}

// appendForgeAccount adds the new account to the saved settings.
func (s *Shell) appendForgeAccount(c *ui.Context, id, provider, host, username string) {
	loc := c.Locale()
	s.mu.Lock()
	accts := asObj(s.ensure("gh-accounts", "get_github_accounts", nil))
	list := asListVal(accts["accounts"])
	list = append(list, map[string]any{
		"id":         id,
		"provider":   provider,
		"server_url": host,
		"username":   username,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	})
	payload, _ := json.Marshal(map[string]any{"accounts": list})
	s.mu.Unlock()
	if _, err := s.B.raw("update_github_accounts", map[string]any{"settings": json.RawMessage(payload)}); err != nil {
		s.notifyErr(locales.In(loc).VcAccountSavedListFailed(err.Error()))
		return
	}
	s.invalidate("gh-accounts")
	s.notify(locales.In(loc).VcAccountAdded())
}

// removeForgeAccount drops an account: its token first, then its row.
func (s *Shell) removeForgeAccount(c *ui.Context, id string) {
	loc := c.Locale()
	svc := s.svc
	go func() {
		_, _ = s.B.raw("delete_account_token", map[string]any{"accountId": id})
		s.mu.Lock()
		accts := asObj(s.ensure("gh-accounts", "get_github_accounts", nil))
		list := asListVal(accts["accounts"])
		kept := make([]map[string]any, 0, len(list))
		for _, a := range list {
			if str(a, "id") != id {
				kept = append(kept, a)
			}
		}
		payload, _ := json.Marshal(map[string]any{"accounts": kept})
		s.vcRemove = ""
		s.vcRotate = ""
		s.mu.Unlock()
		if _, err := s.B.raw("update_github_accounts", map[string]any{"settings": json.RawMessage(payload)}); err != nil {
			s.notifyErr(locales.In(loc).VcRemoveFailed(err.Error()))
			svc.Invalidate()
			return
		}
		s.invalidate("acct-token:" + id)
		s.invalidate("gh-accounts")
		s.notify(locales.In(loc).VcAccountRemoved())
		svc.Invalidate()
	}()
}
