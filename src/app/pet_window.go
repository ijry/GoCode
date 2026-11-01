package app

// pet_window.go is the desktop sprite window: the upstream `src/app/pet` route,
// which opens a separate frameless window with a transparent background that
// floats above other apps and shows the active pet's spritesheet animating.
//
// It is a second mygo window (ui.View of its own), not part of the workspace:
//   - the sprite fills the window and drags it (mygo's DragWindow); while it
//     moves, the pet plays running_left / running_right, as upstream does from
//     the window's position deltas;
//   - the top-right badge counts the active sessions (waiting > error >
//     running) and opens the Pet page's session list;
//   - a ⋯ button (shown while the pointer is over the pet) opens a native
//     context menu: one-shot animations, size, always-on-top, the Pet page and
//     closing the window;
//   - the position, size and always-on-top choice persist through
//     pet_save_window_state, and `enabled` remembers whether the window was open
//     so the next launch restores it.
//
// Upstream drives the sprite from `pet://state` / `pet://oneshot` events and a
// global hover watcher; this port reads the ambient state once and takes the
// one-shots from the firehose (stream.go), and uses hover for the menu button
// instead of a wave-on-hover (a native window has no cross-window hover watcher).

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"gocode/locales"
)

// petBadgeCounts is what the badge sums from the active-session payload.
type petBadgeCounts struct {
	running int
	waiting int
	errors  int
}

// petWindow is the floating sprite window and the state its view draws.
type petWindow struct {
	s   *Shell
	win *mygo.Window

	mu          sync.Mutex
	loc         string
	petID       string
	name        string
	sheet       *petSheet
	errMsg      string
	scale       float64
	alwaysOnTop bool
	ambient     string // agent-driven state (pet_get_current_state / pet://state)
	override    string // interaction or one-shot state; "" = ambient
	overrideEnd time.Time
	anim        petAnimator
	flourishAt  time.Time
	counts      petBadgeCounts
	lastX       int
	lastY       int
	saveTimer   *time.Timer
	closed      bool
}

// petDefaultScale / petMinScale / petMaxScale bound the sprite size, matching
// upstream's default (0.75) and its menu steps.
const (
	petDefaultScale = 0.75
	petMinScale     = 0.5
	petMaxScale     = 3.0
	petScaleStep    = 0.25
)

// openPetWindow opens the sprite window, or brings it forward when it is already
// open. It reads the persisted config (active pet, size, position) in the
// background and shows the window once the sheet is decoded.
func (s *Shell) openPetWindow(loc string) {
	s.mu.Lock()
	if w := s.petWin; w != nil {
		s.mu.Unlock()
		w.reveal()
		return
	}
	w := &petWindow{s: s, loc: loc, scale: petDefaultScale, alwaysOnTop: true, ambient: "idle"}
	s.petWin = w
	s.mu.Unlock()
	w.open()
}

// RestorePetWindow reopens the sprite window at launch when it was open when the
// app last quit (the pet config keeps `enabled` for exactly this).
func (s *Shell) RestorePetWindow() {
	go func() {
		raw, err := s.B.raw("pet_get_settings", nil)
		if err != nil {
			return
		}
		cfg := asObj(raw)
		if !asBool(cfg["enabled"]) || str(cfg, "activePetId") == "" {
			return
		}
		s.openPetWindow(i18n.Locale())
	}()
}

// closePetWindow closes the sprite window if it is open.
func (s *Shell) closePetWindow() {
	s.mu.Lock()
	w := s.petWin
	s.mu.Unlock()
	if w != nil {
		w.close()
	}
}

// petWindowOpen reports whether the sprite window is open (for the page's
// button label).
func (s *Shell) petWindowOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.petWin != nil
}

// petWindowClosed is the window's OnClosed hook: forget it so the next open
// starts afresh.
func (s *Shell) petWindowClosed(w *petWindow) {
	s.mu.Lock()
	if s.petWin == w {
		s.petWin = nil
	}
	s.mu.Unlock()
}

// openPetPage brings the workspace to the Pet section's session list — what the
// badge opens upstream (togglePetPanel).
func (s *Shell) openPetPage(loc string) {
	s.mu.Lock()
	s.section = "pet"
	s.petTab = petTabSessions
	win := s.mainWin
	s.mu.Unlock()
	if win != nil {
		win.Show()
		win.Focus()
	}
	s.svc.Invalidate()
}

