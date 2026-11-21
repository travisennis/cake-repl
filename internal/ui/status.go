package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type Status struct {
	State   string
	Session string
	Next    string
	Model   string
	Context string
	Cwd     string
}

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

func ContextValue(used, window int, estimated bool) string {
	if used <= 0 {
		return ""
	}
	value := CompactTokens(used)
	if estimated {
		value = "~" + value
	}
	if window > 0 {
		value += "/" + CompactTokens(window)
	}
	return value
}

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

func ShortID(id string) string {
	runes := []rune(id)
	if len(runes) > 8 {
		return string(runes[:8])
	}
	return id
}
