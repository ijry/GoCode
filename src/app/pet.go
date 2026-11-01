package app

// Pet hosts the two upstream pet routes as one section: the live active-session
// panel (upstream src/app/pet-panel, a separate window there) and the desktop
// sprite (upstream src/app/pet, also a window of its own).
//
// The sprite tab is the manager: which pet is active, its size, opening the
// floating sprite window (pet_window.go), adding a pet from a spritesheet the
// user picks, importing the user's own Codex pets, and deleting one. The
// marketplace is not fetched (see handlers/pet_codex.go).

import (
	"encoding/base64"
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gocode/locales"
)

func (s *Shell) viewPet(c *ui.Context) {
	m := cat(c)
	if s.petTab == "" {
		s.petTab = petTabSessions
	}
	ui.Column(c).Fill().Padding(20).Gap(12).Children(func() {
		ui.Row(c.Key("pet-tabs")).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, m.SectionPet()).FontSize(22).Bold()
			ui.Spacer(c)
			for _, tab := range []struct{ id, label string }{
				{petTabSessions, m.PpActiveSessions()},
				{petTabSprite, m.PpSprite()},
			} {
				active := s.petTab == tab.id
				var clicked bool
				if active {
					clicked = ui.PrimaryButton(c.Key("pet-tab-"+tab.id), tab.label).Clicked()
				} else {
					clicked = ui.Button(c.Key("pet-tab-"+tab.id), tab.label).Clicked()
				}
				if clicked {
					s.petTab = tab.id
				}
			}
		})
		if s.petTab == petTabSprite {
			s.viewPetSprite(c)
			return
		}
		s.viewPetPanel(c)
	})
}

// viewPetSprite is the sprite manager page.
func (s *Shell) viewPetSprite(c *ui.Context) {
	t := c.Theme()
	m := cat(c)
	loc := c.Locale()

	pets := asList(s.ensure("pets", "pet_list", nil))
	cfg := asObj(s.ensure("pet-config", "pet_get_settings", nil))
	activeID := str(cfg, "activePetId")
	cfgScale := petDefaultScale
	if v, ok := cfg["scale"].(float64); ok && v > 0 {
		cfgScale = v
	}
	s.seedPetScale(cfgScale)

	ui.Column(c.Key("ps-page")).Fill().Gap(12).Children(func() {
		ui.Text(c, m.PsTitle()).FontSize(18).Bold()
		ui.Text(c, m.PsIntro()).FontSize(12).TextColor(t.TextMuted).Wrap()

		ui.Row(c.Key("ps-actions")).Gap(8).AlignItems(ui.Center).Children(func() {
			open := s.petWindowOpen()
			label := m.PsOpenWindow()
			if open {
				label = m.PsCloseWindow()
			}
			var clicked bool
			if open {
				clicked = ui.PrimaryButton(c.Key("ps-window"), label).Clicked()
			} else {
				clicked = ui.Button(c.Key("ps-window"), label).Clicked()
			}
			if clicked {
				if open {
					s.closePetWindow()
				} else if activeID == "" {
					s.notifyErr(m.PsNoActivePet())
				} else {
					s.openPetWindow(loc)
				}
			}
			if len(pets) > 0 {
				if ui.Button(c.Key("ps-add"), m.PsAdd()).Clicked() {
					s.petAdd(loc)
				}
				if ui.Button(c.Key("ps-import"), m.PsImportCodex()).Clicked() {
					s.petImportCodex(loc)
				}
			}
			ui.Spacer(c)
			ui.Text(c, m.PsSize()).FontSize(12).TextColor(t.TextMuted)
			ui.Slider(c.Key("ps-scale"), &s.petScale, petMinScale, petMaxScale).Width(160)
			ui.Textf(c, "%.2f×", s.petScale).FontSize(12).TextColor(t.TextMuted)
		})
		s.petScaleChanged(loc)

		if s.errFor("pets") != "" {
			s.errorState(c, "pets", nil)
			return
		}
		if pets == nil {
			loadingState(c, "")
			return
		}
		if len(pets) == 0 {
			s.viewPetEmpty(c, loc)
			return
		}
		s.viewPetPreview(c, activeID)

		ui.Column(c.Key("ps-list")).Fill().Gap(6).Children(func() {
			for _, p := range pets {
				s.petRow(c, p, activeID, loc)
			}
		})
	})
}

