package app

// selector_row.go renders a compact row of pill buttons for a single
// categorical choice (mode or model). The agent's live snapshot advertises
// the available options; clicking a pill fires onPick with the option's
// value, which the composer dispatches as the matching acp_set_* call.

import (
	"github.com/egoist/mygo/ui"
)

// selectorOption is one entry in a selector row: the wire value (sent to the
// backend) and the human label (shown in the pill).
type selectorOption struct {
	value string
	label string
}

// viewSelectorRow draws a muted left-hand label followed by one pill per
// option. The active pill is a static Row with a pressed-tone background
// (no click); the others are Buttons so they get the focus + hover affordance.
// A row of 0 or 1 options is suppressed — nothing to choose.
func (s *Shell) viewSelectorRow(c *ui.Context, key, label, current string, opts []selectorOption, onPick func(string)) {
	if len(opts) <= 1 {
		return
	}
	t := c.Theme()
	ui.Row(c.Key(key)).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		if label != "" {
			ui.Text(c, label).FontSize(11).TextColor(t.TextMuted)
		}
		for _, o := range opts {
			pillKey := key + "-pill-" + o.value
			if o.value == current {
				ui.Row(c.Key(pillKey)).Padding(3, 10, 3, 10).Radius(10).
					Background(t.SurfacePressed).Children(func() {
					ui.Text(c, o.label).FontSize(11).TextColor(t.Text)
				})
				continue
			}
			if ui.Button(c.Key(pillKey), o.label).Clicked() {
				onPick(o.value)
			}
		}
	})
}
