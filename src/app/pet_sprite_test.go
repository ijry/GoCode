package app

// pet_sprite_test.go covers the sheet model the floating pet window draws from:
// the Codex grid math (row per state, 192×208 cells, rows derived from height),
// the per-state frame counts and durations, the idle-flourish and one-shot
// budgets, and the animator's clock (frame advance, wrap, catch-up).

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"
)

// petTestSheetPNG builds a synthetic Codex-shaped sheet: 8 columns × rows rows of
// 192×208 cells, every cell a solid colour that encodes its (row, col).
func petTestSheetPNG(t *testing.T, rows int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, petFrameWidth*petSheetCols, petFrameHeight*rows))
	for row := 0; row < rows; row++ {
		for col := 0; col < petSheetCols; col++ {
			c := color.RGBA{R: uint8(row * 16), G: uint8(col * 24), B: 200, A: 255}
			for y := row * petFrameHeight; y < (row+1)*petFrameHeight; y++ {
				for x := col * petFrameWidth; x < (col+1)*petFrameWidth; x++ {
					img.SetRGBA(x, y, c)
				}
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return buf.Bytes()
}

func TestPetRowsForHeight(t *testing.T) {
	cases := []struct{ height, want int }{
		{0, 9}, {-4, 9}, {1872, 9}, {2288, 11}, {200, 9}, {1872 + 104, 10},
	}
	for _, c := range cases {
		if got := petRowsForHeight(c.height); got != c.want {
			t.Errorf("petRowsForHeight(%d) = %d, want %d", c.height, got, c.want)
		}
	}
}

func TestPetFrameRectMatchesUpstreamGrid(t *testing.T) {
	bounds := image.Rect(0, 0, petFrameWidth*petSheetCols, petFrameHeight*petBaseRows)
	// "waving" is row 3 upstream, so its third frame is column 2 of that row.
	got := petFrameRect(bounds, petFrameWidth, petStateRow["waving"], 2)
	want := image.Rect(2*petFrameWidth, 3*petFrameHeight, 3*petFrameWidth, 4*petFrameHeight)
	if got != want {
		t.Fatalf("waving frame 2 = %v, want %v", got, want)
	}
	// Every state keeps upstream's row order, top to bottom.
	for i, state := range petStates {
		if petStateRow[state] != i {
			t.Errorf("row of %q = %d, want %d", state, petStateRow[state], i)
		}
	}
}

func TestSlicePetSheetCutsEveryState(t *testing.T) {
	img, err := decodePetSheet(petTestSheetPNG(t, petBaseRows))
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	sh := slicePetSheet(img)
	if !sh.hasFrames() {
		t.Fatal("sheet has no frames")
	}
	if sh.rows != petBaseRows {
		t.Errorf("rows = %d, want %d", sh.rows, petBaseRows)
	}
	for _, state := range petStates {
		frames := sh.frames[state]
		if want := len(petFrameMillisFor(state)); len(frames) != want {
			t.Errorf("%s: %d frames, want %d", state, len(frames), want)
		}
		for i, bm := range frames {
			if w, h := bm.Size(); w != petFrameWidth || h != petFrameHeight {
				t.Errorf("%s frame %d is %dx%d, want %dx%d", state, i, w, h, petFrameWidth, petFrameHeight)
			}
		}
	}
	// A column past the state's frame count wraps into that state's own frames
	// (upstream restarts the cycle rather than running into the blank columns).
	if sh.frameFor("waving", 9) == nil || sh.frameFor("waving", 9) != sh.frameFor("waving", 1) {
		t.Error("frameFor must wrap the column into the state's own frames")
	}
	// An unknown state falls back to idle rather than drawing nothing.
	if sh.frameFor("teleporting", 0) != sh.frameFor("idle", 0) {
		t.Error("unknown state must fall back to idle")
	}
}

func TestSlicePetSheetSkipsMissingRows(t *testing.T) {
	// A sheet shorter than the 9 base rows: rows it lacks simply have no frames,
	// and frameFor falls back to idle.
	short := image.NewRGBA(image.Rect(0, 0, petFrameWidth*petSheetCols, petFrameHeight*2))
	sh := slicePetSheet(short)
	if len(sh.frames["idle"]) == 0 || len(sh.frames["review"]) != 0 {
		t.Fatalf("short sheet: idle=%d review=%d frames", len(sh.frames["idle"]), len(sh.frames["review"]))
	}
	if sh.frameFor("review", 0) != sh.frameFor("idle", 0) {
		t.Error("a state with no row must fall back to idle")
	}
}

func TestDecodePetSheetRejectsGarbage(t *testing.T) {
	if _, err := decodePetSheet(nil); err == nil {
		t.Error("empty sheet must fail")
	}
	if _, err := decodePetSheet([]byte("not an image")); err == nil {
		t.Error("garbage sheet must fail")
	}
}

func TestPetAnimatorWalksDurationsAndWraps(t *testing.T) {
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	var a petAnimator
	a.set("waving", start) // 140, 140, 140, 280
	if a.col != 0 || a.state != "waving" {
		t.Fatalf("set: state=%q col=%d", a.state, a.col)
	}
	// Nothing elapsed yet: the first frame has its whole duration left.
	if got := a.tick(start); got != 140*time.Millisecond {
		t.Errorf("first wait = %v, want 140ms", got)
	}
	// 140ms in: the second frame, 140ms left of it.
	if got := a.tick(start.Add(140 * time.Millisecond)); got != 140*time.Millisecond || a.col != 1 {
		t.Errorf("second frame: col=%d wait=%v, want col 1 / 140ms", a.col, got)
	}
	// 400ms in: 140+140+140 = 420 is where the last (280ms) frame starts, so at
	// 400 the third frame still has 20ms to run.
	if got := a.tick(start.Add(400 * time.Millisecond)); got != 20*time.Millisecond || a.col != 2 {
		t.Errorf("third frame: col=%d wait=%v, want col 2 / 20ms", a.col, got)
	}
	// 420ms in: the last frame, with its full 280ms.
	if got := a.tick(start.Add(420 * time.Millisecond)); got != 280*time.Millisecond || a.col != 3 {
		t.Errorf("last frame: col=%d wait=%v, want col 3 / 280ms", a.col, got)
	}
	// One full cycle later the animator is back on the first frame.
	if got := a.tick(start.Add(time.Duration(petCycleMillis("waving")) * time.Millisecond)); a.col != 0 || got != 140*time.Millisecond {
		t.Errorf("wrap: col=%d wait=%v, want col 0 / 140ms", a.col, got)
	}
}

func TestPetAnimatorCatchesUpWithoutSpinning(t *testing.T) {
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	var a petAnimator
	a.set("idle", start)
	// A window hidden for an hour: the animator skips whole cycles and still
	// returns a wait inside one frame duration.
	got := a.tick(start.Add(time.Hour))
	if got <= 0 || got > 1920*time.Millisecond {
		t.Fatalf("catch-up wait = %v, want within one frame", got)
	}
	if a.col < 0 || a.col >= len(petFrameMillisFor("idle")) {
		t.Fatalf("catch-up col = %d", a.col)
	}
}

func TestPetOneShotBudgets(t *testing.T) {
	// Three cycles plus slack for the celebratory ones, two for `failed` so the
	// frown does not linger.
	for _, state := range []string{"jumping", "waving", "review"} {
		if !petIsOneShot(state) {
			t.Errorf("%q must be a one-shot state", state)
		}
		want := time.Duration(petCycleMillis(state)*3)*time.Millisecond + 80*time.Millisecond
		if got := petOneShotMillis(state); got != want {
			t.Errorf("petOneShotMillis(%q) = %v, want %v", state, got, want)
		}
	}
	want := time.Duration(petCycleMillis("failed")*2)*time.Millisecond + 80*time.Millisecond
	if got := petOneShotMillis("failed"); got != want {
		t.Errorf("failed one-shot = %v, want %v", got, want)
	}
	if petIsOneShot("idle") || petIsOneShot("running") {
		t.Error("ambient states are not one-shots")
	}
}

func TestPetScaledFrame(t *testing.T) {
	cases := []struct {
		scale float64
		w, h  int
	}{
		{1, 192, 208},
		{0.75, 144, 156},
		{0, 192, 208},  // unset scale draws at 1:1
		{0.01, 24, 26}, // clamped to a clickable minimum
	}
	for _, c := range cases {
		w, h := petScaledFrame(c.scale)
		if w != c.w || h != c.h {
			t.Errorf("petScaledFrame(%v) = %dx%d, want %dx%d", c.scale, w, h, c.w, c.h)
		}
	}
}
