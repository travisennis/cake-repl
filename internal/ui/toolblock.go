package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// DefaultOutputLimit is how many characters of tool output are shown before
// truncation.
const DefaultOutputLimit = 2000

// unknownArgsLimit caps the JSON summary shown for unrecognized tools.
const unknownArgsLimit = 160

// maxEditPreviews caps how many edit pairs are summarized for the edit tool.
const maxEditPreviews = 3

// SummarizeToolArgs renders a concise, single-purpose summary of a tool
// call's arguments. Tool names are matched case-insensitively; the original
// name is left untouched by callers.
//
// The summary is not sanitized: decoded arguments can contain terminal control
// sequences, and callers must pass the result through [Sanitize] before it
// reaches the terminal. Sanitizing here instead would not work, because
// control characters arrive JSON-escaped and only become control bytes once
// this function decodes them.
func SummarizeToolArgs(name, argsJSON string) string {
	switch strings.ToLower(name) {
	case "bash":
		return summarizeBash(argsJSON)
	case "read":
		return summarizeRead(argsJSON)
	case "edit":
		return summarizeEdit(argsJSON)
	case "write":
		return summarizeWrite(argsJSON)
	default:
		return compactJSON(argsJSON)
	}
}

func summarizeBash(argsJSON string) string {
	var args struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.Command == "" {
		return compactJSON(argsJSON)
	}
	// Keep the command (first and last lines for multi-line commands) so the
	// header can wrap it instead of truncating; width-based clipping happened
	// here previously and lost the tail of long commands.
	s := "$ " + summarizeCommand(args.Command)
	if args.Cwd != "" {
		s += "  (cwd: " + args.Cwd + ")"
	}
	return s
}

func summarizeRead(argsJSON string) string {
	var args struct {
		Path      string `json:"path"`
		StartLine *int   `json:"start_line"`
		EndLine   *int   `json:"end_line"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.Path == "" {
		return compactJSON(argsJSON)
	}
	switch {
	case args.StartLine != nil && args.EndLine != nil:
		return fmt.Sprintf("%s (lines %d-%d)", args.Path, *args.StartLine, *args.EndLine)
	case args.StartLine != nil:
		return fmt.Sprintf("%s (from line %d)", args.Path, *args.StartLine)
	case args.EndLine != nil:
		return fmt.Sprintf("%s (through line %d)", args.Path, *args.EndLine)
	default:
		return args.Path
	}
}

func summarizeEdit(argsJSON string) string {
	var args struct {
		Path  string `json:"path"`
		Edits []struct {
			OldText string `json:"old_text"`
			NewText string `json:"new_text"`
		} `json:"edits"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.Path == "" {
		return compactJSON(argsJSON)
	}
	lines := []string{fmt.Sprintf("%s (%d edit%s)", args.Path, len(args.Edits), plural(len(args.Edits)))}
	for i, e := range args.Edits {
		if i == maxEditPreviews {
			lines = append(lines, fmt.Sprintf("  … %d more", len(args.Edits)-maxEditPreviews))
			break
		}
		lines = append(lines, fmt.Sprintf("  - %s → %s", firstLineOf(e.OldText, 60), firstLineOf(e.NewText, 60)))
	}
	return strings.Join(lines, "\n")
}

func summarizeWrite(argsJSON string) string {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.Path == "" {
		return compactJSON(argsJSON)
	}
	lineCount := strings.Count(args.Content, "\n")
	if args.Content != "" && !strings.HasSuffix(args.Content, "\n") {
		lineCount++
	}
	s := fmt.Sprintf("%s (%d line%s, %d bytes)", args.Path, lineCount, plural(lineCount), len(args.Content))
	if first := firstNonEmptyLine(args.Content); first != "" {
		s += fmt.Sprintf(" %q", truncateString(first, 60))
	}
	return s
}

// TruncateOutput limits s to roughly limit bytes, preferring to cut at a
// line boundary, and appends a marker with the original size. A limit of 0
// or less uses DefaultOutputLimit.
//
// It does not sanitize: callers pass already-[Sanitize]d text, so the reported
// size is that of the sanitized output and the byte budget is spent on
// characters the user can actually see.
func TruncateOutput(s string, limit int) string {
	s = strings.TrimRight(s, "\n")
	if limit <= 0 {
		limit = DefaultOutputLimit
	}
	if len(s) <= limit {
		return s
	}
	original := len(s)
	cut := cutRuneSafe(s, limit)
	// Prefer a line boundary if one exists in the back half of the cut.
	if i := strings.LastIndexByte(cut, '\n'); i > limit/2 {
		cut = cut[:i]
	}
	return cut + fmt.Sprintf("\n… truncated (%d bytes total)", original)
}

// cutRuneSafe returns at most n leading bytes of s without splitting a
// multibyte rune.
func cutRuneSafe(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func compactJSON(s string) string {
	var buf strings.Builder
	var v any
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		if b, err := json.Marshal(v); err == nil {
			buf.Write(b)
		}
	}
	out := buf.String()
	if out == "" {
		out = strings.TrimSpace(s)
	}
	return truncateString(strings.ReplaceAll(out, "\n", " "), unknownArgsLimit)
}

// truncateString limits s to n terminal cells, ellipsized. Counting cells
// rather than bytes keeps multibyte runes intact and makes width-based
// limits honest for wide (CJK, emoji) characters.
func truncateString(s string, n int) string {
	return ansi.Truncate(s, n, "…")
}

func firstLineOf(s string, n int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + "…"
	}
	return truncateString(s, n)
}

