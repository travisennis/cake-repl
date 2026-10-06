package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// injectionPayload is the crafted stream content from the code-review finding:
// erase-display, a clipboard write, and bare C0 bytes.
const injectionPayload = "hello\x1b[2Jworld\x1b]52;c;aGF4\x07 \r\n"

// hyperlinkPayload forges a hyperlink over attacker-chosen text.
const hyperlinkPayload = "\x1b]8;;https://evil.example\x07trusted link\x1b]8;;\x07"

// assertNoInjection fails when a rendered string still carries any sequence
// the terminal would act on beyond the theme's own styling.
func assertNoInjection(t *testing.T, got string) {
	t.Helper()
	for _, bad := range []struct {
		name string
		seq  string
	}{
		{"erase display (CSI 2J)", "\x1b[2J"},
		{"clipboard write (OSC 52)", "\x1b]52"},
		{"hyperlink (OSC 8)", "\x1b]8;"},
		{"carriage return", "\r"},
		{"backspace", "\b"},
		{"bel", "\x07"},
		{"nul", "\x00"},
	} {
		if strings.Contains(got, bad.seq) {
			t.Errorf("%s survived rendering: %q", bad.name, got)
		}
	}
}

// TestRenderItem_StripsControlSequences covers every item kind that carries
// untrusted stream content: tool output and arguments, cake stderr in errors,
// hook stderr, the malformed-line warning snippet, and the echoed prompt.
func TestRenderItem_StripsControlSequences(t *testing.T) {
	th := DefaultTheme()

	items := map[string]Item{
		"user":      {Kind: KindUser, Text: injectionPayload},
		"assistant": {Kind: KindAssistant, Text: injectionPayload},
		"reasoning": {Kind: KindReasoning, Text: injectionPayload},
		"hook":      {Kind: KindHook, Text: injectionPayload},
		"error":     {Kind: KindError, Text: injectionPayload},
		"warning":   {Kind: KindWarning, Text: injectionPayload},
		"info":      {Kind: KindInfo, Text: injectionPayload},
		"complete":  {Kind: KindComplete, Text: injectionPayload},
		"taskstart": {Kind: KindTaskStart, Text: injectionPayload},
		"unknown":   {Kind: Kind(99), Text: injectionPayload},
		"tool output": {Kind: KindTool, Tool: &ToolBlock{
			Name: "bash", Arguments: `{"command":"ls"}`,
			Output: injectionPayload, Done: true,
		}},
		// Control characters reach a tool call JSON-escaped, so this exercises
		// the decoded-argument path rather than the raw-JSON path.
		"tool arguments": {Kind: KindTool, Tool: &ToolBlock{
			Name: "bash", Arguments: `{"command":"ls \u001b[2J\u001b]52;c;aGF4\u0007"}`,
			Output: "ok", Done: true,
		}},
		"tool name": {Kind: KindTool, Tool: &ToolBlock{
			Name: "ba\x1b[2Jsh", Arguments: `{"command":"ls"}`,
			Output: "ok", Done: true,
		}},
		"tool hyperlink": {Kind: KindTool, Tool: &ToolBlock{
			Name: "bash", Arguments: `{"command":"ls"}`,
			Output: hyperlinkPayload, Done: true,
		}},
		"tool unknown args": {Kind: KindTool, Tool: &ToolBlock{
			Name: "mystery", Arguments: `{"payload":"a\u001b[2Jb"}`,
			Output: "ok", Done: true,
		}},
	}

	// Both profiles matter: Ascii is `-no-color`, and a color profile proves
	// the check is not passing simply because nothing emits escapes at all.
	profiles := map[string]termenv.Profile{
		"ascii":   termenv.Ascii,
		"ansi256": termenv.ANSI256,
	}

	for profileName, profile := range profiles {
		for name, item := range items {
			t.Run(profileName+"/"+name, func(t *testing.T) {
				orig := lipgloss.ColorProfile()
				defer lipgloss.SetColorProfile(orig)
				lipgloss.SetColorProfile(profile)

				got := RenderItem(th, item, 80, DefaultOutputLimit, ToolOutputTruncated)
				assertNoInjection(t, got)
				if profile == termenv.Ascii && strings.Contains(got, "\x1b") {
					t.Errorf("no-color render contains an escape byte: %q", got)
				}
			})
		}
	}
}

// TestRenderItem_SanitizationPreservesVisibleText checks that stripping the
// escapes keeps the surrounding content, so sanitization does not silently
// swallow the message the user needs to read.
func TestRenderItem_SanitizationPreservesVisibleText(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.Ascii)

	th := DefaultTheme()
	for _, tt := range []struct {
		name string
		item Item
	}{
		{"error", Item{Kind: KindError, Text: injectionPayload}},
		{"user", Item{Kind: KindUser, Text: injectionPayload}},
		{"assistant", Item{Kind: KindAssistant, Text: injectionPayload}},
		{"tool", Item{Kind: KindTool, Tool: &ToolBlock{
			Name: "bash", Arguments: `{"command":"ls"}`,
			Output: injectionPayload, Done: true,
		}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderItem(th, tt.item, 80, DefaultOutputLimit, ToolOutputTruncated)
			for _, want := range []string{"hello", "world"} {
				if !strings.Contains(got, want) {
					t.Errorf("visible text %q lost: %q", want, got)
				}
			}
		})
	}
}

