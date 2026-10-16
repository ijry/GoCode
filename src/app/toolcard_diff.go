package app

// toolcard_diff.go renders the body of a tool-call card the way upstream's Tool
// shell does: when the agent's tool result (or its raw arguments) carry a file
// edit, the card shows a colorized unified diff instead of a bare prose dump.
// Edit/Write/Read/Bash each get a purpose-built rendering; everything else
// falls back to the existing muted one-line detail.
//
// The upstream card detects a `@@` / `diff --git` signature in the tool output
// and routes it to UnifiedDiffPreview; we mirror that and additionally compute a
// diff from an Edit tool's old_string/new_string arguments (our edit result is
// often "Edited N lines", not a diff), so the change is visible either way.

import (
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// toolInput is the subset of a tool call's raw JSON arguments we surface: the
// file path, the edit operands, a write's full content, or a shell command.
type toolInput struct {
	FilePath  string `json:"file_path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
	Content   string `json:"content"`
	Command   string `json:"command"`
	Edits     []struct {
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	} `json:"edits"`
}

// parseToolInput decodes the tool call's raw JSON arguments; an empty or
// malformed body yields a zero toolInput (the card then falls back to prose).
func parseToolInput(raw string) toolInput {
	if strings.TrimSpace(raw) == "" {
		return toolInput{}
	}
	var in toolInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return toolInput{}
	}
	return in
}

// looksLikeDiff reports whether text is a unified diff (so the card colorizes
// it) rather than free-form output.
func looksLikeDiff(text string) bool {
	return strings.Contains(text, "@@") || strings.Contains(text, "diff --git")
}

// viewToolBody renders the expanded tool-call body: a diff when the result or
// arguments carry a file change, highlighted code for Read/Write, a command +
// output block for Bash, and a muted fallback otherwise. key identifies the card
// so the diff views can keep their per-card state.
func (s *Shell) viewToolBody(c *ui.Context, tc *toolCard, key string, t *ui.Theme) {
	// A real unified diff in the result wins: show it as a patch.
	if tc.output != "" && looksLikeDiff(tc.output) {
		if s.viewPatch(c, key, tc.output, t) {
			return
		}
	}
	in := parseToolInput(tc.rawInput)
	switch strings.ToLower(tc.kind) {
	case "edit":
		s.viewFileHeader(c, t, in.FilePath)
		if in.OldString != "" || in.NewString != "" {
			s.viewToolDiff(c, key, in.OldString, in.NewString, in.FilePath)
			return
		}
	case "multi_edit":
		s.viewFileHeader(c, t, in.FilePath)
		rendered := false
		for i, e := range in.Edits {
			if e.OldString == "" && e.NewString == "" {
				continue
			}
			ui.Text(c, "edit "+itoa("", i+1)).FontSize(11).TextColor(t.TextMuted)
			s.viewToolDiff(c, itoa(key+"-e", i), e.OldString, e.NewString, in.FilePath)
			rendered = true
		}
		if rendered {
			return
		}
	case "write":
		s.viewFileHeader(c, t, in.FilePath)
		if in.Content != "" {
			s.viewHighlightedCode(c, t, in.Content, langFromFile(in.FilePath))
			return
		}
	case "read":
		s.viewFileHeader(c, t, in.FilePath)
		if tc.output != "" {
			s.viewHighlightedCode(c, t, tc.output, langFromFile(in.FilePath))
			return
		}
	case "bash", "shell", "command":
		if in.Command != "" {
			ui.RichText(c, ui.Span{Text: "$ " + in.Command, Font: monoFont, Color: t.Text}).
				FontSize(11)
		}
		if tc.output != "" {
			s.viewHighlightedCode(c, t, tc.output, "")
			return
		}
	}
	// Fallback: the existing muted one-line detail (or a friendly empty state).
	ui.Text(c, firstNonEmpty(tc.output, tc.kind, "No output recorded.")).
		FontSize(12).TextColor(t.TextMuted).Wrap()
}

// viewFileHeader shows the touched file path (muted monospace) above the diff.
func (s *Shell) viewFileHeader(c *ui.Context, t *ui.Theme, path string) {
	if path == "" {
		return
	}
	ui.RichText(c, ui.Span{Text: path, Font: monoFont, Color: t.TextMuted}).
		FontSize(11).Wrap()
}

// viewHighlightedCode renders multi-line code with per-line token highlight.
func (s *Shell) viewHighlightedCode(c *ui.Context, t *ui.Theme, text, lang string) {
	for _, line := range strings.Split(text, "\n") {
		ui.RichText(c, highlightLine(line, lang, t)...).FontSize(11).Wrap()
	}
}

// viewDiff renders old→new with mygo's editor.DiffView: a code editor on each
// side carrying the editor's language highlighting, git's histogram line
// alignment, word-level inline marks on changed lines, collapsed context lines
// behind "N unmodified lines" separators, and +/− indicators in the gutter.
// The state is kept per card so its expansions and scroll survive repaints.
func (s *Shell) viewToolDiff(c *ui.Context, key, oldText, newText, path string) {
	editor.DiffView(c, s.diffState(key, oldText, newText, path)).FillWidth()
}

// diffState returns the card's DiffState, rebuilding a buffer when its text
// changed (an Edit's arguments can stream in more than once).
func (s *Shell) diffState(key, oldText, newText, path string) *editor.DiffState {
	if s.toolDiff == nil {
		s.toolDiff = map[string]*editor.DiffState{}
	}
	lang := editor.LanguageFor(path)
	d := s.toolDiff[key]
	if d == nil {
		old, nw := editor.NewBuffer(oldText), editor.NewBuffer(newText)
		old.SetLanguage(lang)
		nw.SetLanguage(lang)
		d = &editor.DiffState{Old: old, New: nw, Layout: editor.DiffUnified,
			Wrap: true, FitContent: true}
		s.toolDiff[key] = d
		return d
	}
	if d.Old.String() != oldText {
		d.Old.Set(oldText)
		d.Old.SetLanguage(lang)
	}
	if d.New.String() != newText {
		d.New.Set(newText)
		d.New.SetLanguage(lang)
	}
	return d
}

// viewPatch shows a real unified diff (git's output) through the editor's patch
// reader, which numbers lines as the files do and puts each hunk under its
// header. It reports whether the text was a patch it could show.
func (s *Shell) viewPatch(c *ui.Context, key, patch string, t *ui.Theme) bool {
	files, err := editor.ParsePatch(patch)
	if err != nil || len(files) == 0 {
		return false
	}
	if s.toolDiff == nil {
		s.toolDiff = map[string]*editor.DiffState{}
	}
	for i := range files {
		// The diff view draws the lines itself; keep the path in our own
		// header (muted monospace) as the card's file label.
		if name := firstNonEmpty(files[i].NewName, files[i].OldName); name != "" {
			ui.RichText(c, ui.Span{Text: name, Font: monoFont, Color: t.TextMuted}).
				FontSize(11)
		}
		k := itoa(key+"-p", i)
		d, ok := s.toolDiff[k]
		if !ok {
			d = &editor.DiffState{Layout: editor.DiffUnified, Wrap: true, FitContent: true}
			s.toolDiff[k] = d
		}
		d.SetPatch(files[i])
		editor.DiffView(c, d).FillWidth()
	}
	return true
}

// langFromFile infers a highlight language from a file path's extension.
func langFromFile(path string) string {
	p := strings.ToLower(path)
	switch {
	case strings.HasSuffix(p, ".go"):
		return "go"
	case strings.HasSuffix(p, ".py"):
		return "python"
	case strings.HasSuffix(p, ".rs"):
		return "rust"
	case strings.HasSuffix(p, ".rb"):
		return "ruby"
	case strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".tsx"):
		return "typescript"
	case strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".jsx"):
		return "javascript"
	case strings.HasSuffix(p, ".java"), strings.HasSuffix(p, ".kt"), strings.HasSuffix(p, ".kotlin"):
		return "java"
	case strings.HasSuffix(p, ".json"), strings.HasSuffix(p, ".jsonc"):
		return "json"
	case strings.HasSuffix(p, ".yml"), strings.HasSuffix(p, ".yaml"):
		return "yaml"
	case strings.HasSuffix(p, ".toml"):
		return "toml"
	case strings.HasSuffix(p, ".sh"):
		return "bash"
	case strings.HasSuffix(p, ".md"):
		return "markdown"
	default:
		return ""
	}
}
