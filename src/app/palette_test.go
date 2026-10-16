package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestCommandPalette covers the ⌘K palette: it lists the command catalog, the
// query narrows the list, and running a command navigates and closes the modal.
func TestCommandPalette(t *testing.T) {
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.paletteOpen = true

	view := func(c *ui.Context) { s.viewCommandPalette(c) }
	tt := ui.NewTester(view, 900, 600)

	for _, want := range []string{"New chat", "Automations", "To-dos", "Settings", "Canvas", "Appearance: Dark"} {
		if !tt.HasText(want) {
			t.Errorf("palette missing %q; texts = %v", want, tt.Texts())
		}
	}

	// The query narrows the catalog.
	s.paletteQuery = "canvas"
	tt.Frame()
	if !tt.HasText("Canvas") {
		t.Errorf("filtered palette missing Canvas; texts = %v", tt.Texts())
	}
	if tt.HasText("New chat") {
		t.Errorf("filtered palette should not list New chat; texts = %v", tt.Texts())
	}

	// Running a command navigates and closes the palette.
	s.paletteQuery = ""
	tt.Frame()
	if err := tt.Click("Settings: Agents"); err != nil {
		t.Fatalf("click command: %v", err)
	}
	tt.Frame()
	if s.section != "settings" || s.settingsPage != "agents" {
		t.Errorf("command did not navigate: section=%q page=%q", s.section, s.settingsPage)
	}
	if s.paletteOpen {
		t.Error("running a command should close the palette")
	}
}
