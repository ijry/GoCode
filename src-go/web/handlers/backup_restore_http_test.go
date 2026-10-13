package handlers_test

// backup_restore_stage / backup_discard_pending over HTTP: prepare + stage a
// backup (→ needsRestart), refuse a second stage while one is pending, then
// discard it. Isolated via YOYOCODE_HOME.

import (
	"testing"

	"yoyocode/src-go/backup"
)

func TestBackupRestoreStageAndDiscardHTTP(t *testing.T) {
	t.Setenv("YOYOCODE_HOME", t.TempDir())
	srv := newTestServer(t)

	// Prepare a source from an uploaded backup.
	uploadID := uploadBackupBytes(t, srv.URL, buildBackupZipBytes(t))
	pc, pout := call(t, srv, "backup_prepare_source", map[string]any{"uploadId": uploadID})
	if pc != 200 {
		t.Fatalf("prepare status %d: %v", pc, pout)
	}
	sourceID, _ := pout["sourceId"].(string)

	// Stage it → needs_restart + a staging dir + manifest.
	sc, sout := call(t, srv, "backup_restore_stage", map[string]any{"sourceId": sourceID})
	if sc != 200 {
		t.Fatalf("stage status %d: %v", sc, sout)
	}
	if sout["needsRestart"] != true {
		t.Fatalf("stage should report needsRestart: %v", sout)
	}
	if d, ok := sout["restartDelayMs"].(float64); !ok || d <= 0 {
		t.Fatalf("stage should report a positive restartDelayMs: %v", sout)
	}
	staged, _ := sout["staged"].(map[string]any)
	if staged == nil || staged["stagingDir"] == "" {
		t.Fatalf("stage should carry a staging dir: %v", sout)
	}
	if m, ok := staged["manifest"].(map[string]any); !ok || m["kind"] != backup.BackupKind {
		t.Fatalf("staged manifest wrong: %v", staged)
	}

	// A second stage (different source) must be refused: one pending at a time.
	uploadID2 := uploadBackupBytes(t, srv.URL, buildBackupZipBytes(t))
	_, p2 := call(t, srv, "backup_prepare_source", map[string]any{"uploadId": uploadID2})
	sid2, _ := p2["sourceId"].(string)
	if sc2, _ := call(t, srv, "backup_restore_stage", map[string]any{"sourceId": sid2}); sc2 == 200 {
		t.Fatalf("staging while a restore is pending should be refused")
	}

	// Discard → true, then false.
	if c, ok := callBool(t, srv, "backup_discard_pending", nil); c != 200 || !ok {
		t.Fatalf("discard = (%d,%v), want (200,true)", c, ok)
	}
	if c, ok := callBool(t, srv, "backup_discard_pending", nil); c != 200 || ok {
		t.Fatalf("discard again = (%d,%v), want (200,false)", c, ok)
	}

	// Rollback with a bogus snapshot id → error.
	if rc, _ := call(t, srv, "backup_rollback", map[string]any{"snapshotId": "does-not-exist"}); rc == 200 {
		t.Fatalf("rollback of a missing snapshot should fail")
	}
}
