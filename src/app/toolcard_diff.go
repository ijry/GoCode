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
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
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

// viewToolBody renders the expanded tool-call body: a colorized diff when the
// result or arguments carry a file change, highlighted code for Read/Write, a
// command + output block for Bash, and a muted fallback otherwise.
func (s *Shell) viewToolBody(c *ui.Context, tc *toolCard, t *ui.Theme) {
	// A real unified diff in the result wins: colorize it as-is.
	if tc.output != "" && looksLikeDiff(tc.output) {
		s.viewUnifiedDiff(c, t, tc.output)
		return
	}
	in := parseToolInput(tc.rawInput)
	switch strings.ToLower(tc.kind) {
	case "edit":
		s.viewFileHeader(c, t, in.FilePath)
		if in.OldString != "" || in.NewString != "" {
			s.viewUnifiedDiff(c, t, unifiedDiff(in.OldString, in.NewString, in.FilePath))
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
			s.viewUnifiedDiff(c, t, unifiedDiff(e.OldString, e.NewString, in.FilePath))
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

// viewUnifiedDiff renders a unified-diff text with per-line coloring: removed
// lines red, added lines green, headers and hunk marks muted, each line
// fronted by its old/new line number in a narrow monospace gutter. Each line is
// one RichText (gutter + content spans) rather than a Row of children, which
// keeps the offscreen renderer cheap for large diffs.
func (s *Shell) viewUnifiedDiff(c *ui.Context, t *ui.Theme, text string) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	// Drop a single trailing blank line so "a\nb\n" reads as two lines.
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	oldNo, newNo := 0, 0
	ui.Column(c).FillWidth().Gap(0).Children(func() {
		for _, line := range lines {
			line := line
			switch {
			case strings.HasPrefix(line, "diff --git"),
				strings.HasPrefix(line, "index "),
				strings.HasPrefix(line, "--- "),
				strings.HasPrefix(line, "+++ "):
				ui.RichText(c, ui.Span{Text: line, Font: monoFont, Color: t.TextMuted}).
					FontSize(11)
			case strings.HasPrefix(line, "@@"):
				oldNo, newNo = parseHunkStart(line)
				ui.RichText(c, ui.Span{Text: line, Font: monoFont, Color: t.TextMuted}).
					FontSize(11)
			default:
				var col ui.Color
				var lno, rno string
				switch {
				case strings.HasPrefix(line, "-"):
					col = t.Danger
					lno = itoa("", oldNo)
					oldNo++
				case strings.HasPrefix(line, "+"):
					col = t.Success
					rno = itoa("", newNo)
					newNo++
				default:
					col = t.Text
					lno = itoa("", oldNo)
					rno = itoa("", newNo)
					oldNo++
					newNo++
				}
				content := line
				if len(content) > 0 {
					content = content[1:]
				}
				ui.RichText(c,
					ui.Span{Text: pad3(lno) + " ", Font: monoFont, Color: t.TextMuted},
					ui.Span{Text: pad3(rno) + " ", Font: monoFont, Color: t.TextMuted},
					ui.Span{Text: content, Font: monoFont, Color: col},
				).FontSize(11).Wrap()
			}
		}
	})
}

// pad3 right-pads a line number to three columns so the gutter aligns.
func pad3(s string) string {
	for len(s) < 3 {
		s = " " + s
	}
	return s
}

// reHunk matches a unified-diff hunk header like "@@ -1,4 +1,5 @@".
var reHunk = regexp.MustCompile(`@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// parseHunkStart returns the old/new starting line numbers from a hunk header.
func parseHunkStart(line string) (int, int) {
	m := reHunk.FindStringSubmatch(line)
	if len(m) < 3 {
		return 1, 1
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	return a, b
}

// unifiedDiff computes a minimal unified diff between two texts (one hunk
// covering the whole file) using a longest-common-subsequence line alignment.
func unifiedDiff(oldText, newText, path string) string {
	oldLines := splitLines(oldText)
	newLines := splitLines(newText)
	n, m := len(oldLines), len(newLines)
	if n == 0 && m == 0 {
		return ""
	}
	// Longest-common-subsequence table for a minimal line diff.
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var b strings.Builder
	b.WriteString("--- " + path + "\n")
	b.WriteString("+++ " + path + "\n")
	fmt.Fprintf(&b, "@@ -1,%d +1,%d @@\n", n, m)
	i, j := 0, 0
	for i < n && j < m {
		if oldLines[i] == newLines[j] {
			b.WriteString(" " + oldLines[i] + "\n")
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			b.WriteString("-" + oldLines[i] + "\n")
			i++
		} else {
			b.WriteString("+" + newLines[j] + "\n")
			j++
		}
	}
	for ; i < n; i++ {
		b.WriteString("-" + oldLines[i] + "\n")
	}
	for ; j < m; j++ {
		b.WriteString("+" + newLines[j] + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// splitLines turns text into lines on \n (normalizing \r\n) without inventing a
// trailing empty line for a final newline.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
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
