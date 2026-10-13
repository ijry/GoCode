// Package backup is the YoYoCode port of upstream commands/backup: the
// runtime-agnostic archive engine (create / read / validate / extract / verify).
// The web layer (handlers/backup.go) resolves inputs and wraps this over HTTP;
// the DB and section roots are passed in so the engine stays free of env
// lookups and is directly testable.
package backup

// Archive layout version. Bumped only on an incompatible change to the archive
// layout / manifest shape. Restores reject any archive whose format_version
// exceeds this. Matches upstream BACKUP_FORMAT_VERSION = 2.
const BackupFormatVersion = 2

// Magic discriminator in every manifest so a stray ZIP can't pass as a backup.
// Rebranded from upstream "codeg-backup" (the archive is YoYoCode's own).
const BackupKind = "yoyocode-backup"

// ManifestEntryName is the fixed entry name of the manifest inside the archive.
const ManifestEntryName = "manifest.json"

// DBEntry is the archive path of the SQLite snapshot. Rebranded from
// "db/codeg.db"; both create and validate use this constant.
const DBEntry = "db/yoyocode.db"

// Progress channel both runtimes emit on.
const BackupProgressEvent = "backup://progress"

// LatestMigration is the schema-compatibility contract recorded in the manifest
// (upstream keys off the newest sea-orm migration name; this port carries a
// single schema generation instead — same role, self-consistent across builds).
const LatestMigration = "yoyocode-schema-1"

// Reject-reason i18n keys (values mirrored from upstream app_error.rs).
const (
	I18NUnknownFormat = "backup.restore.error.unknownFormat"
	I18NNewerVersion  = "backup.restore.error.newerVersion"
)

// ManifestEntry is a per-file integrity record: sha256 of the plaintext bytes
// (before compression / encryption).
type ManifestEntry struct {
	Path   string `json:"path"`
	Size   uint64 `json:"size"`
	SHA256 string `json:"sha256"`
}

// BackupManifest is the logical description of the archive's contents, written
// as a single manifest.json entry inside the ZIP. camelCase to match the rest of
// the wire format.
type BackupManifest struct {
	FormatVersion               int              `json:"formatVersion"`
	Kind                        string           `json:"kind"`
	CreatedAt                   string           `json:"createdAt"`
	AppVersion                  string           `json:"appVersion"`
	LatestMigration             string           `json:"latestMigration"`
	Runtime                     string           `json:"runtime"`
	IncludesExternalTranscripts bool             `json:"includesExternalTranscripts"`
	IncludesSecrets             bool             `json:"includesSecrets"`
	ManagedSections             []string         `json:"managedSections,omitempty"`
	DegradedSqlite              []DegradedSqlite `json:"degradedSqlite,omitempty"`
	Entries                     []ManifestEntry  `json:"entries"`
}

// DegradedSqlite reports one third-party SQLite store that could not be
// snapshotted cleanly (external transcripts — deferred in this port today).
type DegradedSqlite struct {
	Agent       string `json:"agent"`
	ArchivePath string `json:"archivePath"`
	Level       string `json:"level"`
}

// TotalBytes is the total uncompressed size of all recorded entries.
func (m *BackupManifest) TotalBytes() uint64 {
	var t uint64
	for _, e := range m.Entries {
		t += e.Size
	}
	return t
}

// BackupPhase is the coarse phase surfaced to the UI.
type BackupPhase string

const (
	PhaseSnapshotting BackupPhase = "snapshotting"
	PhaseArchiving    BackupPhase = "archiving"
	PhaseEncrypting   BackupPhase = "encrypting"
	PhaseDone         BackupPhase = "done"
	PhaseCancelled    BackupPhase = "cancelled"
	PhaseError        BackupPhase = "error"
)

// Progress is the payload emitted on BackupProgressEvent.
type Progress struct {
	OpID           string      `json:"opId"`
	Phase          BackupPhase `json:"phase"`
	ProcessedBytes uint64      `json:"processedBytes"`
	TotalBytes     *uint64     `json:"totalBytes,omitempty"`
	CurrentPath    string      `json:"currentPath,omitempty"`
	Error          string      `json:"error,omitempty"`
}

// EvaluateCompat decides whether a manifest can be restored by this binary:
// unknown layout (format_version too new / wrong kind) → unknown format; an
// unrecognized migration → newer version.
func EvaluateCompat(m *BackupManifest) (bool, string) {
	if m.FormatVersion > BackupFormatVersion || m.Kind != BackupKind {
		return false, I18NUnknownFormat
	}
	if m.LatestMigration != LatestMigration {
		return false, I18NNewerVersion
	}
	return true, ""
}
