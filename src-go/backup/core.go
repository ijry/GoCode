package backup

// CreateBackup orchestrates a backup: consistent DB snapshot (VACUUM INTO) →
// ZIP payload → atomic delivery to dest. Mirrors upstream core.rs
// create_backup_core. Encrypted (.codegbak-equivalent) delivery and external
// agent-transcript packing are deferred (see errors below); the plaintext,
// managed-sections path is complete and round-trips.

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupExportTmpDir is the scratch root under the data dir (upstream
// EXPORT_TMP_DIR ".codeg-backup-tmp", rebranded). Never the system temp: a
// multi-GB archive staged in tmpfs would live in RAM.
const BackupExportTmpDir = ".yoyocode-backup-tmp"

// ErrEncryptedDeferred marks that encrypted (.passphrase) backups are not yet
// ported; the crypto envelope is a separate slice.
var ErrEncryptedDeferred = errors.New("encrypted backups are not supported yet")

// ErrExternalDeferred marks that external agent-transcript packing is not yet
// ported (external.rs is a separate slice).
var ErrExternalDeferred = errors.New("external transcript packing is not supported yet")

// Options shapes a backup.
type Options struct {
	IncludeExternalTranscripts bool
	Passphrase                 string
}

// CreateBackup builds a backup archive at dest and returns its manifest. emit
// (may be nil) reports progress on the backup://progress channel. opID keys the
// progress events.
func CreateBackup(conn *sql.DB, opID, dataDir, appVersion string, roots LiveRoots, opts Options, dest string, emit func(Progress)) (BackupManifest, error) {
	// Reject the deferred options up front: silently producing a plaintext
	// archive for a passphrase request would be a security footgun, and
	// silently dropping external transcripts would be a data-loss footgun.
	if opts.Passphrase != "" {
		return BackupManifest{}, ErrEncryptedDeferred
	}
	if opts.IncludeExternalTranscripts {
		return BackupManifest{}, ErrExternalDeferred
	}

	emitPhase := func(phase BackupPhase, processed uint64, total *uint64) {
		if emit != nil {
			emit(Progress{OpID: opID, Phase: phase, ProcessedBytes: processed, TotalBytes: total})
		}
	}

	emitPhase(PhaseSnapshotting, 0, nil)

	scratch := filepath.Join(dataDir, BackupExportTmpDir)
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		return BackupManifest{}, err
	}
	work, err := os.MkdirTemp(scratch, "build-")
	if err != nil {
		return BackupManifest{}, err
	}
	defer os.RemoveAll(work)

	dbSnap := filepath.Join(work, "yoyocode.db")
	if err := snapshotDB(conn, dbSnap); err != nil {
		return BackupManifest{}, err
	}

	manifest := BackupManifest{
		FormatVersion:               BackupFormatVersion,
		Kind:                        BackupKind,
		CreatedAt:                   time.Now().UTC().Format(time.RFC3339),
		AppVersion:                  appVersion,
		LatestMigration:             LatestMigration,
		Runtime:                     "server",
		IncludesExternalTranscripts: false,
		IncludesSecrets:             true,
		ManagedSections:             AllSectionIDs(),
	}

	emitPhase(PhaseArchiving, 0, nil)
	zipTmp := filepath.Join(work, "payload.zip")
	builder, err := NewArchiveBuilder(zipTmp)
	if err != nil {
		return BackupManifest{}, err
	}
	prog := func(_ string, processed uint64) { emitPhase(PhaseArchiving, processed, nil) }

	if err := builder.AddFile(DBEntry, dbSnap, prog); err != nil {
		return BackupManifest{}, err
	}
	for _, sec := range ManagedSections {
		live, ok := roots.Path(sec.ID)
		if !ok || live == "" {
			continue
		}
		if sec.Kind == SectionDir {
			if err := builder.AddDir(sec.ID, live, IsExcludedSectionEntry, prog); err != nil {
				return BackupManifest{}, err
			}
		} else {
			if _, serr := os.Stat(live); serr == nil {
				if err := builder.AddFile(sec.ID, live, prog); err != nil {
					return BackupManifest{}, err
				}
			}
		}
	}

	manifest, err = builder.Finish(manifest)
	if err != nil {
		return BackupManifest{}, err
	}

	// Deliver into dest atomically (sibling .part → rename).
	if err := deliver(zipTmp, dest); err != nil {
		return BackupManifest{}, err
	}
	total := manifest.TotalBytes()
	emitPhase(PhaseDone, total, &total)
	return manifest, nil
}

// deliver writes the finished payload to dest atomically. A passphrase-selects
// the encrypted envelope, which is not yet ported.
func deliver(zipTmp, dest string) error {
	if dest == "" {
		return fmt.Errorf("destination path required")
	}
	if dir := filepath.Dir(dest); dir != "" {
		_ = os.MkdirAll(dir, 0o700)
	}
	part := dest + ".part"
	_ = os.Remove(part)
	in, err := os.Open(zipTmp)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(part)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(part)
		return err
	}
	return os.Rename(part, dest)
}

// snapshotDB produces a consistent single-file copy via VACUUM INTO, sidestepping
// WAL sidecars. The destination must not exist.
func snapshotDB(conn *sql.DB, dest string) error {
	if conn == nil {
		return fmt.Errorf("no database connection for snapshot")
	}
	_ = os.Remove(dest)
	lit := strings.ReplaceAll(dest, "'", "''")
	if _, err := conn.Exec("VACUUM INTO '" + lit + "'"); err != nil {
		return fmt.Errorf("VACUUM INTO failed: %w", err)
	}
	if _, err := os.Stat(dest); err != nil {
		return fmt.Errorf("DB snapshot missing after VACUUM INTO: %w", err)
	}
	return nil
}
