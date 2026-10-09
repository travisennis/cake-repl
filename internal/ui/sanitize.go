package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/parser"
)

// tabStop is the column interval between tab stops. It matches the
// conventional terminal default of eight columns, so tab-separated output
// (column -t, go test, df, hand-made ASCII tables) lines up the way it does in
// a normal terminal.
const tabStop = 8

// Sanitize makes untrusted text safe to write to the terminal.
//
// Timeline content originates in cake's stream: tool output is the stdout of
// arbitrary commands and the contents of arbitrary files, so it must be
// treated as attacker-influenceable. Left alone, it can clear the screen
// (CSI 2J), write the user's clipboard (OSC 52), forge hyperlinks (OSC 8), or
// silently break frame alignment, because width measurement ignores escape
// sequences and C0 bytes that the terminal still acts on.
//
// Sanitize therefore removes every ANSI escape sequence, expands tabs to the
// next eight-column stop, drops the remaining C0 and C1 control characters and
// DEL, and replaces invalid UTF-8 with U+FFFD. Newlines are the only control
// character preserved; callers rely on them for line structure. Sanitization
// is unconditional except for tool output, whose SGR color the user may opt
// into with [SanitizeToolOutput]; the raw bytes are still recorded in the
// debug log.
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

// SanitizeToolOutput is the tool-output sibling of [Sanitize]. With keepSGR
// false it is exactly Sanitize, so the default path is unchanged.
//
// With keepSGR true it parses s and preserves only SGR (CSI ... m) sequences
// whose every parameter is in the reviewed set below; every other escape
// family — OSC, DCS, APC, and every cursor or erase CSI final — is dropped just
// as Sanitize drops it, and the surrounding text is scrubbed identically. Color
// only means rendition here: SGR cannot move the cursor, clear the screen,
// write the clipboard, or forge a hyperlink, and width measurement ignores it,
// so passthrough does not reopen the alignment bug Sanitize closes (ADR 005,
// ADR 009).
//
// Tool output is rendered inside a styled lipgloss block, and a stream-embedded
// reset ends that style mid-line, letting the theme bleed through the rest of
// the block. openSeq is the sequence that reopens the enclosing style (see
// [styleOpen]); it is re-emitted after every reset the passthrough keeps.
func SanitizeToolOutput(s string, keepSGR bool, openSeq string) string {
	if !keepSGR {
		return Sanitize(s)
	}
	return scrubKeepingSGR(s, openSeq)
}

// scrubKeepingSGR is the reviewed-SGR policy shared by every render path that
// has legitimate styling of its own to protect: the opt-in tool-output
// passthrough ([SanitizeToolOutput]) and glamour's rendered markdown, whose
// output carries the theme's SGR but is decoded from untrusted source (see
// [RenderMarkdown]). It keeps SGR sequences whose parameters are in the
// reviewed set and drops everything else the way [Sanitize] does, so no cursor
// motion, erase, clipboard write, or hyperlink can ride along, and it scrubs
// the surrounding text — including any control byte a decode step produced —
// identically.
//
// openSeq is re-emitted after every kept reset so an enclosing style resumes;
// callers with no enclosing style pass the empty string.
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
		// A chunk that starts with ESC, or with a C1 introducer byte, is a
		// sequence; printable chunks are valid UTF-8, or a stray byte. An
		// incomplete trailing sequence leaves the parser inside an escape state
		// and is dropped by the keepSGRSequence check below.
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
		// Invalid bytes are handed to ansi.Strip first so a stray byte becomes
		// exactly what Sanitize makes of it (dropped or U+FFFD); valid text has
		// no escapes left to strip, because the decoder already split them out.
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

// keepSGRSequence reports whether an escape chunk that is a well-formed SGR
// sequence may be passed through. Only the 7-bit form (ESC [ ... m) with no
// private prefix and no intermediate byte qualifies: real tools emit the 7-bit
// form, and an 8-bit C1 CSI (0x9b) is dropped like every other control-string
// family so a terminal that renders it literally cannot desync the width math.
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

