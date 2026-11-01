package app

// settings_quick_messages.go is the Settings → Quick Messages page (UI_GAPS
// P1-8 depth): reusable composer snippets, with full CRUD, inline form
// validation, status feedback and help text — the backend
// (quick_messages_create/update/delete) does no validation of its own, so the
// page is where a blank title / blank content is caught.
//
// Wire contract (src-go/web/handlers/quick_messages.go):
//   quick_messages_list               → [{id,title,content,sort_order,…}]
//   quick_messages_create {title,content} → record
//   quick_messages_update {id,title?,content?} → record
//   quick_messages_delete {id}        → empty
//   quick_messages_reorder {ids:[…]}

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"gocode/locales"
)

// quickMsgEditor holds the create/edit form's working state. id == "" = create.
type quickMsgEditor struct {
	id      string
	title   string
	content string
}

// viewSettingsQuickMessages renders the Quick Messages page.
func (s *Shell) viewSettingsQuickMessages(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	msgs := asList(s.ensure("quick-messages", "quick_messages_list", nil))

	ui.Row(c.Key("qm-head")).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.NavQuickMessages()).FontSize(22).Bold()
		ui.Spacer(c)
		iconButton(c, "qm-refresh", iconRefresh, m.ActionRefresh(), func() {
			s.invalidate("quick-messages")
		})
		if ui.PrimaryButton(c.Key("qm-new"), m.SetNewMessage()).Clicked() {
			s.startNewQuickMessage()
		}
	})
	ui.Text(c, m.SetQuickMessagesHint()).
		FontSize(12).TextColor(t.TextMuted).Wrap()

	if s.qmEditor != nil {
		s.viewQuickMessageEditor(c)
		return
	}

	if s.errFor("quick-messages") != "" {
		s.errorState(c, "quick-messages", nil)
		return
	}
	if msgs == nil {
		loadingState(c, "")
		return
	}
	if len(msgs) == 0 {
		ui.Column(c.Key("qm-empty")).Fill().Center().Gap(10).Children(func() {
			ui.Text(c, m.SetNoQuickMessages()).FontSize(16).Bold()
			ui.Text(c, m.SetNoQuickMessagesHint()).
				FontSize(12).TextColor(t.TextMuted)
			if ui.PrimaryButton(c.Key("qm-empty-new"), m.SetNewMessage()).Clicked() {
				s.startNewQuickMessage()
			}
		})
		return
	}

	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(6).Children(func() {
			for i, m := range msgs {
				s.quickMessageRow(c, m, itoa("qm", i))
			}
		})
	})
}

// quickMessageRow is one snippet: title + content, an edit control, and a
// delete that goes through an inline confirmation.
func (s *Shell) quickMessageRow(c *ui.Context, m map[string]any, key string) {
	t := c.Theme()
	cat := cat(c)
	id := fmtAny(m["id"])
	ui.Column(c.Key(key)).Gap(3).Padding(10).Background(t.Surface).Radius(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, firstNonEmpty(str(m, "title"), cat.Untitled())).Grow(1).MaxLines(1).TextColor(t.Text)
			if ui.Button(c.Key(key+"-edit"), cat.ActionEdit()).Clicked() {
				s.startEditQuickMessage(m)
			}
			iconButton(c, key+"-del", iconX, cat.SetDeleteNamed(firstNonEmpty(str(m, "title"), cat.SetMessageFallback())), func() {
				s.qmConfirm = id
			})
		})
		if content := str(m, "content"); content != "" {
			ui.Text(c, content).FontSize(12).TextColor(t.TextMuted).MaxLines(3).Wrap()
		}
		if s.qmConfirm == id {
			ui.Row(c.Key(key + "-confirm")).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, cat.SetDeleteThis()).FontSize(12).TextColor(t.Danger)
				if ui.PrimaryButton(c.Key(key+"-confirm-yes"), cat.ActionDelete()).Clicked() {
					s.qmConfirm = ""
					s.deleteQuickMessage(c, id)
				}
				if ui.Button(c.Key(key+"-confirm-no"), cat.ActionCancel()).Clicked() {
					s.qmConfirm = ""
				}
			})
		}
	})
}

