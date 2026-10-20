package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// generatedLink is one OSC 8 hyperlink found in rendered output.
type generatedLink struct {
	target string
	text   string
}

// renderedLinks parses the generated hyperlinks in rendered. It fails on a
// malformed link so tests cannot accidentally accept a half-written sequence.
func renderedLinks(t *testing.T, rendered string) []generatedLink {
	t.Helper()
	const (
		open  = "\x1b]8;;"
		close = "\x1b]8;;\x1b\\"
	)
	var links []generatedLink
	for i := 0; ; {
		rel := strings.Index(rendered[i:], open)
		if rel < 0 {
			return links
		}
		start := i + rel
		payloadStart := start + len(open)
		payloadEnd := strings.Index(rendered[payloadStart:], "\x1b\\")
		if payloadEnd < 0 {
			t.Fatalf("unterminated OSC 8 open sequence: %q", rendered[start:])
		}
		target := rendered[payloadStart : payloadStart+payloadEnd]
		textStart := payloadStart + payloadEnd + len("\x1b\\")
		textEnd := strings.Index(rendered[textStart:], close)
		if textEnd < 0 {
			t.Fatalf("unterminated OSC 8 link: %q", rendered[start:])
		}
		text := stripANSI(rendered[textStart : textStart+textEnd])
		if strings.ContainsRune(text, 0x1b) {
			t.Fatalf("link text carries an unexpected escape sequence: %q", text)
		}
		links = append(links, generatedLink{target: target, text: text})
		i = textStart + textEnd + len(close)
	}
}

// generatedHyperlinkAt validates the generated OSC 8 link starting at s[i] and
// returns the offset just past its closing sequence. It is the security
// assertion's allowance: a link may exist only when its destination is http(s)
// and its visible text is a substring of that destination, so no stream input
// can attach a target to unrelated text.
func generatedHyperlinkAt(s string, i int) (int, bool) {
	const (
		open  = "\x1b]8;;"
		close = "\x1b]8;;\x1b\\"
	)
	if !strings.HasPrefix(s[i:], open) {
		return 0, false
	}
	payload := s[i+len(open):]
	n := strings.Index(payload, "\x1b\\")
	if n < 0 {
		return 0, false
	}
	target := payload[:n]
	if len(target) == 0 || len(target) > maxHyperlinkURL || !hasHTTPScheme(strings.ToLower(target)) {
		return 0, false
	}
	for _, r := range target {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return 0, false
		}
	}
	textStart := i + len(open) + n + len("\x1b\\")
	rest := s[textStart:]
	end := strings.Index(rest, close)
	if end < 0 {
		return 0, false
	}
	text := stripANSI(rest[:end])
	if text == "" || strings.ContainsAny(text, "\r\n") || strings.ContainsRune(text, 0x1b) {
		return 0, false
	}
	if !strings.Contains(target, text) {
		return 0, false
	}
	return textStart + end + len(close), true
}

// stripGeneratedHyperlinks removes the generated OSC 8 wrappers while keeping
// their visible text, for comparisons against the pre-hyperlink render.
func stripGeneratedHyperlinks(s string) string {
	const (
		open  = "\x1b]8;;"
		close = "\x1b]8;;\x1b\\"
	)
	for {
		i := strings.Index(s, open)
		if i < 0 {
			return s
		}
		payload := s[i+len(open):]
		n := strings.Index(payload, "\x1b\\")
		if n < 0 {
			return s
		}
		textStart := i + len(open) + n + len("\x1b\\")
		end := strings.Index(s[textStart:], close)
		if end < 0 {
			return s
		}
		s = s[:i] + s[textStart:textStart+end] + s[textStart+end+len(close):]
	}
}

func TestRenderMarkdown_WrappedURLIsLinkedPerSegment(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	const width = 40
	const url = "https://github.com/travisennis/cake-repl/blob/master/docs/adr/020-repaint-the-frame-after-content-changes-to-heal-terminal-side-scrolls.md"

	got := RenderMarkdown(url, width)
	links := renderedLinks(t, got)
	if len(links) < 2 {
		t.Fatalf("wrapped URL produced %d links, want one per line segment:\n%q", len(links), got)
	}

	var joined strings.Builder
	for _, link := range links {
		if link.target != url {
			t.Errorf("link target = %q, want the full URL", link.target)
		}
		if link.text == "" || !strings.Contains(url, link.text) {
			t.Errorf("link text %q is not a fragment of %q", link.text, url)
		}
		joined.WriteString(link.text)
	}
	if joined.String() != url {
		t.Errorf("linked fragments join to %q, want %q", joined.String(), url)
	}

	for i, line := range strings.Split(got, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("linked line %d width = %d, exceeds %d: %q", i, w, width, line)
		}
	}
}

func TestRenderMarkdown_WrappedURLInBlockquoteIsLinked(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	const width = 40
	const url = "https://github.com/travisennis/cake-repl/blob/master/docs/adr/020-repaint-the-frame-after-content-changes-to-heal-terminal-side-scrolls.md"

	// Glamour prefixes every blockquote line with "│ ", so the wrapped
	// continuation starts behind decoration rather than at the line start.
	got := RenderMarkdown("> "+url, width)
	links := renderedLinks(t, got)
	if len(links) < 2 {
		t.Fatalf("wrapped blockquote URL produced %d links, want one per line segment:\n%q", len(links), got)
	}

	var joined strings.Builder
	for _, link := range links {
		if link.target != url {
			t.Errorf("link target = %q, want the full URL", link.target)
		}
		joined.WriteString(link.text)
	}
	if joined.String() != url {
		t.Errorf("linked fragments join to %q, want %q", joined.String(), url)
	}
}

