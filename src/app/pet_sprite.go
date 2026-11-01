package app

// pet_sprite.go ports upstream src/lib/pet/animation.ts + PetSprite.tsx: the
// Codex `/pet` sprite-sheet layout and its animation clock. A sheet is a grid
// of 192×208 cells, 8 columns wide and one row per state, top to bottom; each
// state's row holds a fixed number of frames (its duration list length) and the
// remaining columns are blank. Frame cells are pixel art, so every frame is
// drawn with mygo's pixelated filter.
//
// The sheet arrives as bytes (pet_read_spritesheet returns mime + base64) and
// is cut once into per-frame bitmaps: 8 columns × 9 states of 192×208 RGBA is a
// few megabytes, and drawing a frame then costs one textured quad.

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"time"

	"github.com/egoist/mygo/ui"

	// The decoders a sheet may come in. mygo's ui package registers WebP too
	// (golang.org/x/image/webp), which is what Codex pet packs ship.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

const (
	// petSheetCols is the fixed column count of a sheet (upstream
	// SPRITE_GRID_COLS).
	petSheetCols = 8
	// petFrameWidth / petFrameHeight are one cell of the grid (upstream
	// SPRITE_FRAME_WIDTH / SPRITE_FRAME_HEIGHT: 1536/8 × 208).
	petFrameWidth  = 192
	petFrameHeight = 208
	// petBaseRows is the row count of a v1 sheet (9 states); v2 marketplace
	// sheets carry 11, and the extra rows are simply not mapped to a state.
	petBaseRows = 9
)

// petStates lists the states in sheet order, which is also the row order.
var petStates = []string{
	"idle",
	"running_right",
	"running_left",
	"waving",
	"jumping",
	"failed",
	"waiting",
	"running",
	"review",
}

// petStateRow is the sheet row of each state (upstream PET_STATE_ROW).
var petStateRow = func() map[string]int {
	m := make(map[string]int, len(petStates))
	for i, st := range petStates {
		m[st] = i
	}
	return m
}()

// petFrameMillis holds each state's per-frame durations in milliseconds, in
// column order; the length is the state's frame count (upstream
// PET_FRAME_DURATIONS_MS).
var petFrameMillis = map[string][]int{
	"idle":          {1680, 660, 660, 840, 840, 1920},
	"running_right": {120, 120, 120, 120, 120, 120, 120, 220},
	"running_left":  {120, 120, 120, 120, 120, 120, 120, 220},
	"waving":        {140, 140, 140, 280},
	"jumping":       {140, 140, 140, 140, 280},
	"failed":        {140, 140, 140, 140, 140, 140, 140, 240},
	"waiting":       {150, 150, 150, 150, 150, 260},
	"running":       {120, 120, 120, 120, 120, 220},
	"review":        {150, 150, 150, 150, 150, 280},
}

// petIdleFlourishMin/Max bound the random pause between idle flourishes, and
// petIdleFlourish picks them (upstream IDLE_FLOURISH_*).
const (
	petIdleFlourishMin = 8 * time.Second
	petIdleFlourishMax = 15 * time.Second
)

var petIdleFlourish = []string{"waving", "jumping"}

// petOneShotLoops is how many cycles a backend-driven one-shot plays before the
// sprite returns to its ambient state (upstream PET_ONESHOT_LOOPS).
var petOneShotLoops = map[string]int{
	"jumping": 3,
	"waving":  3,
	"failed":  2,
	"review":  3,
}

// petRowsForHeight derives a sheet's row count from its pixel height: the cell
// is a constant 208 tall, and never fewer than the 9 base states, so a blank or
// mis-measured sheet still maps the known states to valid rows.
func petRowsForHeight(height int) int {
	if height <= 0 {
		return petBaseRows
	}
	rows := (height + petFrameHeight/2) / petFrameHeight
	if rows < petBaseRows {
		return petBaseRows
	}
	return rows
}

// petFrameMillisFor returns a state's frame durations, falling back to idle's
// for a state this build does not know.
func petFrameMillisFor(state string) []int {
	if d, ok := petFrameMillis[state]; ok && len(d) > 0 {
		return d
	}
	return petFrameMillis["idle"]
}

// petCycleMillis is how long one full cycle of a state runs.
func petCycleMillis(state string) int {
	total := 0
	for _, d := range petFrameMillisFor(state) {
		total += d
	}
	return total
}

// petOneShotMillis is how long a backend one-shot stays on screen: its loop
// count times one cycle, plus a little slack so the last frame is not cut.
func petOneShotMillis(state string) time.Duration {
	loops := petOneShotLoops[state]
	if loops <= 0 {
		loops = 1
	}
	return time.Duration(petCycleMillis(state)*loops)*time.Millisecond + 80*time.Millisecond
}

// petIsOneShot reports whether the backend may drive the state as a one-shot.
func petIsOneShot(state string) bool {
	_, ok := petOneShotLoops[state]
	return ok
}

