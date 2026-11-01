package handlers

// Backup / restore read endpoints (upstream commands/backup/restore.rs +
// web/handlers/backup.rs). This implements the two advisory read routes that
// need neither the archive file-transfer machinery nor a running restore:
//   - backup_active_agents: which agents are live, so the UI can warn before a
//     restore to original locations. The headless build tracks no live ACP
//     agents at this layer, so it reports none (advisory only).
//   - backup_list_safety_snapshots: the restore safety snapshots on disk under
//     <data_dir>/.gocode-restore-backup/, newest first.
// The write/transfer surface (ticket/upload/stage/restore/rollback/cancel/
// download) is a larger archive-streaming subsystem handled separately.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocode/src-go/backup"
	"gocode/src-go/web"
)

const (
	backupSafetyDir        = ".gocode-restore-backup"
	backupSnapshotManifest = ".gocode-snapshot.json"
	// backupUploadTmpDir is where restore uploads land (upstream UPLOAD_TMP_DIR
	// ".codeg-restore-upload", rebranded).
	backupUploadTmpDir = ".gocode-restore-upload"
	// backupAppVersion is recorded in the archive manifest.
	backupAppVersion = "0.1.0"
	// backupExportReap is the grace period before an undownloaded export archive
	// is reaped (upstream EXPORT_REAP_SECS).
	backupExportReap = 120 * time.Second
	// backupTicketTTL bounds how long a download ticket is honored.
	backupTicketTTL = 5 * time.Minute
)

// errBackupUploadTooLarge flags an upload that crossed the optional cap so the
// handler maps it to invalid_input rather than a generic IO error.
var errBackupUploadTooLarge = errors.New("upload exceeds configured size limit")

// backupUploadMaxBytes reads the optional hard upload cap (default unlimited),
// matching upstream CODEG_BACKUP_UPLOAD_MAX_BYTES (rebranded).
func backupUploadMaxBytes() int64 {
	v := strings.TrimSpace(os.Getenv("GOCODE_BACKUP_UPLOAD_MAX_BYTES"))
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func backupDirSize(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// backupReadSnapshotManifest returns (createdAt, true) when the dir holds a
// valid v1/staging snapshot manifest.
func backupReadSnapshotManifest(dir string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, backupSnapshotManifest))
	if err != nil {
		return "", false
	}
	var m struct {
		V         int    `json:"v"`
		Layout    string `json:"layout"`
		CreatedAt string `json:"created_at"`
	}
	if json.Unmarshal(raw, &m) != nil || m.V != 1 || m.Layout != "staging" {
		return "", false
	}
	return m.CreatedAt, true
}