// open creates the native window (hidden until the sheet is ready) and starts
// loading the active pet.
func (w *petWindow) open() {
	fw, fh := petScaledFrame(petDefaultScale)
	w.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           locales.In(w.loc).PsTitle(),
		Width:           fw,
		Height:          fh,
		Frameless:       true,
		Transparent:     true,
		AlwaysOnTop:     true,
		SkipTaskbar:     true,
		DisableResize:   true,
		DisableMaximize: true,
		DisableShadow:   true,
		Hidden:          true,
		Content:         ui.View(w.view),
	})
	w.win.OnMove(func() { w.onMove() })
	w.win.OnClose(func(e *mygo.CloseEvent) {
		// Save where it was, and remember that it is closed, before the window
		// goes: a native close must not lose the user's placement.
		w.saveState(false)
	})
	w.win.OnClosed(func() { w.s.petWindowClosed(w) })
	go w.load()
}

// reveal shows the window again (it was hidden or is already open).
func (w *petWindow) reveal() {
	w.mu.Lock()
	win := w.win
	w.mu.Unlock()
	if win != nil {
		win.Show()
		win.Focus()
	}
}

// close closes the window, which persists the closed state.
func (w *petWindow) close() {
	w.mu.Lock()
	w.closed = true
	win := w.win
	w.mu.Unlock()
	if win != nil {
		win.Close()
		return
	}
	w.saveState(false)
}

// load reads the persisted config and the active pet's sheet, then shows the
// window. Failures keep the window open with the reason on it, so the user can
// still reach the menu and close it.
func (w *petWindow) load() {
	m := locales.In(w.loc)
	cfg := asObj(w.raw("pet_get_settings", nil))
	petID := str(cfg, "activePetId")
	scale := petDefaultScale
	if v, ok := cfg["scale"].(float64); ok && v > 0 {
		scale = v
	}
	onTop := true
	if v, ok := cfg["alwaysOnTop"].(bool); ok {
		onTop = v
	}
	x, hasX := intOf(cfg["x"]), cfg["x"] != nil
	y, hasY := intOf(cfg["y"]), cfg["y"] != nil

	w.mu.Lock()
	w.scale = scale
	w.alwaysOnTop = onTop
	w.petID = petID
	w.mu.Unlock()

	if petID == "" {
		w.fail(m.PsNoActivePet())
		return
	}
	raw, err := w.s.B.raw("pet_read_spritesheet", map[string]any{"id": petID})
	if err != nil {
		w.fail(m.PsLoadFailed(err.Error()))
		return
	}
	obj := asObj(raw)
	data, derr := base64.StdEncoding.DecodeString(str(obj, "dataBase64"))
	if derr != nil {
		w.fail(m.PsLoadFailed(derr.Error()))
		return
	}
	img, derr := decodePetSheet(data)
	if derr != nil {
		w.fail(m.PsLoadFailed(derr.Error()))
		return
	}
	detail := asObj(w.raw("pet_get", map[string]any{"id": petID}))
	name := firstNonEmpty(str(detail, "displayName"), petID)

	w.win.Update(func() {
		// Bitmaps belong to the main thread, so the sheet is cut here.
		sh := slicePetSheet(img)
		w.mu.Lock()
		w.sheet = sh
		w.errMsg = ""
		w.name = name
		w.mu.Unlock()
		fw, fh := petScaledFrame(scale)
		w.win.SetContentSize(fw, fh)
		if hasX && hasY {
			w.win.SetPosition(x, y)
		}
		w.win.SetAlwaysOnTop(onTop)
	})
	w.win.Show()
	w.saveState(true) // enabled: the next launch restores the window
	go w.pollSessions()
}

// fail records a load error and shows the window with it, sized for the text.
func (w *petWindow) fail(msg string) {
	w.mu.Lock()
	w.errMsg = msg
	w.sheet = nil
	w.mu.Unlock()
	w.win.Update(func() {
		w.win.SetContentSize(280, 90)
	})
	w.win.Show()
}

