package handlers_test

// backup_upload coverage: the multipart `file` save that opens the decoupled
// restore flow (upload once → prepare/stage). Asserts the wire shape + id
// format on success, and the two error paths (missing file field, over cap).

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
)

func callMultipart(t *testing.T, srvURL, cmd string, body []byte, contentType string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srvURL+"/api/"+cmd, bytes.NewReader(body))
	req.Header.Set("content-type", contentType)
	req.Header.Set("authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", cmd, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func isHex32(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func TestBackupUploadSaveAndShape(t *testing.T) {
	srv := newTestServer(t)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", "restore.me")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("PK-archive-bytes-here"))
	_ = w.WriteField("note", "ignored")
	_ = w.Close()

	code, raw := callMultipart(t, srv.URL, "backup_upload", buf.Bytes(), w.FormDataContentType())
	if code != 200 {
		t.Fatalf("upload status %d: %s", code, raw)
	}
	var out struct {
		UploadID string `json:"upload_id"`
		FileName string `json:"file_name"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	if !isHex32(out.UploadID) {
		t.Fatalf("upload_id must be a 32-char hex id, got %q", out.UploadID)
	}
	if out.FileName != "restore.me" {
		t.Fatalf("file_name = %q (want restore.me)", out.FileName)
	}
}

func TestBackupUploadMissingFileField(t *testing.T) {
	srv := newTestServer(t)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("note", "no file here")
	_ = w.Close()
	code, _ := callMultipart(t, srv.URL, "backup_upload", buf.Bytes(), w.FormDataContentType())
	if code == 200 {
		t.Fatalf("missing `file` field should be rejected, got 200")
	}
}

func TestBackupUploadOverCap(t *testing.T) {
	t.Setenv("GOCODE_BACKUP_UPLOAD_MAX_BYTES", "8")
	srv := newTestServer(t)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", "big.bin")
	_, _ = fw.Write([]byte("this-is-definitely-more-than-eight-bytes"))
	_ = w.Close()
	code, _ := callMultipart(t, srv.URL, "backup_upload", buf.Bytes(), w.FormDataContentType())
	if code == 200 {
		t.Fatalf("upload over the cap should be rejected, got 200")
	}
}
