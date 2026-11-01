package app

// forge.go is the Repositories workbench (UI_GAPS P1-7), mirroring upstream
// src/components/forge/forge-page.tsx: it resolves the selected folder's forge
// remote (provider + owner/repo), then — once an account is connected — lists
// issues / pull requests with a state filter, lets you close/reopen an item and
// open a new issue. With no remote, an unsupported host, or no connected
// account it shows the matching setup state instead of a bare placeholder.
//
// Wire contract (src-go/web/handlers/forge.go + forge_rest.go):
//   folder_forge_remote {folderId} → {server_host,owner_repo,remote_url,provider,
//                                    supported} | null  (no auth needed)
//   forge_identity  {folderId} → {username,avatar_url}  (needs an account)
//   forge_list_issues {folderId,query:{tab,state,page,perPage}} → {rows:[{number,
//                                    title,state,labels,author,comments,is_pr,…}],…}
//   forge_create_issue {folderId,draft:{title,body,labels?}} → row
//   forge_set_item_state {folderId,request:{number,kind,action}} → row

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"gocode/locales"
)

// forgeEditor holds the new-issue form.
type forgeEditor struct {
	title string
	body  string
}

// viewForge renders the Repositories workbench.
func (s *Shell) viewForge(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	if s.forgeTab == "" {
		s.forgeTab = "issues"
	}
	if s.forgeState == "" {
		s.forgeState = "open"
	}

	ui.Row(c.Key("fg-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.NavRepositories()).FontSize(22).Bold()
		ui.Spacer(c)
		iconButton(c, "fg-refresh", iconRefresh, m.ActionRefresh(), func() {
			s.invalidateForge()
		})
	})
	ui.Text(c, m.FgHint()).
		FontSize(12).TextColor(t.TextMuted)

	if s.selectedFolder == "" {
		ui.Text(c, m.FgSelectFolder()).FontSize(12).TextColor(t.TextMuted)
		return
	}
	fid := s.selectedFolder

	remoteKey := "forge-remote:" + fid
	rawRemote := s.ensure(remoteKey, "folder_forge_remote", map[string]any{"folderId": numOrString(fid)})
	if rawRemote == nil {
		if msg := s.errFor(remoteKey); msg != "" {
			s.viewForgeNotice(c, m.FgRemoteErr(), msg, "")
			return
		}
		loadingState(c, "")
		return
	}
	remote := asObj(rawRemote)
	if remote == nil {
		// A null body means the folder has no parseable origin remote.
		s.viewForgeNotice(c, m.FgNoRemote(), m.FgNoRemoteBody(), "")
		return
	}

	provider := str(remote, "provider")
	ownerRepo := str(remote, "owner_repo")
	host := str(remote, "server_host")
	remoteURL := str(remote, "remote_url")
	supported := truthy(remote["supported"])

	// Remote summary card.
	ui.Column(c.Key("fg-remote")).Gap(2).Padding(10).Background(t.Surface).Radius(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, firstNonEmpty(ownerRepo, m.FgNoRepository())).Bold().TextColor(t.Text)
			ui.Spacer(c)
			ui.Text(c, providerLabel(provider)).FontSize(11).TextColor(t.TextMuted)
		})
		if line := strings.TrimSpace(firstNonEmpty(host, "") + " " + remoteURL); line != "" {
			ui.Text(c, line).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
		}
	})

	if !supported {
		s.viewForgeNotice(c, m.FgUnrecognized(), m.FgUnrecognizedBody(firstNonEmpty(host, remoteURL)), "")
		return
	}

	// Identity — its failure is the "connect an account" signal.
	identKey := "forge-identity:" + fid
	ident := asObj(s.ensure(identKey, "forge_identity", map[string]any{"folderId": numOrString(fid)}))
	if ident == nil {
		if msg := s.errFor(identKey); msg != "" {
			s.viewForgeConnect(c, provider, host, s.errHintFor(identKey))
			return
		}
		loadingState(c, "")
		return
	}
	ui.Text(c, m.FgSignedIn(firstNonEmpty(str(ident, "username"), m.FgUnknown()))).
		FontSize(12).TextColor(t.TextMuted)

	// Tab (issues | prs) + state filter.
	ui.Row(c.Key("fg-filters")).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Gap(6).Children(func() {
			for _, tab := range []struct{ id, label string }{
				{"issues", m.FgTabIssues()}, {"prs", m.FgTabPRs()},
			} {
				tab := tab
				active := s.forgeTab == tab.id
				color := t.TextMuted
				if active {
					color = t.Text
				}
				pill := ui.Row(c.Key("fg-tab-" + tab.id)).PaddingX(11).PaddingY(4).Radius(12).AlignItems(ui.Center)
				if active {
					pill.Background(t.SurfacePressed)
				}
				pill.Children(func() { ui.Text(c, tab.label).FontSize(12).TextColor(color) })
				if pill.Clicked() {
					s.forgeTab = tab.id
				}
			}
		})
		ui.Spacer(c)
		ui.Select(c.Key("fg-state"), &s.forgeState, []string{"open", "closed", "all"})
		if ui.PrimaryButton(c.Key("fg-new"), m.FgNewIssue()).Clicked() {
			s.forgeEditor = &forgeEditor{}
		}
	})

	if s.forgeEditor != nil {
		s.viewForgeEditor(c, fid)
		return
	}

	issuesKey := "forge-issues:" + fid + ":" + s.forgeTab + ":" + s.forgeState
	page := asObj(s.ensure(issuesKey, "forge_list_issues", map[string]any{
		"folderId": numOrString(fid),
		"query":    map[string]any{"tab": s.forgeTab, "state": s.forgeState},
	}))
	if page == nil {
		if msg := s.errFor(issuesKey); msg != "" {
			s.viewForgeNotice(c, m.FgLoadErr(), msg, s.errHintFor(issuesKey))
			return
		}
		loadingState(c, "")
		return
	}
	rows := asListVal(page["rows"])
	if len(rows) == 0 {
		ui.Text(c, m.FgNoItems()).FontSize(12).TextColor(t.TextMuted)
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(4).Children(func() {
			for _, it := range rows {
				s.forgeIssueRow(c, fid, it)
			}
		})
	})
}

