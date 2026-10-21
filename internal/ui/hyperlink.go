package ui

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Generated hyperlinks (ADR 022).
//
// Glamour hard-wraps a long assistant URL at the timeline width, so the
// terminal sees only the fragment on one line. Wrapping each rendered line
// segment in an OSC 8 hyperlink that names the full URL makes every fragment
// clickable and lets the terminal's link preview show the destination.
//
// These sequences are generated here, after [RenderMarkdown]'s scrub, from
// http(s) text that is already visible in the rendered output. Untrusted bytes
// never become a link: the scrub has removed every OSC 8 that came from the
// stream, and link text is always the URL's own rendered characters (or a line
// fragment of them), never an unrelated markdown label (ADR 005, ADR 022).
const (
	hyperlinkOpenPrefix = "\x1b]8;;"
	hyperlinkClose      = "\x1b]8;;\x1b\\"

	// maxHyperlinkURL bounds the work and the sequence a pathological URL can
	// add to a rendered item.
	maxHyperlinkURL = 4096
)

// hyperlinkSpan is one visible run of URL characters, in plain-text byte
// offsets, and the full URL it opens.
type hyperlinkSpan struct {
	start int
	end   int
	url   string
}

// addHyperlinks wraps each visible http(s) URL in rendered with OSC 8
// hyperlinks. A URL hard-wrapped by glamour is linked per line segment with
// the full destination, so any fragment opens the whole URL.
//
// source is the sanitized markdown that produced rendered. It is used only to
// validate the visible URL: text on the next line joins the URL only when the
// joined string still occurs in the source, and the finished URL must occur in
// the source as a complete URL rather than as the prefix of a longer token.
// Together those guards keep surrounding prose out of a link and keep a
// hard-wrapped fragment from becoming a truncated link target.
func addHyperlinks(rendered, source string) string {
	if !strings.Contains(rendered, "://") {
		return rendered
	}

	plain, offsets := plainTextWithOffsets(rendered)
	lower := asciiLower(plain)
	var links []hyperlinkSpan
	for search := 0; search < len(lower); {
		i := strings.Index(lower[search:], "http")
		if i < 0 {
			break
		}
		start := search + i
		search = start + len("http")
		if !hasHTTPScheme(lower[start:]) {
			continue
		}
		if start > 0 && urlContinuationByte(plain[start-1]) {
			continue
		}
		spans, end := linkSpans(plain, start, source)
		if len(spans) == 0 {
			continue
		}
		links = append(links, spans...)
		if end > search {
			search = end
		}
	}
	if len(links) == 0 {
		return rendered
	}

	sort.Slice(links, func(i, j int) bool { return links[i].start < links[j].start })
	var b strings.Builder
	b.Grow(len(rendered) + len(links)*len(hyperlinkClose)*2)
	prev, prevPlain := 0, 0
	for _, link := range links {
		if link.start < prevPlain || link.end <= link.start || link.end > len(plain) {
			continue
		}
		start, end := offsets[link.start], offsets[link.end]
		if start < prev || end <= start {
			continue
		}
		b.WriteString(rendered[prev:start])
		b.WriteString(hyperlinkOpenPrefix)
		b.WriteString(link.url)
		b.WriteString("\x1b\\")
		b.WriteString(rendered[start:end])
		b.WriteString(hyperlinkClose)
		prev, prevPlain = end, link.end
	}
	b.WriteString(rendered[prev:])
	return b.String()
}