// summarizeCommand renders a shell command for the tool header. A single-line
// command is shown in full. A multi-line command shows its first and last
// non-empty lines joined by " … " so setup lines (cd, export) never hide the
// actual payload, while the opening line keeps the command's context. No width
// clipping is applied; callers wrap the result themselves.
func summarizeCommand(s string) string {
	lines := nonEmptyLines(s)
	if len(lines) == 0 {
		return ""
	}
	if len(lines) == 1 {
		return lines[0]
	}
	return lines[0] + " … " + lines[len(lines)-1]
}

// nonEmptyLines splits s on newlines and returns the space-trimmed non-empty
// lines in order.
func nonEmptyLines(s string) []string {
	raw := strings.Split(s, "\n")
	lines := make([]string, 0, len(raw))
	for _, l := range raw {
		if t := strings.TrimSpace(l); t != "" {
			lines = append(lines, t)
		}
	}
	return lines
}

// firstLineOnly returns the portion of s before the first newline, with no
// truncation or marker. Used to split a multi-line summary into the header
// line (returned here) and the trailing detail lines (handled by the caller).
func firstLineOnly(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// wrapToolArgs wraps s to fit within contentWidth terminal cells, breaking at
// whitespace boundaries and only hard-wrapping a single token that is longer
// than the available width (so long paths or flags are never lost and CLI
// flags like --no-pager are not split at their hyphens). Wrap is cell-aware so
// wide runes count correctly. Continuation lines are indented by indent cells
// so wrapped args align under the argument column of the header.
func wrapToolArgs(s string, contentWidth, indent int) string {
	if contentWidth < 1 {
		contentWidth = 1
	}
	fields := strings.Fields(s)
	var lines []string
	var cur string
	flush := func() {
		if cur != "" {
			lines = append(lines, cur)
			cur = ""
		}
	}
	for _, f := range fields {
		if lipgloss.Width(f) > contentWidth {
			// Oversized single token: hard-wrap it, emitting full lines and
			// carrying the trailing fragment onto the current line.
			flush()
			parts := strings.Split(ansi.Hardwrap(f, contentWidth, false), "\n")
			for i := 0; i+1 < len(parts); i++ {
				lines = append(lines, parts[i])
			}
			cur = parts[len(parts)-1]
			continue
		}
		if cur == "" {
			cur = f
			continue
		}
		if lipgloss.Width(cur)+1+lipgloss.Width(f) > contentWidth {
			lines = append(lines, cur)
			cur = f
			continue
		}
		cur += " " + f
	}
	flush()
	if len(lines) == 0 {
		return ""
	}
	out := lines[0]
	pad := strings.Repeat(" ", indent)
	for _, line := range lines[1:] {
		out += "\n" + pad + line
	}
	return out
}

// renderToolHeader renders the tool-name prefix plus the first line of the
// argument summary, wrapping the summary to fit width instead of truncating.
// Continuation lines indent to align under the argument column. Each line is
// styled independently so lipgloss does not pad the block to its longest line
// (which would push the prefix past the available width).
func renderToolHeader(th Theme, name, summary string, width int) string {
	prefix := th.ToolHeader.Render("⚙ "+strings.ToLower(name)) + " "
	prefixWidth := lipgloss.Width(prefix)
	// Continuation indent aligns wrapped args under the argument column; cap
	// it at narrow widths so there is always room for content.
	contIndent := prefixWidth
	if contIndent > width-1 {
		contIndent = width - 1
		if contIndent < 0 {
			contIndent = 0
		}
	}
	avail := width - contIndent
	if avail < 1 {
		avail = 1
	}
	firstLine := firstLineOnly(summary)
	wrapped := wrapToolArgs(firstLine, avail, contIndent)
	lines := strings.Split(wrapped, "\n")
	for i := range lines {
		lines[i] = th.ToolArgs.Render(lines[i])
	}
	lines[0] = prefix + lines[0]
	return strings.Join(lines, "\n")
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