// petSheet is a decoded sheet: its row count and the frames of every state,
// already cut and ready to draw.
type petSheet struct {
	rows   int
	frames map[string][]*ui.Bitmap
}

// hasFrames reports whether the sheet produced anything drawable.
func (sh *petSheet) hasFrames() bool {
	if sh == nil {
		return false
	}
	for _, f := range sh.frames {
		if len(f) > 0 {
			return true
		}
	}
	return false
}

// frameFor returns the bitmap of a state's frame, wrapping the column into the
// state's own frame count and falling back to idle for a state the sheet has no
// row for.
func (sh *petSheet) frameFor(state string, col int) *ui.Bitmap {
	if sh == nil {
		return nil
	}
	frames := sh.frames[state]
	if len(frames) == 0 {
		frames = sh.frames["idle"]
	}
	if len(frames) == 0 {
		return nil
	}
	if col < 0 {
		col = 0
	}
	return frames[col%len(frames)]
}

// decodePetSheet decodes a sheet's bytes (PNG, WebP, GIF or JPEG).
func decodePetSheet(data []byte) (image.Image, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("pet: empty spritesheet")
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("pet: decode spritesheet: %w", err)
	}
	_ = format
	return img, nil
}

// slicePetSheet cuts a decoded sheet into per-state frames. States whose row
// falls outside the image (a short sheet) get no frames, and frameFor then falls
// back to idle, so a truncated sheet still animates.
func slicePetSheet(img image.Image) *petSheet {
	if img == nil {
		return nil
	}
	b := img.Bounds()
	rows := petRowsForHeight(b.Dy())
	cellW := b.Dx() / petSheetCols
	if cellW <= 0 {
		return nil
	}
	sh := &petSheet{rows: rows, frames: make(map[string][]*ui.Bitmap, len(petStates))}
	for _, state := range petStates {
		row := petStateRow[state]
		if (row+1)*petFrameHeight > b.Dy() {
			continue
		}
		durs := petFrameMillisFor(state)
		frames := make([]*ui.Bitmap, 0, len(durs))
		for col := 0; col < len(durs); col++ {
			if (col+1)*cellW > b.Dx() {
				break
			}
			r := petFrameRect(b, cellW, row, col)
			frames = append(frames, ui.NewBitmap(subImage(img, r)).Pixelated())
		}
		if len(frames) > 0 {
			sh.frames[state] = frames
		}
	}
	return sh
}

// petFrameRect is the rectangle of a grid cell, in the sheet's own coordinates.
func petFrameRect(bounds image.Rectangle, cellW, row, col int) image.Rectangle {
	return image.Rect(
		bounds.Min.X+col*cellW, bounds.Min.Y+row*petFrameHeight,
		bounds.Min.X+(col+1)*cellW, bounds.Min.Y+(row+1)*petFrameHeight,
	)
}

// subImage returns the part of img in r, sharing its pixels when the concrete
// type can (every decoder's type does), and copying otherwise.
func subImage(img image.Image, r image.Rectangle) image.Image {
	if si, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return si.SubImage(r)
	}
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(dst, dst.Bounds(), img, r.Min, draw.Src)
	return dst
}

// petAnimator walks one state's frames. The window keeps one and calls tick on
// every frame it builds; tick advances the column by the wall clock and says how
// long until the next frame, so the view can ask mygo for a repaint exactly
// then instead of running a timer of its own.
type petAnimator struct {
	state string
	col   int
	at    time.Time // when the current frame started showing (zero = not yet)
}

// set starts state at its first frame.
func (a *petAnimator) set(state string, now time.Time) {
	a.state = state
	a.col = 0
	a.at = now
}

// tick advances the animator to now and returns the time until the next frame.
func (a *petAnimator) tick(now time.Time) time.Duration {
	durs := petFrameMillisFor(a.state)
	if a.at.IsZero() || now.Before(a.at) {
		a.at = now
		return time.Duration(durs[0]) * time.Millisecond
	}
	elapsed := now.Sub(a.at)
	// A window that was hidden for a while must not spin through thousands of
	// frames: skip whole cycles first, then walk the remainder frame by frame.
	if cycle := time.Duration(petCycleMillis(a.state)) * time.Millisecond; cycle > 0 && elapsed >= cycle {
		skipped := elapsed / cycle
		elapsed -= skipped * cycle
		a.at = a.at.Add(skipped * cycle)
	}
	for {
		dur := time.Duration(durs[a.col%len(durs)]) * time.Millisecond
		if elapsed < dur {
			return dur - elapsed
		}
		elapsed -= dur
		a.at = a.at.Add(dur)
		a.col = (a.col + 1) % len(durs)
	}
}

// petScaledFrame is the on-screen size of one frame at scale.
func petScaledFrame(scale float64) (int, int) {
	if scale <= 0 {
		scale = 1
	}
	w := int(float64(petFrameWidth)*scale + 0.5)
	h := int(float64(petFrameHeight)*scale + 0.5)
	if w < 24 {
		w = 24
	}
	if h < 26 {
		h = 26
	}
	return w, h
}
