package app

// composer_mention_test.go covers the @-file mention menu: the query the draft
// tail yields, the filtered list, and writing the chosen path back.

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestMentionQueryOf(t *testing.T) {
	cases := []struct {
		text  string
		want  string
		at    int
		ok    bool
	}{
		{"look at @src", "src", 8, true},
		{"@", "", 0, true},
		{"(@main", "main", 1, true},
		{"email me@host", "", 0, false},   // not the start of a word
		{"a @b c", "", 0, false},          // the query ended with a space
		{"no mention", "", 0, false},
	}
	for _, c := range cases {
		got, at, ok := mentionQueryOf(c.text)
		if got != c.want || ok != c.ok || (ok && at != c.at) {
			t.Fatalf("mentionQueryOf(%q) = (%q,%d,%v), want (%q,%d,%v)",
				c.text, got, at, ok, c.want, c.at, c.ok)
		}
	}
}

func TestComposerMentionMenuPicksFile(t *testing.T) {
	s := conversationShellForTest(t)
	s.composer = "please read @ma"
	// The folder's file tree is seeded under the folder path.
	s.cache["mention:/tmp/demo"] = json.RawMessage(`[
		{"kind":"dir","name":"main_dir","path":"main_dir","children":[]},
		{"kind":"file","name":"main.go","path":"main.go"},
		{"kind":"file","name":"README.md","path":"README.md"}]`)

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	// Both matches show; README.md is filtered out.
	if !tt.HasText("main.go") || tt.HasText("README.md") {
		t.Fatalf("menu did not filter; texts = %v", tt.Texts())
	}
	dumpFrame(t, tt, "composer-mention")
	if err := tt.Click("main.go"); err != nil {
		t.Fatalf("click mention: %v", err)
	}
	if want := "please read @main.go "; s.composer != want {
		t.Fatalf("composer = %q, want %q", s.composer, want)
	}
}

func TestComposerMentionMenuEmptyState(t *testing.T) {
	s := conversationShellForTest(t)
	s.composer = "@zzz"
	s.cache["mention:/tmp/demo"] = json.RawMessage(`[{"kind":"file","name":"main.go","path":"main.go"}]`)

	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()
	if !tt.HasText("no matching files") {
		t.Fatalf("empty state missing; texts = %v", tt.Texts())
	}
}
