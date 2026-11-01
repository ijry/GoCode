package app

// pet_snapshot_test.go renders the sprite window and the sprite page to PNGs so
// the layout can be eyeballed. Off by default:
//
//	GOCODE_PET_SNAPSHOT=render-out go test ./src/app -run TestZZPetSnapshot
//
// The sprite window draws on a transparent background (it floats over the
// desktop), so its snapshot is composited onto a neutral backdrop to be legible.

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestZZPetSnapshot(t *testing.T) {
	out := os.Getenv("GOCODE_PET_SNAPSHOT")
	if out == "" {
		t.Skip("set GOCODE_PET_SNAPSHOT=<dir> to render")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}

	// The floating window: sprite + a running badge, pointer over the pet so the
	// menu button shows.
	w := petWindowForTest(t, "idle")
	w.counts = petBadgeCounts{running: 2, waiting: 1}
	fw, fh := petScaledFrame(w.scale)
	tt := ui.NewTester(w.view, fw, fh)
	tt.Move(float32(fw)/2, float32(fh)/2)
	tt.Frame()
	writePNG(t, filepath.Join(out, "pet-window.png"), overBackdrop(tt.Image()))

	// The page: two pets, one active.
	s := petSpriteShell(t, `[
		{"id":"boba","displayName":"Boba","description":"calm"},
		{"id":"pixel-cat","displayName":"Pixel Cat","description":"chases cursors"}
	]`, `{"activePetId":"boba","scale":0.75,"enabled":true}`)
	s.cache["pet-sessions"] = json.RawMessage(`{"running_count":0,"waiting_count":0,"error_count":0,"sessions":[]}`)
	writePNG(t, filepath.Join(out, "pet-sprite-page.png"), ui.NewTester(s.View, 1240, 820).Image())

	// The empty state, which is what a fresh install shows.
	s2 := petSpriteShell(t, `[]`, `{"activePetId":null,"scale":0.75}`)
	writePNG(t, filepath.Join(out, "pet-sprite-empty.png"), ui.NewTester(s2.View, 1240, 820).Image())
}

// overBackdrop composites a transparent render onto a neutral grey, as a desktop
// would show it.
func overBackdrop(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), &image.Uniform{color.RGBA{0xE8, 0xEA, 0xED, 0xFF}}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Over)
	return dst
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