// RegisterBackup wires the backup/restore advisory read endpoints.
func RegisterBackup(reg *Registry) {
	reg.Register("backup_active_agents", func(c *web.Context) (any, error) {
		// Advisory only; this build tracks no live ACP agents here.
		return []string{}, nil
	})

	// Cancel an in-flight workspace transfer by op id (upstream
	// WorkspaceTransferManager::cancel). This build's export/restore run
	// synchronously and register no cancellable transfer, so this honestly
	// returns false (nothing to cancel) — the registry is wired so async
	// transfers added later can be cancelled here.
	reg.Register("backup_cancel", func(c *web.Context) (any, error) {
		opID := GetString(c, "opId", "op_id")
		st, ok := appState(c)
		if !ok || st == nil || st.Transfers == nil {
			return false, nil
		}
		return st.Transfers.Cancel(opID), nil
	})

	reg.Register("backup_list_safety_snapshots", func(c *web.Context) (any, error) {
		root := filepath.Join(appDataDir(c), backupSafetyDir)
		entries, err := os.ReadDir(root)
		if err != nil {
			return []map[string]any{}, nil
		}
		type sortable struct {
			key  string
			snap map[string]any
		}
		var items []sortable
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(root, e.Name())
			createdAt, hasManifest := backupReadSnapshotManifest(dir)
			var createdVal any
			if hasManifest && createdAt != "" {
				createdVal = createdAt
			}
			snap := map[string]any{
				"id":                 e.Name(),
				"path":               dir,
				"created_at":         createdVal,
				"size_bytes":         backupDirSize(dir),
				"rollback_supported": hasManifest,
			}
			// Newest first: sort key is the created timestamp when known, else
			// the directory name (both monotonic in practice).
			key := e.Name()
			if createdAt != "" {
				key = createdAt
			}
			items = append(items, sortable{key: key, snap: snap})
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].key > items[j].key })
		out := make([]map[string]any, 0, len(items))
		for _, it := range items {
			out = append(out, it.snap)
		}
		return out, nil
	})

	reg.Register("backup_upload", backupUpload)
	backupTickets := newBackupTicketStore()
	reg.Register("backup_create_ticket", func(c *web.Context) (any, error) {
		return backupCreateTicket(c, backupTickets)
	})
	reg.RegisterRaw("/backup_download/", func(w http.ResponseWriter, req *http.Request) {
		backupDownload(w, req, backupTickets)
	})
	reg.Register("backup_prepare_source", backupPrepareSource)
	reg.Register("backup_release_source", backupReleaseSource)
	reg.Register("backup_scan_external_conflicts", backupScanExternalConflicts)
	reg.Register("backup_restore_stage", backupRestoreStage)
	reg.Register("backup_rollback", backupRollback)
	reg.Register("backup_discard_pending", backupDiscardPending)
}

// backupTicket is a single-use download ticket for a built export archive.
// root scopes consumption to the backup temp dir (a workspace ticket seeded
// into this store must never stream an arbitrary file).
type backupTicket struct {
	targetPath string
	root       string
	filename   string
	expiresAt  time.Time
}

type backupTicketStore struct {
	mu      sync.Mutex
	tickets map[string]backupTicket
}

func newBackupTicketStore() *backupTicketStore {
	return &backupTicketStore{tickets: map[string]backupTicket{}}
}

func (s *backupTicketStore) issue(target, root, filename string) (string, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := newChatUUID()
	exp := time.Now().Add(backupTicketTTL)
	s.tickets[id] = backupTicket{targetPath: target, root: root, filename: filename, expiresAt: exp}
	return id, exp.Unix()
}

func (s *backupTicketStore) consume(id string) (backupTicket, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tickets[id]
	if !ok {
		return backupTicket{}, false
	}
	delete(s.tickets, id)
	if time.Now().Before(t.expiresAt) {
		return t, true
	}
	return backupTicket{}, false
}

// backupCreateTicket builds an export archive and returns a single-use download
// ticket (upstream BackupTicketResult): {ticket, url, filename, expires_at,
// degradedSqlite}. Encrypted + external-inclusive backups are not yet ported
// (CreateBackup rejects them cleanly).
func backupCreateTicket(c *web.Context, store *backupTicketStore) (any, error) {
	d, err := requireDB(c)
	if err != nil {
		return nil, err
	}
	dataDir := appDataDir(c)
	tmpDir := filepath.Join(dataDir, backup.BackupExportTmpDir)
	if mkErr := os.MkdirAll(tmpDir, 0o700); mkErr != nil {
		return nil, web.ErrIO("Failed to create backup temp dir: " + mkErr.Error())
	}
	ext := "gocode.zip" // passphrase (encrypted) rejected below
	dest := filepath.Join(tmpDir, newChatUUID()+"."+ext)
	opID := newChatUUID()
	manifest, cerr := backup.CreateBackup(
		d.Conn, opID, dataDir, backupAppVersion, backupLiveRoots(c),
		backup.Options{
			IncludeExternalTranscripts: GetBool(c, "includeExternalTranscripts", "include_external_transcripts"),
			Passphrase:                 GetString(c, "passphrase"),
		},
		dest, func(p backup.Progress) { Emit(c, backup.BackupProgressEvent, p) },
	)
	if cerr != nil {
		_ = os.Remove(dest)
		return nil, web.ErrTaskExecution("Backup failed").WithDetail(cerr.Error())
	}
	downloadName := fmt.Sprintf("gocode-backup-%s.%s", time.Now().UTC().Format("20060102-150405"), ext)
	ticket, expiresAt := store.issue(dest, tmpDir, downloadName)
	// Reap the temp archive after the TTL whether or not it was downloaded (the
	// download stream does not delete it).
	go func() { time.Sleep(backupExportReap); _ = os.Remove(dest) }()

	degraded := manifest.DegradedSqlite
	if degraded == nil {
		degraded = []backup.DegradedSqlite{}
	}
	return map[string]any{
		"ticket":         ticket,
		"url":            "/backup_download/" + ticket,
		"filename":       downloadName,
		"expires_at":     expiresAt,
		"degradedSqlite": degraded,
	}, nil
}

