package ui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestStatusLine_EmptySegments(t *testing.T) {
	th := DefaultTheme()
	got := StatusLine(th, 10)
	// nonEmpty is empty → line = "  " (2 spaces), padded to 10.
	want := th.StatusBar.MaxWidth(10).Render(strings.Repeat(" ", 10))
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusLine_SingleSegment(t *testing.T) {
	th := DefaultTheme()
	got := StatusLine(th, 20, "hello")
	// line = " hello " (7 visible cells), padded to 20.
	padded := " hello " + strings.Repeat(" ", 20-7)
	want := th.StatusBar.MaxWidth(20).Render(padded)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusLine_MultipleSegments(t *testing.T) {
	th := DefaultTheme()
	got := StatusLine(th, 30, "a", "b", "c")
	// line = " a │ b │ c " (11 visible cells), padded to 30.
	padded := " a │ b │ c " + strings.Repeat(" ", 30-11)
	want := th.StatusBar.MaxWidth(30).Render(padded)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusLine_Truncation(t *testing.T) {
	th := DefaultTheme()
	got := StatusLine(th, 5, "abcdefghij")
	// line = " abcdefghij " (12 cells) exceeds width=5, MaxWidth truncates.
	want := th.StatusBar.MaxWidth(5).Render(" abcdefghij ")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusLine_ExcludeEmpty(t *testing.T) {
	th := DefaultTheme()
	got := StatusLine(th, 20, "a", "", "b")
	// nonEmpty = ["a","b"], line = " a │ b " (7 cells), padded to 20.
	padded := " a │ b " + strings.Repeat(" ", 20-7)
	want := th.StatusBar.MaxWidth(20).Render(padded)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
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

	th := DefaultTheme()
	got := StatusLine(th, 40, "session abc", "idle", "next: ask")
	if strings.Contains(got, "\x1b[") {
		t.Errorf("status line contains ANSI escapes in Ascii mode: %q", got)
	}
}
