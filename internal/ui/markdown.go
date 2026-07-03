package ui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// RenderMarkdown renders markdown text to ANSI-formatted output at the given
// width, respecting the current terminal color profile (so --no-color produces
// readable plain text). It uses glamour's built-in dark style.
func RenderMarkdown(text string, width int) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	if width < 8 {
		width = 8
	}

	profile := lipgloss.ColorProfile()
	r, err := glamour.NewTermRenderer(
		glamour.WithWordWrap(width),
		glamour.WithColorProfile(profile),
		glamour.WithStandardStyle("dark"),
	)
	if err != nil {
		// Fallback: plain text wrapped at width.
		return lipgloss.NewStyle().Width(width).Render(text)
	}

	out, err := r.Render(text)
	if err != nil {
		return lipgloss.NewStyle().Width(width).Render(text)
	}

	// glamour wraps output in block formatting with a leading and trailing
	// newline; strip them for consistency with other timeline item rendering.
	out = strings.TrimPrefix(out, "\n")
	out = strings.TrimSuffix(out, "\n")
	if profile == termenv.Ascii {
		// Even without ANSI codes, glamour may produce empty output for some
		// inputs (e.g. empty paragraphs). Fall back to plain text.
		if strings.TrimSpace(out) == "" {
			out = text
		}
		return lipgloss.NewStyle().Width(width).Render(out)
	}

	return out
}
