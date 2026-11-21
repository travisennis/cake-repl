package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/muesli/termenv"
)

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

type Item struct {
	ID   uint64
	Kind Kind
	Text string
	Tool *ToolBlock
}

type ToolOutputMode int

const (
	ToolOutputTruncated ToolOutputMode = iota
	ToolOutputFull
	ToolOutputHidden
)

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

type ToolBlock struct {
	Name      string
	Arguments string
	Output    string
	Done      bool
}

func RenderItem(th Theme, it Item, width int, outputLimit int, toolOutputMode ToolOutputMode) string {
	if width < 8 {
		width = 8
	}
	if outputLimit <= 0 {
		outputLimit = DefaultOutputLimit
	}
	it.Text = Sanitize(it.Text)
	wrap := func(style lipgloss.Style, s string) string {
		return style.Width(width).Render(s)
	}
	switch it.Kind {
	case KindUser:
		header, gutter, bodyWidth := conversationFrame(th, th.UserLabel, "YOU", "YOU", width, true)
		return header + "\n" + indent(th.UserText.Width(bodyWidth).Render(it.Text), gutter)
	case KindAssistant:
		header, _, bodyWidth := conversationFrame(th, th.Assistant, "ASSISTANT", "AI", width, false)
		return header + "\n" + RenderMarkdown(it.Text, bodyWidth)
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

func conversationFrame(th Theme, labelStyle lipgloss.Style, label, narrowLabel string, width int, showGutter bool) (header, gutter string, bodyWidth int) {
	if width < 12 {
		label = narrowLabel
	}
	header = th.TimelineSeparator.Render("── ") + labelStyle.Render(label)
	bodyWidth = width
	if showGutter && width >= 10 {
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
	name := Sanitize(tool.Name)
	intent := Sanitize(IntentFromToolArgs(tool.Arguments))
	summary := Sanitize(SummarizeToolArgs(tool.Name, tool.Arguments))
	headerSummary := summary
	if intent != "" {
		headerSummary = intent
	}
	header := renderToolHeader(th, name, headerSummary, width)
	lines := []string{header}
	switch {
	case intent != "" && showToolDetail(tool.Name):
		lines = append(lines, indent(th.ToolDetail.Width(width-2).Render(summary), "  "))
	case intent == "":
		if rest := strings.SplitN(summary, "\n", 2); len(rest) == 2 {
			lines = append(lines, th.ToolArgs.Width(width).Render(rest[1]))
		}
	}
	if outputMode != ToolOutputHidden {
		output := tool.Output
		if keepOutputSGR(th) {
			output = SanitizeToolOutput(output, true, styleOpen(th.ToolOutput.Width(width-2)))
		} else {
			output = Sanitize(output)
		}
		switch {
		case !tool.Done:
			lines = append(lines, th.ToolOutput.Render("  … running"))
		case strings.TrimSpace(output) == "":
			lines = append(lines, th.ToolOutput.Render("  (no output)"))
		default:
			if outputMode == ToolOutputFull {
				out := trimPartialEscape(strings.TrimRight(output, "\n"))
				lines = append(lines, renderFullToolOutput(th.ToolOutput, out, width-2))
			} else {
				lines = append(lines, indent(th.ToolOutput.Width(width-2).Render(TruncateOutput(output, outputLimit)), "  "))
			}
		}
	}
	return strings.Join(lines, "\n")
}

func showToolDetail(name string) bool {
	switch strings.ToLower(name) {
	case "bash", "edit", "write":
		return true
	default:
		return false
	}
}

func renderFullToolOutput(style lipgloss.Style, out string, width int) string {
	wrapped := cellbuf.Wrap(out, width, "")
	open, close := styleSequences(style)
	var b strings.Builder
	b.Grow(len(wrapped) + (len(open)+len(close)+2)*(strings.Count(wrapped, "\n")+1))
	for {
		line, rest, more := strings.Cut(wrapped, "\n")
		if line != "" {
			b.WriteString("  ")
			b.WriteString(open)
			b.WriteString(line)
			b.WriteString(close)
		}
		if !more {
			return b.String()
		}
		b.WriteByte('\n')
		wrapped = rest
	}
}

func keepOutputSGR(th Theme) bool {
	return th.ToolColor && lipgloss.ColorProfile() != termenv.Ascii
}

func styleOpen(style lipgloss.Style) string {
	open, _ := styleSequences(style)
	return open
}

func styleSequences(style lipgloss.Style) (open, close string) {
	const sentinel = "\ue000"
	open, rest, ok := strings.Cut(style.Render(sentinel), sentinel)
	if !ok {
		return "", ""
	}
	return open, rest
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
