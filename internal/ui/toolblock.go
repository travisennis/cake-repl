package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const DefaultOutputLimit = 2000

const unknownArgsLimit = 160

const maxIntentCells = 160

const maxEditPreviews = 3

func IntentFromToolArgs(argsJSON string) string {
	var args struct {
		Intent string `json:"intent"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	intent := strings.Join(strings.Fields(args.Intent), " ")
	if intent == "" {
		return ""
	}
	return truncateString(intent, maxIntentCells)
}

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

func TruncateOutput(s string, limit int) string {
	s = strings.TrimRight(s, "\n")
	if limit <= 0 {
		limit = DefaultOutputLimit
	}
	if len(s) <= limit {
		return s
	}
	return cutAndMark(s, limit)
}

func CapOutputBytes(s string, limit int) string {
	if limit <= 0 {
		limit = DefaultOutputLimit
	}
	if len(s) <= limit {
		return s
	}
	return cutAndMark(s, limit)
}

func cutAndMark(s string, limit int) string {
	original := len(s)
	cut := cutRuneSafe(s, limit)
	if i := strings.LastIndexByte(cut, '\n'); i > limit/2 {
		cut = cut[:i]
	}
	cut = trimPartialEscape(cut)
	return cut + fmt.Sprintf("\n… truncated (%d bytes total)", original)
}

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

func firstLineOnly(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

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
	pad := strings.Repeat(" ", indent)
	size := len(lines[0]) + (len(lines)-1)*(1+len(pad))
	for _, line := range lines[1:] {
		size += len(line)
	}
	var out strings.Builder
	out.Grow(size)
	out.WriteString(lines[0])
	for _, line := range lines[1:] {
		out.WriteByte('\n')
		out.WriteString(pad)
		out.WriteString(line)
	}
	return out.String()
}

func renderToolHeader(th Theme, name, summary string, width int) string {
	prefix := th.ToolHeader.Render("⚙ "+strings.ToLower(name)) + " "
	prefixWidth := lipgloss.Width(prefix)
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
