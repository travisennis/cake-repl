package ui

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

const (
	hyperlinkOpenPrefix = "\x1b]8;;"
	hyperlinkClose      = "\x1b]8;;\x1b\\"

	maxHyperlinkURL = 4096
)

type hyperlinkSpan struct {
	start int
	end   int
	url   string
}

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
		for chunkEnd == 0 && !long && next < len(plain) && !lineHasURLRun(plain, next) {
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
		if strings.ContainsRune("-:~/?@$&+=%[]", r) {
			return true
		}
		if !strings.ContainsRune(".,;!?'\"()#{}^|*_", r) {
			return false
		}
		rest = rest[size:]
	}
	return false
}

func hasHTTPScheme(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func scanURLRun(plain string, start int) int {
	i := start
	for i < len(plain) && urlByte(plain[i]) {
		i++
	}
	return i
}

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

func urlContinuationByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	}
	return strings.IndexByte("-._~:/?#@!$&*+,;=%[]", b) >= 0
}

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
