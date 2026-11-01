package app

// git_remotes.go — the Remotes git page (workbench route "git-remotes"). Lists
// the folder's configured remotes and supports add / set-url / remove / fetch.
//
// Backend contract (handlers/git.go + git_branch_remote.go + git_write.go):
// git_list_remotes → [{name,url}], git_add_remote {path,name,url},
// git_set_remote_url {path,name,url}, git_remove_remote {path,name},
// git_fetch_remote {path,name}.

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"gocode/locales"
)

// gitRemotesKey builds the cache key for a folder's remote list.
func gitRemotesKey(root string) string { return "gitremotes:" + root }

// viewGitRemotesPage renders the remotes manager for root.
func (s *Shell) viewGitRemotesPage(c *ui.Context, root string) {
	remotes := asList(s.ensure(gitRemotesKey(root), "git_list_remotes", map[string]any{"path": root}))

	// Add-remote form.
	ui.Column(c.Key("gr-add")).Gap(6).FillWidth().Children(func() {
		ui.Text(c, cat(c).GitRemoteAdd()).Bold()
		ui.Row(c.Key("gr-add-fields")).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("gr-add-name"), &s.remoteNewName).Placeholder(cat(c).GitRemoteNamePlaceholder()).Width(180)
			ui.TextInput(c.Key("gr-add-url"), &s.remoteNewURL).Placeholder(cat(c).GitRemoteURLPlaceholder()).Grow(1)
			btn := ui.PrimaryButton(c.Key("gr-add-btn"), cat(c).GitRemoteAddAction())
			btn.Disabled(strings.TrimSpace(s.remoteNewName) == "" || strings.TrimSpace(s.remoteNewURL) == "")
			if btn.Clicked() {
				s.gitAddRemote(c, root)
			}
		})
	})

	// List.
	if len(remotes) == 0 {
		emptyState(c, iconInfo, cat(c).GitNoRemotes(), cat(c).GitNoRemotesHelp())
	} else {
		ui.Column(c.Key("gr-list")).Gap(8).FillWidth().Children(func() {
			for _, r := range remotes {
				name := str(r, "name")
				url := str(r, "url")
				if s.remoteEditName == name {
					s.viewRemoteEditRow(c, root, name)
				} else {
					s.viewRemoteRow(c, root, name, url)
				}
			}
		})
	}
	s.viewActionOrError(c)
}

// viewRemoteRow renders one remote: name + URL + Fetch / Set URL / Remove.
func (s *Shell) viewRemoteRow(c *ui.Context, root, name, url string) {
	t := c.Theme()
	ui.Row(c.Key("gr-row-" + name)).Gap(10).AlignItems(ui.Center).PaddingY(6).Children(func() {
		ui.Column(c.Key("gr-meta-" + name)).Gap(2).Grow(1).Children(func() {
			ui.Text(c, name).FontSize(13).Bold()
			ui.Text(c, url).FontSize(12).TextColor(t.TextMuted).Wrap()
		})
		if ui.Button(c.Key("gr-fetch-"+name), cat(c).GitRemoteFetchAction()).Clicked() {
			s.gitFetchRemote(c, root, name)
		}
		if ui.Button(c.Key("gr-edit-"+name), cat(c).GitRemoteSetURLAction()).Clicked() {
			s.remoteEditName = name
			s.remoteEditURL = url
		}
		if s.remoteRemoveConfirm == name {
			ui.Text(c, cat(c).GitRemoteRemoveConfirm(name)).FontSize(12).TextColor(t.Danger)
			if ui.Button(c.Key("gr-confirm-"+name), cat(c).GitRemoteConfirmRemove()).Clicked() {
				s.remoteRemoveConfirm = ""
				s.gitRemoveRemote(c, root, name)
			}
			if ui.Button(c.Key("gr-cancel-"+name), cat(c).GitRemoteCancel()).Clicked() {
				s.remoteRemoveConfirm = ""
			}
		} else {
			iconButton(c, "gr-del-"+name, iconX, cat(c).GitRemoteRemoveAction(), func() {
				s.remoteRemoveConfirm = name
			})
		}
	})
	ui.Divider(c)
}

