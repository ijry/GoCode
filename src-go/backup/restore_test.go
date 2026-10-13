package backup_test

// Restore engine roundtrip: stage a prepared backup, apply it (DB + sections
// swapped, safety snapshot taken), roll back to the snapshot (revert), and
// discard a staged-but-unapplied restore. Mirrors upstream restore tests.

import (
	"os"
	"path/filepath"
	"testing"

	"yoyocode/src-go/backup"
)

func fileExistsLocal(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func stageZipInto(t *testing.T, zip, dataDir string) backup.StagedRestore {
	t.Helper()
	prepared, err := backup.PrepareSource(zip, dataDir, "", true)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	id := *prepared.SourceID
	pzip, err := backup.ResolvePreparedZip(dataDir, id)
	if err != nil {
		t.Fatalf("resolve prepared: %v", err)
	}
	staged, err := backup.StageRestore(pzip, dataDir, "op-"+id[:8], nil)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	_, _ = backup.ReleaseSource(dataDir, id) // staging consumed the source
	return staged
}

func firstSnapshotID(t *testing.T, dataDir string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dataDir, backup.SafetyDir))
	if err != nil {
		t.Fatalf("read safety dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			return e.Name()
		}
	}
	t.Fatalf("no safety snapshot under %s", backup.SafetyDir)
	return ""
}

func TestStageApplyRollbackRestore(t *testing.T) {
	conn := openTestDB(t)
	base := t.TempDir()
	srcData := filepath.Join(base, "src")
	srcLive := filepath.Join(base, "srclive")
	mustWrite(t, filepath.Join(srcLive, "uploads", "a.txt"), "A")
	mustWrite(t, filepath.Join(srcLive, "uploads", "sub", "b.txt"), "B")
	zip := filepath.Join(base, "backup.zip")
	if _, err := backup.CreateBackup(conn, "op", srcData, "0.1.0",
		backup.NewLiveRoots(func(id string) string { return filepath.Join(srcLive, id) }),
		backup.Options{}, zip, nil); err != nil {
		t.Fatalf("create backup: %v", err)
	}

	// Fresh target with a seeded DB + a stale uploads file.
	tgt := filepath.Join(base, "tgt")
	tgtLive := filepath.Join(base, "tgtlive")
	if err := os.MkdirAll(tgt, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(tgt, "yoyocode.db"), "SEED")
	mustWrite(t, filepath.Join(tgtLive, "uploads", "stale.txt"), "OLD")
	tgtRoots := func() backup.LiveRoots {
		return backup.NewLiveRoots(func(id string) string { return filepath.Join(tgtLive, id) })
	}

	// Stage → marker committed.
	stageZipInto(t, zip, tgt)
	if !fileExistsLocal(filepath.Join(tgt, backup.PendingMarker)) {
		t.Fatalf("pending marker not committed")
	}
	// A second stage must be refused while one is pending.
	if _, err := backup.StageRestore(zip, tgt, "op-again", nil); err == nil {
		t.Fatalf("staging while pending should be refused")
	}

	// Apply → DB + sections swapped, snapshot taken.
	applied, err := backup.ApplyPendingRestore(tgt, tgtRoots())
	if err != nil || !applied.Applied {
		t.Fatalf("apply = (%+v,%v), want applied", applied, err)
	}
	if fileExistsLocal(filepath.Join(tgt, backup.PendingMarker)) {
		t.Fatalf("marker must be gone after apply")
	}
	if got := readFile(t, filepath.Join(tgt, "yoyocode.db")); got == "SEED" {
		t.Fatalf("live DB must be replaced by the snapshot")
	}
	if readFile(t, filepath.Join(tgtLive, "uploads", "a.txt")) != "A" {
		t.Fatalf("a.txt not restored")
	}
	if readFile(t, filepath.Join(tgtLive, "uploads", "sub", "b.txt")) != "B" {
		t.Fatalf("b.txt not restored")
	}
	if fileExistsLocal(filepath.Join(tgtLive, "uploads", "stale.txt")) {
		t.Fatalf("AlwaysReplace must clear the stale uploads file")
	}
	if got := readFile(t, filepath.Join(applied.SafetySnapshot, "db", "yoyocode.db")); got != "SEED" {
		t.Fatalf("safety snapshot must hold the pre-restore DB, got %q", got)
	}

	// Rollback → commit a marker at the snapshot; apply again reverts.
	snapID := firstSnapshotID(t, tgt)
	if _, err := backup.RollbackToSnapshot(tgt, snapID); err != nil {
		t.Fatalf("rollback stage: %v", err)
	}
	rb, err := backup.ApplyPendingRestore(tgt, tgtRoots())
	if err != nil || !rb.Applied {
		t.Fatalf("rollback apply = (%+v,%v)", rb, err)
	}
	if got := readFile(t, filepath.Join(tgt, "yoyocode.db")); got != "SEED" {
		t.Fatalf("rollback must restore the pre-restore DB, got %q", got)
	}
	if readFile(t, filepath.Join(tgtLive, "uploads", "stale.txt")) != "OLD" {
		t.Fatalf("rollback must restore the pre-restore uploads")
	}
	if fileExistsLocal(filepath.Join(tgtLive, "uploads", "a.txt")) {
		t.Fatalf("rollback must remove the restored a.txt")
	}
}

func TestDiscardPendingRestore(t *testing.T) {
	conn := openTestDB(t)
	base := t.TempDir()
	srcData := filepath.Join(base, "src")
	srcLive := filepath.Join(base, "srclive")
	_ = os.MkdirAll(srcLive, 0o755)
	zip := filepath.Join(base, "backup.zip")
	if _, err := backup.CreateBackup(conn, "op", srcData, "0.1.0",
		backup.NewLiveRoots(func(id string) string { return filepath.Join(srcLive, id) }),
		backup.Options{}, zip, nil); err != nil {
		t.Fatalf("create backup: %v", err)
	}

	tgt := filepath.Join(base, "tgt")
	_ = os.MkdirAll(tgt, 0o700)
	staged := stageZipInto(t, zip, tgt)

	ok, err := backup.DiscardPendingRestore(tgt)
	if err != nil || !ok {
		t.Fatalf("discard = (%v,%v), want (true,nil)", ok, err)
	}
	if fileExistsLocal(filepath.Join(tgt, backup.PendingMarker)) {
		t.Fatalf("marker must be gone")
	}
	if fileExistsLocal(staged.StagingDir) {
		t.Fatalf("staging dir must be removed")
	}
	again, err := backup.DiscardPendingRestore(tgt)
	if err != nil || again {
		t.Fatalf("discard again = (%v,%v), want (false,nil)", again, err)
	}
}
