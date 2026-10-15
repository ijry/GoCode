package app

// attachment_upload.go streams a staged local file to the backend's
// upload_attachment endpoint and returns the stored attachmentItem the prompt
// can reference. It is the client half of the already-wired upload_attachment
// port (src-go/web/handlers/upload_attachment.go).

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

// uploadAttachmentHTTP POSTs one local file as multipart/form-data to
// <backend>/api/upload_attachment (fields: session_id, file) and parses the
// stored {path,name,size,mimeType} back into an attachmentItem. The stored path
// is what the agent reads, so callers should use the returned item, not the
// original pick.
func (s *Shell) uploadAttachmentHTTP(connID, filePath string) (attachmentItem, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if connID != "" {
		_ = w.WriteField("session_id", connID)
	}
	part, err := w.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return attachmentItem{}, err
	}
	f, err := os.Open(filePath)
	if err != nil {
		return attachmentItem{}, err
	}
	_, err = io.Copy(part, f)
	f.Close()
	if err != nil {
		return attachmentItem{}, err
	}
	if err := w.Close(); err != nil {
		return attachmentItem{}, err
	}

	req, err := http.NewRequest(http.MethodPost, s.B.URL+"/api/upload_attachment", &buf)
	if err != nil {
		return attachmentItem{}, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if s.B.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.B.Token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return attachmentItem{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return attachmentItem{}, &uploadError{status: resp.StatusCode, body: string(body)}
	}
	// Decode into a generic map and project onto attachmentItem; tolerate a
	// missing mimeType (the port returns it, but be defensive).
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	if raw == nil {
		return attachmentItem{}, &uploadError{status: resp.StatusCode, body: string(body)}
	}
	name := str(raw, "name")
	if name == "" {
		name = filepath.Base(filePath)
	}
	item := attachmentItem{
		Path:     str(raw, "path"),
		Name:     name,
		MimeType: str(raw, "mimeType"),
		Size:     intVal(raw, "size"),
	}
	if item.MimeType == "" {
		item.MimeType, item.IsImage = inferFromPath(item.Name)
	} else {
		_, item.IsImage = inferFromPath(item.Name)
	}
	return item, nil
}

// uploadError carries a non-2xx upload status for friendlier surfacing.
type uploadError struct {
	status int
	body   string
}

func (e *uploadError) Error() string {
	return "upload_attachment failed: status " + itoa("", e.status)
}

// buildPromptBlocks turns the user's text and uploaded attachments into the
// ACP content-block list acp_prompt expects. Images become image blocks with a
// file source (the agent reads the stored copy); other files become a generic
// file block the agent may attach or ignore. An empty text still sends a blank
// text block so attachments are never dropped when the prompt itself is empty.
func buildPromptBlocks(text string, atts []attachmentItem) []any {
	blocks := []any{map[string]any{"type": "text", "text": text}}
	for _, a := range atts {
		if a.IsImage {
			blocks = append(blocks, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type": "file",
					"file": map[string]any{"path": a.Path},
				},
			})
		} else {
			blocks = append(blocks, map[string]any{
				"type": "file",
				"source": map[string]any{
					"type": "file",
					"file": map[string]any{"path": a.Path, "filename": a.Name},
				},
			})
		}
	}
	return blocks
}
