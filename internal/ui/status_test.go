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
