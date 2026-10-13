package backup

// Restore staging / apply / rollback / discard (upstream commands/backup/
// restore.rs). The swap happens on the NEXT process start: staging extracts +
// verifies a prepared archive into a staging dir and commits a pending marker;
// ApplyPendingRestore moves it into place with a crash-safe per-unit state
// machine, taking a safety snapshot first so Rollback can undo it. Pure
// filesystem; the DB connection is closed by the caller before apply.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// PendingMarker arms the swap (applied next startup).
	PendingMarker = ".yoyocode-restore-pending.json"
	// StagingDir holds extracted archives awaiting the swap.
	StagingDir = ".yoyocode-restore-staging"
	// SafetyDir holds pre-restore snapshots (rollback sources).
	SafetyDir = ".yoyocode-restore-backup"
	// SwapStateDir is the durable per-unit state inside a staging dir.
	SwapStateDir = ".yoyocode-swap-state"
	// SnapshotManifestName names a safety snapshot's manifest (layout=="staging").
	SnapshotManifestName = ".yoyocode-snapshot.json"
	// DBStagingDir / DBStagingName are the archive path of the DB snapshot.
	DBStagingDir  = "db"
	DBStagingName = "yoyocode.db"
	// SafetySnapshotKeep prunes all but the newest N safety snapshots.
	SafetySnapshotKeep = 2
	// liveDBFileName mirrors db.Open's data-dir file.
	liveDBFileName = "yoyocode.db"
)

// ErrAlreadyPending marks another restore already staged.
var ErrAlreadyPending = errors.New("a restore is already pending")

// StagedRestore is returned to the UI before the restart (camelCase).
type StagedRestore struct {
	StagingDir           string         `json:"stagingDir"`
	Manifest             BackupManifest `json:"manifest"`
	RestoredExternalPath *string        `json:"restoredExternalPath,omitempty"`
	SkippedConflicts     []string       `json:"skippedConflicts"`
	RefusedExternal      []any          `json:"refusedExternal,omitempty"`
	ExternalDowngraded   *any           `json:"externalDowngraded,omitempty"`
}

// RestoreApplied is the outcome of a startup swap.
type RestoreApplied struct {
	Applied        bool
	SafetySnapshot string
}

// pendingRestore is the on-disk marker.
type pendingRestore struct {
	StagingDir      string   `json:"staging_dir"`
	CreatedAt       string   `json:"created_at"`
	AppVersion      string   `json:"app_version"`
	LatestMigration string   `json:"latest_migration"`
	ManagedSections []string `json:"managed_sections,omitempty"`
	AbsentSections  []string `json:"absent_sections,omitempty"`
}

func (p pendingRestore) sections() []string {
	if p.ManagedSections == nil {
		return LegacySectionIDs
	}
	return NormalizeSectionIDs(p.ManagedSections)
}

type snapshotManifest struct {
	V         int      `json:"v"`
	Layout    string   `json:"layout"`
	CreatedAt string   `json:"created_at"`
	Present   []string `json:"present"`
	Absent    []string `json:"absent"`
}

type swapUnit struct {
	id              string
	staged          string
	live            string
	snapshot        string
	evictWhenAbsent bool
}

// StageRestore extracts + verifies a prepared archive into a fresh staging dir
// and commits the pending marker. Does NOT touch live data.
func StageRestore(zipPath, dataDir, opID string, progress func(string, uint64)) (StagedRestore, error) {
	if _, err := os.Stat(filepath.Join(dataDir, PendingMarker)); err == nil {
		return StagedRestore{}, ErrAlreadyPending
	}
	manifest, err := ReadManifest(zipPath)
	if err != nil {
		return StagedRestore{}, err
	}
	if ok, reason := EvaluateCompat(&manifest); !ok {
		return StagedRestore{}, fmt.Errorf("backup not compatible: %s", reason)
	}
	if verr := ValidateManifest(&manifest); verr != nil {
		return StagedRestore{}, verr
	}

	stagingRoot := filepath.Join(dataDir, StagingDir, opID)
	_ = os.RemoveAll(stagingRoot)
	if err := os.MkdirAll(stagingRoot, 0o700); err != nil {
		return StagedRestore{}, err
	}
	if err := ExtractAll(zipPath, stagingRoot, &manifest, progress); err != nil {
		return StagedRestore{}, err
	}
	if err := VerifyChecksums(stagingRoot, &manifest); err != nil {
		return StagedRestore{}, err
	}
	// Force-create every AlwaysReplace dir section the archive DECLARES, so
	// "the backup had nothing here" clears the live tree rather than merging.
	for _, id := range manifestSectionIDs(&manifest) {
		sec := FindSection(id)
		if sec == nil || sec.Policy != AlwaysReplace || sec.Kind != SectionDir {
			continue
		}
		if err := os.MkdirAll(filepath.Join(stagingRoot, id), 0o700); err != nil {
			return StagedRestore{}, err
		}
	}
	// Commit the marker (point of no return for the swap). No-clobber.
	if err := writePendingMarker(dataDir, pendingRestore{
		StagingDir:      stagingRoot,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
		AppVersion:      "", // filled by caller via manifest below
		LatestMigration: manifest.LatestMigration,
		ManagedSections: manifestSectionIDs(&manifest),
	}); err != nil {
		return StagedRestore{}, err
	}
	return StagedRestore{
		StagingDir:       stagingRoot,
		Manifest:         manifest,
		SkippedConflicts: []string{},
	}, nil
}