// reload swaps in a newly chosen active pet without closing the window
// (upstream reacts to `pet://active-changed` the same way).
func (w *petWindow) reload() {
	go w.load()
}

// onMove tracks the window's position: it drives the walking animation and, a
// moment after the last move, persists where the user put the pet.
func (w *petWindow) onMove() {
	w.mu.Lock()
	win := w.win
	w.mu.Unlock()
	if win == nil {
		return
	}
	x, y := win.Position()
	w.mu.Lock()
	dx := x - w.lastX
	w.lastX, w.lastY = x, y
	now := time.Now()
	if dx > 0 {
		w.override, w.overrideEnd = "running_right", now.Add(240*time.Millisecond)
	} else if dx < 0 {
		w.override, w.overrideEnd = "running_left", now.Add(240*time.Millisecond)
	}
	w.mu.Unlock()
	w.scheduleSave()
	win.Invalidate()
}

// scheduleSave debounces the position write so a drag stores one row, not one
// per pixel.
func (w *petWindow) scheduleSave() {
	w.mu.Lock()
	if w.saveTimer != nil {
		w.saveTimer.Stop()
	}
	w.saveTimer = time.AfterFunc(700*time.Millisecond, func() { w.saveState(true) })
	w.mu.Unlock()
}

// saveState writes the window's geometry (and whether it should reopen) to the
// pet config. Best effort: a failed save must never disturb the window.
func (w *petWindow) saveState(enabled bool) {
	w.mu.Lock()
	win := w.win
	x, y := w.lastX, w.lastY
	scale, onTop := w.scale, w.alwaysOnTop
	w.mu.Unlock()
	if win != nil {
		if px, py := win.Position(); px != 0 || py != 0 {
			x, y = px, py
		}
	}
	_, _ = w.s.B.raw("pet_save_window_state", map[string]any{
		"x": x, "y": y, "scale": scale, "alwaysOnTop": onTop, "enabled": enabled,
	})
}

// pollSessions refreshes the badge counts while the window is open. Upstream
// pushes `pet://sessions`; this port has no such mapper, so the window asks.
func (w *petWindow) pollSessions() {
	for {
		w.mu.Lock()
		closed := w.closed
		w.mu.Unlock()
		if closed {
			return
		}
		raw, err := w.s.B.raw("pet_list_active_sessions", nil)
		if err == nil {
			w.applySessions(raw)
		}
		time.Sleep(5 * time.Second)
	}
}

// applySessions folds a pet_list_active_sessions payload into the badge counts.
func (w *petWindow) applySessions(raw []byte) {
	obj := asObj(raw)
	var c petBadgeCounts
	for _, sess := range asListVal(obj["sessions"]) {
		switch petStatusKind(sess) {
		case "waiting":
			c.waiting++
		case "error":
			c.errors++
		default:
			c.running++
		}
	}
	w.mu.Lock()
	changed := c != w.counts
	w.counts = c
	win := w.win
	w.mu.Unlock()
	if changed && win != nil {
		win.Invalidate()
	}
}

// setAmbient records the agent-driven state (idle / running / review / …).
func (w *petWindow) setAmbient(state string) {
	if state == "" {
		return
	}
	w.mu.Lock()
	changed := w.ambient != state
	w.ambient = state
	win := w.win
	w.mu.Unlock()
	if changed && win != nil {
		win.Invalidate()
	}
}

// playOneShot shows a state for its upstream loop budget, then falls back to the
// ambient one.
func (w *petWindow) playOneShot(state string) {
	if !petIsOneShot(state) {
		return
	}
	w.mu.Lock()
	w.override = state
	w.overrideEnd = time.Now().Add(petOneShotMillis(state))
	win := w.win
	w.mu.Unlock()
	if win != nil {
		win.Invalidate()
	}
}

// setScale resizes the sprite (and the window with it) and persists the choice.
func (w *petWindow) setScale(v float64) {
	if v < petMinScale {
		v = petMinScale
	}
	if v > petMaxScale {
		v = petMaxScale
	}
	w.mu.Lock()
	w.scale = v
	win := w.win
	w.mu.Unlock()
	if win != nil {
		fw, fh := petScaledFrame(v)
		win.SetContentSize(fw, fh)
		win.Invalidate()
	}
	w.scheduleSave()
}

