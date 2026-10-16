// Package documents wires mygo.NewDocuments so YoYoCode can open, edit and
// autosave Markdown and plain-text files in their own windows — the NSDocument
// / SwiftUI DocumentGroup model: a window per file, the File menu (New, Open,
// Open Recent, Save, Save As, Duplicate, Rename, Move To, Revert), autosave in
// place, a recovery copy after a crash, a save prompt on close/quit, and
// detection of files changed by other apps.
//
// The app's primary surface stays its single main window; documents are an
// independent, decoupled feature (no backend, just local files) reached from
// the File menu or by opening a .md / .txt from Explorer / Finder / the Dock.
//
// NoUntitled keeps the app from popping a blank document window on launch.
package documents

import (
	"fmt"
	"io"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// doc is the window of one document: the text its editor shows, plus the
// mygo.Document and mygo.Window it is bound to.
type doc struct {
	doc  *mygo.Document[string]
	win  *mygo.Window
	text string
}

func (d *doc) view(c *ui.Context) {
	t := c.Theme()
	state := d.doc.State()
	ui.Column(c).Fill().Children(func() {
		// Another app changed the file out from under us: offer to keep ours
		// (overwrite) or take theirs (revert). mygo holds the document edited
		// in this state until the user resolves it.
		if state.Conflict {
			ui.Row(c).Padding(10).Gap(8).Background(t.Warning.Alpha(0.15)).Children(func() {
				ui.Text(c, "Another app changed this file. Save Anyway to keep yours, or Revert to theirs.").Grow(1).Wrap()
				if ui.Button(c.Key("revert"), "Revert").Clicked() {
					go d.doc.Revert()
				}
				if ui.PrimaryButton(c.Key("save-anyway"), "Save Anyway").Clicked() {
					go d.doc.SaveAnyway()
				}
			})
		}
		// The editor binds straight to d.text; every keystroke pushes the
		// content into the document (which marks it edited and triggers the
		// debounced autosave), and OnReload / OnStateChange push an external
		// change or revert back into d.text.
		ui.TextArea(c.Key("doc-text"), &d.text).Fill().Font("monospace").Label("Document").AutoFocus().
			OnChange(func() { d.doc.Set(d.text) })
		ui.Row(c).Padding(6, 12).Gap(12).Children(func() {
			lines := strings.Count(d.text, "\n") + 1
			ui.Text(c, fmt.Sprintf("%d words · %d lines", len(strings.Fields(d.text)), lines)).
				FontSize(12).TextColor(t.TextMuted).Grow(1)
			ui.Text(c, docStatusLabel(state)).FontSize(12).TextColor(t.TextMuted)
		})
	})
}

// docStatusLabel is the status line's word for a document's state. Conflict
// outranks Edited: a file another app changed has unsaved changes of ours too,
// but "Conflict" is the actionable state, and the bar above says what to do.
func docStatusLabel(state mygo.DocumentState) string {
	switch {
	case state.Path == "":
		return "Not saved"
	case state.ReadOnly:
		return "Read-only"
	case state.Conflict:
		return "Conflict"
	case state.Edited:
		return "Edited"
	default:
		return "Saved"
	}
}

// Register installs the document model. Call it before mygo.App.Run.
func Register() {
	mygo.NewDocuments(mygo.DocumentsOptions[string]{
		Types: []mygo.DocumentType{
			{Name: "Markdown Document", Extensions: []string{"md", "markdown"}},
			{Name: "Plain Text", Extensions: []string{"txt"}},
		},
		Read: func(r io.Reader) (string, error) {
			b, err := io.ReadAll(r)
			return string(b), err
		},
		Write: func(w io.Writer, text string) error {
			_, err := io.WriteString(w, text)
			return err
		},
		// Undoing back to what the file has leaves the document unedited, so the
		// autosave stops and the close/quit prompts stay accurate.
		Equal: func(a, b string) bool { return a == b },
		// Don't open a blank document on launch — the main window is the app's
		// primary surface, and documents are opened on demand.
		NoUntitled: true,
		Window: func(document *mygo.Document[string]) *mygo.Window {
			d := &doc{doc: document, text: document.Content()}
			win := mygo.NewWindow(mygo.WindowOptions{
				Width:    760,
				Height:   560,
				StateKey: "document",
				Content:  ui.View(d.view),
			})
			// Reverted, or changed by another app: show the file's text and
			// repaint. OnStateChange repaints so the title's edited dot and the
			// status line track the document.
			document.OnReload(func() { d.text = document.Content(); win.Update(func() {}) })
			document.OnStateChange(func(mygo.DocumentState) { win.Update(func() {}) })
			return win
		},
	})
}