// ApplyPendingRestore applies a staged restore (idempotent, crash-safe). Must
// run before the DB is reopened. Returns {Applied:false} when nothing pending.
func ApplyPendingRestore(dataDir string, roots LiveRoots) (RestoreApplied, error) {
	markerPath := filepath.Join(dataDir, PendingMarker)
	raw, err := os.ReadFile(markerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return RestoreApplied{}, nil
		}
		return RestoreApplied{}, err
	}
	var pending pendingRestore
	if json.Unmarshal(raw, &pending) != nil {
		_ = os.Remove(markerPath) // corrupt marker → discard, don't loop
		return RestoreApplied{}, nil
	}
	staging := pending.StagingDir
	if fi, serr := os.Stat(staging); serr != nil || !fi.IsDir() {
		_ = os.Remove(markerPath)
		return RestoreApplied{}, nil
	}

	backupDir := filepath.Join(dataDir, SafetyDir, snapshotDirName(pending))
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return RestoreApplied{}, err
	}
	units := swapUnits(staging, dataDir, roots, backupDir, pending)
	if err := writeSnapshotManifest(backupDir, pending.CreatedAt, units); err != nil {
		return RestoreApplied{}, err
	}
	stateDir := filepath.Join(staging, SwapStateDir)
	for _, u := range units {
		if err := applySwapUnit(stateDir, u); err != nil {
			return RestoreApplied{}, err // marker + staging + snapshot intact → retry
		}
	}
	if err := os.Remove(markerPath); err != nil {
		return RestoreApplied{}, err
	}
	_ = os.RemoveAll(staging)
	return RestoreApplied{Applied: true, SafetySnapshot: backupDir}, nil
}

// RollbackToSnapshot stages a rollback to a safety snapshot (applied next
// startup, with a fresh snapshot taken of the current data on the way).
func RollbackToSnapshot(dataDir, snapshotID string) (string, error) {
	if snapshotID == "" || strings.ContainsAny(snapshotID, "/\\") || strings.Contains(snapshotID, "..") {
		return "", errInvalidSnapshot
	}
	dir := filepath.Join(dataDir, SafetyDir, snapshotID)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", errSnapshotNotFound
	}
	m, ok := readSnapshotManifest(dir)
	if !ok {
		return "", fmt.Errorf("this snapshot predates rollback support and can only be inspected")
	}
	declared := append(append([]string{}, m.Present...), m.Absent...)
	pending := pendingRestore{
		StagingDir:      dir,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
		AppVersion:      "",
		LatestMigration: "",
		ManagedSections: NormalizeSectionIDs(declared),
		AbsentSections:  NormalizeSectionIDs(m.Absent),
	}
	if err := commitPendingMarker(dataDir, pending); err != nil {
		return "", err
	}
	return dir, nil
}

// DiscardPendingRestore drops a staged-but-unapplied restore (marker + its
// staging dir). A rollback's marker points at a safety snapshot, which is left
// untouched. Returns false when nothing was pending.
func DiscardPendingRestore(dataDir string) (bool, error) {
	markerPath := filepath.Join(dataDir, PendingMarker)
	if fi, err := os.Stat(markerPath); err != nil || !fi.Mode().IsRegular() {
		return false, nil
	}
	var staging string
	if raw, rerr := os.ReadFile(markerPath); rerr == nil {
		var p pendingRestore
		if json.Unmarshal(raw, &p) == nil {
			staging = p.StagingDir
		}
	}
	if err := os.Remove(markerPath); err != nil {
		return false, err
	}
	if staging != "" && withinDir(staging, filepath.Join(dataDir, StagingDir)) {
		_ = os.RemoveAll(staging) // only a staging dir we created
	}
	return true, nil
}

var (
	errInvalidSnapshot  = errors.New("invalid snapshot id")
	errSnapshotNotFound = errors.New("safety snapshot not found")
)

// ── swap internals ──────────────────────────────────────────────────────────

