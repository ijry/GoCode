package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"gocode/locales"
)

// TestInferFromPathAndBytes pins the attachment helpers: extension → MIME and
// image flag, and the byte-size formatting a chip shows under its filename.
func TestInferFromPathAndBytes(t *testing.T) {
	cases := []struct {
		name    string
		mime    string
		isImage bool
	}{
		{"a.PNG", "image/png", true},
		{"b.jpeg", "image/jpeg", true},
		{"c.gif", "image/gif", true},
		{"shot.webp", "image/webp", true},
		{"doc.pdf", "application/pdf", false},
		{"main.go", "text/plain", false},
		{"data.json", "application/json", false},
		{"table.csv", "text/csv", false},
		{"bundle.zip", "application/zip", false},
		{"thing.unknownext", "application/octet-stream", false},
	}
	for _, tc := range cases {
		mime, isImage := inferFromPath(tc.name)
		if mime != tc.mime || isImage != tc.isImage {
			t.Errorf("inferFromPath(%q) = (%q,%v), want (%q,%v)", tc.name, mime, isImage, tc.mime, tc.isImage)
		}
	}
	for _, tc := range []struct {
		n    int64
		want string
	}{
		{512, "512 B"},
		{2048, "2.0 KB"},
		{5 * 1024 * 1024, "5.0 MB"},
		{3 * 1024 * 1024 * 1024, "3.0 GB"},
	} {
		if got := formatBytes(tc.n); got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// TestBuildPromptBlocks pins the wire shape sent to acp_prompt: the text block
// first, an image block per image attachment and a file block per other file.
func TestBuildPromptBlocks(t *testing.T) {
	blocks := buildPromptBlocks("describe this", []attachmentItem{
		{Path: "/up/shot.png", Name: "shot.png", IsImage: true},
		{Path: "/up/notes.txt", Name: "notes.txt"},
	})
	if len(blocks) != 3 {
		t.Fatalf("got %d blocks, want 3", len(blocks))
	}
	first, _ := blocks[0].(map[string]any)
	if first["type"] != "text" || first["text"] != "describe this" {
		t.Errorf("first block = %v, want text", first)
	}
	img, _ := blocks[1].(map[string]any)
	if img["type"] != "image" {
		t.Errorf("second block type = %v, want image", img["type"])
	}
	if src, ok := img["source"].(map[string]any); ok {
		if f, ok := src["file"].(map[string]any); ok && f["path"] != "/up/shot.png" {
			t.Errorf("image path = %v, want /up/shot.png", f["path"])
		} else if !ok {
			t.Errorf("image source has no file map: %v", src)
		}
		if src["type"] != "file" {
			t.Errorf("image source type = %v, want file", src["type"])
		}
	} else {
		t.Errorf("image block has no source: %v", img)
	}
	file, _ := blocks[2].(map[string]any)
	if file["type"] != "file" {
		t.Errorf("third block type = %v, want file", file["type"])
	}
}

// TestAttachmentWireRoundTrip checks the staged→wire→staged projection so the
// local user-message echo renders the same items that were uploaded.
func TestAttachmentWireRoundTrip(t *testing.T) {
	raw := attachmentsToRaw([]attachmentItem{{Path: "/up/a.png", Name: "a.png", MimeType: "image/png", Size: 42, IsImage: true}})
	back := rawAttachments(raw)
	if len(back) != 1 {
		t.Fatalf("round trip lost the attachment: %v", back)
	}
	got := back[0]
	if got.Path != "/up/a.png" || got.Name != "a.png" || got.MimeType != "image/png" || got.Size != 42 || !got.IsImage {
		t.Errorf("round trip = %+v", got)
	}
}

// TestComposerRendersAttachmentTray seeds two staged attachments, renders the
// composer, and removes one through its ✕ — proving the tray draws and its
// remove control dispatches.
func TestComposerRendersAttachmentTray(t *testing.T) {
	s := conversationShellForTest(t)
	s.composerAttachments = []attachmentItem{
		{Path: "/up/notes.txt", Name: "notes.txt", MimeType: "text/plain", Size: 2048},
		{Path: "/up/shot.png", Name: "shot.png", MimeType: "image/png", Size: 5 << 20, IsImage: true},
	}
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{"notes.txt", "2.0 KB", "shot.png"} {
		if !tt.HasText(want) {
			t.Fatalf("tray missing %q; texts = %v", want, tt.Texts())
		}
	}
	if err := tt.Click("Remove notes.txt"); err != nil {
		t.Fatalf("click remove: %v", err)
	}
	if tt.HasText("notes.txt") {
		t.Errorf("notes.txt still shown after remove; texts = %v", tt.Texts())
	}
	if !tt.HasText("shot.png") {
		t.Errorf("shot.png vanished; the wrong chip was removed; texts = %v", tt.Texts())
	}
	if n := len(s.composerAttachments); n != 1 {
		t.Errorf("composerAttachments has %d items, want 1", n)
	}
	dumpFrame(t, tt, "composer-attachments")
}

// TestComposerPaperclipStagesFiles wires the host hook, clicks the paperclip,
// and asserts a real local file gets staged — the end-to-end pick path.
func TestComposerPaperclipStagesFiles(t *testing.T) {
	s := conversationShellForTest(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "picked.png")
	if err := os.WriteFile(p, []byte("\x89PNG\r\n\x1a\nnotreallyapng"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.hostOpenFiles = func() []string { return []string{p} }
	tt := ui.NewTester(s.View, 1240, 820)
	if err := tt.Click("Attach file"); err != nil {
		t.Fatalf("click paperclip: %v", err)
	}
	s.mu.Lock()
	n := len(s.composerAttachments)
	name := ""
	if n == 1 {
		name = s.composerAttachments[0].Name
	}
	s.mu.Unlock()
	if n != 1 || name != "picked.png" {
		t.Fatalf("paperclip staged %d items (name %q), want picked.png", n, name)
	}
	if !tt.HasText("picked.png") {
		t.Errorf("staged file not rendered; texts = %v", tt.Texts())
	}
}

// TestUserMessageRendersAttachments pushes the local user-message echo and
// asserts the sent attachments render inline in the user bubble.
func TestUserMessageRendersAttachments(t *testing.T) {
	s := conversationShellForTest(t)
	ev := map[string]any{
		"connection_id": "c1",
		"seq":           float64(9),
		"type":          "user_message",
		"text":          "here is the schema",
		"attachments": []any{
			map[string]any{"path": "/up/schema.sql", "name": "schema.sql", "mimeType": "application/octet-stream", "size": float64(1024), "isImage": false},
		},
	}
	s.stream.handle(&wsFrame{Type: "event", Envelope: ev})

	items := buildTranscript(s.stream.Events("c1"), locales.In("en"))
	var found bool
	for _, it := range items {
		if it.role == "user" && len(it.atts) == 1 && it.atts[0].Name == "schema.sql" {
			found = true
		}
	}
	if !found {
		t.Fatalf("user_message did not fold into a bubble with attachments: %+v", items)
	}
	tt := ui.NewTester(s.View, 1240, 820)
	for _, want := range []string{"here is the schema", "schema.sql", "1.0 KB"} {
		if !tt.HasText(want) {
			t.Errorf("bubble missing %q; texts = %v", want, tt.Texts())
		}
	}
	dumpFrame(t, tt, "user-attachments")
}

// TestUploadAttachmentHTTP drives the multipart upload against a stub backend
// and asserts the stored item is parsed back out of the response.
func TestUploadAttachmentHTTP(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(src, []byte("hello report"), 0o644); err != nil {
		t.Fatal(err)
	}
	var gotSessionID, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/upload_attachment" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		gotSessionID = r.FormValue("session_id")
		f, _, err := r.FormFile("file")
		if err != nil {
			t.Errorf("multipart file field missing: %v", err)
		}
		if f != nil {
			b, _ := io.ReadAll(f)
			gotBody = string(b)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"path":"/home/u/.gocode/uploads/c1/report.txt","name":"report.txt","size":12,"mimeType":"text/plain"}`))
	}))
	defer srv.Close()

	s := conversationShellForTest(t)
	s.B = &Backend{URL: srv.URL, Token: "tok"}
	item, err := s.uploadAttachmentHTTP("c1", src)
	if err != nil {
		t.Fatalf("uploadAttachmentHTTP: %v", err)
	}
	if gotSessionID != "c1" {
		t.Errorf("session_id = %q, want c1", gotSessionID)
	}
	if gotBody != "hello report" {
		t.Errorf("uploaded body = %q, want the file's bytes", gotBody)
	}
	if item.Path != "/home/u/.gocode/uploads/c1/report.txt" || item.Name != "report.txt" || item.Size != 12 {
		t.Errorf("parsed item = %+v", item)
	}
	if item.IsImage {
		t.Errorf("a .txt must not be treated as an image")
	}
}

// TestUploadAttachmentHTTPError checks a non-2xx upload surfaces as an error
// rather than a bogus item, so sendPrompt reports it.
func TestUploadAttachmentHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(`{"code":"io_error","message":"too big"}`))
	}))
	defer srv.Close()
	src := filepath.Join(t.TempDir(), "nope.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := conversationShellForTest(t)
	s.B = &Backend{URL: srv.URL}
	_, err := s.uploadAttachmentHTTP("c1", src)
	if err == nil {
		t.Fatal("expected an error on a 413 upload")
	}
}
