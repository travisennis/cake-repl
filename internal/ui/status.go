package ui

import (
	"strconv"
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
	// Context is the cake-reported context measurement, already formatted by
	// ContextValue. Empty omits the segment.
	Context string
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

	fields := make([]string, 0, 5)
	for _, field := range []struct {
		key   string
		value string
	}{
		{key: "session", value: status.Session},
		{key: "next", value: status.Next},
		{key: "model", value: status.Model},
		{key: "context", value: status.Context},
		{key: "cwd", value: status.Cwd},
	} {
		// Session id, next-run id, model, and the context segment reach here
		// from cake or config, so they are sanitized for the same reason
		// RenderItem sanitizes timeline text: escapes would survive styling and
		// desync the padding math below.
		// The sanitized value is then flattened to a single line, because the
		// status bar is a one-row layout while Sanitize deliberately keeps
		// newlines for timeline structure. State is exempt because callers
		// compose it from styled local widgets (the spinner), not stream content.
		value := flattenLine(Sanitize(field.value))
		if value == "" {
			continue
		}
		fields = append(fields, th.StatusKey.Render(field.key+":")+" "+th.StatusValue.Render(value))
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

// flattenLine replaces embedded carriage returns and newlines with spaces so a
// field value occupies exactly one row. Sanitize keeps newlines for timeline
// text; the single-row status line must flatten them itself.
func flattenLine(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return ' '
		}
		return r
	}, s)
}

// ContextValue formats one context measurement for the status line: the prompt
// size the model saw on the newest complete attempt, over the model's
// configured window when cake reports one (for example "132k/1M"). A model
// that configures no window gets the bare count instead of a placeholder. It
// returns empty for no measurement, which omits the segment.
func ContextValue(used, window int) string {
	if used <= 0 {
		return ""
	}
	value := CompactTokens(used)
	if window > 0 {
		value += "/" + CompactTokens(window)
	}
	return value
}

// CompactTokens formats a token count for the one-row status line: exact below
// one thousand, rounded to the nearest thousand with a "k" suffix, and rounded
// to one decimal of a million with an "M" suffix (1,048,576 renders as "1M").
func CompactTokens(n int) string {
	if n <= 0 {
		return "0"
	}
	if n < 1_000 {
		return strconv.Itoa(n)
	}
	if n < 1_000_000 {
		thousands := (n + 500) / 1_000
		if thousands >= 1_000 {
			return "1M"
		}
		return strconv.Itoa(thousands) + "k"
	}
	tenths := (n + 50_000) / 100_000
	if tenths%10 == 0 {
		return strconv.Itoa(tenths/10) + "M"
	}
	return strconv.Itoa(tenths/10) + "." + strconv.Itoa(tenths%10) + "M"
}

// ShortID trims a UUID-ish string to its first 8 runes for status display.
func ShortID(id string) string {
	runes := []rune(id)
	if len(runes) > 8 {
		return string(runes[:8])
	}
	return id
}
