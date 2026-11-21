package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/parser"
)

const tabStop = 8

func Sanitize(s string) string {
	if s == "" {
		return s
	}
	s = ansi.Strip(s)
	if !needsScrub(s) {
		return s
	}
	var sc scrubber
	for _, r := range s {
		sc.writeRune(r)
	}
	return sc.sanitized()
}

func SanitizeToolOutput(s string, keepSGR bool, openSeq string) string {
	if !keepSGR {
		return Sanitize(s)
	}
	return scrubKeepingSGR(s, openSeq)
}

func scrubKeepingSGR(s string, openSeq string) string {
	if s == "" {
		return s
	}
	var sc scrubber
	p := ansi.NewParser()
	var state byte
	for len(s) > 0 {
		seq, _, n, newState := ansi.DecodeSequence(s, state, p)
		if n == 0 {
			break
		}
		state = newState
		s = s[n:]
		if seq[0] == 0x1b || (len(seq) > 1 && !utf8.ValidString(seq)) {
			if !keepSGRSequence(seq, p) {
				continue
			}
			sc.writeSequence(seq)
			if isResetSequence(p.Params()) {
				sc.writeSequence(openSeq)
			}
			continue
		}
		text := seq
		if !utf8.ValidString(text) {
			text = ansi.Strip(text)
		}
		for _, r := range text {
			sc.writeRune(r)
		}
	}
	return sc.sanitized()
}

func keepSGRSequence(seq string, p *ansi.Parser) bool {
	if len(seq) < 3 || seq[0] != 0x1b || seq[1] != '[' {
		return false
	}
	cmd := p.Command()
	if parser.Command(cmd) != 'm' || parser.Prefix(cmd) != 0 || parser.Intermediate(cmd) != 0 {
		return false
	}
	return safeSGRParams(p.Params())
}

func safeSGRParams(params ansi.Params) bool {
	vals := make([]int, 0, len(params))
	params.ForEach(0, func(_ int, param int, _ bool) { vals = append(vals, param) })
	for i := 0; i < len(vals); {
		v := vals[i]
		switch {
		case v == 38 || v == 48 || v == 58:
			n, ok := sgrColorArgs(vals[i+1:])
			if !ok {
				return false
			}
			i += n + 1
		case safeSGR(v):
			i++
		default:
			return false
		}
	}
	return true
}

func sgrColorArgs(rest []int) (int, bool) {
	if len(rest) == 0 {
		return 0, false
	}
	switch rest[0] {
	case 5:
		if len(rest) < 2 || !inByteRange(rest[1]) {
			return 0, false
		}
		return 2, true
	case 2:
		if len(rest) < 4 {
			return 0, false
		}
		for _, c := range rest[1:4] {
			if !inByteRange(c) {
				return 0, false
			}
		}
		return 4, true
	default:
		return 0, false
	}
}

func inByteRange(v int) bool {
	return v >= 0 && v <= 255
}

func safeSGR(v int) bool {
	switch v {
	case 0, 1, 2, 3, 4, 7, 9,
		21, 22, 23, 24, 27, 29,
		39, 49, 59:
		return true
	}
	return v >= 30 && v <= 37 || v >= 40 && v <= 47 ||
		v >= 90 && v <= 97 || v >= 100 && v <= 107
}

func isResetSequence(params ansi.Params) bool {
	vals := make([]int, 0, len(params))
	params.ForEach(0, func(_ int, param int, _ bool) { vals = append(vals, param) })
	if len(vals) == 0 {
		return true
	}
	for i := 0; i < len(vals); {
		switch vals[i] {
		case 38, 48, 58:
			n, ok := sgrColorArgs(vals[i+1:])
			if !ok {
				return false
			}
			i += n + 1
		case 0, 39, 49:
			return true
		default:
			i++
		}
	}
	return false
}

func trimPartialEscape(s string) string {
	i := strings.LastIndexByte(s, 0x1b)
	if i < 0 {
		return s
	}
	p := ansi.NewParser()
	var state byte
	rest := s[i:]
	for len(rest) > 0 {
		_, _, n, newState := ansi.DecodeSequence(rest, state, p)
		if n == 0 {
			break
		}
		state = newState
		rest = rest[n:]
	}
	if state != ansi.NormalState {
		return s[:i]
	}
	return s
}

type scrubber struct {
	b   strings.Builder
	run strings.Builder
	col int
}

func (s *scrubber) writeRune(r rune) {
	switch {
	case r == '\n':
		s.flush()
		s.b.WriteByte('\n')
		s.col = 0
	case r == '\t':
		s.flush()
		pad := tabStop - s.col%tabStop
		s.b.WriteString(strings.Repeat(" ", pad))
		s.col += pad
	case isControl(r):
	default:
		s.run.WriteRune(r)
	}
}

func (s *scrubber) writeSequence(seq string) {
	s.flush()
	s.b.WriteString(seq)
}

func (s *scrubber) flush() {
	if s.run.Len() == 0 {
		return
	}
	rs := s.run.String()
	s.col += ansi.StringWidth(rs)
	s.b.WriteString(rs)
	s.run.Reset()
}

func (s *scrubber) sanitized() string {
	s.flush()
	return s.b.String()
}

func needsScrub(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if r != '\n' && (r == '\t' || isControl(r)) {
			return true
		}
	}
	return false
}

func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}
