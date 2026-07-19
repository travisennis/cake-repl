package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// PromptComposer renders the textarea content as the active input region.
// The returned block is exactly width cells wide on every line.
func PromptComposer(th Theme, content string, width int, running bool, lineCount int, hint string) string {
	if width <= 0 {
		return ""
	}

	state := "ready"
	if running {
		state = "running"
	}
	title := th.PromptTitle.Render(" prompt · " + state + " ")
	top := composerBorderLine(th, width, "╭", th.PromptBorder.Render("─ ")+title, "╮")

	contentLines := strings.Split(content, "\n")
	lines := make([]string, 0, len(contentLines)+2)
	lines = append(lines, top)
	for _, line := range contentLines {
		lines = append(lines, composerContentLine(th, width, line))
	}

	lineLabel := "lines"
	if lineCount == 1 {
		lineLabel = "line"
	}
	footer := th.PromptHint.Render(fmt.Sprintf(" %s · %d %s ", hint, lineCount, lineLabel))
	bottom := composerBorderLine(th, width, "╰", th.PromptBorder.Render("─ ")+footer, "╯")
	lines = append(lines, bottom)

	return strings.Join(lines, "\n")
}

func composerBorderLine(th Theme, width int, left, content, right string) string {
	if width == 1 {
		return th.PromptBorder.Render(left)
	}

	innerWidth := width - 2
	content = fitComposerLine(content, innerWidth)
	fill := strings.Repeat("─", innerWidth-lipgloss.Width(content))
	return th.PromptBorder.Render(left) + content + th.PromptBorder.Render(fill+right)
}

func composerContentLine(th Theme, width int, content string) string {
	if width == 1 {
		return th.PromptBorder.Render("│")
	}

	innerWidth := width - 2
	content = fitComposerLine(content, innerWidth)
	padding := strings.Repeat(" ", innerWidth-lipgloss.Width(content))
	border := th.PromptBorder.Render("│")
	return border + content + padding + border
}

func fitComposerLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(line)
}
