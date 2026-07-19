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
)

// Item is one structured timeline entry. Items are stored as data, not
// pre-rendered strings, so the timeline can reflow on resize.
type Item struct {
	Kind Kind
	Text string
	Tool *ToolBlock
}

// ToolOutputMode controls how much of a tool's output is rendered.
type ToolOutputMode int

const (
	// ToolOutputTruncated caps output at the configured output limit. It is the
	// zero value so tool blocks that do not set a mode render the legacy view.
	ToolOutputTruncated ToolOutputMode = iota
	// ToolOutputFull shows the complete raw output regardless of the limit.
	ToolOutputFull
	// ToolOutputHidden shows the tool header but no output/status lines.
	ToolOutputHidden
)

// Cycle returns the next mode in the order truncated → full → hidden → truncated.
func (m ToolOutputMode) Cycle() ToolOutputMode {
	switch m {
	case ToolOutputTruncated:
		return ToolOutputFull
	case ToolOutputFull:
		return ToolOutputHidden
	case ToolOutputHidden:
		return ToolOutputTruncated
	default:
		return ToolOutputFull
	}
}

// ToolBlock pairs a tool call with its (eventual) output.
type ToolBlock struct {
	Name      string // original name as sent by cake
	Arguments string // raw JSON arguments
	Output    string
	Done      bool
}

// RenderItems renders the whole timeline to a single string at the given
// width and output limit.
func RenderItems(th Theme, items []Item, width int, outputLimit int, toolOutputMode ToolOutputMode) string {
	if outputLimit <= 0 {
		outputLimit = DefaultOutputLimit
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, RenderItem(th, it, width, outputLimit, toolOutputMode))
	}
	return strings.Join(parts, "\n\n")
}

// RenderItem renders one timeline item at the given width and output limit.
// Items render independently of each other, so callers can cache results per
// item.
func RenderItem(th Theme, it Item, width int, outputLimit int, toolOutputMode ToolOutputMode) string {
	if width < 8 {
		width = 8
	}
	if outputLimit <= 0 {
		outputLimit = DefaultOutputLimit
	}
	wrap := func(style lipgloss.Style, s string) string {
		return style.Width(width).Render(s)
	}
	switch it.Kind {
	case KindUser:
		header, gutter, bodyWidth := conversationFrame(th, th.UserLabel, "YOU", "YOU", width)
		return header + "\n" + indent(th.UserText.Width(bodyWidth).Render(it.Text), gutter)
	case KindAssistant:
		header, gutter, bodyWidth := conversationFrame(th, th.Assistant, "ASSISTANT", "AI", width)
		return header + "\n" + indent(RenderMarkdown(it.Text, bodyWidth), gutter)
	case KindReasoning:
		return wrap(th.Reasoning, "  · "+it.Text)
	case KindTool:
		return renderTool(th, it.Tool, width, outputLimit, toolOutputMode)
	case KindHook:
		return wrap(th.Hook, "⚑ "+it.Text)
	case KindTaskStart:
		return wrap(th.Debug, "→ "+it.Text)
	case KindComplete:
		return wrap(th.Complete, "✓ "+it.Text)
	case KindError:
		return wrap(th.Error, "✗ "+it.Text)
	case KindWarning:
		return wrap(th.Warning, "! "+it.Text)
	case KindInfo:
		return wrap(th.Info, "i "+it.Text)
	default:
		return wrap(th.Debug, it.Text)
	}
}

// conversationFrame returns a section label, optional body gutter, and the
// width available to the body. Very narrow terminals omit the gutter and use
// a short label so every rendered line stays within the supplied width.
func conversationFrame(th Theme, labelStyle lipgloss.Style, label, narrowLabel string, width int) (header, gutter string, bodyWidth int) {
	if width < 12 {
		label = narrowLabel
	}
	header = th.TimelineSeparator.Render("── ") + labelStyle.Render(label)
	bodyWidth = width
	if width >= 10 {
		gutter = th.TimelineAccent.Render("│ ")
		bodyWidth -= 2
	}
	return header, gutter, bodyWidth
}

func renderTool(th Theme, tool *ToolBlock, width int, outputLimit int, outputMode ToolOutputMode) string {
	if tool == nil {
		return ""
	}
	if outputLimit <= 0 {
		outputLimit = DefaultOutputLimit
	}
	summary := SummarizeToolArgs(tool.Name, tool.Arguments)
	header := renderToolHeader(th, tool.Name, summary, width)
	lines := []string{header}
	// Multi-line argument summaries (edit previews) follow the header.
	if rest := strings.SplitN(summary, "\n", 2); len(rest) == 2 {
		lines = append(lines, th.ToolArgs.Width(width).Render(rest[1]))
	}
	if outputMode != ToolOutputHidden {
		switch {
		case !tool.Done:
			lines = append(lines, th.ToolOutput.Render("  … running"))
		case strings.TrimSpace(tool.Output) == "":
			lines = append(lines, th.ToolOutput.Render("  (no output)"))
		default:
			var out string
			if outputMode == ToolOutputFull {
				out = strings.TrimRight(tool.Output, "\n")
			} else {
				out = TruncateOutput(tool.Output, outputLimit)
			}
			lines = append(lines, indent(th.ToolOutput.Width(width-2).Render(out), "  "))
		}
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