func swapUnits(staging, dataDir string, roots LiveRoots, backupDir string, pending pendingRestore) []swapUnit {
	var units []swapUnit
	for _, u := range []struct {
		id     string
		suffix string
		evict  bool
	}{
		{"db", "", false},
		{"db-wal", "-wal", true},
		{"db-shm", "-shm", true},
	} {
		file := DBStagingName + u.suffix
		units = append(units, swapUnit{
			id:              u.id,
			staged:          filepath.Join(staging, DBStagingDir, file),
			live:            filepath.Join(dataDir, liveDBFileName+u.suffix),
			snapshot:        filepath.Join(backupDir, DBStagingDir, file),
			evictWhenAbsent: u.evict,
		})
	}
	for _, id := range pending.sections() {
		live, ok := roots.Path(id)
		if !ok || live == "" {
			continue
		}
		units = append(units, swapUnit{
			id:              id,
			staged:          filepath.Join(staging, id),
			live:            live,
			snapshot:        filepath.Join(backupDir, id),
			evictWhenAbsent: containsStr(pending.AbsentSections, id),
		})
	}
	return units
}

func applySwapUnit(stateDir string, u swapUnit) error {
	switch readSwapState(stateDir, u.id) {
	case swapPublished:
		return nil
	case swapBackedUp:
		// backup already ran; touching it again would destroy the pre-restore copy
	default:
		if (u.evictWhenAbsent || pathExists(u.staged)) && pathExists(u.live) {
			if err := movePath(u.live, u.snapshot); err != nil {
				return err
			}
		}
		if err := writeSwapState(stateDir, u.id, swapBackedUp); err != nil {
			return err
		}
	}
	if pathExists(u.staged) {
		if err := movePath(u.staged, u.live); err != nil {
			return err
		}
	}
	return writeSwapState(stateDir, u.id, swapPublished)
}

type swapStateStr string

const (
	swapBackedUp  swapStateStr = "backed-up"
	swapPublished swapStateStr = "published"
)

func readSwapState(stateDir, id string) swapStateStr {
	raw, err := os.ReadFile(filepath.Join(stateDir, id))
	if err != nil {
		return ""
	}
	switch swapStateStr(strings.TrimSpace(string(raw))) {
	case swapPublished:
		return swapPublished
	case swapBackedUp:
		return swapBackedUp
	}
	return ""
}

func writeSwapState(stateDir, id string, st swapStateStr) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(stateDir, "."+id+".tmp")
	if err := os.WriteFile(tmp, []byte(st), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(stateDir, id))
}

// movePath moves src→dst, creating the parent, with a copy-then-remove fallback
// across filesystem boundaries.
func movePath(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := copyTree(src, dst); err != nil {
			return err
		}
		return os.RemoveAll(src)
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil // never follow symlinks
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ── markers & snapshot manifests ─────────────────────────────────────────────

// writePendingMarker commits the marker with no-clobber semantics (a second
// concurrent stage fails rather than overwriting).
func writePendingMarker(dataDir string, p pendingRestore) error {
	p.AppVersion = backupAppVersionForManifest
	if p.CreatedAt == "" {
		p.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dataDir, PendingMarker), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return ErrAlreadyPending
		}
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// commitPendingMarker writes the marker atomically (overwrite) — for a rollback
// that stages a fresh marker.
func commitPendingMarker(dataDir string, p pendingRestore) error {
	p.AppVersion = backupAppVersionForManifest
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dataDir, PendingMarker+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dataDir, PendingMarker))
}

// backupAppVersionForManifest is the app version recorded on markers. Set by the
// web layer at init; defaults keep the engine self-contained for tests.
var backupAppVersionForManifest = "0.1.0"

func snapshotDirName(p pendingRestore) string {
	stamp := time.Now().UTC().Format("20060102-150405")
	if t, err := time.Parse(time.RFC3339, p.CreatedAt); err == nil {
		stamp = t.UTC().Format("20060102-150405")
	}
	op := filepath.Base(p.StagingDir)
	if op == "." || op == string(filepath.Separator) || op == "" {
		op = newBackupID()
	}
	return stamp + "-" + op
}

func writeSnapshotManifest(backupDir, createdAt string, units []swapUnit) error {
	path := filepath.Join(backupDir, SnapshotManifestName)
	if _, err := os.Stat(path); err == nil {
		return nil // existence ⇒ complete; retry must not recompute
	}
	var present, absent []string
	for _, u := range units {
		if pathExists(u.live) {
			present = append(present, u.id)
		} else {
			absent = append(absent, u.id)
		}
	}
	b, err := json.MarshalIndent(snapshotManifest{
		V: 1, Layout: "staging", CreatedAt: createdAt, Present: present, Absent: absent,
	}, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(backupDir, SnapshotManifestName+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readSnapshotManifest(dir string) (snapshotManifest, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, SnapshotManifestName))
	if err != nil {
		return snapshotManifest{}, false
	}
	var m snapshotManifest
	if json.Unmarshal(raw, &m) != nil || m.V != 1 || m.Layout != "staging" {
		return snapshotManifest{}, false
	}
	return m, true
}

// ── small helpers ─────────────────────────────────────────────────────────────

func manifestSectionIDs(m *BackupManifest) []string {
	if m.ManagedSections == nil {
		return LegacySectionIDs
	}
	return NormalizeSectionIDs(m.ManagedSections)
}

func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// withinDir reports p is inside root (path-segment bounded).
func withinDir(p, root string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
