package app

// model_providers.go — the Settings → Model providers page. Plain storage CRUD
// over list/create/update/delete_model_provider (upstream keeps provider
// *logic* out of scope; these are the storage routes mcode-app uses).
//
// Wire shape (handlers/model_provider.go): {id, name, api_url, api_key,
// api_key_masked, agent_type, model, created_at, updated_at}.

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"gocode/locales"
)

const mpCacheKey = "model-providers"

// viewSettingsModelProviders renders the provider list + create form.
func (s *Shell) viewSettingsModelProviders(c *ui.Context) {
	t := c.Theme()
	list := asList(s.ensure(mpCacheKey, "list_model_providers", nil))

	ui.Row(c.Key("mp-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, cat(c).MpTitle()).FontSize(22).Bold()
		ui.Spacer(c)
		iconButton(c, "mp-refresh", iconRefresh, cat(c).ActionRefresh(), func() { s.invalidate(mpCacheKey) })
	})
	ui.Text(c, cat(c).MpKeyHint()).FontSize(12).TextColor(t.TextMuted)

	if s.errFor(mpCacheKey) != "" {
		s.errorState(c, mpCacheKey, nil)
		return
	}
	if list == nil {
		loadingState(c, "")
		return
	}

	// Create form.
	ui.Column(c.Key("mp-add")).Gap(6).Padding(10).Background(t.Surface).Radius(8).FillWidth().Children(func() {
		ui.Text(c, cat(c).MpAdd()).Bold()
		ui.Row(c.Key("mp-add-r1")).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("mp-new-name"), &s.mpNewName).Placeholder(cat(c).MpName()).Width(160)
			ui.TextInput(c.Key("mp-new-url"), &s.mpNewURL).Placeholder(cat(c).MpAPIURL()).Grow(1)
		})
		ui.Row(c.Key("mp-add-r2")).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("mp-new-key"), &s.mpNewKey).Placeholder(cat(c).MpAPIKey()).Width(200)
			ui.TextInput(c.Key("mp-new-agent"), &s.mpNewAgent).Placeholder(cat(c).MpAgentType()).Width(140)
			ui.TextInput(c.Key("mp-new-model"), &s.mpNewModel).Placeholder(cat(c).MpModel()).Width(160)
			btn := ui.PrimaryButton(c.Key("mp-create"), cat(c).MpCreate())
			btn.Disabled(strings.TrimSpace(s.mpNewName) == "" || strings.TrimSpace(s.mpNewURL) == "")
			if btn.Clicked() {
				s.createModelProvider(c)
			}
		})
	})

	if len(list) == 0 {
		emptyState(c, iconInfo, cat(c).MpEmpty(), "")
		s.viewActionOrError(c)
		return
	}

	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c.Key("mp-list")).Gap(8).FillWidth().Children(func() {
			for _, p := range list {
				id := fmt.Sprint(intOf(p["id"]))
				if s.mpEditID == id {
					s.viewModelProviderEdit(c, id)
				} else {
					s.viewModelProviderRow(c, id, p)
				}
			}
		})
	})
	s.viewActionOrError(c)
}

// viewModelProviderRow renders one provider with Edit / Delete affordances.
func (s *Shell) viewModelProviderRow(c *ui.Context, id string, p map[string]any) {
	t := c.Theme()
	ui.Column(c.Key("mp-row-" + id)).Gap(2).Padding(10).Background(t.Surface).Radius(8).FillWidth().Children(func() {
		ui.Row(c.Key("mp-row-top-" + id)).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, str(p, "name")).Bold()
			ui.Text(c, str(p, "agent_type")).FontSize(11).TextColor(t.TextMuted)
			if model := str(p, "model"); model != "" {
				ui.Text(c, model).FontSize(11).TextColor(t.TextMuted)
			}
			ui.Spacer(c)
			if ui.Button(c.Key("mp-editbtn-"+id), cat(c).MpEdit()).Clicked() {
				s.mpEditID = id
				s.mpEditName = str(p, "name")
				s.mpEditURL = str(p, "api_url")
				s.mpEditKey = "" // blank = keep the existing key
				s.mpEditAgent = str(p, "agent_type")
				s.mpEditModel = str(p, "model")
			}
			if s.mpDeleteConfirm == id {
				ui.Text(c, cat(c).MpDeletePrompt(firstNonEmpty(str(p, "name"), id))).FontSize(12).TextColor(t.Danger)
				if ui.Button(c.Key("mp-confirm-"+id), cat(c).MpConfirmDelete()).Clicked() {
					s.mpDeleteConfirm = ""
					s.deleteModelProvider(c, id)
				}
				if ui.Button(c.Key("mp-cancel-"+id), cat(c).MpCancel()).Clicked() {
					s.mpDeleteConfirm = ""
				}
			} else {
				iconButton(c, "mp-del-"+id, iconX, cat(c).MpDelete(), func() { s.mpDeleteConfirm = id })
			}
		})
		ui.Text(c, str(p, "api_url")).FontSize(12).TextColor(t.TextMuted).MaxLines(1)
		if masked := str(p, "api_key_masked"); masked != "" {
			ui.Text(c, cat(c).MpAPIKey()+": "+masked).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
		}
	})
}

