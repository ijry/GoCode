package handlers_test

// backup_create_ticket + backup_download (raw GET) coverage, end to end over
// HTTP: build an archive, download it once via the ticket, confirm it is a real
// ZIP (magic + readable manifest), and that the ticket is single-use. Isolated
// via GOCODE_HOME so no real ~/.gocode section trees are packed.

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocode/src-go/backup"
)

func TestBackupCreateTicketAndDownload(t *testing.T) {
	t.Setenv("GOCODE_HOME", t.TempDir())
	srv := newTestServer(t)

	code, out := call(t, srv, "backup_create_ticket", nil)
	if code != 200 {
		t.Fatalf("create status %d: %v", code, out)
	}
	url, _ := out["url"].(string)
	if !strings.HasPrefix(url, "/backup_download/") {
		t.Fatalf("ticket url wrong: %v", url)
	}
	if _, ok := out["degradedSqlite"]; !ok {
		t.Fatalf("ticket result must carry degradedSqlite: %v", out)
	}

	resp, err := http.Get(srv.URL + url)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	zbody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("download status %d", resp.StatusCode)
	}
	if !bytes.HasPrefix(zbody, []byte("PK")) {
		t.Fatalf("downloaded archive is not a zip (len %d)", len(zbody))
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("download should be an attachment, got %q", cd)
	}

	zp := filepath.Join(t.TempDir(), "b.zip")
	if err := os.WriteFile(zp, zbody, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := backup.ReadManifest(zp)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if m.Kind != backup.BackupKind {
		t.Fatalf("manifest kind = %q", m.Kind)
	}
	hasDB := false
	for _, e := range m.Entries {
		if e.Path == backup.DBEntry {
			hasDB = true
		}
	}
	if !hasDB {
		t.Fatalf("archive is missing the db snapshot entry: %v", m.Entries)
	}

	// Single-use: redeeming the same ticket again → 404.
	resp2, _ := http.Get(srv.URL + url)
	io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode == 200 {
		t.Fatalf("download ticket should be single-use")
	}
}

func TestBackupCreateTicketRejectsPassphrase(t *testing.T) {
	t.Setenv("GOCODE_HOME", t.TempDir())
	srv := newTestServer(t)
	if code, out := call(t, srv, "backup_create_ticket", map[string]any{"passphrase": "secret"}); code == 200 {
		t.Fatalf("encrypted backup must be rejected (crypto not yet ported), got 200: %v", out)
	}
}

func TestBackupDownloadBogusTicket(t *testing.T) {
	srv := newTestServer(t)
	resp, _ := http.Get(srv.URL + "/backup_download/deadbeef")
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode == 200 {
		t.Fatalf("bogus ticket should 404")
	}
}
