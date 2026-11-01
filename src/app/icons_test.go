package app

// icons_test.go covers the shared icon button: its label is also its hover
// tooltip, so an icon-only control says what it does to a pointer user.

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestIconButtonShowsTooltipOnHover(t *testing.T) {
	s := conversationShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()

	const label = "Copy as Markdown"
	// The button's own label counts as text, so presence alone says nothing:
	// the tooltip adds one more entry.
	before := countText(tt.Texts(), label)
	rect, ok := tt.Find(label)
	if !ok {
		t.Fatalf("icon button %q not found", label)
	}
	tt.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.Advance(700 * time.Millisecond) // past the tooltip's dwell
	tt.Frame()
	if after := countText(tt.Texts(), label); after <= before {
		t.Fatalf("no tooltip after hovering %q (%d → %d); texts = %v", label, before, after, tt.Texts())
	}
	dumpFrame(t, tt, "icon-tooltip")
}

// countText counts exact matches of s in a text list.
func countText(texts []string, s string) int {
	n := 0
	for _, t := range texts {
		if t == s {
			n++
		}
	}
	return n
}
