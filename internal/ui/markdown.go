package ui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var (
	mdMu       sync.Mutex
	mdRenderer *glamour.TermRenderer
	mdWidth    int
	mdProfile  termenv.Profile
)

func RenderMarkdown(text string, width int) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	if width < 8 {
		width = 8
	}

	profile := lipgloss.ColorProfile()

	mdMu.Lock()
	r := getRendererLocked(width, profile)
	if r == nil {
		mdMu.Unlock()
		return lipgloss.NewStyle().Width(width).Render(text)
	}

	out, err := r.Render(text)
	mdMu.Unlock()

	if err != nil {
		return lipgloss.NewStyle().Width(width).Render(text)
	}

	out = strings.TrimPrefix(out, "\n")
	out = strings.TrimSuffix(out, "\n")

	if profile == termenv.Ascii {
		out = Sanitize(out)
		if strings.TrimSpace(out) == "" {
			out = text
		}
		return lipgloss.NewStyle().Width(width).Render(out)
	}

	return addHyperlinks(scrubKeepingSGR(out, ""), text)
}

func getRendererLocked(width int, profile termenv.Profile) *glamour.TermRenderer {
	if mdRenderer != nil && mdWidth == width && mdProfile == profile {
		return mdRenderer
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithWordWrap(width),
		glamour.WithColorProfile(profile),
		glamour.WithStyles(markdownStyle(profile)),
	)
	if err != nil {
		return nil
	}

	mdRenderer = r
	mdWidth = width
	mdProfile = profile
	return r
}

func markdownStyle(profile termenv.Profile) ansi.StyleConfig {
	if profile == termenv.Ascii {
		style := styles.ASCIIStyleConfig
		style.Code.Prefix = "`"
		style.Code.Suffix = "`"
		return style
	}

	style := styles.DarkStyleConfig
	accent, info, warning, muted := "6", "4", "3", "8"
	zero, one := uint(0), uint(1)
	bold, underline, faint := true, true, true

	style.Document.Color = nil
	style.Document.Margin = &zero
	style.Heading.Color = &accent
	style.H1.Prefix = "# "
	style.H1.Suffix = ""
	style.H1.Color = &accent
	style.H1.BackgroundColor = nil
	style.H1.Bold = &bold
	style.BlockQuote.Color = &muted
	style.BlockQuote.Faint = &faint
	style.Link.Color = &info
	style.Link.Underline = &underline
	style.LinkText.Color = &info
	style.Code.Prefix = "`"
	style.Code.Suffix = "`"
	style.Code.Color = &warning
	style.Code.BackgroundColor = nil
	style.CodeBlock.Margin = &one
	style.HorizontalRule.Color = &muted
	return style
}