// safeSGRParams reports whether every parameter of an SGR sequence is in the
// bounded set cake-repl is willing to pass through. It walks the flattened
// parameter list, because the extended-color selectors (38/48/58) carry their
// arguments as a run of values that only means anything as a group.
//
// The reviewed set is the rendition attributes that carry legible information:
// intensity, italic, underline, reverse, strikethrough, their resets, the
// basic and bright 8-color foreground and background sets, and the extended
// color forms. Blink and conceal are rejected — they add no information and
// concealment can hide text the user needs to see — and so are font selection
// (10-20) and the framed, superscript, and other terminal-specific codes, so
// nothing passes through unexamined.
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

// sgrColorArgs consumes the arguments of an extended color selector (38/48/58)
// and returns how many values after the selector they occupied: either
// `5 ; n` (256-color, n in 0-255) or `2 ; r ; g ; b` (truecolor, each in
// 0-255). It reports false for anything else, including a selector with no or
// malformed arguments.
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

// inByteRange reports whether v is a valid color component.
func inByteRange(v int) bool {
	return v >= 0 && v <= 255
}

// safeSGR reports whether a single SGR parameter is one cake-repl has reviewed
// for passthrough (see safeSGRParams for the set and its rationale).
func safeSGR(v int) bool {
	switch v {
	case 0, 1, 2, 3, 4, 7, 9, // reset, bold, faint, italic, underline, reverse, strikethrough
		21, 22, 23, 24, 27, 29, // their resets
		39, 49, 59: // default foreground, background, underline color
		return true
	}
	return v >= 30 && v <= 37 || v >= 40 && v <= 47 ||
		v >= 90 && v <= 97 || v >= 100 && v <= 107
}

// isResetSequence reports whether a kept SGR sequence gives back the enclosing
// style: a bare reset (CSI m or CSI 0 m) or the color defaults CSI 39/49 m,
// which clear a foreground or background the theme set. A reset may also be one
// parameter of a multi-parameter sequence (CSI 0 ; 31 m), which clears the
// enclosing style just the same, so any reset parameter counts; the arguments of
// an extended-color selector are skipped, because there 0, 39, and 49 are color
// values, not resets. The caller re-emits the enclosing style after one of these
// so the theme resumes for the rest of the block.
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

// trimPartialEscape removes an escape sequence left incomplete at the end of s.
// Truncation cuts on byte offsets and can land inside a sequence; a dangling
// CSI prefix would swallow the bytes that follow it and desync the width math.
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

// scrubber accumulates sanitized text with tab expansion and column tracking.
// Contiguous printable runs are buffered so their width is measured as a whole:
// ansi.StringWidth counts grapheme clusters, so CJK, emoji, and combining
// sequences advance by the cells the terminal actually draws, not by rune
// count. Kept escape sequences are written between runs and carry no cells.
type scrubber struct {
	b   strings.Builder
	run strings.Builder
	col int
}

// writeRune appends one rune of already-escape-free text. Newlines reset the
// column, tabs expand to the next stop, control characters are dropped, and
// everything else is buffered. Dropped controls never reach b or advance the
// column, and the run stays buffered so a grapheme spanning a dropped byte
// (for example ❤\x00\ufe0f) is still measured whole when it is flushed.
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
		// Dropped: never reaches the terminal and does not advance the column.
	default:
		s.run.WriteRune(r)
	}
}

// writeSequence appends a kept escape sequence. Sequences carry no cells, so
// the column is unchanged; the pending run is flushed first so order is kept.
func (s *scrubber) writeSequence(seq string) {
	s.flush()
	s.b.WriteString(seq)
}

// flush writes the buffered printable run, advancing the column by the cells
// the terminal draws for it.
func (s *scrubber) flush() {
	if s.run.Len() == 0 {
		return
	}
	rs := s.run.String()
	s.col += ansi.StringWidth(rs)
	s.b.WriteString(rs)
	s.run.Reset()
}

// sanitized returns the accumulated output.
func (s *scrubber) sanitized() string {
	s.flush()
	return s.b.String()
}

// needsScrub reports whether s contains anything the rune loop would change,
// so clean text (the common case) is returned without allocating. Invalid
// UTF-8 counts: a lone 0x80-0x9f byte is a C1 control to a terminal, and the
// rune loop rewrites it to U+FFFD.
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

// isControl reports whether r is a C0 control character, DEL, or a C1 control
// character. U+FFFD is not treated as control: ranging over a string already
// converts invalid UTF-8 into it, which is the desired replacement.
func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}
