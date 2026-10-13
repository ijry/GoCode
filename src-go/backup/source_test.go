package backup_test

// source.go coverage: a plaintext archive is prepared (referenced/owned),
// resolved to its manifest by a handle, and released (idempotent). Mirrors
// upstream plaintext roundtrip; the encrypted branch is deferred.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yoyocode/src-go/backup"
)

func plaintextArchive(t *testing.T, dir string) string {
	t.Helper()
	dbFile := filepath.Join(dir, "snap.db")
	if err := os.WriteFile(dbFile, []byte("DBBODY"), 0o644); err != nil {
		t.Fatal(err)
	}
	zip := filepath.Join(dir, "backup.zip")
	b, err := backup.NewArchiveBuilder(zip)
	if err != nil {
		t.Fatal(err)
	}
	m := backup.BackupManifest{
		FormatVersion:   backup.BackupFormatVersion,
		Kind:            backup.BackupKind,
		CreatedAt:       "2026-10-14T00:00:00Z",
		AppVersion:      "0.1.0",
		LatestMigration: backup.LatestMigration,
		Runtime:         "server",
		IncludesSecrets: true,
		ManagedSections: backup.AllSectionIDs(),
	}
	if err := b.AddFile(backup.DBEntry, dbFile, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Finish(m); err != nil {
		t.Fatal(err)
	}
	return zip
}

func TestPrepareResolveReleaseRoundtrip(t *testing.T) {
	dir := t.TempDir()
	zip := plaintextArchive(t, dir)
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// take_ownership: the transient upload is consumed into the prepared dir.
	prepared, err := backup.PrepareSource(zip, dataDir, "", true)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepared.SourceID == nil {
		t.Fatalf("expected a source id handle")
	}
	if !prepared.Preview.Compatible {
		t.Fatalf("our own backup must preview compatible: %+v", prepared.Preview)
	}
	if prepared.Preview.Manifest == nil || prepared.Preview.Manifest.Kind != backup.BackupKind {
		t.Fatalf("preview must carry the manifest: %+v", prepared.Preview)
	}
	id := *prepared.SourceID

	resolved, err := backup.ResolvePreparedZip(dataDir, id)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if want := filepath.Join(dataDir, backup.PreparedDir, id, "plain.zip"); resolved != want {
		t.Fatalf("resolved = %q, want %q (owned copy)", resolved, want)
	}
	if _, err := os.Stat(zip); err == nil {
		t.Fatalf("take_ownership must consume the original upload")
	}

	// Release removes the prepared copy (never an archive the caller owns).
	ok, err := backup.ReleaseSource(dataDir, id)
	if err != nil || !ok {
		t.Fatalf("release = (%v,%v), want (true,nil)", ok, err)
	}
	if _, err := os.Stat(resolved); err == nil {
		t.Fatalf("release must delete the prepared plain.zip")
	}
	if _, err := backup.ResolvePreparedZip(dataDir, id); err == nil {
		t.Fatalf("resolve after release must fail")
	}
	again, err := backup.ReleaseSource(dataDir, id)
	if err != nil || again {
		t.Fatalf("second release = (%v,%v), want (false,nil)", again, err)
	}
}

func TestSourceRejectsNonHexID(t *testing.T) {
	dataDir := t.TempDir()
	for _, bad := range []string{"", "not-hex", "../escape", strings.Repeat("z", 32)} {
		if _, err := backup.ResolvePreparedZip(dataDir, bad); !errors.Is(err, backup.ErrInvalidSourceID) {
			t.Fatalf("resolve(%q) err = %v, want ErrInvalidSourceID", bad, err)
		}
		if _, err := backup.ReleaseSource(dataDir, bad); !errors.Is(err, backup.ErrInvalidSourceID) {
			t.Fatalf("release(%q) err = %v, want ErrInvalidSourceID", bad, err)
		}
	}
}