// backupDownload streams a built export archive via a single-use ticket
// (public route; scoped to the backup temp dir).
func backupDownload(w http.ResponseWriter, req *http.Request, store *backupTicketStore) {
	id := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/backup_download/"), "/")
	t, ok := store.consume(id)
	if !ok || !withinRoot(t.targetPath, t.root) {
		web.WritePlain(w, http.StatusNotFound, "Download ticket is invalid or expired")
		return
	}
	info, err := os.Stat(t.targetPath)
	if err != nil || info.IsDir() {
		web.WritePlain(w, http.StatusNotFound, "Backup archive does not exist")
		return
	}
	f, oerr := os.Open(t.targetPath)
	if oerr != nil {
		web.WritePlain(w, http.StatusInternalServerError, "Failed to open backup archive")
		return
	}
	defer f.Close()
	streamFileToWriter(w, f, info.Size(), t.filename)
}

// withinRoot reports p is inside root (same volume, path-segment bounded).
func withinRoot(p, root string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

// backupUpload streams a multipart `file` field into <data_dir>/.gocode-restore-upload/
// <uuid32>.bin and returns the handle the restore flow reuses ({upload_id, file_name}),
// so the (potentially large) archive is uploaded only once. Mirrors upstream
// receive_upload: optional size cap, partial file deleted on any failure.
func backupUpload(c *web.Context) (any, error) {
	ct := ""
	if c.R != nil {
		ct = c.R.Header.Get("Content-Type")
	}
	mediaType, params, perr := mime.ParseMediaType(ct)
	if perr != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return nil, web.ErrInvalidInput("expected multipart/form-data upload")
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, web.ErrInvalidInput("multipart boundary missing")
	}
	mr := multipart.NewReader(bytes.NewReader(c.RawBody), boundary)

	uploadDir := filepath.Join(appDataDir(c), backupUploadTmpDir)
	if mkErr := os.MkdirAll(uploadDir, 0o755); mkErr != nil {
		return nil, web.ErrIO("Failed to create upload directory: " + mkErr.Error())
	}
	maxBytes := backupUploadMaxBytes()

	id := newChatUUID() // 32 hex chars, matching the resolve_upload guard
	dest := filepath.Join(uploadDir, id+".bin")

	fileName := ""
	received := false
	for {
		part, nerr := mr.NextPart()
		if nerr == io.EOF {
			break
		}
		if nerr != nil {
			_ = os.Remove(dest)
			return nil, web.ErrIO("Invalid multipart upload").WithDetail(nerr.Error())
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		fileName = part.FileName()
		if fileName == "" {
			fileName = "backup"
		}
		out, ocErr := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if ocErr != nil {
			_ = part.Close()
			_ = os.Remove(dest)
			return nil, web.ErrIO("Failed to create upload file: " + ocErr.Error())
		}
		cpErr := copyWithUploadCap(out, part, maxBytes)
		_ = out.Close()
		_ = part.Close()
		if cpErr != nil {
			_ = os.Remove(dest) // never linger a failed/aborted upload
			if errors.Is(cpErr, errBackupUploadTooLarge) {
				return nil, web.ErrInvalidInput("Uploaded backup exceeds the configured size limit")
			}
			return nil, web.ErrIO("Failed to write uploaded data: " + cpErr.Error())
		}
		received = true
	}
	if !received {
		_ = os.Remove(dest)
		return nil, web.ErrInvalidInput("Multipart upload is missing the `file` field")
	}
	return map[string]any{
		"upload_id": id,
		"file_name": fileName,
	}, nil
}

// copyWithUploadCap streams src to dst, aborting once the running total crosses
// a positive cap.
func copyWithUploadCap(dst io.Writer, src io.Reader, cap int64) error {
	buf := make([]byte, 64*1024)
	var total int64
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			total += int64(n)
			if cap > 0 && total > cap {
				return errBackupUploadTooLarge
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// backupHome is the GoCode home root (GOCODE_HOME / GOCODE_DATA_DIR /
// ~/.gocode), the port's convention for data sections.
func backupHome() string {
	if v := strings.TrimSpace(os.Getenv("GOCODE_HOME")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("GOCODE_DATA_DIR")); v != "" {
		return v
	}
	return filepath.Join(skillHomeDir(), ".gocode")
}

// backupLiveRoots resolves each managed section to where THIS port actually
// writes it, so a backup never silently packs an empty tree. Mirrors the port's
// per-section writers (uploads/pets/env, backgrounds+tokens under DataDir,
// skills under ~/.gocode/skills, the rest under the home root).
// backupSectionRoots resolves each managed section for a given data dir.
func backupSectionRoots(dataDir string) func(string) string {
	home := backupHome()
	return func(id string) string {
		switch id {
		case "uploads":
			return gocodeUploadsRoot()
		case "pets":
			return petsRoot()
		case "backgrounds":
			return filepath.Join(dataDir, "backgrounds")
		case "skills":
			return filepath.Join(skillHomeDir(), ".gocode", "skills")
		case "tokens.json":
			return filepath.Join(dataDir, "tokens.json")
		default: // acp-transcripts, turn-timings, preferences.json
			return filepath.Join(home, id)
		}
	}
}

func backupLiveRoots(c *web.Context) backup.LiveRoots {
	return backup.NewLiveRoots(backupSectionRoots(appDataDir(c)))
}

// ApplyPendingRestoreAtStartup applies a staged restore before the DB is
// reopened (the swap's real trigger). Safe to call every boot.
func ApplyPendingRestoreAtStartup(dataDir string) (backup.RestoreApplied, error) {
	return backup.ApplyPendingRestore(dataDir, backup.NewLiveRoots(backupSectionRoots(dataDir)))
}

// ── restore source prepare / release / scan ────────────────────────────────

// backupResolveUpload resolves an upload_id to its temp file, rejecting anything
// that is not a bare 32-char hex id (defends the join against traversal).
func backupResolveUpload(c *web.Context, uploadID string) (string, error) {
	if !isBackupHexID(uploadID) {
		return "", web.ErrInvalidInput("Invalid upload id")
	}
	p := filepath.Join(appDataDir(c), backupUploadTmpDir, uploadID+".bin")
	if fi, err := os.Stat(p); err != nil || fi.IsDir() {
		return "", web.ErrNotFound("Uploaded backup not found")
	}
	return p, nil
}

func isBackupHexID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, ch := range s {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return false
		}
	}
	return true
}

// backupPrepareSource decrypts-if-needed once and hands back a reusable handle +
// preview (web flow: the upload is a transient temp file, so take ownership).
func backupPrepareSource(c *web.Context) (any, error) {
	uploadID := GetString(c, "uploadId", "upload_id")
	src, err := backupResolveUpload(c, uploadID)
	if err != nil {
		return nil, err
	}
	prepared, perr := backup.PrepareSource(src, appDataDir(c), GetString(c, "passphrase"), true)
	if perr != nil {
		return nil, web.ErrTaskExecution("Prepare backup source failed").WithDetail(perr.Error())
	}
	return prepared, nil
}

// backupReleaseSource drops a prepared source (idempotent). Invalid id → 400.
func backupReleaseSource(c *web.Context) (any, error) {
	ok, rerr := backup.ReleaseSource(appDataDir(c), GetString(c, "sourceId", "source_id"))
	if rerr != nil {
		if errors.Is(rerr, backup.ErrInvalidSourceID) {
			return nil, web.ErrInvalidInput(rerr.Error())
		}
		return nil, web.ErrIO(rerr.Error())
	}
	return ok, nil
}

// backupScanExternalConflicts reports external transcript collisions. External
// packing is deferred, so archives carry no external entries → always [].
func backupScanExternalConflicts(c *web.Context) (any, error) {
	zip, err := backup.ResolvePreparedZip(appDataDir(c), GetString(c, "sourceId", "source_id"))
	if err != nil {
		if errors.Is(err, backup.ErrInvalidSourceID) {
			return nil, web.ErrInvalidInput(err.Error())
		}
		return nil, web.ErrNotFound("Prepared backup source not found")
	}
	conflicts, serr := backup.ScanExternalConflicts(zip)
	if serr != nil {
		return nil, web.ErrTaskExecution("Scan external conflicts failed").WithDetail(serr.Error())
	}
	return conflicts, nil
}

// ── restore stage / rollback / discard ───────────────────────────────────────

// backupRestartDelayMs mirrors upstream CODEG_RESTART_DELAY_MS (rebranded),
// default 2000ms — the relaunch delay after a staged restore.
func backupRestartDelayMs() int {
	if v := strings.TrimSpace(os.Getenv("GOCODE_RESTART_DELAY_MS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 2000
}

// backupRestoreStage stages a prepared archive (extract + verify + pending
// marker); the actual swap applies on the next launch. External transcript
// dispatch is skipped (external packing deferred).
func backupRestoreStage(c *web.Context) (any, error) {
	dataDir := appDataDir(c)
	sourceID := GetString(c, "sourceId", "source_id")
	zip, err := backup.ResolvePreparedZip(dataDir, sourceID)
	if err != nil {
		if errors.Is(err, backup.ErrInvalidSourceID) {
			return nil, web.ErrInvalidInput(err.Error())
		}
		return nil, web.ErrNotFound("Prepared backup source not found")
	}
	opID := newChatUUID()
	staged, serr := backup.StageRestore(zip, dataDir, opID, func(path string, processed uint64) {
		Emit(c, backup.BackupProgressEvent, backup.Progress{OpID: opID, Phase: backup.PhaseExtracting, ProcessedBytes: processed})
	})
	if serr != nil {
		if errors.Is(serr, backup.ErrAlreadyPending) {
			return nil, web.ErrAlreadyExists("A restore is already pending")
		}
		return nil, web.ErrTaskExecution("Stage restore failed").WithDetail(serr.Error())
	}
	_, _ = backup.ReleaseSource(dataDir, sourceID)
	return map[string]any{
		"needsRestart":   true,
		"restartDelayMs": backupRestartDelayMs(),
		"staged":         staged,
	}, nil
}

// backupRollback commits a pending marker pointing at a safety snapshot; it
// applies (reverting the data) on the next launch, like a normal restore.
func backupRollback(c *web.Context) (any, error) {
	snapshotID := GetString(c, "snapshotId", "snapshot_id")
	path, err := backup.RollbackToSnapshot(appDataDir(c), snapshotID)
	if err != nil {
		return nil, web.ErrTaskExecution("Rollback failed").WithDetail(err.Error())
	}
	return map[string]any{
		"needsRestart":   true,
		"restartDelayMs": backupRestartDelayMs(),
		"snapshotPath":   path,
	}, nil
}

// backupDiscardPending drops a staged-but-unapplied restore (marker + staging).
func backupDiscardPending(c *web.Context) (any, error) {
	ok, err := backup.DiscardPendingRestore(appDataDir(c))
	if err != nil {
		return nil, web.ErrIO(err.Error())
	}
	return ok, nil
}
