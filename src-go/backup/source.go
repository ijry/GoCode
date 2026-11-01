package backup

// One prepare per restore (upstream commands/backup/source.rs). Inspecting,
// scanning and staging each used to decrypt the whole archive again; a
// PreparedSource does it once and hands back a handle the later steps reuse.
//
// The encrypted-envelope branch (crypto.rs) is deferred in this port, so every
// archive this build produces is plaintext and Prepare takes the reference /
// take-ownership path directly. The decrypted-copy path is stubbed for when
// crypto lands.

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PreparedDir holds prepared sources awaiting inspect/scan/stage (upstream
// PREPARED_DIR, rebranded). Wiped at startup by CleanupPreparedSources.
const PreparedDir = ".gocode-restore-prepared"

const (
	sourceMetaFile = "meta.json"
	sourcePlainZip = "plain.zip"
	// sourceIdleTTL bounds how long an abandoned prepared source lingers.
	sourceIdleTTL = 30 * time.Minute
)

// Preview mirrors upstream BackupPreview (camelCase).
type Preview struct {
	Encrypted       bool            `json:"encrypted"`
	NeedsPassphrase bool            `json:"needsPassphrase"`
	Manifest        *BackupManifest `json:"manifest,omitempty"`
	Compatible      bool            `json:"compatible"`
	RejectReason    string          `json:"rejectReason,omitempty"`
}

// PreparedSource is the handle + the preview the caller wanted anyway.
type PreparedSource struct {
	SourceID *string `json:"sourceId,omitempty"`
	Preview  Preview `json:"preview"`
}

// sourceMeta is the on-disk record a source handle resolves by.
type sourceMeta struct {
	V             int    `json:"v"`
	ZipPath       string `json:"zip_path"`
	OwnsZip       bool   `json:"owns_zip"`
	LastUsedEpoch uint64 `json:"last_used_epoch"`
}

// IsEncrypted reports whether src is a wrapped (encrypted) archive. This port
// never produces encrypted archives (crypto deferred), so this is always false;
// kept as a seam so the crypto slice flips it without touching callers.
func IsEncrypted(src string) bool { return false }

// PrepareSource registers src as a reusable restore source. takeOwnership says
// whether src is a transient upload this module may consume (the web flow sets
// it; the desktop flow does not).
func PrepareSource(src, dataDir, passphrase string, takeOwnership bool) (PreparedSource, error) {
	if IsEncrypted(src) {
		// Encrypted archives need crypto (deferred). If no usable passphrase the
		// UI would prompt; otherwise decryption is required — both unsupported.
		if passphrase == "" {
			return PreparedSource{Preview: Preview{Encrypted: true, NeedsPassphrase: true}}, nil
		}
		return PreparedSource{}, errEncryptedUnsupported
	}

	root, err := preparedRoot(dataDir)
	if err != nil {
		return PreparedSource{}, err
	}
	id := newBackupID()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return PreparedSource{}, err
	}

	// A plaintext archive the caller owns is referenced in place; a transient
	// upload is taken over (rename, same filesystem) so it does not linger.
	zipPath := src
	ownsZip := false
	if takeOwnership {
		out := filepath.Join(dir, sourcePlainZip)
		if err := os.Rename(src, out); err == nil {
			zipPath, ownsZip = out, true
		} else {
			zipPath, ownsZip = src, false // rename failed: reference where it lies
		}
	}

	preview, perr := previewZip(zipPath, false)
	if perr != nil {
		_ = os.RemoveAll(dir)
		return PreparedSource{}, perr
	}
	if err := writeSourceMeta(dir, sourceMeta{
		V: 1, ZipPath: zipPath, OwnsZip: ownsZip, LastUsedEpoch: nowEpoch(),
	}); err != nil {
		_ = os.RemoveAll(dir)
		return PreparedSource{}, err
	}
	spawnIdleReaper(dataDir, id)

	sid := id
	return PreparedSource{SourceID: &sid, Preview: preview}, nil
}

