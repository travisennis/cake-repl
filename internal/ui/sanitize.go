package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
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
// is unconditional: no feature needs escapes from the stream to survive, and
// the raw bytes are still recorded in the debug log.
func Sanitize(s string) string {
	if s == "" {
		return s
	}
	s = ansi.Strip(s)
	if !needsScrub(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	// Column tracks the rendered cell count of what has been written to b on
	// the current line. Tab stops are relative to column zero of the string and
	// reset after every newline. Contiguous printable runs are buffered so
	// their width is measured as a whole: ansi.StringWidth counts grapheme
	// clusters, so CJK, emoji, and combining sequences advance by the cells the
	// terminal actually draws, not by rune count.
	col := 0
	var run strings.Builder
	flush := func() {
		if run.Len() == 0 {
			return
		}
		rs := run.String()
		col += ansi.StringWidth(rs)
		b.WriteString(rs)
		run.Reset()
	}
	for _, r := range s {
		switch {
		case r == '\n':
			flush()
			b.WriteRune(r)
			col = 0
		case r == '\t':
			flush()
			pad := tabStop - col%tabStop
			b.WriteString(strings.Repeat(" ", pad))
			col += pad
		case isControl(r):
			// Dropped: never reaches the terminal and does not advance the
			// column. The run stays buffered so a grapheme spanning the
			// dropped byte (for example ❤\x00\ufe0f) is still measured whole
			// when it is flushed at the next tab or newline.
		default:
			run.WriteRune(r)
		}
	}
	flush()
	return b.String()
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
