package backup_test

// Roundtrip coverage for the export engine: build a real backup of a migrated
// SQLite DB + a couple of managed sections, then read/validate/extract/verify it
// the way the restore path will. Mirrors upstream's plaintext roundtrip test.

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"gocode/src-go/backup"
	"gocode/src-go/db"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { d.Conn.Close() })
	if _, err := d.Conn.Exec("CREATE TABLE IF NOT EXISTS probe(x TEXT); INSERT INTO probe VALUES('hi')"); err != nil {
		t.Fatalf("seed probe: %v", err)
	}
	return d.Conn
}

func TestCreateBackupRoundtrip(t *testing.T) {
	conn := openTestDB(t)

	base := t.TempDir()
	dataDir := filepath.Join(base, "data")
	live := filepath.Join(base, "live")
	// Two Dir sections + one File section with real content; plus a `.tmp`
	// scaffolding file that MUST be excluded.
	mustWrite(t, filepath.Join(live, "uploads", "att.txt"), "attachment")
	mustWrite(t, filepath.Join(live, "uploads", ".tmp", "skip.txt"), "nope")
	mustWrite(t, filepath.Join(live, "pets", "sprite.png"), "png")
	mustWrite(t, filepath.Join(live, "tokens.json"), `{"token":"secret"}`)

	roots := backup.NewLiveRoots(func(id string) string { return filepath.Join(live, id) })

	dest := filepath.Join(base, "backup.zip")
	var phases []backup.BackupPhase
	manifest, err := backup.CreateBackup(conn, "op1", dataDir, "0.1.0", roots, backup.Options{}, dest,
		func(p backup.Progress) { phases = append(phases, p.Phase) })
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Manifest shape.
	if manifest.FormatVersion != backup.BackupFormatVersion || manifest.Kind != backup.BackupKind {
		t.Fatalf("manifest id/version wrong: %+v", manifest)
	}
	if len(manifest.ManagedSections) != len(backup.AllSectionIDs()) {
		t.Fatalf("managed sections must declare the full table: %v", manifest.ManagedSections)
	}
	assertHasEntry(t, manifest, backup.DBEntry)
	assertHasEntry(t, manifest, "uploads/att.txt")
	assertHasEntry(t, manifest, "pets/sprite.png")
	assertHasEntry(t, manifest, "tokens.json")
	for _, e := range manifest.Entries {
		if containsSub(e.Path, ".tmp") {
			t.Fatalf("excluded .tmp entry leaked into manifest: %s", e.Path)
		}
	}
	// Progress phases emitted.
	assertPhase(t, phases, backup.PhaseSnapshotting)
	assertPhase(t, phases, backup.PhaseDone)

	// Read the archived manifest back.
	reread, err := backup.ReadManifest(dest)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if len(reread.Entries) != len(manifest.Entries) {
		t.Fatalf("reread manifest entry count mismatch")
	}
	if err := backup.ValidateManifest(&reread); err != nil {
		t.Fatalf("validate manifest: %v", err)
	}

	// Extract + verify checksums.
	out := filepath.Join(base, "out")
	if err := backup.ExtractAll(dest, out, &reread, nil); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got := readFile(t, filepath.Join(out, "uploads", "att.txt")); got != "attachment" {
		t.Fatalf("upload content mismatch: %q", got)
	}
	if _, err := os.Stat(filepath.Join(out, "uploads", ".tmp", "skip.txt")); err == nil {
		t.Fatalf("excluded .tmp file must not be in the archive")
	}
	if err := backup.VerifyChecksums(out, &reread); err != nil {
		t.Fatalf("verify checksums: %v", err)
	}
	// The extracted DB snapshot is a real SQLite file.
	if hdr := readHeader(t, filepath.Join(out, backup.DBEntry)); hdr != "SQLite format 3\x00" {
		t.Fatalf("db snapshot is not a SQLite file: got magic %q", hdr)
	}

	// Tamper → verify fails.
	mustWrite(t, filepath.Join(out, "uploads", "att.txt"), "tampered")
	if err := backup.VerifyChecksums(out, &reread); err == nil {
		t.Fatalf("tampered file must fail checksum verification")
	}
}

func TestCreateBackupRejectsDeferredOptions(t *testing.T) {
	conn := openTestDB(t)
	base := t.TempDir()
	roots := backup.NewLiveRoots(func(id string) string { return filepath.Join(base, "live", id) })
	if _, err := backup.CreateBackup(conn, "op", base, "0.1.0", roots, backup.Options{Passphrase: "x"}, filepath.Join(base, "b.zip"), nil); err == nil {
		t.Fatalf("passphrase request must not silently produce a plaintext archive")
	}
	if _, err := backup.CreateBackup(conn, "op", base, "0.1.0", roots, backup.Options{IncludeExternalTranscripts: true}, filepath.Join(base, "b2.zip"), nil); err == nil {
		t.Fatalf("external transcripts request must not be silently dropped")
	}
}

// helpers

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func readHeader(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, 16)
	if _, err := f.Read(buf); err != nil {
		t.Fatal(err)
	}
	return string(buf)
}

func assertHasEntry(t *testing.T, m backup.BackupManifest, path string) {
	t.Helper()
	for _, e := range m.Entries {
		if e.Path == path {
			return
		}
	}
	t.Fatalf("manifest missing entry %q", path)
}

func assertPhase(t *testing.T, got []backup.BackupPhase, want backup.BackupPhase) {
	t.Helper()
	for _, p := range got {
		if p == want {
			return
		}
	}
	t.Fatalf("phase %q not emitted (got %v)", want, got)
}

func containsSub(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