// TestRenderItem_DoesNotMutateItem guards the copy-on-sanitize contract: the
// timeline keeps items as data and re-renders them on resize, so rendering
// must not rewrite the caller's item or its shared tool block.
func TestRenderItem_DoesNotMutateItem(t *testing.T) {
	th := DefaultTheme()
	tool := &ToolBlock{
		Name: "bash", Arguments: `{"command":"ls"}`,
		Output: injectionPayload, Done: true,
	}
	item := Item{Kind: KindTool, Tool: tool}
	RenderItem(th, item, 80, DefaultOutputLimit, ToolOutputTruncated)
	if tool.Output != injectionPayload {
		t.Errorf("tool output mutated: %q", tool.Output)
	}

	text := Item{Kind: KindError, Text: injectionPayload}
	RenderItem(th, text, 80, DefaultOutputLimit, ToolOutputTruncated)
	if text.Text != injectionPayload {
		t.Errorf("item text mutated: %q", text.Text)
	}
}

// TestRenderItem_WidthCorrectWithControlBytes covers the padding math: bare
// C0 bytes measure as zero cells but move the terminal cursor, so every
// rendered line must fit the width once they are removed.
func TestRenderItem_WidthCorrectWithControlBytes(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.Ascii)

	th := DefaultTheme()
	const width = 40
	noisy := "col1\tcol2\rlong overwritten value\bx " + strings.Repeat("word\r ", 20)

	for _, tt := range []struct {
		name string
		item Item
	}{
		{"tool output", Item{Kind: KindTool, Tool: &ToolBlock{
			Name: "bash", Arguments: `{"command":"` + "cat file\\u001b[2J" + `"}`,
			Output: noisy, Done: true,
		}}},
		{"error", Item{Kind: KindError, Text: noisy}},
		{"user", Item{Kind: KindUser, Text: noisy}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderItem(th, tt.item, width, DefaultOutputLimit, ToolOutputTruncated)
			for i, line := range strings.Split(got, "\n") {
				if w := lipgloss.Width(line); w > width {
					t.Errorf("line %d width = %d, exceeds %d: %q", i, w, width, line)
				}
				if strings.ContainsAny(line, "\r\b\t") {
					t.Errorf("line %d retains a cursor-moving control byte: %q", i, line)
				}
			}
		})
	}
}

// TestRenderItem_TabSeparatedTableAligns covers the behavior the tab stops
// exist for: tab-separated tool output (column -t, go test, df) lines up in
// the rendered timeline. In the no-color profile, the three body rows must
// place their second column on the same cell, and no tab byte may reach the
// rendered output.
func TestRenderItem_TabSeparatedTableAligns(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.Ascii)

	th := DefaultTheme()
	got := RenderItem(th, Item{Kind: KindTool, Tool: &ToolBlock{
		Name: "bash", Arguments: `{"command":"column -t"}`,
		Output: "name\tvalue\nalpha\t1\nlongnm\t2\n", Done: true,
	}}, 80, DefaultOutputLimit, ToolOutputTruncated)

	if strings.Contains(got, "\t") {
		t.Fatalf("tab byte survived rendering: %q", got)
	}
	var cols []int
	for _, line := range strings.Split(got, "\n") {
		body, ok := strings.CutPrefix(line, "  ")
		if !ok {
			continue // header and other chrome, not a body row
		}
		var marker string
		switch {
		case strings.HasPrefix(body, "name"):
			marker = "value"
		case strings.HasPrefix(body, "alpha"):
			marker = "1"
		case strings.HasPrefix(body, "longnm"):
			marker = "2"
		default:
			continue
		}
		i := strings.Index(body, marker)
		if i < 0 {
			t.Fatalf("marker %q missing from body row %q", marker, body)
		}
		cols = append(cols, 2+lipgloss.Width(body[:i]))
	}
	if len(cols) != 3 {
		t.Fatalf("expected 3 body rows with a second column, got %d in %q", len(cols), got)
	}
	for _, c := range cols[1:] {
		if c != cols[0] {
			t.Errorf("second column does not line up: %v", cols)
		}
	}
}

// TestStatusLine_StripsControlSequences covers the status bar, whose session
// id and model text originate outside the REPL.
func TestStatusLine_StripsControlSequences(t *testing.T) {
	th := DefaultTheme()
	const width = 60
	got := StatusLine(th, width, Status{
		State:   "idle",
		Session: "ab\x1b[2Jcd",
		Next:    "resume \x1b]52;c;aGF4\x07",
		Model:   "sonnet\r\b",
		Cwd:     "repo",
	})
	assertNoInjection(t, got)
	if w := lipgloss.Width(got); w != width {
		t.Errorf("status line width = %d, want %d", w, width)
	}
}

