package app

// custom_skill_feedback_test.go covers the custom-skill create/delete paths: a
// rejected write must say why and must not throw away what the user typed. Both
// used to discard the backend error and clear the input, so a bad id looked like
// the button did nothing.

import (
	"testing"
	"time"
)

// waitForQueuedToast polls the queue directly. Without a View nothing drains it,
// which is what makes this deterministic.
func waitForQueuedToast(s *Shell, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.toastMu.Lock()
		if len(s.toastQueue) > 0 {
			title := s.toastQueue[0].Title
			s.toastMu.Unlock()
			return title
		}
		s.toastMu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	return ""
}

func TestCreateCustomSkillFailureKeepsInputAndToasts(t *testing.T) {
	s := New(newBackendForTest(t))
	s.stream = newStreamForTest()
	s.newSkillID = "bad id!"

	s.createCustomSkill("bad id!") // invalid id → the backend rejects it

	if msg := waitForQueuedToast(s, 3*time.Second); msg == "" {
		t.Fatalf("a rejected create raised no toast")
	}
	if s.newSkillID != "bad id!" {
		t.Fatalf("newSkillID = %q, want the typed value kept", s.newSkillID)
	}
}