// linkSpans returns the clickable spans for the URL starting at plain[start]
// and the plain-text offset just past the last character it consumed.
func linkSpans(plain string, start int, source string) ([]hyperlinkSpan, int) {
	firstEnd := scanURLRun(plain, start)
	if firstEnd == start {
		return nil, start
	}
	ranges := [][2]int{{start, firstEnd}}
	joined := plain[start:firstEnd]
	end := firstEnd
	tooLong := false

	for {
		next := end
		for next < len(plain) && (plain[next] == ' ' || plain[next] == '\t') {
			next++
		}
		if next >= len(plain) || plain[next] != '\n' {
			break
		}
		next++
		for next < len(plain) && (plain[next] == ' ' || plain[next] == '\t') {
			next++
		}
		chunkStart, chunkEnd, long := continuationRun(plain, next, joined, source)
		// A blockquote can put decoration-only lines between a wrapped URL's
		// segments. They carry no URL text, so look past them; continuationRun
		// still decides from the source whether the next run continues the URL.
		for chunkEnd == 0 && !long && next < len(plain) && !lineHasURLRun(plain, next) {
			// Look past decoration only while the visible prefix is itself cut
			// short in the source. Otherwise the prefix is already a complete
			// URL, and a later line that happens to extend it (a different URL
			// elsewhere in the message) is not its continuation.
			if completeURLInSource(source, joined) {
				break
			}
			lineEnd := strings.IndexByte(plain[next:], '\n')
			if lineEnd < 0 {
				break
			}
			next += lineEnd + 1
			for next < len(plain) && (plain[next] == ' ' || plain[next] == '\t') {
				next++
			}
			if next >= len(plain) {
				break
			}
			chunkStart, chunkEnd, long = continuationRun(plain, next, joined, source)
		}
		if chunkEnd == 0 {
			tooLong = long
			break
		}
		joined += plain[chunkStart:chunkEnd]
		ranges = append(ranges, [2]int{chunkStart, chunkEnd})
		end = chunkEnd
	}

	trimmed := trimURLTail(joined)
	if tooLong || trimmed == "" || len(trimmed) > maxHyperlinkURL || !completeURLInSource(source, trimmed) {
		return nil, end
	}
	// Drop the trimmed punctuation from the tail of the last range, then from
	// any range it empties. A URL is one unbroken run per line, so trimming
	// only ever touches the end of the final range.
	drop := len(joined) - len(trimmed)
	for drop > 0 && len(ranges) > 0 {
		last := &ranges[len(ranges)-1]
		run := last[1] - last[0]
		if run <= drop {
			drop -= run
			ranges = ranges[:len(ranges)-1]
			continue
		}
		last[1] -= drop
		drop = 0
	}
	if len(ranges) == 0 {
		return nil, end
	}

	spans := make([]hyperlinkSpan, 0, len(ranges))
	for _, r := range ranges {
		spans = append(spans, hyperlinkSpan{start: r[0], end: r[1], url: trimmed})
	}
	return spans, end
}

// continuationRun returns the run on the next rendered line that continues
// joined, and its byte offsets in plain. The continuation does not always start
// the line: the blockquote style prefixes every rendered line with "│ ", so a
// wrapped URL segment can sit behind decoration. The markdown source decides
// which run is real instead of the rendered layout: only a run that, appended
// to what is already visible, still occurs in the source continues the URL.
//
// The search is bounded by the rendered line, which glamour already wraps to the
// terminal width, and it only tries positions that begin a run: the line start
// or the byte after a non-URL byte such as the blockquote's decoration. Scanning
// inside a long unbreakable token would cost quadratic work on one line. When no
// run qualifies but one was too long to link, tooLong tells the caller to abandon
// the URL rather than link a truncated prefix.
func continuationRun(plain string, lineStart int, joined, source string) (start, end int, tooLong bool) {
	for p := lineStart; p < len(plain) && plain[p] != '\n'; p++ {
		if p > lineStart && urlByte(plain[p-1]) {
			continue
		}
		if runEnd := scanURLRun(plain, p); runEnd > p {
			if strings.Contains(source, joined+plain[p:runEnd]) {
				if len(joined)+runEnd-p > maxHyperlinkURL {
					return 0, 0, true
				}
				return p, runEnd, false
			}
		}
	}
	return 0, 0, false
}

// lineHasURLRun reports whether the line starting at start contains a run of
// URL characters. A decoration-only line (a blockquote's "│" or padding) does
// not, so linkSpans can look past one for a wrapped continuation.
func lineHasURLRun(plain string, start int) bool {
	for p := start; p < len(plain) && plain[p] != '\n'; p++ {
		if p > start && urlByte(plain[p-1]) {
			continue
		}
		if scanURLRun(plain, p) > p {
			return true
		}
	}
	return false
}

// asciiLower lowercases ASCII letters without changing the byte length, so
// offsets into the result remain offsets into the original string. Unicode
// case folding can change length and would break that mapping.
func asciiLower(s string) string {
	b := []byte(s)
	changed := false
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
			changed = true
		}
	}
	if !changed {
		return s
	}
	return string(b)
}

// completeURLInSource reports whether url occurs in source as a complete URL
// rather than as the visible prefix of a longer run of URL characters. It is
// what keeps a hard-wrapped fragment from being linked with a truncated target
// when the wrap could not be validated.
func completeURLInSource(source, url string) bool {
	for search := 0; search < len(source); {
		i := strings.Index(source[search:], url)
		if i < 0 {
			return false
		}
		end := search + i + len(url)
		if end >= len(source) || !urlContinuesInSource(source[end:]) {
			return true
		}
		search += i + 1
	}
	return false
}

