package app

import (
	"encoding/json"
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

	for _, want := range []string{"New chat", "Automations", "To-dos", "Settings", "Canvas", "Appearance: Dark",
		// The conversation exports live in the header; the palette lists them too.
		"Copy as Markdown", "Export as HTML"} {
		if !tt.HasText(want) {
			t.Errorf("palette missing %q; texts = %v", want, tt.Texts())
		}
	}

	dumpFrame(t, tt, "palette-export")

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

// TestPaletteSearchesConversations covers ⌘K finding a chat by name: the catalog
// only lists commands, so a conversation used to be unreachable from search.
func TestPaletteSearchesConversations(t *testing.T) {
	s := conversationShellForTest(t)
	s.paletteOpen = true
	view := func(c *ui.Context) { s.viewCommandPalette(c) }
	tt := ui.NewTester(view, 900, 600)

	// With no query the palette lists commands only — not every conversation.
	if tt.HasText("Fix the parser") {
		t.Fatalf("the empty palette listed a conversation")
	}

	s.paletteQuery = "parser"
	tt.Frame()
	if !tt.HasText("Fix the parser") {
		t.Fatalf("conversation not found by title; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "palette-conversation")

	// Running the row opens the conversation as a tab.
	if err := tt.Click("Fix the parser"); err != nil {
		t.Fatalf("click conversation row: %v", err)
	}
	s.mu.Lock()
	open := len(s.openTabs)
	s.mu.Unlock()
	if open != 1 {
		t.Fatalf("openTabs = %d after picking the conversation, want 1", open)
	}
}

// TestPaletteSearchesEveryFolder covers the global scope: the palette reads the
// cached all-convs list, so a conversation living in another folder is findable
// without switching folders first.
func TestPaletteSearchesEveryFolder(t *testing.T) {
	s := conversationShellForTest(t)
	s.paletteOpen = true
	// A conversation in a different folder (the open one is folder 1).
	s.cache["all-convs"] = json.RawMessage(`[
		{"id":1,"title":"Fix the parser","agent_type":"claude_code","folder_id":1},
		{"id":7,"title":"Rewrite the parser tests","agent_type":"codex","folder_id":2}
	]`)
	s.paletteQuery = "parser"

	tt := ui.NewTester(func(c *ui.Context) { s.viewCommandPalette(c) }, 900, 600)
	for _, want := range []string{"Fix the parser", "Rewrite the parser tests"} {
		if !tt.HasText(want) {
			t.Fatalf("global search missed %q; texts = %v", want, tt.Texts())
		}
	}
}
