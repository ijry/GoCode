package app

// pet_sprite_page_test.go covers the Pet → Sprite manager page: the installed
// pets list with its active badge, the two-step delete, the empty state, and the
// guard that keeps the sprite window from opening with no pet chosen.

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// petSpriteShell parks the Shell on the Pet section's sprite tab with the pets
// list and config pre-seeded, so the page renders without the network.
func petSpriteShell(t *testing.T, pets, cfg string) *Shell {
	t.Helper()
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.section = "pet"
	s.petTab = petTabSprite
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	s.cache["pets"] = json.RawMessage(pets)
	s.cache["pet-config"] = json.RawMessage(cfg)
	return s
}

func TestPetSpritePageListsPets(t *testing.T) {
	s := petSpriteShell(t, `[
		{"id":"boba","displayName":"Boba","description":"calm"},
		{"id":"pixel-cat","displayName":"Pixel Cat"}
	]`, `{"activePetId":"boba","scale":0.75,"enabled":true}`)
	tt := ui.NewTester(s.View, 900, 600)

	for _, want := range []string{
		"Desktop sprite",      // the page heading
		"Open pet window",     // the window toggle
		"Size",                // the slider's label
		"Boba", "boba · calm", // the active row: name and id · description
		"Pixel Cat", "pixel-cat", // the second row
		"Active pet",    // the active badge
		"Use", "Delete", // the row actions
	} {
		if !tt.HasText(want) {
			t.Errorf("page missing %q; texts = %v", want, tt.Texts())
		}
	}
}

func TestPetSpritePageDeleteAsksTwice(t *testing.T) {
	s := petSpriteShell(t, `[{"id":"boba","displayName":"Boba"}]`,
		`{"activePetId":"boba","scale":0.75}`)
	tt := ui.NewTester(s.View, 900, 600)

	if err := tt.Click("Delete"); err != nil {
		t.Fatalf("click delete: %v", err)
	}
	tt.Frame()
	s.mu.Lock()
	confirm := s.petConfirm
	msg := s.actionMsg
	s.mu.Unlock()
	if confirm != "boba" {
		t.Fatalf("petConfirm = %q, want boba", confirm)
	}
	if msg == "" {
		t.Error("the confirmation line must explain what deleting does")
	}
	// The armed row offers Delete again plus a way out.
	if !tt.HasText("Cancel") {
		t.Errorf("armed row has no cancel; texts = %v", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatalf("click cancel: %v", err)
	}
	tt.Frame()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.petConfirm != "" {
		t.Errorf("petConfirm = %q after cancel, want empty", s.petConfirm)
	}
}

func TestPetSpritePageEmptyState(t *testing.T) {
	s := petSpriteShell(t, `[]`, `{"activePetId":null,"scale":0.75}`)
	tt := ui.NewTester(s.View, 900, 600)
	for _, want := range []string{
		"No pets installed yet",
		"Add pet…",
		"Import from Codex",
	} {
		if !tt.HasText(want) {
			t.Errorf("empty page missing %q; texts = %v", want, tt.Texts())
		}
	}
}

// TestPetSpritePageRefusesWindowWithoutPet locks the guard: opening the sprite
// window with no active pet explains itself instead of failing silently.
func TestPetSpritePageRefusesWindowWithoutPet(t *testing.T) {
	s := petSpriteShell(t, `[{"id":"boba","displayName":"Boba"}]`, `{"activePetId":null,"scale":0.75}`)
	tt := ui.NewTester(s.View, 900, 600)
	if err := tt.Click("Open pet window"); err != nil {
		t.Fatalf("click open: %v", err)
	}
	if !waitForToastText(tt, "Choose a pet", 2*time.Second) {
		t.Errorf("no toast about the missing pet; texts = %v", tt.Texts())
	}
	if s.petWindowOpen() {
		t.Error("the sprite window must not open without an active pet")
	}
}

func TestPetSpritePageSeedsScaleFromConfig(t *testing.T) {
	s := petSpriteShell(t, `[{"id":"boba","displayName":"Boba"}]`,
		`{"activePetId":"boba","scale":1.5}`)
	_ = ui.NewTester(s.View, 900, 600)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.petScale != 1.5 || s.petScaleSaved != 1.5 {
		t.Errorf("scale = %v (saved %v), want 1.5", s.petScale, s.petScaleSaved)
	}
}

// TestPetSheetRoundTripThroughBackend is the end-to-end guard for the sprite
// path: a sheet added through pet_add comes back from pet_read_spritesheet as
// bytes this package decodes and cuts into frames, and pet_set_active makes it
// the pet the window would show.
func TestPetSheetRoundTripThroughBackend(t *testing.T) {
	t.Setenv("GOCODE_DATA_DIR", t.TempDir())
	s := New(newBackendForTest(t))

	sheet := petTestSheetPNG(t, petBaseRows)
	if len(sheet) < 1024 {
		t.Fatalf("fixture is %d bytes; the backend rejects sheets under 1KB", len(sheet))
	}
	if _, err := s.B.raw("pet_add", map[string]any{
		"id":                "boba",
		"displayName":       "Boba",
		"spritesheetBase64": base64.StdEncoding.EncodeToString(sheet),
	}); err != nil {
		t.Fatalf("pet_add: %v", err)
	}

	raw, err := s.B.raw("pet_list", nil)
	if err != nil {
		t.Fatalf("pet_list: %v", err)
	}
	if pets := asList(raw); len(pets) != 1 || str(pets[0], "id") != "boba" {
		t.Fatalf("pet_list = %v, want one pet named boba", asList(raw))
	}

	spriteRaw, err := s.B.raw("pet_read_spritesheet", map[string]any{"id": "boba"})
	if err != nil {
		t.Fatalf("pet_read_spritesheet: %v", err)
	}
	img, err := decodePetSheet(mustBase64(t, str(asObj(spriteRaw), "dataBase64")))
	if err != nil {
		t.Fatalf("decode spritesheet: %v", err)
	}
	sh := slicePetSheet(img)
	if got, want := len(sh.frames["idle"]), len(petFrameMillisFor("idle")); got != want {
		t.Errorf("idle frames = %d, want %d", got, want)
	}

	if _, err := s.B.raw("pet_set_active", map[string]any{"petId": "boba"}); err != nil {
		t.Fatalf("pet_set_active: %v", err)
	}
	cfg, err := s.B.raw("pet_get_settings", nil)
	if err != nil {
		t.Fatalf("pet_get_settings: %v", err)
	}
	if got := str(asObj(cfg), "activePetId"); got != "boba" {
		t.Errorf("activePetId = %q, want boba", got)
	}
}

// mustBase64 decodes a base64 payload, failing the test on a malformed one.
func mustBase64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	return b
}
