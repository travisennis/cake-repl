package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestPromptComposer_RendersFocusedChromeAndHints(t *testing.T) {
	got := PromptComposer(DefaultTheme(), "first\nsecond", 80, false, 2,
		"Ctrl+S submit · Enter newline · /help")

	for _, want := range []string{"prompt · ready", "first", "second", "Ctrl+S submit", "Enter newline", "/help", "2 lines"} {
		if !strings.Contains(got, want) {
			t.Errorf("composer missing %q:\n%s", want, got)
		}
	}
	if lines := strings.Split(got, "\n"); len(lines) != 4 {
		t.Fatalf("composer rendered %d lines, want 4:\n%s", len(lines), got)
	}
}

func TestPromptComposer_ConstrainsEveryLineToWidth(t *testing.T) {
	got := PromptComposer(DefaultTheme(), strings.Repeat("x", 60)+"\nshort", 24, true, 1,
		"Ctrl+S submit · Enter newline · /help")

	for i, line := range strings.Split(got, "\n") {
		if gotWidth := lipgloss.Width(line); gotWidth != 24 {
			t.Errorf("line %d width = %d, want 24: %q", i, gotWidth, line)
		}
	}
	if !strings.Contains(got, "prompt · running") {
		t.Errorf("composer missing running state:\n%s", got)
	}
}

func TestPromptComposer_AsciiNoANSIAndVisibleBoundaries(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.Ascii)

	got := PromptComposer(DefaultTheme(), "hello", 40, false, 1,
		"Ctrl+S submit · Enter newline · /help")
	if strings.Contains(got, "\x1b[") {
		t.Errorf("composer contains ANSI escapes in Ascii mode: %q", got)
	}
	if !strings.Contains(got, "│hello") || !strings.HasPrefix(got, "╭") {
		t.Errorf("composer boundaries are not visible in Ascii mode:\n%s", got)
	}
}