// viewQuickMessageEditor renders the create/edit form with inline validation.
func (s *Shell) viewQuickMessageEditor(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	e := s.qmEditor
	if e == nil {
		return
	}
	title := m.SetNewQuickMessage()
	if e.id != "" {
		title = m.SetEditQuickMessage()
	}
	ui.Column(c.Key("qm-form")).Gap(10).Children(func() {
		ui.Text(c, title).FontSize(18).Bold()
		ui.Text(c, m.SetTitle()).FontSize(12).TextColor(t.TextMuted)
		ui.TextInput(c.Key("qm-f-title"), &e.title).Placeholder(m.SetTitlePlaceholder())
		ui.Text(c, m.SetContent()).FontSize(12).TextColor(t.TextMuted)
		ui.TextArea(c.Key("qm-f-content"), &e.content).Placeholder(m.SetContentPlaceholder()).MinHeight(80)
		if s.qmErr != "" {
			ui.Row(c.Key("qm-form-err")).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconAlert).Width(13).Height(13).TextColor(t.Danger)
				ui.Text(c, s.qmErr).FontSize(12).TextColor(t.Danger)
			})
		}
		ui.Row(c).Gap(6).Children(func() {
			if ui.PrimaryButton(c.Key("qm-f-save"), m.ActionSave()).Clicked() {
				s.saveQuickMessage(c)
			}
			if ui.Button(c.Key("qm-f-cancel"), m.ActionCancel()).Clicked() {
				s.qmEditor = nil
				s.qmErr = ""
			}
		})
	})
}

// ── helpers ──

// fmtAny renders a decoded JSON scalar for use as a string key/id.
func fmtAny(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		// ids arrive as JSON numbers; print without a trailing ".0"
		if x == float64(int64(x)) {
			return itoa("", int(x))
		}
	}
	return ""
}

// ── mutations ──

func (s *Shell) startNewQuickMessage() {
	s.qmEditor = &quickMsgEditor{}
	s.qmErr = ""
	s.qmConfirm = ""
}

func (s *Shell) startEditQuickMessage(m map[string]any) {
	s.qmEditor = &quickMsgEditor{
		id:      fmtAny(m["id"]),
		title:   str(m, "title"),
		content: str(m, "content"),
	}
	s.qmErr = ""
	s.qmConfirm = ""
}

// validateQuickMessage is the page's own check — the backend stores whatever it
// is given, so a blank snippet would silently become a dead entry.
func (s *Shell) validateQuickMessage(c *ui.Context) bool {
	e := s.qmEditor
	if e == nil {
		return false
	}
	m := cat(c)
	if strings.TrimSpace(e.title) == "" {
		s.qmErr = m.SetTitleRequired()
		return false
	}
	if strings.TrimSpace(e.content) == "" {
		s.qmErr = m.SetContentRequired()
		return false
	}
	s.qmErr = ""
	return true
}

func (s *Shell) saveQuickMessage(c *ui.Context) {
	if !s.validateQuickMessage(c) {
		return
	}
	e := s.qmEditor
	title := strings.TrimSpace(e.title)
	content := strings.TrimSpace(e.content)
	id := e.id
	svc := s.svc
	loc := c.Locale()
	go func() {
		var err error
		if id == "" {
			_, err = s.B.raw("quick_messages_create", map[string]any{"title": title, "content": content})
		} else {
			_, err = s.B.raw("quick_messages_update", map[string]any{"id": numOrString(id), "title": title, "content": content})
		}
		s.mu.Lock()
		if err != nil {
			s.qmErr = locales.In(loc).SetSaveFailed(err.Error())
		} else {
			s.notify(locales.In(loc).SetSaved())
			s.qmEditor = nil
			s.qmErr = ""
		}
		delete(s.cache, "quick-messages")
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

func (s *Shell) deleteQuickMessage(c *ui.Context, id string) {
	svc := s.svc
	loc := c.Locale()
	go func() {
		_, err := s.B.raw("quick_messages_delete", map[string]any{"id": numOrString(id)})
		s.mu.Lock()
		if err != nil {
			s.notifyErr(locales.In(loc).SetDeleteFailed(err.Error()))
		} else {
			s.notify(locales.In(loc).SetDeleted())
		}
		delete(s.cache, "quick-messages")
		s.mu.Unlock()
		svc.Invalidate()
	}()
}
