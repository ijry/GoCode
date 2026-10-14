package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAPIErrorFriendly(t *testing.T) {
	e := newAPIError("acp_connect",
		[]byte(`{"code":"dependency_missing","message":"failed to start agent claude_code: context deadline exceeded"}`))
	if e.Error() != "failed to start agent claude_code: context deadline exceeded" {
		t.Fatalf("Error() = %q (must not leak the JSON envelope)", e.Error())
	}
	if e.hint() == "" {
		t.Fatalf("dependency_missing should carry an actionable hint")
	}
	// A non-JSON body falls back to "cmd: body".
	if got := newAPIError("x", []byte("boom")).Error(); got != "x: boom" {
		t.Fatalf("fallback = %q", got)
	}
	// A detail is appended after the message.
	if got := newAPIError("x", []byte(`{"message":"m","detail":"d"}`)).Error(); got != "m — d" {
		t.Fatalf("detail = %q", got)
	}
}

func TestErrorBannerRendersFriendlyMessage(t *testing.T) {
	s := conversationShellForTest(t)
	s.actionMsg = ""
	s.errorMsg = "failed to start agent claude_code: context deadline exceeded"
	s.errorHint = "The agent runtime could not start. Open Settings → Agents to install it, then retry."

	tt := ui.NewTester(s.View, 1240, 820)
	if !tt.HasText(s.errorMsg) {
		t.Fatalf("error message not rendered; texts = %v", tt.Texts())
	}
	if !tt.HasText(s.errorHint) {
		t.Fatalf("error hint not rendered; texts = %v", tt.Texts())
	}
}

func TestSetActionClearsError(t *testing.T) {
	s := conversationShellForTest(t)
	s.mu.Lock()
	s.errorMsg = "boom"
	s.errorHint = "hint"
	s.mu.Unlock()

	s.setAction("connecting…", ui.Services{})
	if s.errorMsg != "" || s.errorHint != "" {
		t.Fatalf("a fresh status must clear the stale error banner")
	}
}
