package app

// conversation_drop_test.go covers dropping files onto the composer: mygo 0.6
// hands dropped paths to the element that asks (DroppedFiles), and each one is
// staged as an attachment.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestComposerDropsFiles(t *testing.T) {
	s := conversationShellForTest(t)
	tt := ui.NewTester(s.View, 1240, 820)
	tt.Frame()

	f := filepath.Join(t.TempDir(), "dropped.txt")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Inside the composer dock (the transcript sits above it).
	if !tt.DropFiles(700, 690, f) {
		t.Fatalf("no element took the drop; texts = %v", tt.Texts())
	}
	tt.Frame()
	if !tt.HasText("dropped.txt") {
		t.Fatalf("dropped file not staged; texts = %v", tt.Texts())
	}
	if got := len(s.stagedAttachments()); got != 1 {
		t.Fatalf("staged attachments = %d, want 1", got)
	}
	dumpFrame(t, tt, "composer-drop-attachment")
}