// viewPetEmpty is the no-pets-yet state: the hint plus the two ways to get one.
func (s *Shell) viewPetEmpty(c *ui.Context, loc string) {
	t := c.Theme()
	m := cat(c)
	ui.Column(c.Key("ps-empty")).Fill().Center().Gap(8).Children(func() {
		ui.Text(c, m.PsNoPets()).FontSize(15).Bold()
		ui.Text(c, m.PsNoPetsHint()).FontSize(12).TextColor(t.TextMuted).Wrap()
		ui.Row(c.Key("ps-empty-actions")).Gap(8).Children(func() {
			if ui.PrimaryButton(c.Key("ps-add-empty"), m.PsAdd()).Clicked() {
				s.petAdd(loc)
			}
			if ui.Button(c.Key("ps-import-empty"), m.PsImportCodex()).Clicked() {
				s.petImportCodex(loc)
			}
		})
	})
}

// viewPetPreview draws the active pet's first idle frame, decoded on demand and
// kept until the active pet changes.
func (s *Shell) viewPetPreview(c *ui.Context, activeID string) {
	if activeID == "" {
		return
	}
	s.mu.Lock()
	img := s.petPreviewImg
	id := s.petPreviewID
	sheet := s.petPreview
	s.mu.Unlock()
	if id != activeID {
		s.loadPetPreview(activeID)
		return
	}
	if sheet == nil && img != nil {
		// Cutting a sheet makes bitmaps, which belong to the main thread — this
		// view is the main thread.
		sheet = slicePetSheet(img)
		s.mu.Lock()
		s.petPreview = sheet
		s.mu.Unlock()
	}
	if sheet == nil {
		return
	}
	frame := sheet.frameFor("idle", 0)
	if frame == nil {
		return
	}
	ui.Row(c.Key("ps-preview")).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Image(c.Key("ps-preview-img"), frame).Size(96, 104)
	})
}

// seedPetScale adopts the persisted scale once, so the slider starts where the
// window is.
func (s *Shell) seedPetScale(v float64) {
	s.mu.Lock()
	if s.petScale == 0 {
		s.petScale = v
		s.petScaleSaved = v
	}
	s.mu.Unlock()
}

// petScaleChanged persists a slider move (debounced) and resizes an open window.
func (s *Shell) petScaleChanged(loc string) {
	s.mu.Lock()
	v := s.petScale
	if v == s.petScaleSaved || v <= 0 {
		s.mu.Unlock()
		return
	}
	if s.petScaleTimer != nil {
		s.petScaleTimer.Stop()
	}
	s.petScaleTimer = time.AfterFunc(500*time.Millisecond, func() { s.savePetScale(loc, v) })
	s.mu.Unlock()
}

// savePetScale writes the size, then tells an open sprite window to follow.
func (s *Shell) savePetScale(loc string, v float64) {
	svc := s.svc
	_, err := s.B.raw("pet_save_window_state", map[string]any{"scale": v})
	s.mu.Lock()
	if err == nil {
		s.petScaleSaved = v
		delete(s.cache, "pet-config")
	}
	s.mu.Unlock()
	if err != nil {
		s.notifyErr(locales.In(loc).PsLoadFailed(err.Error()))
	}
	if w := s.petWindow(); w != nil {
		w.setScale(v)
	}
	svc.Invalidate()
}

// petWindow returns the open sprite window, if any.
func (s *Shell) petWindow() *petWindow {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.petWin
}

// petRow is one installed pet: name, id, and the use / delete actions.
func (s *Shell) petRow(c *ui.Context, p map[string]any, activeID, loc string) {
	t := c.Theme()
	m := cat(c)
	id := str(p, "id")
	name := firstNonEmpty(str(p, "displayName"), id)
	desc := str(p, "description")

	ui.Row(c.Key("ps-row-" + id)).FillWidth().Padding(10).Gap(10).AlignItems(ui.Center).
		Background(t.Surface).Radius(8).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, name).FontSize(13).Bold()
			line := id
			if desc != "" {
				line = id + " · " + desc
			}
			ui.Text(c, line).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
		})
		if id == activeID {
			ui.Badge(c.Key("ps-active-"+id), m.PsActivePet()).Background(t.Accent).TextColor(t.AccentText)
		} else if ui.Button(c.Key("ps-use-"+id), m.PsUse()).Clicked() {
			s.petSetActive(loc, id)
		}
		if s.petConfirm == id {
			if ui.PrimaryButton(c.Key("ps-del-yes-"+id), m.PsDelete()).Clicked() {
				s.petDelete(loc, id)
			}
			if ui.Button(c.Key("ps-del-no-"+id), m.ActionCancel()).Clicked() {
				s.mu.Lock()
				s.petConfirm = ""
				s.mu.Unlock()
			}
		} else if ui.Button(c.Key("ps-del-"+id), m.PsDelete()).Clicked() {
			s.mu.Lock()
			s.petConfirm = id
			s.actionMsg = m.PsDeleteConfirm(name)
			s.mu.Unlock()
		}
	})
}

