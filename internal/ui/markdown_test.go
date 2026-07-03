package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// stripANSI removes ANSI escape sequences for test comparisons.
var ansiRegexp = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

func stripANSI(s string) string {
	return ansiRegexp.ReplaceAllString(s, "")
}

func TestRenderMarkdown_Empty(t *testing.T) {
	if got := RenderMarkdown("", 80); got != "" {
		t.Errorf("empty input should return empty, got %q", got)
	}
	if got := RenderMarkdown("  ", 80); got != "  " {
		t.Errorf("whitespace input should return as-is, got %q", got)
	}
}

func TestRenderMarkdown_SimpleText(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	input := "hello world"
	got := RenderMarkdown(input, 80)
	if got == "" {
		t.Fatal("expected non-empty output")
	}
	plain := stripANSI(got)
	if !strings.Contains(plain, "hello world") {
		t.Errorf("output should contain input text, plain=%q, raw=%q", plain, got)
	}
}

func TestRenderMarkdown_Headers(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	input := "# Heading 1\n\n## Heading 2\n\n### Heading 3"
	got := RenderMarkdown(input, 80)
	if got == "" {
		t.Fatal("expected non-empty output")
	}
	plain := stripANSI(got)
	if !strings.Contains(plain, "Heading 1") {
		t.Errorf("output should contain heading text, plain=%q", plain)
	}
	if !strings.Contains(plain, "Heading 2") {
		t.Errorf("output should contain heading text, plain=%q", plain)
	}
	if !strings.Contains(plain, "Heading 3") {
		t.Errorf("output should contain heading text, plain=%q", plain)
	}
}

func TestRenderMarkdown_CodeBlock(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	input := "```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```"
	got := RenderMarkdown(input, 80)
	if got == "" {
		t.Fatal("expected non-empty output")
	}
	plain := stripANSI(got)
	if !strings.Contains(plain, "func main()") {
		t.Errorf("output should contain code contents, plain=%q", plain)
	}
	if !strings.Contains(plain, "fmt.Println") {
		t.Errorf("output should contain code contents, plain=%q", plain)
	}
}

func TestRenderMarkdown_List(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	input := "- item one\n- item two\n- item three"
	got := RenderMarkdown(input, 80)
	if got == "" {
		t.Fatal("expected non-empty output")
	}
	plain := stripANSI(got)
	if !strings.Contains(plain, "item one") {
		t.Errorf("output should contain list items, plain=%q", plain)
	}
	if !strings.Contains(plain, "item two") {
		t.Errorf("output should contain list items, plain=%q", plain)
	}
	if !strings.Contains(plain, "item three") {
		t.Errorf("output should contain list items, plain=%q", plain)
	}
}

func TestRenderMarkdown_AsciiProfile(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.Ascii)

	input := "# Hello\n\nThis is **bold** and *italic* text.\n\n- list item"
	got := RenderMarkdown(input, 80)
	if got == "" {
		t.Fatal("expected non-empty output")
	}
	// In Ascii mode, glamour may still emit bold/italic escapes (\x1b[;1m,
	// \x1b[;3m) but should not emit color codes (38;5;NNN or 48;5;NNN).
	if strings.Contains(got, "\x1b[38;5;") || strings.Contains(got, "\x1b[48;5;") {
		t.Errorf("output should not contain color ANSI escapes in Ascii mode, got %q", got)
	}
	// Content should still be present.
	plain := stripANSI(got)
	if !strings.Contains(plain, "Hello") {
		t.Errorf("output should contain 'Hello', plain=%q", plain)
	}
	if !strings.Contains(plain, "list item") {
		t.Errorf("output should contain 'list item', plain=%q", plain)
	}
}

func TestRenderMarkdown_WidthWrapping(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.Ascii)

	input := "a very long line that should be wrapped at a narrow width because it exceeds the configured word wrap limit"
	got := RenderMarkdown(input, 20)
	if got == "" {
		t.Fatal("expected non-empty output")
	}
	plain := stripANSI(got)
	for i, line := range strings.Split(plain, "\n") {
		if len(line) > 20 {
			t.Errorf("line %d exceeds width 20: %q (len=%d)", i, line, len(line))
		}
	}
}

func TestRenderMarkdown_FallbackOnError(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	input := ">>> not really markdown <<<"
	got := RenderMarkdown(input, 80)
	if got == "" {
		t.Fatal("expected non-empty output")
	}
	plain := stripANSI(got)
	if !strings.Contains(plain, "not really markdown") {
		t.Errorf("output should contain the input text, plain=%q", plain)
	}
}
