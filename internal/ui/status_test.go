package ui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestStatusLine_EmptyStatusDefaultsToIdle(t *testing.T) {
	th := DefaultTheme()
	got := StatusLine(th, 10, Status{})
	prefix := " " + th.StatusState.Render("[ idle ]") + " "
	wantLine := prefix + strings.Repeat(" ", 10-lipgloss.Width(prefix))
	want := th.StatusBar.MaxWidth(10).Render(wantLine)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusLine_ZeroWidth(t *testing.T) {
	if got := StatusLine(DefaultTheme(), 0, Status{State: "idle"}); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestStatusLine_StateOnly(t *testing.T) {
	th := DefaultTheme()
	got := StatusLine(th, 20, Status{State: "running"})
	prefix := " " + th.StatusState.Render("[ running ]") + " "
	wantLine := prefix + strings.Repeat(" ", 20-lipgloss.Width(prefix))
	want := th.StatusBar.MaxWidth(20).Render(wantLine)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusLine_StructuredContext(t *testing.T) {
	th := DefaultTheme()
	status := Status{State: "idle", Session: "abc12345", Next: "resume abc12345", Model: "gpt-x", Cwd: "project"}
	got := StatusLine(th, 100, status)
	for _, want := range []string{"[ idle ]", "session:", "abc12345", "next:", "resume abc12345", "model:", "gpt-x", "cwd:", "project", " │ ", " · "} {
		if !strings.Contains(got, want) {
			t.Errorf("status line missing %q: %q", want, got)
		}
	}
	if width := lipgloss.Width(got); width != 100 {
		t.Errorf("width = %d, want 100", width)
	}
}

func TestStatusLine_ExcludesEmptyOptionalContext(t *testing.T) {
	got := StatusLine(DefaultTheme(), 60, Status{State: "idle", Session: "–", Next: "fresh", Cwd: "project"})
	if strings.Contains(got, "model:") {
		t.Errorf("empty model was rendered: %q", got)
	}
}

func TestStatusLine_Truncation(t *testing.T) {
	got := StatusLine(DefaultTheme(), 12, Status{State: "running", Session: "abcdefghij", Next: "resume abcdefghij"})
	if width := lipgloss.Width(got); width != 12 {
		t.Errorf("width = %d, want 12: %q", width, got)
	}
}

func TestStatusLine_WidthPadding(t *testing.T) {
	got := StatusLine(DefaultTheme(), 30, Status{State: "idle"})
	if width := lipgloss.Width(got); width != 30 {
		t.Errorf("width = %d, want 30: %q", width, got)
	}
}

func TestShortID(t *testing.T) {
	if got := ShortID("11111111-2222-3333-4444-555555555555"); got != "11111111" {
		t.Errorf("got %q", got)
	}
	if got := ShortID("short"); got != "short" {
		t.Errorf("short id should be unchanged, got %q", got)
	}
	if got := ShortID("ééééééééé"); got != "éééééééé" || !utf8.ValidString(got) {
		t.Errorf("multibyte id mangled: %q", got)
	}
}

// TestStatusLine_FlattensMultilineMetadata checks that newline-bearing field
// values (cwd, model, session, next) render on exactly one row at the requested
// cell width. Sanitize deliberately preserves newlines for timeline text, so
// the one-row status line must flatten them itself; otherwise a cwd or model
// carrying a newline pushes the layout to a second row.
func TestStatusLine_FlattensMultilineMetadata(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.Ascii)

	const width = 40
	cases := []struct {
		name   string
		status Status
	}{
		{"cwd", Status{State: "idle", Cwd: "project\nsub"}},
		{"model", Status{State: "idle", Model: "gpt-x\nsecond"}},
		{"session", Status{State: "idle", Session: "abc12345\nmore"}},
		{"next", Status{State: "idle", Next: "resume abc\n12345"}},
		{"controls", Status{State: "idle", Cwd: "pro\nject\x1b[2J\x07"}},
	}
	for _, tc := range cases {
		got := StatusLine(DefaultTheme(), width, tc.status)
		if h := lipgloss.Height(got); h != 1 {
			t.Errorf("%s: height = %d, want 1: %q", tc.name, h, got)
		}
		if w := lipgloss.Width(got); w != width {
			t.Errorf("%s: width = %d, want %d: %q", tc.name, w, width, got)
		}
		for _, r := range got {
			if r < 0x20 || r == 0x7f {
				t.Errorf("%s: unsafe control %q survived: %q", tc.name, r, got)
				break
			}
		}
	}
}

func TestStatusLine_AsciiNoANSI(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.Ascii)

	got := StatusLine(DefaultTheme(), 60, Status{State: "idle", Session: "abc", Next: "fresh", Cwd: "project"})
	if strings.Contains(got, "\x1b[") {
		t.Errorf("status line contains ANSI escapes in Ascii mode: %q", got)
	}
	for _, want := range []string{"[ idle ]", "session: abc", "next: fresh", "cwd: project"} {
		if !strings.Contains(got, want) {
			t.Errorf("no-color status missing %q: %q", want, got)
		}
	}
}

func TestCompactTokens(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{-5, "0"},
		{999, "999"},
		{1_000, "1k"},
		{14_383, "14k"},
		{132_000, "132k"},
		{999_499, "999k"},
		{999_999, "1M"},
		{1_000_000, "1M"},
		{1_048_576, "1M"},
		{1_500_000, "1.5M"},
	}
	for _, tt := range tests {
		if got := CompactTokens(tt.in); got != tt.want {
			t.Errorf("CompactTokens(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestContextValue(t *testing.T) {
	tests := []struct {
		name      string
		used      int
		window    int
		estimated bool
		want      string
	}{
		{"known window", 132_000, 1_000_000, false, "132k/1M"},
		{"unknown window", 132_000, 0, false, "132k"},
		{"estimated with window", 24_000, 200_000, true, "~24k/200k"},
		{"estimated without window", 24_000, 0, true, "~24k"},
		{"no measurement", 0, 0, false, ""},
		{"no measurement with window", 0, 1_000_000, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContextValue(tt.used, tt.window, tt.estimated); got != tt.want {
				t.Errorf("ContextValue(%d, %d, %t) = %q, want %q", tt.used, tt.window, tt.estimated, got, tt.want)
			}
		})
	}
}

func TestStatusLine_ContextSegment(t *testing.T) {
	th := DefaultTheme()
	got := StatusLine(th, 100, Status{State: "idle", Session: "abc12345", Context: ContextValue(132_000, 1_000_000, false), Cwd: "project"})
	if !strings.Contains(got, "context: 132k/1M") {
		t.Errorf("status line missing context segment: %q", got)
	}
	if w := lipgloss.Width(got); w != 100 {
		t.Errorf("width = %d, want 100: %q", w, got)
	}
	if h := lipgloss.Height(got); h != 1 {
		t.Errorf("height = %d, want 1: %q", h, got)
	}
}

func TestStatusLine_OmitsContextWhenUnmeasured(t *testing.T) {
	got := StatusLine(DefaultTheme(), 100, Status{State: "idle", Session: "abc12345", Context: "", Cwd: "project"})
	if strings.Contains(got, "context:") {
		t.Errorf("unmeasured context rendered a segment: %q", got)
	}
}
