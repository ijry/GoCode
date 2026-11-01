package app

// clone.go — the Clone repository workbench route ("clone"). Clones a remote
// repo into a target directory (clone_repository) and can then open the clone as
// a workspace folder (add_folder_to_history). Not folder-scoped: it CREATES a
// folder, so it renders its own page rather than living under the git routes.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"gocode/locales"
)

// viewCloneRepository renders the clone form + the post-clone success line.
func (s *Shell) viewCloneRepository(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, cat(c).CloneTitle()).FontSize(18).Bold()

	ui.Column(c.Key("clone-form")).Gap(8).FillWidth().Children(func() {
		ui.Text(c, cat(c).CloneURL()).FontSize(12).TextColor(t.TextMuted)
		ui.TextInput(c.Key("clone-url"), &s.cloneURL).Placeholder("https://github.com/owner/repo.git").FillWidth()

		ui.Text(c, cat(c).CloneTarget()).FontSize(12).TextColor(t.TextMuted)
		ui.Row(c.Key("clone-target-row")).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("clone-target"), &s.cloneTarget).Grow(1)
			if ui.Button(c.Key("clone-choose"), cat(c).CloneChoose()).Clicked() {
				// The Tauri host hook opens the OS folder dialog; the headless
				// build leaves it nil, so Choose is a no-op there.
				if s.hostOpenFiles != nil {
					if picked := s.hostOpenFiles(); len(picked) > 0 {
						s.cloneTarget = picked[0]
					}
				}
			}
		})

		btn := ui.PrimaryButton(c.Key("clone-go"), cat(c).CloneAction())
		btn.Disabled(strings.TrimSpace(s.cloneURL) == "" || strings.TrimSpace(s.cloneTarget) == "")
		if btn.Clicked() {
			s.cloneRepository(c)
		}
	})

	if s.cloneResult != "" {
		ui.Row(c.Key("clone-result")).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, s.cloneResult).FontSize(12).TextColor(t.Success).Wrap()
			if ui.Button(c.Key("clone-open"), cat(c).CloneOpenFolder()).Clicked() {
				s.openClonedFolder()
			}
		})
	}
	s.viewActionOrError(c)
}

// cloneRepository runs git clone off the UI thread and reports the outcome.
func (s *Shell) cloneRepository(c *ui.Context) {
	loc := c.Locale()
	url := strings.TrimSpace(s.cloneURL)
	target := strings.TrimSpace(s.cloneTarget)
	if url == "" || target == "" {
		s.mu.Lock()
		s.errorMsg = locales.In(loc).CloneRequired()
		s.mu.Unlock()
		return
	}
	svc := s.svc
	go func() {
		_, err := s.B.raw("clone_repository", map[string]any{"url": url, "targetDir": target})
		s.mu.Lock()
		s.cloneResult = ""
		if err != nil {
			s.errorMsg = err.Error()
		} else {
			s.errorMsg = ""
			s.actionMsg = ""
			s.cloneResult = locales.In(loc).CloneCreated(target)
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// openClonedFolder adds the cloned target as a workspace folder and selects it.
func (s *Shell) openClonedFolder() {
	target := strings.TrimSpace(s.cloneTarget)
	if target == "" {
		return
	}
	svc := s.svc
	go func() {
		raw, err := s.B.raw("add_folder_to_history", map[string]any{"path": target})
		s.mu.Lock()
		if err != nil {
			s.errorMsg = "open failed: " + err.Error()
		} else {
			var f map[string]any
			_ = json.Unmarshal(raw, &f)
			if id, ok := f["id"]; ok {
				s.selectedFolder = fmt.Sprintf("%v", id)
				s.selectedConv = ""
				s.workbench = ""
			}
			delete(s.cache, "folders")
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}