// viewRemoteEditRow renders the inline set-url form for one remote.
func (s *Shell) viewRemoteEditRow(c *ui.Context, root, name string) {
	ui.Row(c.Key("gr-edit-" + name)).Gap(8).AlignItems(ui.Center).PaddingY(6).Children(func() {
		ui.Text(c, name).FontSize(13).Bold().Width(120)
		ui.TextInput(c.Key("gr-edit-url-"+name), &s.remoteEditURL).Placeholder(cat(c).GitRemoteURLPlaceholder()).Grow(1)
		if ui.Button(c.Key("gr-save-"+name), cat(c).GitRemoteSave()).Clicked() {
			s.gitSetRemoteURL(c, root, name)
		}
		if ui.Button(c.Key("gr-edit-cancel-"+name), cat(c).GitRemoteCancel()).Clicked() {
			s.remoteEditName = ""
			s.remoteEditURL = ""
		}
	})
	ui.Divider(c)
}

// gitAddRemote adds a named remote.
func (s *Shell) gitAddRemote(c *ui.Context, root string) {
	loc := c.Locale()
	name := strings.TrimSpace(s.remoteNewName)
	url := strings.TrimSpace(s.remoteNewURL)
	if name == "" || url == "" {
		s.mu.Lock()
		s.errorMsg = locales.In(loc).GitRemoteRequired()
		s.mu.Unlock()
		return
	}
	s.remoteNewName, s.remoteNewURL = "", ""
	s.gitRemoteMutation(loc, "git_add_remote", map[string]any{"path": root, "name": name, "url": url}, root, "added")
}

// gitSetRemoteURL updates a remote's URL.
func (s *Shell) gitSetRemoteURL(c *ui.Context, root, name string) {
	loc := c.Locale()
	url := strings.TrimSpace(s.remoteEditURL)
	if url == "" {
		s.mu.Lock()
		s.errorMsg = locales.In(loc).GitRemoteRequired()
		s.mu.Unlock()
		return
	}
	s.remoteEditName, s.remoteEditURL = "", ""
	s.gitRemoteMutation(loc, "git_set_remote_url", map[string]any{"path": root, "name": name, "url": url}, root, "updated")
}

// gitRemoveRemote removes a remote.
func (s *Shell) gitRemoveRemote(c *ui.Context, root, name string) {
	loc := c.Locale()
	s.gitRemoteMutation(loc, "git_remove_remote", map[string]any{"path": root, "name": name}, root, "removed")
}

// gitFetchRemote fetches one remote.
func (s *Shell) gitFetchRemote(c *ui.Context, root, name string) {
	loc := c.Locale()
	s.gitRemoteMutation(loc, "git_fetch_remote", map[string]any{"path": root, "name": name}, root, "fetched")
}

// gitRemoteMutation runs a remote mutation off the UI thread and reports the
// outcome, refreshing the remotes list on success.
func (s *Shell) gitRemoteMutation(loc, cmd string, body map[string]any, root, kind string) {
	svc := s.svc
	go func() {
		_, err := s.B.raw(cmd, body)
		s.mu.Lock()
		if err != nil {
			s.errorMsg = err.Error()
		} else {
			s.errorMsg = ""
			switch kind {
			case "added":
				s.actionMsg = locales.In(loc).GitRemoteAdded()
			case "updated":
				s.actionMsg = locales.In(loc).GitRemoteUpdated()
			case "removed":
				s.actionMsg = locales.In(loc).GitRemoteRemoved()
			case "fetched":
				s.actionMsg = locales.In(loc).GitRemoteFetched()
			}
			delete(s.cache, gitRemotesKey(root))
		}
		s.mu.Unlock()
		if err == nil {
			// The remotes and every per-branch push fact described them.
			s.invalidatePrefix("pushinfo:" + root + ":")
			s.invalidatePrefix("pushlog:" + root + ":")
			s.invalidate("gitlog:" + root)
		}
		svc.Invalidate()
	}()
}