// forgeIssueRow is one issue/PR entry: number + title, state, labels, author and
// comments, with a close/reopen action.
func (s *Shell) forgeIssueRow(c *ui.Context, fid string, it map[string]any) {
	t := c.Theme()
	m := cat(c)
	number := intOf(it["number"])
	state := str(it, "state")
	isPR := truthy(it["is_pr"])
	ui.Column(c.Key(fmt.Sprintf("fg-item-%d", number))).Gap(2).Padding(8).Background(t.Surface).Radius(6).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Textf(c, "#%d", number).FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, firstNonEmpty(str(it, "title"), m.Untitled())).Grow(1).MaxLines(1).TextColor(t.Text)
			if isPR {
				ui.Text(c, "PR").FontSize(10).TextColor(t.Accent)
			}
			forgeStateChip(c, fmt.Sprintf("fg-state-%d", number), state)
		})
		meta := []string{}
		if a := str(it, "author"); a != "" {
			meta = append(meta, m.FgBy(a))
		}
		if n := intOf(it["comments"]); n > 0 {
			meta = append(meta, m.FgComments(n))
		}
		for _, l := range asListVal(it["labels"]) {
			if name := str(l, "name"); name != "" {
				meta = append(meta, name)
			}
		}
		if len(meta) > 0 {
			ui.Text(c, strings.Join(meta, " · ")).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
		}
		ui.Row(c).Gap(6).Children(func() {
			if state == "open" {
				if ui.Button(c.Key(fmt.Sprintf("fg-close-%d", number)), m.ActionClose()).Clicked() {
					s.forgeSetState(c, fid, number, "close")
				}
			} else {
				if ui.Button(c.Key(fmt.Sprintf("fg-reopen-%d", number)), m.FgReopen()).Clicked() {
					s.forgeSetState(c, fid, number, "reopen")
				}
			}
		})
	})
}

// viewForgeEditor renders the new-issue form.
func (s *Shell) viewForgeEditor(c *ui.Context, fid string) {
	t := c.Theme()
	m := cat(c)
	e := s.forgeEditor
	if e == nil {
		return
	}
	ui.Column(c.Key("fg-form")).Gap(10).Children(func() {
		ui.Text(c, m.FgNewIssue()).FontSize(18).Bold()
		ui.Text(c, m.SetTitle()).FontSize(12).TextColor(t.TextMuted)
		ui.TextInput(c.Key("fg-f-title"), &e.title).Placeholder(m.FgTitlePlaceholder())
		ui.Text(c, m.FgBody()).FontSize(12).TextColor(t.TextMuted)
		ui.TextArea(c.Key("fg-f-body"), &e.body).Placeholder(m.FgBodyPlaceholder()).MinHeight(80)
		ui.Row(c).Gap(6).Children(func() {
			if ui.PrimaryButton(c.Key("fg-f-save"), m.FgCreate()).Disabled(strings.TrimSpace(e.title) == "").Clicked() {
				s.forgeCreateIssue(c, fid)
			}
			if ui.Button(c.Key("fg-f-cancel"), m.ActionCancel()).Clicked() {
				s.forgeEditor = nil
			}
		})
	})
}