// viewModelProviderEdit renders the inline edit form for one provider.
func (s *Shell) viewModelProviderEdit(c *ui.Context, id string) {
	ui.Column(c.Key("mp-editbox-" + id)).Gap(6).Padding(10).Background(c.Theme().Surface).Radius(8).FillWidth().Children(func() {
		ui.Row(c.Key("mp-edit-r1-" + id)).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("mp-e-name-"+id), &s.mpEditName).Placeholder(cat(c).MpName()).Width(160)
			ui.TextInput(c.Key("mp-e-url-"+id), &s.mpEditURL).Placeholder(cat(c).MpAPIURL()).Grow(1)
		})
		ui.Row(c.Key("mp-edit-r2-" + id)).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("mp-e-key-"+id), &s.mpEditKey).Placeholder(cat(c).MpAPIKey()).Width(200)
			ui.TextInput(c.Key("mp-e-agent-"+id), &s.mpEditAgent).Placeholder(cat(c).MpAgentType()).Width(140)
			ui.TextInput(c.Key("mp-e-model-"+id), &s.mpEditModel).Placeholder(cat(c).MpModel()).Width(160)
			if ui.Button(c.Key("mp-save-"+id), cat(c).MpSave()).Clicked() {
				s.updateModelProvider(c, id)
			}
			if ui.Button(c.Key("mp-ecancel-"+id), cat(c).MpCancel()).Clicked() {
				s.mpEditID = ""
			}
		})
	})
}

// createModelProvider adds a provider.
func (s *Shell) createModelProvider(c *ui.Context) {
	loc := c.Locale()
	name := strings.TrimSpace(s.mpNewName)
	url := strings.TrimSpace(s.mpNewURL)
	if name == "" || url == "" {
		s.mu.Lock()
		s.errorMsg = locales.In(loc).MpNameURLRequired()
		s.mu.Unlock()
		return
	}
	body := map[string]any{
		"name": name, "apiUrl": url,
		"apiKey":    strings.TrimSpace(s.mpNewKey),
		"agentType": strings.TrimSpace(s.mpNewAgent),
		"model":     strings.TrimSpace(s.mpNewModel),
	}
	s.mpNewName, s.mpNewURL, s.mpNewKey, s.mpNewAgent, s.mpNewModel = "", "", "", "", ""
	s.mpMutation(loc, "create_model_provider", body, "created")
}

// updateModelProvider saves the edit form (blank api key keeps the existing one).
func (s *Shell) updateModelProvider(c *ui.Context, id string) {
	loc := c.Locale()
	body := map[string]any{
		"id":        numOrString(id),
		"name":      strings.TrimSpace(s.mpEditName),
		"apiUrl":    strings.TrimSpace(s.mpEditURL),
		"agentType": strings.TrimSpace(s.mpEditAgent),
		"model":     strings.TrimSpace(s.mpEditModel),
	}
	if k := strings.TrimSpace(s.mpEditKey); k != "" {
		body["apiKey"] = k
	}
	s.mpEditID = ""
	s.mpMutation(loc, "update_model_provider", body, "updated")
}

// deleteModelProvider removes a provider.
func (s *Shell) deleteModelProvider(c *ui.Context, id string) {
	loc := c.Locale()
	s.mpMutation(loc, "delete_model_provider", map[string]any{"id": numOrString(id)}, "deleted")
}

// mpMutation runs a provider mutation off the UI thread and refreshes the list.
func (s *Shell) mpMutation(loc, cmd string, body map[string]any, kind string) {
	svc := s.svc
	go func() {
		_, err := s.B.raw(cmd, body)
		s.mu.Lock()
		if err != nil {
			s.errorMsg = err.Error()
		} else {
			s.errorMsg = ""
			switch kind {
			case "created":
				s.actionMsg = locales.In(loc).MpCreated()
			case "updated":
				s.actionMsg = locales.In(loc).MpUpdated()
			case "deleted":
				s.actionMsg = locales.In(loc).MpDeleted()
			}
			delete(s.cache, mpCacheKey)
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}
