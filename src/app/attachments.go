package app

// attachments.go renders staged and inline file/image attachments: the
// composer's attachment tray (with remove controls) and the read-only chips
// shown under a sent user message. It mirrors upstream's composer attachment
// preview and the MessageContent image/file blocks, translated into mygo.

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

// attachmentItem is one file the user staged in the composer (or that rode
// along in a sent turn). Path is the local file the agent can read — for a
// staged item it is the original pick; for a sent one it is the copy
// upload_attachment wrote under the uploads root.
type attachmentItem struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
	IsImage  bool   `json:"isImage"`
}

// inferFromPath maps a filename to a MIME type and an image flag. Anything we
// cannot name falls back to octet-stream (non-image), so it still renders as a
// file chip rather than erroring out.
func inferFromPath(name string) (mime string, isImage bool) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	case ".bmp":
		return "image/bmp", true
	case ".svg":
		return "image/svg+xml", true
	case ".pdf":
		return "application/pdf", false
	case ".txt", ".md", ".go", ".py", ".js", ".ts", ".rs", ".c", ".h", ".log":
		return "text/plain", false
	case ".json":
		return "application/json", false
	case ".csv":
		return "text/csv", false
	case ".zip", ".tar", ".gz", ".tgz":
		return "application/zip", false
	default:
		return "application/octet-stream", false
	}
}

// formatBytes renders a byte count the way a file manager would.
func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// loadLocalImage decodes a local image file into a mygo Bitmap for thumbnails.
// Returns nil when the file is missing or is not a decodable image, so callers
// can fall back to a file glyph.
func loadLocalImage(path string) *ui.Bitmap {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	return ui.NewBitmap(img)
}

// viewAttachmentChip renders one attachment: a decoded thumbnail for images,
// otherwise a file glyph plus name and size. When onRemove is non-nil a small
// ✕ is drawn and wired to it (the composer tray); inline bubbles pass nil.
func (s *Shell) viewAttachmentChip(c *ui.Context, key string, a attachmentItem, onRemove func()) {
	t := c.Theme()
	ui.Row(c.Key(key)).Gap(8).AlignItems(ui.Center).Children(func() {
		if a.IsImage {
			if b := loadLocalImage(a.Path); b != nil {
				ui.Image(c, b).Size(36, 36).Radius(6)
			} else {
				ui.Icon(c, iconImage).Width(20).Height(20).TextColor(t.TextMuted)
			}
		} else {
			ui.Icon(c, iconFile).Width(20).Height(20).TextColor(t.TextMuted)
		}
		ui.Column(c).Gap(0).Children(func() {
			ui.Text(c, a.Name).FontSize(12).MaxLines(1).TextColor(t.Text)
			if a.Size > 0 {
				ui.Text(c, formatBytes(a.Size)).FontSize(11).TextColor(t.TextMuted)
			}
		})
		if onRemove != nil {
			x := ui.Row(c).Width(20).Height(20).AlignItems(ui.Center).Justify(ui.Center).
				Label(cat(c).AttRemove(a.Name)).Tooltip(cat(c).AttRemove(a.Name))
			x.Children(func() {
				ui.Icon(c, iconX).Width(13).Height(13).TextColor(t.TextMuted)
			})
			if x.Clicked() {
				onRemove()
			}
		}
	})
}

// viewAttachmentTray renders the composer's staged attachments above the input.
// An empty tray draws nothing, so the composer chrome is unchanged when there
// is nothing staged.
// stagedAttachments returns a snapshot of the composer's staged attachments.
// The view reads the slice every frame while the composer path reassigns it, so
// it copies under the lock rather than racing the backing array.
func (s *Shell) stagedAttachments() []attachmentItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.composerAttachments) == 0 {
		return nil
	}
	out := make([]attachmentItem, len(s.composerAttachments))
	copy(out, s.composerAttachments)
	return out
}

func (s *Shell) viewAttachmentTray(c *ui.Context) {
	atts := s.stagedAttachments()
	if len(atts) == 0 {
		return
	}
	ui.Column(c.Key("composer-attach")).FillWidth().Gap(4).Padding(0, 0, 6, 0).Children(func() {
		for _, a := range atts {
			a := a
			s.viewAttachmentChip(c, "attach-"+a.Name, a, func() {
				s.removeAttachment(a.Path)
			})
		}
	})
}

// viewAttachmentsInline renders read-only chips for a sent message's
// attachments (images as thumbnails, everything else as file chips).
func (s *Shell) viewAttachmentsInline(c *ui.Context, key string, atts []attachmentItem) {
	if len(atts) == 0 {
		return
	}
	ui.Column(c.Key(key)).FillWidth().Gap(6).Padding(6, 0, 0, 0).Children(func() {
		for i, a := range atts {
			s.viewAttachmentChip(c, key+"-"+itoa("", i), a, nil)
		}
	})
}

// removeAttachment drops one staged attachment by path and repaints.
func (s *Shell) removeAttachment(path string) {
	s.mu.Lock()
	out := s.composerAttachments[:0]
	for _, a := range s.composerAttachments {
		if a.Path != path {
			out = append(out, a)
		}
	}
	s.composerAttachments = out
	s.mu.Unlock()
	s.svc.Invalidate()
}

// addAttachment stages a local file the user picked, inferring its type and
// size. Unreadable paths are ignored rather than surfaced as a broken chip.
func (s *Shell) addAttachment(path string) {
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	name := filepath.Base(path)
	mime, isImage := inferFromPath(name)
	s.mu.Lock()
	s.composerAttachments = append(s.composerAttachments, attachmentItem{
		Path:     path,
		Name:     name,
		MimeType: mime,
		Size:     fi.Size(),
		IsImage:  isImage,
	})
	s.mu.Unlock()
}

// pickAttachment opens the OS file picker (multi-select) and stages every
// path it returns. Headless tests inject hostOpenFiles; the real build runs
// mygo.Dialog on a goroutine (addAttachment takes s.mu itself).
func (s *Shell) pickAttachment() {
	s.pickPaths("Attach files", false, true, func(paths []string) {
		for _, p := range paths {
			s.addAttachment(p)
		}
		s.svc.Invalidate()
	})
}

// attachmentsToRaw projects staged items into the wire shape the local
// transcript uses, so a sent turn can render its attachments without re-reading
// the uploads root.
func attachmentsToRaw(atts []attachmentItem) []map[string]any {
	out := make([]map[string]any, 0, len(atts))
	for _, a := range atts {
		out = append(out, map[string]any{
			"path":     a.Path,
			"name":     a.Name,
			"mimeType": a.MimeType,
			"size":     a.Size,
			"isImage":  a.IsImage,
		})
	}
	return out
}

// rawAttachments reconstructs attachmentItems from the wire shape.
func rawAttachments(raws []map[string]any) []attachmentItem {
	out := make([]attachmentItem, 0, len(raws))
	for _, r := range raws {
		out = append(out, attachmentItem{
			Path:     str(r, "path"),
			Name:     str(r, "name"),
			MimeType: str(r, "mimeType"),
			Size:     intVal(r, "size"),
			IsImage:  asBool(r["isImage"]),
		})
	}
	return out
}
