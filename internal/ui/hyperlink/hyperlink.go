package hyperlink

import (
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

const (
	openPrefix = "\x1b]8;;"
	closeSeq   = "\x1b]8;;\x1b\\"
	maxURL     = 4096
)

type span struct {
	start int
	end   int
	url   string
}

var (
	parserMu sync.Mutex
	parser   = goldmark.New(goldmark.WithExtensions(extension.GFM))
)

func Add(rendered, source string) string {
	if !strings.Contains(rendered, "://") {
		return rendered
	}
	urls := sourceURLs(source)
	if len(urls) == 0 {
		return rendered
	}
	plain, offsets := plainTextWithOffsets(rendered)
	lower := asciiLower(plain)
	var spans []span
	for _, url := range urls {
		prefix := asciiLower(url)
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		for search := 0; search < len(lower); {
			i := strings.Index(lower[search:], prefix)
			if i < 0 {
				break
			}
			start := search + i
			search = start + 1
			ranges, end, ok := matchURL(plain, start, url)
			if !ok {
				continue
			}
			for _, r := range ranges {
				spans = append(spans, span{start: r[0], end: r[1], url: url})
			}
			if end > search {
				search = end
			}
		}
	}
	if len(spans) == 0 {
		return rendered
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start == spans[j].start {
			return spans[i].end > spans[j].end
		}
		return spans[i].start < spans[j].start
	})
	var b strings.Builder
	b.Grow(len(rendered) + len(spans)*(len(openPrefix)+len(closeSeq)))
	prevRendered, prevPlain := 0, 0
	for _, sp := range spans {
		if sp.start < prevPlain || sp.end <= sp.start || sp.end > len(plain) {
			continue
		}
		start, end := offsets[sp.start], offsets[sp.end]
		if start < prevRendered || end <= start {
			continue
		}
		b.WriteString(rendered[prevRendered:start])
		b.WriteString(openPrefix)
		b.WriteString(sp.url)
		b.WriteString("\x1b\\")
		b.WriteString(rendered[start:end])
		b.WriteString(closeSeq)
		prevRendered, prevPlain = end, sp.end
	}
	b.WriteString(rendered[prevRendered:])
	return b.String()
}

func sourceURLs(source string) []string {
	parserMu.Lock()
	doc := parser.Parser().Parse(text.NewReader([]byte(source)))
	parserMu.Unlock()
	seen := make(map[string]struct{})
	var urls []string
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var raw string
		switch n := node.(type) {
		case *ast.Link:
			raw = string(n.Destination)
		case *ast.AutoLink:
			raw = string(n.URL([]byte(source)))
		default:
			return ast.WalkContinue, nil
		}
		if !validURL(raw) {
			return ast.WalkContinue, nil
		}
		if _, ok := seen[raw]; ok {
			return ast.WalkContinue, nil
		}
		seen[raw] = struct{}{}
		urls = append(urls, raw)
		return ast.WalkContinue, nil
	})
	return urls
}

func validURL(url string) bool {
	lower := asciiLower(url)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return false
	}
	if len(url) > maxURL {
		return false
	}
	for i := 0; i < len(url); i++ {
		if url[i] < 0x20 || url[i] == 0x7f {
			return false
		}
	}
	return true
}

func matchURL(plain string, start int, url string) ([][2]int, int, bool) {
	p := start
	i := 0
	rangeStart := p
	var ranges [][2]int
	for i < len(url) {
		if p < len(plain) && plain[p] == url[i] {
			p++
			i++
			continue
		}
		next, ok := skipWrap(plain, p, url[i])
		if !ok {
			return nil, p, false
		}
		if p > rangeStart {
			ranges = append(ranges, [2]int{rangeStart, p})
		}
		p = next
		rangeStart = p
	}
	if p > rangeStart {
		ranges = append(ranges, [2]int{rangeStart, p})
	}
	return ranges, p, true
}

func skipWrap(plain string, p int, want byte) (int, bool) {
	q := p
	for q < len(plain) && (plain[q] == ' ' || plain[q] == '\t' || plain[q] == '\r') {
		q++
	}
	if q == p || q >= len(plain) || plain[q] != '\n' {
		return 0, false
	}
	q++
	for q < len(plain) {
		switch plain[q] {
		case ' ', '\t', '\r', '>':
			q++
			continue
		}
		r, size := utf8.DecodeRuneInString(plain[q:])
		if r == '│' || r == '┃' {
			q += size
			continue
		}
		if plain[q] == want {
			return q, true
		}
		return 0, false
	}
	return 0, false
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
