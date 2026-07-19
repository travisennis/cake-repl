package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestDefaultThemeNotEmpty(t *testing.T) {
	// Force TrueColor so we can verify styles produce ANSI output.
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	th := DefaultTheme()

	// Every semantic role should have at least one attribute, producing styled output.
	styles := map[string]lipgloss.Style{
		"UserLabel":         th.UserLabel,
		"UserText":          th.UserText,
		"Assistant":         th.Assistant,
		"Reasoning":         th.Reasoning,
		"ToolHeader":        th.ToolHeader,
		"ToolArgs":          th.ToolArgs,
		"ToolOutput":        th.ToolOutput,
		"Hook":              th.Hook,
		"Complete":          th.Complete,
		"Error":             th.Error,
		"Warning":           th.Warning,
		"Info":              th.Info,
		"Debug":             th.Debug,
		"Rule":              th.Rule,
		"Chrome":            th.Chrome,
		"PromptTitle":       th.PromptTitle,
		"PromptBorder":      th.PromptBorder,
		"PromptHint":        th.PromptHint,
		"Placeholder":       th.Placeholder,
		"StatusBar":         th.StatusBar,
		"StatusKey":         th.StatusKey,
		"StatusValue":       th.StatusValue,
		"StatusSeparator":   th.StatusSeparator,
		"Muted":             th.Muted,
		"TimelineSeparator": th.TimelineSeparator,
		"TimelineAccent":    th.TimelineAccent,
	}
	for name, s := range styles {
		t.Run(name, func(t *testing.T) {
			if rendered := s.Render("x"); rendered == "x" {
				t.Errorf("%s style renders as plain text", name)
			}
		})
	}
}

func TestDefaultThemeAsciiDegradation(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.Ascii)

	th := DefaultTheme()
	styles := []lipgloss.Style{
		th.UserLabel,
		th.UserText,
		th.Assistant,
		th.Reasoning,
		th.ToolHeader,
		th.ToolArgs,
		th.ToolOutput,
		th.Hook,
		th.Complete,
		th.Error,
		th.Warning,
		th.Info,
		th.Debug,
		th.Rule,
		th.Chrome,
		th.PromptTitle,
		th.PromptBorder,
		th.PromptHint,
		th.Placeholder,
		th.StatusBar,
		th.StatusKey,
		th.StatusValue,
		th.StatusSeparator,
		th.Muted,
		th.TimelineSeparator,
		th.TimelineAccent,
	}
	for i, s := range styles {
		if rendered := s.Render("test"); strings.Contains(rendered, "\x1b[") {
			t.Errorf("style %d contains ANSI escapes in Ascii mode: %q", i, rendered)
		}
	}
}
