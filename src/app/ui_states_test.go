package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestSharedStates locks the shared empty / loading / error placeholders: the
// error state shows the message, the backend's actionable hint and a Retry that
// drops the cache key; the empty and loading states show their text.
func TestSharedStates(t *testing.T) {
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.cache["demo"] = json.RawMessage(`{}`)
	s.errs["demo"] = "connect failed: acme"
	s.errHints["demo"] = "Open Settings → Agents"

	view := func(c *ui.Context) {
		ui.Column(c).Children(func() {
			s.errorState(c, "demo", nil)
			emptyState(c, iconInfo, "Nothing here", "Do the thing to see it.")
			loadingState(c, "")
		})
	}
	tt := ui.NewTester(view, 700, 400)
	for _, want := range []string{
		"connect failed: acme", "Open Settings → Agents", "Retry",
		"Nothing here", "Do the thing to see it.", "Loading…",
	} {
		if !tt.HasText(want) {
			t.Errorf("shared states missing %q; texts = %v", want, tt.Texts())
		}
	}

	// Retry drops the cache key so the next ensure refetches it.
	if err := tt.Click("Retry"); err != nil {
		t.Fatalf("click Retry: %v", err)
	}
	tt.Frame()
	if _, ok := s.cache["demo"]; ok {
		t.Error("Retry should have invalidated the cache key")
	}
}
