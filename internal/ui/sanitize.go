package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// sanitizeTab is what replaces a tab in sanitized text. Tabs are expanded
// rather than kept because terminals advance them to the next tab stop while
// width measurement counts them as zero cells, which desynchronizes wrapping
// and padding math.
const sanitizeTab = "    "

// Sanitize makes untrusted text safe to write to the terminal.
//
// Timeline content originates in cake's stream: tool output is the stdout of
// arbitrary commands and the contents of arbitrary files, so it must be
// treated as attacker-influenceable. Left alone, it can clear the screen
// (CSI 2J), write the user's clipboard (OSC 52), forge hyperlinks (OSC 8), or
// silently break frame alignment, because width measurement ignores escape
// sequences and C0 bytes that the terminal still acts on.
//
// Sanitize therefore removes every ANSI escape sequence, expands tabs, drops
// the remaining C0 and C1 control characters and DEL, and replaces invalid
// UTF-8 with U+FFFD. Newlines are the only control character preserved;
// callers rely on them for line structure. Sanitization is unconditional: no
// feature needs escapes from the stream to survive, and the raw bytes are
// still recorded in the debug log.
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
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(sanitizeTab)
		case isControl(r):
			// Dropped: never reaches the terminal.
		default:
			b.WriteRune(r)
		}
	}
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