// setAlwaysOnTop flips the window's float-above-others flag.
func (w *petWindow) setAlwaysOnTop(v bool) {
	w.mu.Lock()
	w.alwaysOnTop = v
	win := w.win
	w.mu.Unlock()
	if win != nil {
		win.SetAlwaysOnTop(v)
	}
	w.scheduleSave()
}

// scaleNow returns the current scale (for the menu's relative steps).
func (w *petWindow) scaleNow() float64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.scale
}

// topNow reports the always-on-top flag (for the menu's checkbox).
func (w *petWindow) topNow() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.alwaysOnTop
}

// popupMenu shows the pet's native context menu at the pointer. It blocks until
// the menu closes (a native menu runs its own event loop), so the view calls it
// from a goroutine.
func (w *petWindow) popupMenu(loc string) {
	m := locales.In(loc)
	w.mu.Lock()
	win := w.win
	w.mu.Unlock()
	if win == nil {
		return
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: m.PsJump(), Click: func(*mygo.MenuItem, *mygo.Window) { w.playOneShot("jumping") }},
		{Label: m.PsWave(), Click: func(*mygo.MenuItem, *mygo.Window) { w.playOneShot("waving") }},
		mygo.Separator(),
		{Label: m.PsBigger(), Click: func(*mygo.MenuItem, *mygo.Window) { w.setScale(w.scaleNow() + petScaleStep) }},
		{Label: m.PsSmaller(), Click: func(*mygo.MenuItem, *mygo.Window) { w.setScale(w.scaleNow() - petScaleStep) }},
		{Label: m.PsResetSize(), Click: func(*mygo.MenuItem, *mygo.Window) { w.setScale(petDefaultScale) }},
		{Label: m.PsAlwaysOnTop(), Type: mygo.MenuItemCheckbox, Checked: w.topNow(),
			Click: func(it *mygo.MenuItem, _ *mygo.Window) { w.setAlwaysOnTop(it.Checked) }},
		mygo.Separator(),
		{Label: m.PsOpenPage(), Click: func(*mygo.MenuItem, *mygo.Window) { w.s.openPetPage(loc) }},
		{Label: m.PsCloseWindow(), Click: func(*mygo.MenuItem, *mygo.Window) { w.close() }},
	})
	menu.Popup(win)
}

// stateLocked picks the state to draw now: an interaction/one-shot while it
// lasts, otherwise the ambient state, with upstream's idle flourishes inserted
// every 8–15 seconds so a resting pet is not a statue.
func (w *petWindow) stateLocked(now time.Time) string {
	if w.override != "" {
		if now.Before(w.overrideEnd) {
			return w.override
		}
		w.override = ""
	}
	state := w.ambient
	if state == "" {
		state = "idle"
	}
	if state != "idle" {
		return state
	}
	if w.flourishAt.IsZero() {
		w.flourishAt = now.Add(petFlourishDelay())
	}
	if now.Before(w.flourishAt) {
		return state
	}
	pick := petIdleFlourish[rand.Intn(len(petIdleFlourish))]
	w.override = pick
	w.overrideEnd = now.Add(petOneShotMillis(pick))
	w.flourishAt = w.overrideEnd.Add(petFlourishDelay())
	return pick
}

// petFlourishDelay is the random pause before the next idle flourish.
func petFlourishDelay() time.Duration {
	span := petIdleFlourishMax - petIdleFlourishMin
	if span <= 0 {
		return petIdleFlourishMin
	}
	return petIdleFlourishMin + time.Duration(rand.Int63n(int64(span)))
}

// petBadgeKind picks the badge's colour and wording: error > waiting > running,
// the same precedence as the backend's ambient state.
func petBadgeKind(c petBadgeCounts) (string, int) {
	switch {
	case c.errors > 0:
		return "error", c.errors
	case c.waiting > 0:
		return "waiting", c.waiting
	case c.running > 0:
		return "running", c.running
	default:
		return "", 0
	}
}

// petBadgeLabel is the badge's text and accessible name.
func petBadgeLabel(m locales.Messages, kind string, count int) string {
	switch kind {
	case "error":
		return m.PsBadgeError(count)
	case "waiting":
		return m.PsBadgeWaiting(count)
	default:
		return m.PsBadgeRunning(count)
	}
}

