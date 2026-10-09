package ui

import (
	"regexp"
	"strings"
	"sync"
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

func TestRenderMarkdown_CacheKeyedOnWidth(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	input := "# A heading that should wrap differently at different widths"

	// First render at a wide width.
	wide := RenderMarkdown(input, 120)
	if wide == "" {
		t.Fatal("expected non-empty output for wide render")
	}

	// Second render at a narrow width — must produce correctly wrapped output.
	narrow := RenderMarkdown(input, 40)
	if narrow == "" {
		t.Fatal("expected non-empty output for narrow render")
	}

	// The narrow output should have more lines (or different wrapping) than wide.
	wideLines := strings.Count(stripANSI(wide), "\n")
	narrowLines := strings.Count(stripANSI(narrow), "\n")
	if narrowLines <= wideLines || narrowLines < 1 {
		t.Errorf("narrow render should have more line breaks than wide (wide=%d, narrow=%d):\nwide: %q\nnarrow: %q", wideLines, narrowLines, wide, narrow)
	}
}

// TestRenderMarkdown_ConcurrentSafety calls RenderMarkdown from multiple
// goroutines to prove the mutex-protected renderer is safe for concurrent use.
func TestRenderMarkdown_ConcurrentSafety(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	input := "# Concurrent Rendering\n\nThis **test** verifies that `RenderMarkdown` is safe to call from multiple goroutines."

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := RenderMarkdown(input, 80)
			if got == "" {
				t.Error("concurrent render returned empty output")
			}
			plain := stripANSI(got)
			if !strings.Contains(plain, "Concurrent Rendering") {
				t.Errorf("concurrent render missing content, plain=%q", plain)
			}
		}()
	}
	wg.Wait()
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

// TestRenderMarkdown_ScrubsDecodedControls covers the boundary markdown
// decoding opens: glamour unescapes HTML character references after the
// caller's [Sanitize] pass, so a message can reintroduce C0 controls and whole
// escape sequences into styled output. Every context that decodes — prose,
// emphasis, heading, code span, link text, blockquote, list, strikethrough, and
// inline HTML — must come out clean under both profiles, with the visible text
// intact.
func TestRenderMarkdown_ScrubsDecodedControls(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)

	decoded := []struct {
		name string
		text string
	}{
		{"prose bel", "before&#7;after"},
		{"emphasis backspace", "**before&#8;after**"},
		{"heading carriage return", "# before&#13;after"},
		{"code span del", "`before&#127;after`"},
		{"link text nul", "[before&#0;after](https://example.com)"},
		{"blockquote bel", "> before&#7;after"},
		{"list backspace", "- before&#8;after"},
		{"strikethrough carriage return", "~~before&#13;after~~"},
		{"inline html bel", "<span>before&#7;after</span>"},
		{"erase display", "before&#27;[2Jafter"},
		{"clipboard write", "before&#27;]52;c;aGF4&#7;after"},
		{"hyperlink", "before&#27;]8;;https://evil.example&#7;after"},
		{"hex escape", "before&#x1b;[2Jafter"},
		// A decoded escape consumes the byte after it (ESC | is a two-byte
		// escape sequence), so the scrub drops both.
		{"lone escape", "before&#27;|after"},
		{"tab", "before&#9;after"},
		// C1 references decode to printable characters (&#155; is "›"), never
		// to a C1 rune; the assertion still rejects any C1 control.
		{"c1 reference", "before&#155;after"},
	}

	for profileName, profile := range map[string]termenv.Profile{
		"ascii":     termenv.Ascii,
		"truecolor": termenv.TrueColor,
	} {
		for _, tt := range decoded {
			t.Run(profileName+"/"+tt.name, func(t *testing.T) {
				lipgloss.SetColorProfile(profile)

				got := RenderMarkdown(tt.text, 60)
				assertNoTerminalControls(t, got)
				if profile == termenv.Ascii && strings.Contains(got, "\x1b") {
					t.Errorf("no-color render contains an escape byte: %q", got)
				}
				for _, want := range []string{"before", "after"} {
					if !strings.Contains(got, want) {
						t.Errorf("visible text %q lost: %q", want, got)
					}
				}
			})
		}
	}
}

// TestRenderMarkdown_PreservesCleanStyledOutput pins that the decode scrub is
// invisible for clean markdown: the rendered output is exactly what the glamour
// renderer produced, so no theme styling is lost. Glamour emits only reviewed,
// rendition-only SGR for this theme, which is what makes keeping it compatible
// with the decode scrub (ADR 009).
func TestRenderMarkdown_PreservesCleanStyledOutput(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	const input = "# Heading\n\n> quote\n\n- **bold** and *italic*\n\n`code` and [link](https://example.com)\n\n```go\nfmt.Println(\"hi\")\n```\n"
	const width = 60

	mdMu.Lock()
	r := getRendererLocked(width, termenv.TrueColor)
	mdMu.Unlock()
	if r == nil {
		t.Fatal("renderer construction failed")
	}
	raw, err := r.Render(input)
	if err != nil {
		t.Fatalf("glamour render failed: %v", err)
	}
	// Mirror RenderMarkdown's newline trim.
	raw = strings.TrimPrefix(raw, "\n")
	raw = strings.TrimSuffix(raw, "\n")
	if !strings.Contains(raw, "\x1b[") {
		t.Fatal("expected the theme to emit SGR under TrueColor")
	}

	if got := RenderMarkdown(input, width); got != raw {
		t.Errorf("decode scrub changed clean styled output:\ngot  %q\nwant %q", got, raw)
	}
}

// TestRenderMarkdown_RetainsMarkdownContent covers what the scrub must not
// touch: ordinary markdown and links, character references in prose that decode
// to visible text, and code samples, which keep character references literal
// because glamour does not decode inside a fenced code block. That last case is
// why the decode boundary is enforced on the rendered output rather than by
// rewriting the source.
func TestRenderMarkdown_RetainsMarkdownContent(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	markdown := RenderMarkdown("**bold** and `code` and [link](https://example.com)", 60)
	for _, want := range []string{"bold", "code", "link", "https://example.com"} {
		if !strings.Contains(stripANSI(markdown), want) {
			t.Errorf("markdown content %q lost: %q", want, markdown)
		}
	}

	decoded := RenderMarkdown("a &amp; b &lt; c", 60)
	if !strings.Contains(stripANSI(decoded), "a & b < c") {
		t.Errorf("visible character references did not decode: %q", decoded)
	}

	literal := RenderMarkdown("```\nkeep &#7; literal\n```\n", 60)
	if !strings.Contains(stripANSI(literal), "keep &#7; literal") {
		t.Errorf("code sample lost its literal character reference: %q", literal)
	}

	// A tab-indented code sample keeps its code text: the scrub expands the tab
	// to the next eight-column stop (ADR 005) rather than dropping it.
	code := RenderMarkdown("```go\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```\n", 60)
	if !strings.Contains(stripANSI(code), "fmt.Println(\"hi\")") {
		t.Errorf("tab-indented code sample lost its code: %q", code)
	}
	if strings.Contains(code, "\t") {
		t.Errorf("tab byte survived markdown rendering: %q", code)
	}
}
