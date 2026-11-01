package app

// welcome_tip_test.go covers the empty-state tip pill (upstream's WelcomeTip):
// with no conversation selected, the hero shows a stable feature tip.

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestWelcomeTipShown(t *testing.T) {
	s := conversationShellForTest(t)
	s.selectedConv = "" // no conversation → the welcome hero
	s.welcomeTip = 0    // deterministic: the @-mention tip

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if !tt.HasText("Type @ in the composer to reference a file.") {
		t.Fatalf("welcome tip missing; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "welcome-hero")
}