// view draws the sprite window: a transparent root, the current frame (which
// drags the window), the session badge and, while the pointer is over the pet,
// the menu button.
func (w *petWindow) view(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	m := cat(c)
	loc := c.Locale()
	now := c.Now()

	w.mu.Lock()
	state := w.stateLocked(now)
	if w.anim.state != state {
		w.anim.set(state, now)
	}
	wait := w.anim.tick(now)
	col := w.anim.col
	frame := w.sheet.frameFor(state, col)
	scale := w.scale
	name := w.name
	errMsg := w.errMsg
	counts := w.counts
	overrideEnd := w.overrideEnd
	flourishAt := w.flourishAt
	w.mu.Unlock()

	// Wake up when the frame changes, when an interaction ends, and when the next
	// idle flourish is due — whichever comes first.
	next := now.Add(wait)
	if !overrideEnd.IsZero() && overrideEnd.Before(next) {
		next = overrideEnd
	}
	if !flourishAt.IsZero() && flourishAt.Before(next) {
		next = flourishAt
	}
	if d := next.Sub(now); d > 0 {
		c.After(d)
	}

	fw, fh := petScaledFrame(scale)
	ui.Box(c.Key("pet-window-root")).Fill().Center().Children(func() {
		if frame == nil {
			if errMsg != "" {
				ui.Text(c.Key("pet-error"), errMsg).FontSize(11).Wrap().Padding(10)
			} else {
				ui.Spinner(c.Key("pet-loading"))
			}
			return
		}
		sprite := ui.Image(c.Key("pet-sprite"), frame).
			Size(float32(fw), float32(fh)).
			Label(m.PsSpriteLabel(name)).
			Tooltip(name).
			DragWindow()
		hovered := sprite.Hovered()

		ui.Overlay(c, func() {
			if kind, count := petBadgeKind(counts); kind != "" {
				label := petBadgeLabel(m, kind, count)
				badge := ui.Badge(c.Key("pet-badge"), petBadgeText(kind, count)).
					Background(petBadgeColor(kind)).
					Tooltip(label).
					Label(label).
					Absolute().Right(1).Top(1)
				if badge.Clicked() {
					w.s.openPetPage(loc)
				}
			}
			if hovered {
				btn := ui.Box(c.Key("pet-menu")).Size(18, 18).Center().
					Background(c.Theme().Surface).
					Radius(9).
					Tooltip(m.PsMenu()).
					Label(m.PsMenu()).
					Absolute().Right(2).Bottom(2)
				btn.Children(func() {
					ui.Text(c, "⋯").FontSize(12).TextColor(c.Theme().TextMuted)
				})
				if btn.Clicked() {
					go w.popupMenu(loc)
				}
			}
		})
	})
}

// petBadgeText is the badge's visible text: the count, and an exclamation for an
// errored session so the state reads at a glance (upstream draws an icon).
func petBadgeText(kind string, count int) string {
	if kind == "error" {
		return fmt.Sprintf("!%d", count)
	}
	return fmt.Sprintf("%d", count)
}

// petBadgeColor is the badge's fill: red for errors, amber for waiting, blue
// while agents run (upstream's palette).
func petBadgeColor(kind string) ui.Color {
	switch kind {
	case "error":
		return ui.RGB(0xEF, 0x44, 0x44)
	case "waiting":
		return ui.RGB(0xF5, 0x9E, 0x0B)
	default:
		return ui.RGB(0x3B, 0x82, 0xF6)
	}
}

// raw is Backend.raw for reads whose absence is an empty state rather than an
// error: a missing config or manifest simply leaves the window on its defaults.
func (w *petWindow) raw(cmd string, body map[string]any) json.RawMessage {
	raw, err := w.s.B.raw(cmd, body)
	if err != nil {
		return nil
	}
	return raw
}

// petEventString reads the state or one-shot kind out of a pet:// payload: a bare
// JSON string upstream, or an object carrying it under kind / state.
func petEventString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		return firstNonEmpty(str(obj, "kind"), str(obj, "state"), str(obj, "payload"))
	}
	return ""
}