// viewForgeNotice is a titled, muted notice block.
func (s *Shell) viewForgeNotice(c *ui.Context, title, body, hint string) {
	t := c.Theme()
	ui.Column(c.Key("fg-notice")).Gap(4).Padding(12).Background(t.Surface).Radius(8).Children(func() {
		ui.Text(c, title).FontSize(14).Bold().TextColor(t.Text)
		if body != "" {
			ui.Text(c, body).FontSize(12).TextColor(t.TextMuted).Wrap()
		}
		if hint != "" {
			ui.Text(c, hint).FontSize(12).TextColor(t.TextMuted).Wrap()
		}
	})
}

// viewForgeConnect is the "connect an account" state — the panel's most common
// one on a machine that hasn't signed in to a forge.
func (s *Shell) viewForgeConnect(c *ui.Context, provider, host, hint string) {
	t := c.Theme()
	m := cat(c)
	label := providerLabel(provider)
	ui.Column(c.Key("fg-connect")).Gap(8).Padding(12).Background(t.Surface).Radius(8).Children(func() {
		ui.Text(c, m.FgConnectTitle(label)).FontSize(14).Bold().TextColor(t.Text)
		ui.Text(c, m.FgConnectBody(firstNonEmpty(host, m.FgConnectHost()), label)).FontSize(12).TextColor(t.TextMuted).Wrap()
		if hint == "" {
			hint = m.FgConnectHint()
		}
		ui.Text(c, hint).FontSize(12).TextColor(t.TextMuted).Wrap()
		ui.Row(c).Gap(6).Children(func() {
			if ui.PrimaryButton(c.Key("fg-open-settings"), m.FgOpenSettings()).Clicked() {
				s.section = "settings"
				s.settingsPage = "version-control"
			}
		})
	})
}

// ── helpers ──

func providerLabel(p string) string {
	switch p {
	case "github":
		return "GitHub"
	case "gitlab":
		return "GitLab"
	case "gitea":
		return "Gitea"
	}
	if p == "" {
		return "forge"
	}
	return p
}

// forgeStateChip tints an issue state (open/closed).
func forgeStateChip(c *ui.Context, key, state string) {
	if state == "" {
		return
	}
	t := c.Theme()
	col := t.TextMuted
	if state == "open" {
		col = t.Success
	}
	ui.Row(c.Key(key)).PaddingX(8).PaddingY(2).Radius(10).Background(col.Alpha(0.12)).Children(func() {
		ui.Text(c, strings.ToUpper(state)).FontSize(10).TextColor(col)
	})
}

// invalidateForge drops the forge caches for the selected folder.
func (s *Shell) invalidateForge() {
	if s.selectedFolder == "" {
		return
	}
	fid := s.selectedFolder
	s.invalidate("forge-remote:" + fid)
	s.invalidate("forge-identity:" + fid)
	// Issue lists are keyed per tab+state; drop the current one and let the rest
	// re-fetch on demand.
	s.invalidate("forge-issues:" + fid + ":" + s.forgeTab + ":" + s.forgeState)
}

// ── mutations ──

func (s *Shell) forgeSetState(c *ui.Context, fid string, number int, action string) {
	svc := s.svc
	loc := c.Locale()
	go func() {
		_, err := s.B.raw("forge_set_item_state", map[string]any{
			"folderId": numOrString(fid),
			"request":  map[string]any{"number": number, "kind": "issue", "action": action},
		})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).FgUpdateFailed(err.Error()))
		} else if action == "close" {
			s.notify(locales.In(loc).FgIssueClosed())
		} else {
			s.notify(locales.In(loc).FgIssueReopened())
		}
		delete(s.cache, "forge-issues:"+fid+":"+s.forgeTab+":"+s.forgeState)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

func (s *Shell) forgeCreateIssue(c *ui.Context, fid string) {
	e := s.forgeEditor
	if e == nil {
		return
	}
	title := strings.TrimSpace(e.title)
	body := strings.TrimSpace(e.body)
	svc := s.svc
	loc := c.Locale()
	go func() {
		_, err := s.B.raw("forge_create_issue", map[string]any{
			"folderId": numOrString(fid),
			"draft":    map[string]any{"title": title, "body": body},
		})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).FgCreateFailed(err.Error()))
		} else {
			s.notify(locales.In(loc).FgIssueCreated())
			s.forgeEditor = nil
		}
		delete(s.cache, "forge-issues:"+fid+":"+s.forgeTab+":"+s.forgeState)
		s.mu.Unlock()
		svc.Invalidate()
	}()
}