// urlContinuesInSource reports whether the text after url in the markdown
// source means the match is only a prefix of a longer URL. Sentence
// punctuation (.,;!?'") terminates the URL only when the source does not resume
// a URL after it: trimURLTail already removed a trailing sentence mark from the
// visible URL, but the mark can also be an interior dot in a hostname
// ("https://example.com") whose visible prefix a blockquote decoration splits
// away from the rest. Looking past the marks keeps such a prefix unlinked
// instead of generating a truncated target.
//
// A non-ASCII rune is ambiguous: it is either prose punctuation that follows
// the URL without a space (CJK full stops, an em dash), or a prose word or an
// internationalized URL that cannot be told apart from a longer URL. Only
// Unicode punctuation and space end the URL; anything else keeps the match
// incomplete, so a truncated target is never generated.
func urlContinuesInSource(rest string) bool {
	for len(rest) > 0 {
		r, size := utf8.DecodeRuneInString(rest)
		if r >= utf8.RuneSelf {
			return !unicode.IsPunct(r) && !unicode.IsSpace(r)
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return true
		}
		if strings.ContainsRune("-_:~/?@$&*+=%[]", r) {
			return true
		}
		if !strings.ContainsRune(".,;!?'\"", r) {
			return false
		}
		rest = rest[size:]
	}
	return false
}

// hasHTTPScheme reports whether s begins a linkable absolute URL.
func hasHTTPScheme(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// scanURLRun consumes URL characters on one rendered line and returns the
// offset just past the last one. Whitespace ends the run; the next line is a
// separate run that linkSpans may join.
func scanURLRun(plain string, start int) int {
	i := start
	for i < len(plain) && urlByte(plain[i]) {
		i++
	}
	return i
}

// urlByte reports whether b can be part of a rendered URL. Prose punctuation
// that commonly follows a URL is intentionally allowed here and trimmed later,
// because the same characters are legal inside a URL. Only ASCII is accepted:
// a URL is opened as its visible text, and treating non-ASCII bytes as part of
// it would swallow prose that follows without a space (CJK sentences, an em
// dash). An internationalized URL still renders, but stays unlinked.
func urlByte(b byte) bool {
	if b >= utf8.RuneSelf {
		return false
	}
	if b <= ' ' || b == 0x7f {
		return false
	}
	switch b {
	case '<', '>', '"', '`', '\\':
		return false
	}
	return true
}

// urlContinuationByte reports whether b, immediately before a URL, means the
// URL is really the tail of a longer token. It is narrower than urlByte so
// prose punctuation such as "(" still starts a link.
func urlContinuationByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	}
	return strings.IndexByte("-._~:/?#@!$&*+,;=%[]", b) >= 0
}

// trimURLTail removes sentence punctuation and unbalanced closing brackets
// that follow a URL in prose, keeping brackets that belong to the URL itself.
func trimURLTail(u string) string {
	for len(u) > 0 {
		last := u[len(u)-1]
		switch last {
		case '.', ',', ';', ':', '!', '?', '\'', '"':
			u = u[:len(u)-1]
			continue
		case ')', ']', '}':
			var open byte
			switch last {
			case ')':
				open = '('
			case ']':
				open = '['
			default:
				open = '{'
			}
			if strings.Count(u, string(open)) < strings.Count(u, string(last)) {
				u = u[:len(u)-1]
				continue
			}
		}
		return u
	}
	return u
}

// plainTextWithOffsets returns rendered with its ANSI sequences removed and a
// mapping from each plain-text byte offset to its byte offset in rendered.
// The final mapping entry is the insertion point just past the last generated
// plain byte.
func plainTextWithOffsets(rendered string) (string, []int) {
	var b strings.Builder
	b.Grow(len(rendered))
	offsets := make([]int, 0, len(rendered)+1)

	p := ansi.NewParser()
	var state byte
	pos := 0
	for len(rendered) > 0 {
		seq, _, n, newState := ansi.DecodeSequence(rendered, state, p)
		if n == 0 {
			break
		}
		state = newState
		// Escape sequences occupy no cells and are not part of a URL. The
		// decode scrub (RenderMarkdown) has already reduced them to reviewed
		// SGR, so this branch is styling only.
		if len(seq) > 0 && seq[0] != 0x1b && (len(seq) == 1 || utf8.ValidString(seq)) {
			for k := 0; k < len(seq); k++ {
				offsets = append(offsets, pos+k)
				b.WriteByte(seq[k])
			}
		}
		pos += n
		rendered = rendered[n:]
	}
	offsets = append(offsets, pos)
	return b.String(), offsets
}
