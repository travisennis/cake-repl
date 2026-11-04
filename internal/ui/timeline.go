package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/muesli/termenv"
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
	// ID is an optional stable identity assigned by the owner. It is never
	// rendered; zero means unassigned. A row index or text prefix is not an
	// identity because insertion, trimming, and /clear can move or remove it.
	ID   uint64
	Kind Kind
	Text string
	Tool *ToolBlock
}

// ToolOutputMode controls how much of a tool's output is rendered.
type ToolOutputMode int

const (
	// ToolOutputTruncated caps output at the configured output limit. It is the
	// zero value so tool blocks that do not set a mode render the legacy view;
	// the REPL itself starts in ToolOutputHidden. See ADR 033.
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

// RenderItem renders one timeline item at the given width and output limit.
// Items render independently of each other, so callers can cache results per
// item.
//
// Every item's text is [Sanitize]d here, before any styling, so that no path
// out of the timeline can write attacker-controlled escape sequences to the
// terminal. This is the single sanitization boundary: callers pass raw stream
// content, and only the debug log ever sees it unmodified.
func RenderItem(th Theme, it Item, width int, outputLimit int, toolOutputMode ToolOutputMode) string {
	if width < 8 {
		width = 8
	}
	if outputLimit <= 0 {
		outputLimit = DefaultOutputLimit
	}
	// it is a copy, so sanitizing Text does not mutate the caller's item.
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

// conversationFrame returns a section label, optional body gutter, and the
// width available to the body. Very narrow terminals omit the gutter and use
// a short label so every rendered line stays within the supplied width.
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
	// The tool block is shared with the caller, so sanitize into locals rather
	// than writing back through the pointer. Arguments are sanitized after
	// summarizing, not before: control characters arrive JSON-escaped
	// as "\\u001b" and only become real control bytes once decoded.
	name := Sanitize(tool.Name)
	summary := Sanitize(SummarizeToolArgs(tool.Name, tool.Arguments))
	header := renderToolHeader(th, name, summary, width)
	lines := []string{header}
	// Multi-line argument summaries (edit previews) follow the header.
	if rest := strings.SplitN(summary, "\n", 2); len(rest) == 2 {
		lines = append(lines, th.ToolArgs.Width(width).Render(rest[1]))
	}
	if outputMode != ToolOutputHidden {
		// Sanitizing inside this branch keeps hidden mode from scanning output
		// it is not going to show. With tool color enabled the output keeps its
		// SGR sequences, so capture the enclosing style's opening sequence and
		// re-emit it after every embedded reset (ADR 009).
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

// renderFullToolOutput renders all retained output at the given content width
// and skips the block padding lipgloss adds to every line. Full mode can show
// a result of up to 1 MiB; padding each line to the panel width multiplied the
// cached rendering by the terminal width, so a newline-heavy result retained
// at 1 MiB cached tens of MB. The padding is unstyled and visually empty, so
// omitting it keeps the displayed text honest while bounding the cached render
// to the retained text plus a constant per line, independent of terminal
// width.
//
// The wrap call matches what lipgloss Width would do; wrapping each line in
// the style's open and close sequences then avoids the padding. That is safe
// for embedded SGR because cellbuf.Wrap reopens an open color on every line it
// wraps. Blank lines get no indent, so an empty interior line costs one
// newline instead of the block width.
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

// keepOutputSGR reports whether tool output may keep its own SGR color. The
// user opts in with -tool-color (ADR 009), and passthrough is forced off under
// the Ascii profile so -no-color and a non-terminal stdout always get plain
// text regardless of the flag.
func keepOutputSGR(th Theme) bool {
	return th.ToolColor && lipgloss.ColorProfile() != termenv.Ascii
}

// styleOpen returns the sequence that opens style, so it can be re-emitted
// after a stream-embedded reset. It renders a sentinel and keeps whatever the
// style wrote before it; under the Ascii profile the style writes no escapes
// and the result is empty.
func styleOpen(style lipgloss.Style) string {
	open, _ := styleSequences(style)
	return open
}

// styleSequences returns the sequences lipgloss puts before and after a styled
// string. Callers use them to style many lines without constructing a styled
// string per line; both are empty under the Ascii profile. The tool output
// style is rendition-only (no border, margin, or padding), so wrapping each
// line in these sequences matches what lipgloss renders for that line.
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