// TestRenderItem_ToolColor covers the opt-in SGR passthrough (ADR 009) at the
// render boundary: color survives only when ToolColor is set under a color
// profile, only in tool output, and an embedded reset reopens the enclosing
// style instead of letting the theme bleed.
func TestRenderItem_ToolColor(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)

	th := DefaultTheme()
	th.ToolColor = true
	block := &ToolBlock{
		Name:      "bash",
		Arguments: `{"command":"ls"}`,
		Output:    "\x1b[31mred\x1b[0m \x1b[2Jplain",
		Done:      true,
	}

	lipgloss.SetColorProfile(termenv.TrueColor)
	got := RenderItem(th, Item{Kind: KindTool, Tool: block}, 60, DefaultOutputLimit, ToolOutputTruncated)
	if !strings.Contains(got, "\x1b[31mred") {
		t.Errorf("tool color not passed through: %q", got)
	}
	if strings.Contains(got, "\x1b[2J") {
		t.Errorf("erase-display survived tool color passthrough: %q", got)
	}
	// th.ToolOutput is Faint(true); after the embedded reset its opening
	// sequence must be re-emitted so the theme resumes for the rest of the line.
	if !strings.Contains(got, "\x1b[0m\x1b[2m") {
		t.Errorf("enclosing style not re-emitted after reset: %q", got)
	}

	// A reset carried inside a longer sequence (as GNU tools emit, e.g.
	// "\x1b[0;31m") clears the enclosing style too, so the theme must resume
	// after it just the same.
	multi := &ToolBlock{Name: "bash", Arguments: `{"command":"ls"}`, Output: "a\x1b[0;31mb", Done: true}
	got = RenderItem(th, Item{Kind: KindTool, Tool: multi}, 60, DefaultOutputLimit, ToolOutputTruncated)
	if !strings.Contains(got, "\x1b[0;31m\x1b[2m") {
		t.Errorf("enclosing style not re-emitted after multi-param reset: %q", got)
	}

	// The flag is ignored when the profile cannot show color, so -no-color and
	// a redirected terminal always get plain text.
	lipgloss.SetColorProfile(termenv.Ascii)
	got = RenderItem(th, Item{Kind: KindTool, Tool: block}, 60, DefaultOutputLimit, ToolOutputTruncated)
	if strings.Contains(got, "\x1b[") {
		t.Errorf("escapes survived under the Ascii profile: %q", got)
	}
}

// TestRenderItem_ToolColorIsToolOutputOnly checks that enabling tool color
// leaves every other item kind stripped: only the tool output block is allowed
// to keep SGR.
func TestRenderItem_ToolColorIsToolOutputOnly(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	th := DefaultTheme()
	th.ToolColor = true
	const mark = "\x1b[38;5;196mmarker" // a bright red the theme never uses
	for _, kind := range []Kind{KindError, KindWarning, KindHook, KindInfo, KindReasoning, KindUser} {
		got := RenderItem(th, Item{Kind: kind, Text: mark}, 60, DefaultOutputLimit, ToolOutputTruncated)
		if strings.Contains(got, "\x1b[38;5;196m") {
			t.Errorf("kind %d kept stream escapes with tool color on: %q", kind, got)
		}
	}
}

// TestTruncateOutputDropsPartialEscape guards the interaction between the byte
// cut and kept SGR: a cut that lands inside a sequence must not leave a
// dangling prefix for the truncation marker to fall into.
func TestTruncateOutputDropsPartialEscape(t *testing.T) {
	in := "aaaa\x1b[31mbbbb"
	got := TruncateOutput(in, 6)
	if strings.Contains(got, "\x1b") {
		t.Errorf("partial escape survived truncation: %q", got)
	}
	if !strings.HasPrefix(got, "aaaa\n… truncated") {
		t.Errorf("unexpected truncated output: %q", got)
	}
}

// TestRenderItem_AssistantSanitizedBeforeMarkdown documents that the
// assistant path no longer relies on glamour to discard escapes: the text
// handed to glamour is already sanitized, and normal markdown still renders.
func TestRenderItem_AssistantSanitizedBeforeMarkdown(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.Ascii)

	th := DefaultTheme()
	const md = "# Heading\n\nSome **bold** text.\n"

	want := RenderItem(th, Item{Kind: KindAssistant, Text: md}, 60, DefaultOutputLimit, ToolOutputTruncated)
	if !strings.Contains(want, "Heading") || !strings.Contains(want, "bold") {
		t.Fatalf("markdown rendering regressed: %q", want)
	}
	// Sanitizing normal markdown is a no-op, so the rendered output is
	// identical to rendering the raw text.
	if got := RenderItem(th, Item{Kind: KindAssistant, Text: Sanitize(md)}, 60, DefaultOutputLimit, ToolOutputTruncated); got != want {
		t.Errorf("sanitization changed normal markdown rendering:\ngot  %q\nwant %q", got, want)
	}
}
