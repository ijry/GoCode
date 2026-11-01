package handlers_test

// backup_prepare_source / backup_release_source over HTTP: upload a real backup
// archive, prepare it (→ handle + compatible preview), release it, and confirm
// idempotence. Isolated via GOCODE_HOME.

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gocode/src-go/backup"
)

// buildBackupZipBytes builds a minimal, manifest-valid plaintext backup zip.
func buildBackupZipBytes(t *testing.T) []byte {
	t.Helper()
	dir := t.TempDir()
	dbFile := filepath.Join(dir, "snap.db")
	if err := os.WriteFile(dbFile, []byte("DBBODY"), 0o644); err != nil {
		t.Fatal(err)
	}
	zip := filepath.Join(dir, "a.zip")
	b, err := backup.NewArchiveBuilder(zip)
	if err != nil {
		t.Fatal(err)
	}
	m := backup.BackupManifest{
		FormatVersion:   backup.BackupFormatVersion,
		Kind:            backup.BackupKind,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
		AppVersion:      "0.1.0",
		LatestMigration: backup.LatestMigration,
		Runtime:         "server",
		IncludesSecrets: true,
	}
	if err := b.AddFile(backup.DBEntry, dbFile, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Finish(m); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(zip)
	return data
}

func uploadBackupBytes(t *testing.T, srvURL string, data []byte) string {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", "restore.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(data)
	_ = w.Close()
	code, raw := callMultipart(t, srvURL, "backup_upload", buf.Bytes(), w.FormDataContentType())
	if code != 200 {
		t.Fatalf("upload status %d: %s", code, raw)
	}
	var out struct {
		UploadID string `json:"upload_id"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil || !isHex32(out.UploadID) {
		t.Fatalf("bad upload result: %s", raw)
	}
	return out.UploadID
}

func TestBackupPrepareAndReleaseSource(t *testing.T) {
	t.Setenv("GOCODE_HOME", t.TempDir())
	srv := newTestServer(t)

	uploadID := uploadBackupBytes(t, srv.URL, buildBackupZipBytes(t))
	code, out := call(t, srv, "backup_prepare_source", map[string]any{"uploadId": uploadID})
	if code != 200 {
		t.Fatalf("prepare status %d: %v", code, out)
	}
	sourceID, _ := out["sourceId"].(string)
	if !isHex32(sourceID) {
		t.Fatalf("expected a 32-hex sourceId, got %v (full: %v)", sourceID, out)
	}
	preview, _ := out["preview"].(map[string]any)
	if preview == nil || preview["compatible"] != true {
		t.Fatalf("preview should report compatible: %v", out)
	}
	if m, ok := preview["manifest"].(map[string]any); !ok || m["kind"] != backup.BackupKind {
		t.Fatalf("preview should carry a manifest of our kind: %v", preview)
	}

	// Prepare again with a bogus upload id → error.
	if c, _ := call(t, srv, "backup_prepare_source", map[string]any{"uploadId": "deadbeef"}); c == 200 {
		t.Fatalf("prepare with bad upload id should fail")
	}

	// Release once → true, again → false.
	if c, ok := callBool(t, srv, "backup_release_source", map[string]any{"sourceId": sourceID}); c != 200 || !ok {
		t.Fatalf("release = (%d,%v), want (200,true)", c, ok)
	}
	if c, ok := callBool(t, srv, "backup_release_source", map[string]any{"sourceId": sourceID}); c != 200 || ok {
		t.Fatalf("release again = (%d,%v), want (200,false)", c, ok)
	}
	// Invalid source id → 400.
	if c, _ := call(t, srv, "backup_release_source", map[string]any{"sourceId": "nope"}); c == 200 {
		t.Fatalf("invalid source id should fail")
	}
	// scan_external_conflicts on the (now released) source → not found.
	if c, _ := call(t, srv, "backup_scan_external_conflicts", map[string]any{"sourceId": sourceID}); c == 200 {
		t.Fatalf("scan on a released source should 404")
	}
}
