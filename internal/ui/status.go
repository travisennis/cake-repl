package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Status separates the time-sensitive operational state from the longer-lived
// context displayed beside it.
type Status struct {
	State   string
	Session string
	Next    string
	Model   string
	Cwd     string
}

// StatusLine renders a single status bar of exactly the given width. The
// bracketed operational state leads; labeled context fields follow with a
// quieter separator rhythm. The line is truncated or padded as needed.
func StatusLine(th Theme, width int, status Status) string {
	if width <= 0 {
		return ""
	}
	state := status.State
	if state == "" {
		state = "idle"
	}
	stateSegment := th.StatusState.Render("[ " + state + " ]")

	fields := make([]string, 0, 4)
	for _, field := range []struct {
		key   string
		value string
	}{
		{key: "session", value: status.Session},
		{key: "next", value: status.Next},
		{key: "model", value: status.Model},
		{key: "cwd", value: status.Cwd},
	} {
		if field.value == "" {
			continue
		}
		fields = append(fields, th.StatusKey.Render(field.key+":")+" "+th.StatusValue.Render(field.value))
	}

	line := " " + stateSegment
	if len(fields) > 0 {
		line += th.StatusSeparator.Render(" │ ") + strings.Join(fields, th.StatusSeparator.Render(" · "))
	}
	line += " "
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
