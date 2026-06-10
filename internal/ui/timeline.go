package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Kind discriminates timeline items.
type Kind int

const (
	KindUser Kind = iota
	KindAssistant
	KindReasoning
	KindTool
	KindHook
	KindTaskStart
	KindComplete
	KindError
	KindWarning
	KindInfo
	KindDebug
)

// Item is one structured timeline entry. Items are stored as data, not
// pre-rendered strings, so the timeline can reflow on resize.
type Item struct {
	Kind Kind
	Text string
	Tool *ToolBlock
}

// ToolBlock pairs a tool call with its (eventual) output.
type ToolBlock struct {
	Name      string // original name as sent by cake
	Arguments string // raw JSON arguments
	Output    string
	Done      bool
}

// RenderItems renders the whole timeline to a single string at the given
// width.
func RenderItems(th Theme, items []Item, width int) string {
	if width < 8 {
		width = 8
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, renderItem(th, it, width))
	}
	return strings.Join(parts, "\n\n")
}

func renderItem(th Theme, it Item, width int) string {
	wrap := func(style lipgloss.Style, s string) string {
		return style.Width(width).Render(s)
	}
	switch it.Kind {
	case KindUser:
		return th.UserLabel.Render("❯ ") + wrap(th.UserText, it.Text)
	case KindAssistant:
		return wrap(th.Assistant, it.Text)
	case KindReasoning:
		return wrap(th.Reasoning, "· "+it.Text)
	case KindTool:
		return renderTool(th, it.Tool, width)
	case KindHook:
		return wrap(th.Hook, "⚑ "+it.Text)
	case KindTaskStart:
		return wrap(th.Debug, "— "+it.Text)
	case KindComplete:
		return wrap(th.Complete, "✓ "+it.Text)
	case KindError:
		return wrap(th.Error, "✗ "+it.Text)
	case KindWarning:
		return wrap(th.Warning, "! "+it.Text)
	case KindInfo:
		return wrap(th.Info, it.Text)
	default:
		return wrap(th.Debug, it.Text)
	}
}

func renderTool(th Theme, tool *ToolBlock, width int) string {
	if tool == nil {
		return ""
	}
	summary := SummarizeToolArgs(tool.Name, tool.Arguments)
	header := th.ToolHeader.Render("⚙ "+strings.ToLower(tool.Name)) + " " +
		th.ToolArgs.Render(firstLineOf(summary, width))
	lines := []string{header}
	// Multi-line argument summaries (edit previews) follow the header.
	if rest := strings.SplitN(summary, "\n", 2); len(rest) == 2 {
		lines = append(lines, th.ToolArgs.Width(width).Render(rest[1]))
	}
	switch {
	case !tool.Done:
		lines = append(lines, th.ToolOutput.Render("  … running"))
	case strings.TrimSpace(tool.Output) == "":
		lines = append(lines, th.ToolOutput.Render("  (no output)"))
	default:
		out := TruncateOutput(tool.Output, DefaultOutputLimit)
		lines = append(lines, indent(th.ToolOutput.Width(width-2).Render(out), "  "))
	}
	return strings.Join(lines, "\n")
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
