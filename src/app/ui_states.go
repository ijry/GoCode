package app

// ui_states.go — the shared empty / loading / error placeholders. Panels keep
// their own wording; the box, spacing and colors live here, so every fetch that
// is pending, empty or failed looks the same. A failure also offers a Retry
// (and, when the backend gave one, the actionable hint stored for its key),
// which the ad-hoc "backend error: …" text never did.

import "github.com/egoist/mygo/ui"

// loadingState is the shared "fetching" placeholder. An empty label means
// "Loading…".
func loadingState(c *ui.Context, label string) {
	if label == "" {
		label = cat(c).LoadingLabel()
	}
	ui.Text(c, label).FontSize(12).TextColor(c.Theme().TextMuted)
}

// emptyState is the shared empty placeholder: an optional glyph, a bold title
// and a muted hint, in a surface box. Panels pass their own title and hint.
func emptyState(c *ui.Context, icon *ui.SVG, title, hint string) {
	t := c.Theme()
	ui.Column(c).Gap(6).Padding(14).Background(t.Surface).Radius(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if icon != nil {
				ui.Icon(c, icon).Width(15).Height(15).TextColor(t.TextMuted)
			}
			ui.Text(c, title).FontSize(13).Bold()
		})
		if hint != "" {
			ui.Text(c, hint).FontSize(12).TextColor(t.TextMuted).Wrap()
		}
	})
}

// errorState is the shared failed-fetch placeholder: the message, the backend's
// actionable hint when there is one, and a Retry that drops the cache key. When
// a panel must drop several keys it passes onRetry; nil drops key alone.
func (s *Shell) errorState(c *ui.Context, key string, onRetry func()) {
	t := c.Theme()
	ui.Column(c.Key("err-" + key)).Gap(6).Padding(12).Background(t.Danger.Alpha(0.10)).Radius(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconAlert).Width(15).Height(15).TextColor(t.Danger)
			ui.Text(c, s.errFor(key)).FontSize(12).TextColor(t.Danger).Grow(1).Wrap()
		})
		if h := s.errHintFor(key); h != "" {
			ui.Text(c, h).FontSize(11).TextColor(t.TextMuted).Wrap()
		}
		if ui.Button(c.Key("err-retry-"+key), cat(c).ActionRetry()).Clicked() {
			if onRetry != nil {
				onRetry()
			} else {
				s.invalidate(key)
			}
		}
	})
}