// ResolvePreparedZip resolves a source handle to its plaintext archive,
// refreshing the idle timer. Rejects anything but a bare 32-hex id.
func ResolvePreparedZip(dataDir, sourceID string) (string, error) {
	if !validSourceID(sourceID) {
		return "", ErrInvalidSourceID
	}
	dir := filepath.Join(dataDir, PreparedDir, sourceID)
	meta, ok := readSourceMeta(dir)
	if !ok {
		return "", ErrSourceNotFound
	}
	if fi, err := os.Stat(meta.ZipPath); err != nil || fi.IsDir() {
		return "", ErrSourceNotFound
	}
	meta.LastUsedEpoch = nowEpoch()
	_ = writeSourceMeta(dir, meta)
	return meta.ZipPath, nil
}

// ReleaseSource drops a prepared source. Idempotent; false means nothing there.
func ReleaseSource(dataDir, sourceID string) (bool, error) {
	if !validSourceID(sourceID) {
		return false, ErrInvalidSourceID
	}
	dir := filepath.Join(dataDir, PreparedDir, sourceID)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return false, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, err
	}
	return true, nil
}

// CleanupPreparedSources is the startup sweep: nothing is prepared across a
// process boundary, so anything left is a killed process's residue.
func CleanupPreparedSources(dataDir string) {
	_ = os.RemoveAll(filepath.Join(dataDir, PreparedDir))
}

// ScanExternalConflicts reports external transcript entries whose live target
// already exists. External packing is deferred in this port, so archives carry
// no external entries and this is always empty.
func ScanExternalConflicts(zipPath string) ([]map[string]any, error) {
	if _, err := ReadManifest(zipPath); err != nil {
		return nil, err
	}
	return []map[string]any{}, nil
}

// previewZip reads the manifest and mirrors stage's structural checks so the
// preview never reports "compatible" for a backup stage would reject.
func previewZip(zipPath string, encrypted bool) (Preview, error) {
	m, err := ReadManifest(zipPath)
	if err != nil {
		return Preview{}, err
	}
	compatible, reason := EvaluateCompat(&m)
	if compatible {
		if verr := ValidateManifest(&m); verr != nil {
			compatible, reason = false, I18NUnknownFormat
		}
	}
	return Preview{
		Encrypted:       encrypted,
		NeedsPassphrase: false,
		Manifest:        &m,
		Compatible:      compatible,
		RejectReason:    reason,
	}, nil
}

func preparedRoot(dataDir string) (string, error) {
	root := filepath.Join(dataDir, PreparedDir)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	return root, nil
}

func readSourceMeta(dir string) (sourceMeta, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, sourceMetaFile))
	if err != nil {
		return sourceMeta{}, false
	}
	var m sourceMeta
	if json.Unmarshal(raw, &m) != nil {
		return sourceMeta{}, false
	}
	return m, true
}

func writeSourceMeta(dir string, m sourceMeta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, sourceMetaFile), b, 0o600)
}

func validSourceID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func nowEpoch() uint64 {
	return uint64(time.Now().Unix())
}

// spawnIdleReaper reaps the source once it goes unused for the TTL, re-arming
// while still in use. A shared cache keeps at most one goroutine per
// (dataDir,id); released sources simply find no meta and exit.
func spawnIdleReaper(dataDir, id string) {
	key := dataDir + "\x00" + id
	idleMu.Lock()
	if idleSeen[key] {
		idleMu.Unlock()
		return
	}
	idleSeen[key] = true
	idleMu.Unlock()
	go func() {
		for {
			time.Sleep(sourceIdleTTL)
			dir := filepath.Join(dataDir, PreparedDir, id)
			meta, ok := readSourceMeta(dir)
			if !ok {
				return // already released
			}
			if nowEpoch() >= meta.LastUsedEpoch+uint64(sourceIdleTTL/time.Second) {
				_ = os.RemoveAll(dir)
				return
			}
		}
	}()
}

var (
	idleMu   sync.Mutex
	idleSeen = map[string]bool{}

	errEncryptedUnsupported = errors.New("encrypted backups require the crypto layer (not yet ported)")
	ErrInvalidSourceID      = errors.New("invalid backup source id")
	ErrSourceNotFound       = errors.New("prepared backup source not found")
)

// newBackupID returns a bare 32-hex simple UUID (matches the upstream
// Uuid::new_v4().simple() ids that resolve_upload / resolve_prepared_zip guard).
func newBackupID() string {
	var b [16]byte
	_, _ = cryptorand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:])
}
