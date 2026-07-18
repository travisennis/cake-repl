package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// StatusLine renders a single status bar of exactly the given width from the
// provided segments. Segments are joined with separators; the line is
// truncated or padded as needed.
func StatusLine(th Theme, width int, segments ...string) string {
	nonEmpty := make([]string, 0, len(segments))
	for _, s := range segments {
		if s != "" {
			nonEmpty = append(nonEmpty, s)
		}
	}
	line := " " + strings.Join(nonEmpty, th.StatusSeparator.Render(" │ ")) + " "
	if w := lipgloss.Width(line); w < width {
		line += strings.Repeat(" ", width-w)
	}
	return th.StatusBar.MaxWidth(width).Render(line)
}

// ShortID trims a UUID-ish string to its first 8 runes for status display.
func ShortID(id string) string {
	runes := []rune(id)
	if len(runes) > 8 {
		return string(runes[:8])
	}
	return id
}