// TestRenderMarkdown_DoesNotAbsorbProseAfterURL covers prose that follows a URL
// without a space. A generated target must never absorb it: typographic
// punctuation ends the URL, and a non-ASCII word keeps the whole URL unlinked
// because it cannot be told apart from a longer address.
func TestRenderMarkdown_DoesNotAbsorbProseAfterURL(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	for _, tt := range []struct {
		name      string
		text      string
		wantLinks int
	}{
		{"cjk full stop", "See https://example.com/foo。これは続く文章です", 1},
		{"em dash", "See https://example.com/foo—next", 1},
		{"cjk word", "See https://example.com/fooこれは続く", 0},
		{"internationalized url", "See https://example.com/中文", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderMarkdown(tt.text, 80)
			links := renderedLinks(t, got)
			if len(links) != tt.wantLinks {
				t.Fatalf("got %d links, want %d:\n%q", len(links), tt.wantLinks, got)
			}
			for _, link := range links {
				if link.target != "https://example.com/foo" {
					t.Errorf("link target = %q, want only the URL", link.target)
				}
			}
			if visible := stripANSI(stripGeneratedHyperlinks(got)); !strings.Contains(visible, tt.text) {
				t.Errorf("visible text changed:\ngot  %q\nwant %q", visible, tt.text)
			}
		})
	}
}

func TestRenderMarkdown_LinksTheURLNotTheLinkLabel(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	const url = "https://example.com/a/very/long/path/that/wraps"
	got := RenderMarkdown("[click here]("+url+")", 24)
	links := renderedLinks(t, got)
	if len(links) == 0 {
		t.Fatalf("link destination was not hyperlinked:\n%q", got)
	}
	for _, link := range links {
		if link.target != url {
			t.Errorf("link target = %q, want %q", link.target, url)
		}
		if strings.Contains(link.text, "click") {
			t.Errorf("link label was linked instead of the destination: %q", link.text)
		}
	}
}

func TestRenderMarkdown_HyperlinksExcludeProsePunctuation(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	got := RenderMarkdown("See https://example.com/foo.", 80)
	links := renderedLinks(t, got)
	if len(links) != 1 {
		t.Fatalf("got %d links, want 1:\n%q", len(links), got)
	}
	if links[0].target != "https://example.com/foo" || links[0].text != "https://example.com/foo" {
		t.Errorf("link = %+v, want the URL without the sentence period", links[0])
	}
	if visible := stripANSI(stripGeneratedHyperlinks(got)); !strings.Contains(visible, "https://example.com/foo.") {
		t.Errorf("trailing punctuation was lost from the visible line: %q", visible)
	}
}

func TestRenderMarkdown_DoesNotLinkNonHTTPSchemes(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.TrueColor)

	for _, text := range []string{
		"ftp://example.com/file",
		"file:///etc/passwd",
		"mailto:someone@example.com",
	} {
		if got := RenderMarkdown(text, 80); strings.Contains(got, "\x1b]8;;") {
			t.Errorf("%q was hyperlinked: %q", text, got)
		}
	}
}

func TestRenderMarkdown_AsciiProfileEmitsNoHyperlinks(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.Ascii)

	got := RenderMarkdown("See https://example.com/foo.", 80)
	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("no-color render carries an escape sequence: %q", got)
	}
}

// TestAddHyperlinks_DoesNotJoinFollowingProse checks the guard that keeps a
// URL at the end of a rendered line from swallowing the next line's text: the
// joined string must still occur in the markdown source.
func TestAddHyperlinks_DoesNotJoinFollowingProse(t *testing.T) {
	const source = "https://example.com/foo bar"
	rendered := "https://example.com/foo  \nbar"

	got := addHyperlinks(rendered, source)
	links := renderedLinks(t, got)
	if len(links) != 1 {
		t.Fatalf("got %d links, want 1: %q", len(links), got)
	}
	if links[0].target != "https://example.com/foo" || links[0].text != "https://example.com/foo" {
		t.Errorf("link = %+v, want only the URL", links[0])
	}
}

// TestAddHyperlinks_JoinsURLSplitAtWrap reproduces glamour's hard wrap: the
// padding spaces and newline between the two runs are not part of the link.
func TestAddHyperlinks_JoinsURLSplitAtWrap(t *testing.T) {
	const url = "https://example.com/foo-bar-baz"
	rendered := "https://example.com/foo-  \nbar-baz"

	got := addHyperlinks(rendered, url)
	links := renderedLinks(t, got)
	if len(links) != 2 {
		t.Fatalf("got %d links, want 2: %q", len(links), got)
	}
	for _, link := range links {
		if link.target != url {
			t.Errorf("link target = %q, want %q", link.target, url)
		}
	}
	if links[0].text+links[1].text != url {
		t.Errorf("linked fragments join to %q, want %q", links[0].text+links[1].text, url)
	}
	if strings.Contains(got, "foo-  \x1b]8;;") {
		t.Errorf("link spans the line padding: %q", got)
	}
}
