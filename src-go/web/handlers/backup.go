package handlers

// Backup / restore read endpoints (upstream commands/backup/restore.rs +
// web/handlers/backup.rs). This implements the two advisory read routes that
// need neither the archive file-transfer machinery nor a running restore:
//   - backup_active_agents: which agents are live, so the UI can warn before a
//     restore to original locations. The headless build tracks no live ACP
//     agents at this layer, so it reports none (advisory only).
//   - backup_list_safety_snapshots: the restore safety snapshots on disk under
//     <data_dir>/.yoyocode-restore-backup/, newest first.
// The write/transfer surface (ticket/upload/stage/restore/rollback/cancel/
// download) is a larger archive-streaming subsystem handled separately.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"yoyocode/src-go/web"
)

const (
	backupSafetyDir        = ".yoyocode-restore-backup"
	backupSnapshotManifest = ".yoyocode-snapshot.json"
	// backupUploadTmpDir is where restore uploads land (upstream UPLOAD_TMP_DIR
	// ".codeg-restore-upload", rebranded).
	backupUploadTmpDir = ".yoyocode-restore-upload"
)

// errBackupUploadTooLarge flags an upload that crossed the optional cap so the
// handler maps it to invalid_input rather than a generic IO error.
var errBackupUploadTooLarge = errors.New("upload exceeds configured size limit")

// backupUploadMaxBytes reads the optional hard upload cap (default unlimited),
// matching upstream CODEG_BACKUP_UPLOAD_MAX_BYTES (rebranded).
func backupUploadMaxBytes() int64 {
	v := strings.TrimSpace(os.Getenv("YOYOCODE_BACKUP_UPLOAD_MAX_BYTES"))
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
}

// backupUpload streams a multipart `file` field into <data_dir>/.yoyocode-restore-upload/
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