// petSetActive makes a pet the one the sprite window shows.
func (s *Shell) petSetActive(loc, id string) {
	svc := s.svc
	go func() {
		_, err := s.B.raw("pet_set_active", map[string]any{"petId": id})
		s.mu.Lock()
		if err != nil {
			s.errs["pet-config"] = err.Error()
		} else {
			delete(s.cache, "pet-config")
			delete(s.errs, "pet-config")
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// petDelete removes a pet's directory (after the row's second click).
func (s *Shell) petDelete(loc, id string) {
	svc := s.svc
	m := locales.In(loc)
	go func() {
		_, err := s.B.raw("pet_delete", map[string]any{"id": id})
		s.mu.Lock()
		s.petConfirm = ""
		if err != nil {
			s.actionMsg = m.PsAddFailed(err.Error())
		} else {
			s.actionMsg = m.PsDeleted(id)
			delete(s.cache, "pets")
			delete(s.cache, "pet-config")
			if s.petPreviewID == id {
				s.petPreview, s.petPreviewImg, s.petPreviewID = nil, nil, ""
			}
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// petAdd asks for a spritesheet file and installs it as a pet whose id comes
// from the file name. The dialog and the upload run off the UI thread.
func (s *Shell) petAdd(loc string) {
	svc := s.svc
	m := locales.In(loc)
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:   m.PsAdd(),
			Filters: []mygo.FileFilter{{Name: "Spritesheet", Extensions: []string{"png", "webp", "gif", "jpg", "jpeg"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		data, rerr := os.ReadFile(paths[0])
		if rerr != nil {
			s.notifyErr(m.PsAddFailed(rerr.Error()))
			svc.Invalidate()
			return
		}
		base := filepath.Base(paths[0])
		name := strings.TrimSuffix(base, filepath.Ext(base))
		id := petSlug(name)
		_, aerr := s.B.raw("pet_add", map[string]any{
			"id":                id,
			"displayName":       name,
			"spritesheetBase64": base64.StdEncoding.EncodeToString(data),
		})
		s.mu.Lock()
		if aerr != nil {
			s.actionMsg = m.PsAddFailed(aerr.Error())
		} else {
			s.actionMsg = m.PsAdded(name)
			delete(s.cache, "pets")
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// petImportCodex copies the user's own Codex pets into the pet store.
func (s *Shell) petImportCodex(loc string) {
	svc := s.svc
	m := locales.In(loc)
	go func() {
		raw, err := s.B.raw("pet_import_codex", map[string]any{})
		s.mu.Lock()
		if err != nil {
			s.actionMsg = m.PsAddFailed(err.Error())
		} else {
			var res struct {
				ImportedIDs []string `json:"importedIds"`
			}
			_ = json.Unmarshal(raw, &res)
			if len(res.ImportedIDs) == 0 {
				s.actionMsg = m.PsImportNone()
			} else {
				s.actionMsg = m.PsImported(len(res.ImportedIDs))
			}
			delete(s.cache, "pets")
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// loadPetPreview fetches a pet's sheet for the page's thumbnail, once per pet.
func (s *Shell) loadPetPreview(id string) {
	s.mu.Lock()
	if s.petPreviewID == id {
		s.mu.Unlock()
		return
	}
	s.petPreviewID = id
	s.petPreview, s.petPreviewImg = nil, nil
	s.mu.Unlock()

	svc := s.svc
	go func() {
		raw, err := s.B.raw("pet_read_spritesheet", map[string]any{"id": id})
		var img image.Image
		if err == nil {
			if data, derr := base64.StdEncoding.DecodeString(str(asObj(raw), "dataBase64")); derr == nil {
				img, _ = decodePetSheet(data)
			}
		}
		s.mu.Lock()
		if s.petPreviewID == id {
			s.petPreviewImg = img
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// petSlug turns a file name into a pet id the backend accepts: letters, digits,
// '-', '_' and '.', with everything else collapsed to '-'.
func petSlug(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.'
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "pet"
	}
	return out
}
