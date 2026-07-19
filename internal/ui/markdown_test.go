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
	if strings.Contains(got, "\x1b") {
		t.Errorf("output should not contain ANSI escapes in Ascii mode, got %q", got)
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

func TestRenderMarkdown_ThemedRepresentativeElements(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	input := "# Heading\n\n> quoted context\n\n- item with **strong** and *emphasis*\n\n`inline code` and [a link](https://example.com)\n\n```go\nfmt.Println(\"hello\")\n```"
	got := RenderMarkdown(input, 60)
	plain := stripANSI(got)
	for _, want := range []string{"# Heading", "quoted context", "item with", "strong", "emphasis", "inline code", "a link", "fmt.Println"} {
		if !strings.Contains(plain, want) {
			t.Errorf("render missing %q:\n%s", want, plain)
		}
	}
	if !strings.Contains(got, "\x1b[") {
		t.Error("themed markdown did not emit styling in TrueColor mode")
	}
	if strings.Contains(got, "\x1b[48;") {
		t.Errorf("compact theme should not add block backgrounds: %q", got)
	}
}

func TestRenderMarkdown_AsciiRepresentativeElements(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.Ascii)

	input := "# Heading\n\n> quote\n\n- **bold** and *italic*\n\n`code` and [link](https://example.com)"
	got := RenderMarkdown(input, 32)
	if strings.Contains(got, "\x1b") {
		t.Errorf("Ascii representative render contains ANSI: %q", got)
	}
	for _, want := range []string{"# Heading", "| quote", "bold", "italic", "`code`", "link"} {
		if !strings.Contains(got, want) {
			t.Errorf("Ascii render missing %q:\n%s", want, got)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if width := lipgloss.Width(line); width > 32 {
			t.Errorf("line %q width = %d, exceeds 32", line, width)
		}
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
