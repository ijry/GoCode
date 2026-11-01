package app

// pet_window_test.go covers the floating sprite window's view and its state
// machine: the sprite and the session badge, the ⋯ button appearing on hover, the
// ambient → interaction → ambient transitions (including the idle flourish), the
// badge's waiting > error > running precedence, and the pet:// payloads.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// petWindowForTest builds a window over a synthetic sheet, without a native
// window (the view is drawn by the tester).
func petWindowForTest(t *testing.T, state string) *petWindow {
	t.Helper()
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.cache["appearance-mode"] = json.RawMessage(`{"mode":"light"}`)
	img, err := decodePetSheet(petTestSheetPNG(t, petBaseRows))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	w := &petWindow{
		s:           s,
		loc:         "en",
		petID:       "boba",
		name:        "Boba",
		sheet:       slicePetSheet(img),
		scale:       petDefaultScale,
		alwaysOnTop: true,
		ambient:     state,
	}
	return w
}

func TestPetWindowDrawsSpriteAndBadge(t *testing.T) {
	w := petWindowForTest(t, "idle")
	w.counts = petBadgeCounts{running: 2}
	fw, fh := petScaledFrame(w.scale)
	tt := ui.NewTester(w.view, fw, fh)

	// The sprite's accessible name, and the badge's count plus its fuller label.
	for _, want := range []string{"Boba the pet", "2", "2 running"} {
		if !tt.HasText(want) {
			t.Errorf("window missing %q; texts = %v", want, tt.Texts())
		}
	}
	// The menu button stays out of the way until the pointer is over the pet.
	if tt.HasText("Pet menu") {
		t.Errorf("menu button drawn before hover; texts = %v", tt.Texts())
	}
	tt.Move(float32(fw)/2, float32(fh)/2)
	tt.Frame()
	if !tt.HasText("Pet menu") {
		t.Errorf("menu button missing on hover; texts = %v", tt.Texts())
	}
}

func TestPetWindowShowsLoadError(t *testing.T) {
	w := petWindowForTest(t, "idle")
	w.sheet = nil
	w.errMsg = "pet not found: boba"
	tt := ui.NewTester(w.view, 280, 90)
	if !tt.HasText("pet not found: boba") {
		t.Errorf("error state missing; texts = %v", tt.Texts())
	}
}

func TestPetWindowStatePrecedence(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	w := petWindowForTest(t, "running")
	w.mu.Lock()
	defer w.mu.Unlock()

	if got := w.stateLocked(now); got != "running" {
		t.Errorf("ambient state = %q, want running", got)
	}
	// An interaction wins while it lasts, then the ambient state returns.
	w.override, w.overrideEnd = "jumping", now.Add(500*time.Millisecond)
	if got := w.stateLocked(now); got != "jumping" {
		t.Errorf("override = %q, want jumping", got)
	}
	if got := w.stateLocked(now.Add(time.Second)); got != "running" {
		t.Errorf("after override = %q, want running", got)
	}
	if w.override != "" {
		t.Errorf("expired override left set: %q", w.override)
	}
}

// TestPetWindowIdleFlourish checks that a resting pet breaks into waving or
// jumping every 8–15s and comes back to idle.
func TestPetWindowIdleFlourish(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	w := petWindowForTest(t, "idle")
	w.mu.Lock()
	defer w.mu.Unlock()

	if got := w.stateLocked(now); got != "idle" {
		t.Fatalf("first state = %q, want idle", got)
	}
	if w.flourishAt.IsZero() {
		t.Fatal("no flourish scheduled")
	}
	// Just before the flourish it is still idle; at the flourish time it plays one.
	if got := w.stateLocked(w.flourishAt.Add(-time.Millisecond)); got != "idle" {
		t.Errorf("just before flourish = %q, want idle", got)
	}
	got := w.stateLocked(w.flourishAt)
	if got != "waving" && got != "jumping" {
		t.Errorf("flourish state = %q, want waving or jumping", got)
	}
	if w.flourishAt.Before(w.overrideEnd) {
		t.Error("the next flourish must be scheduled after this one ends")
	}
}

func TestPetBadgeKindPrecedence(t *testing.T) {
	cases := []struct {
		counts petBadgeCounts
		want   string
		n      int
	}{
		{petBadgeCounts{}, "", 0},
		{petBadgeCounts{running: 3}, "running", 3},
		{petBadgeCounts{running: 3, waiting: 1}, "waiting", 1},
		{petBadgeCounts{running: 3, waiting: 1, errors: 2}, "error", 2},
	}
	for _, c := range cases {
		kind, n := petBadgeKind(c.counts)
		if kind != c.want || n != c.n {
			t.Errorf("petBadgeKind(%+v) = %q/%d, want %q/%d", c.counts, kind, n, c.want, c.n)
		}
	}
}

func TestPetWindowSessionsFoldIntoBadge(t *testing.T) {
	w := petWindowForTest(t, "idle")
	w.applySessions([]byte(`{"running_count":2,"waiting_count":1,"error_count":0,"sessions":[
		{"connectionId":"a","status":"running"},
		{"connectionId":"b","status":"running"},
		{"connectionId":"c","status":"running","pending":{"requestId":"r"}}
	]}`))
	if want := (petBadgeCounts{running: 2, waiting: 1}); w.counts != want {
		t.Errorf("counts = %+v, want %+v", w.counts, want)
	}
}

func TestPetWindowScaleClamps(t *testing.T) {
	w := petWindowForTest(t, "idle")
	w.setScale(9) // no window: only the stored value moves
	if w.scaleNow() != petMaxScale {
		t.Errorf("scale = %v, want clamped %v", w.scaleNow(), petMaxScale)
	}
	w.setScale(0.1)
	if w.scaleNow() != petMinScale {
		t.Errorf("scale = %v, want clamped %v", w.scaleNow(), petMinScale)
	}
}

func TestPetEventString(t *testing.T) {
	cases := []struct{ raw, want string }{
		{``, ""},
		{`"jumping"`, "jumping"},
		{`{"kind":"waving"}`, "waving"},
		{`{"state":"review"}`, "review"},
		{`{"payload":"failed"}`, "failed"},
		{`{"other":1}`, ""},
	}
	for _, c := range cases {
		if got := petEventString(json.RawMessage(c.raw)); got != c.want {
			t.Errorf("petEventString(%s) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestPetSlug(t *testing.T) {
	cases := map[string]string{
		"Boba Tea":      "boba-tea",
		"  space_cat  ": "space_cat",
		"a/b":           "a-b",
		"ümlaut":        "mlaut",
		"":              "pet",
		"..hidden":      "hidden",
		"pixel.pet.v2":  "pixel.pet.v2",
	}
	for in, want := range cases {
		if got := petSlug(in); got != want {
			t.Errorf("petSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
