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

// RenderMarkdown renders markdown text to ANSI-formatted output at the given
// width, respecting the current terminal color profile (so --no-color produces
// readable plain text). Its compact style mirrors the REPL's cyan accent,
// blue links, yellow code, and muted structural elements.
//
// The returned string is safe to write to the terminal even though the input
// has already been sanitized: markdown decoding unescapes HTML character
// references (&#7;, &#x1b;, ...), so a message can reintroduce C0 controls and
// whole escape sequences after the caller's [Sanitize] pass. The decoded result
// is therefore scrubbed again here — the Ascii profile keeps no sequences at
// all, and a color profile keeps only the reviewed, rendition-only SGR that
// carries the theme's styling (ADR 005, ADR 009).
//
// A private memoization cache avoids constructing a new glamour renderer on
// every call: the renderer is rebuilt only when width or color profile change.
//
// RenderMarkdown is safe for concurrent use: the cached renderer is serialized
// through a package-level mutex from cache lookup through render completion.
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
		// Fallback: plain text wrapped at width.
		return lipgloss.NewStyle().Width(width).Render(text)
	}

	out, err := r.Render(text)
	mdMu.Unlock()

	if err != nil {
		return lipgloss.NewStyle().Width(width).Render(text)
	}

	// glamour wraps output in block formatting with a leading and trailing
	// newline; strip them for consistency with other timeline item rendering.
	out = strings.TrimPrefix(out, "\n")
	out = strings.TrimSuffix(out, "\n")

	// Scrub the decoded output before it can reach the terminal: the Ascii
	// profile keeps no sequences at all, and a color profile keeps only the
	// reviewed SGR that carries the theme's styling. Glamour can retain
	// emphasis control sequences even with an Ascii profile, and it decodes
	// character references into controls under both, so neither profile can
	// skip this step.
	if profile == termenv.Ascii {
		out = Sanitize(out)
		if strings.TrimSpace(out) == "" {
			out = text
		}
		return lipgloss.NewStyle().Width(width).Render(out)
	}

	return scrubKeepingSGR(out, "")
}

// getRendererLocked returns a cached or newly built glamour renderer for the given
// width and profile. It returns nil when renderer construction fails.
//
// The caller must hold mdMu.
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

// markdownStyle keeps Glamour's mature syntax-highlighting defaults while
// replacing its document chrome with cake-repl's compact visual language.
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
