package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestSanitize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"clean text untouched", "hello world\nsecond line", "hello world\nsecond line"},
		{"csi erase display", "hello\x1b[2Jworld", "helloworld"},
		{"csi sgr color", "\x1b[31mred\x1b[0m", "red"},
		{"osc 52 clipboard bel", "a\x1b]52;c;aGF4\x07b", "ab"},
		{"osc 52 clipboard st", "a\x1b]52;c;aGF4\x1b\\b", "ab"},
		{"osc 8 hyperlink", "\x1b]8;;https://evil\x07click\x1b]8;;\x07", "click"},
		{"carriage return dropped", "abc\r\ndef", "abc\ndef"},
		{"lone carriage return dropped", "abc\rXYZ", "abcXYZ"},
		{"backspace dropped", "abc\bd", "abcd"},
		{"nul and bel dropped", "a\x00b\x07c", "abc"},
		{"del dropped", "a\x7fb", "ab"},
		{"tab at column zero", "\ta", "        a"},
		{"tab exactly on a stop", "12345678\ta", "12345678        a"},
		{"tab between stops", "abc\ta", "abc     a"},
		{"tab resets after newline", "a\tb\nc\td", "a       b\nc       d"},
		{"newlines preserved", "a\n\nb", "a\n\nb"},
		{"c1 control rune dropped", "a\u009bb", "ab"},
		{"invalid utf8 dropped by strip", "a\xffc", "ac"},
		{"truncated utf8 replaced", "a\xc2", "a\ufffd"},
		{"lone escape takes its final byte", "a\x1bb", "a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Sanitize(tt.in); got != tt.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestSanitizeToolOutput pins the tool-color passthrough (ADR 009): with
// keepSGR true only reviewed SGR sequences survive, every other escape family
// is still dropped, and an embedded reset is followed by the enclosing style's
// opening sequence so the theme resumes instead of bleeding out.
func TestSanitizeToolOutput(t *testing.T) {
	const open = "\x1b[2m" // the enclosing faint style's opening sequence
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"keeps basic color", "\x1b[31mred\x1b[0m", "\x1b[31mred\x1b[0m" + open},
		{"keeps 256-color", "\x1b[38;5;196mX", "\x1b[38;5;196mX"},
		{"keeps truecolor", "\x1b[48;2;1;2;3mY", "\x1b[48;2;1;2;3mY"},
		{"keeps underline color", "\x1b[58;5;10mU", "\x1b[58;5;10mU"},
		{"keeps multi-parameter", "\x1b[1;31mZ", "\x1b[1;31mZ"},
		{"keeps plain text", "plain output\nline two", "plain output\nline two"},
		{"re-emits after 0 reset", "a\x1b[0mb", "a\x1b[0m" + open + "b"},
		{"re-emits after bare csi m", "a\x1b[mb", "a\x1b[m" + open + "b"},
		{"re-emits after default fg", "a\x1b[39mb", "a\x1b[39m" + open + "b"},
		{"re-emits after default bg", "a\x1b[49mb", "a\x1b[49m" + open + "b"},
		{"re-emits after reset in a multi-param sequence", "a\x1b[0;31mb", "a\x1b[0;31m" + open + "b"},
		{"re-emits after default colors in one sequence", "a\x1b[39;49mb", "a\x1b[39;49m" + open + "b"},
		{"no re-emit after a color", "a\x1b[31mb", "a\x1b[31mb"},
		{"no re-emit after a multi-param color", "a\x1b[1;31mb", "a\x1b[1;31mb"},
		{"no re-emit for color index zero", "a\x1b[38;5;0mb", "a\x1b[38;5;0mb"},
		{"no re-emit for black truecolor", "a\x1b[38;2;0;0;0mb", "a\x1b[38;2;0;0;0mb"},
		{"drops erase display", "a\x1b[2Jb", "ab"},
		{"drops cursor move", "a\x1b[1;1Hb", "ab"},
		{"drops clipboard write", "a\x1b]52;c;aGF4\x07b", "ab"},
		{"drops hyperlink", "a\x1b]8;;https://e\x07x\x1b]8;;\x07b", "axb"},
		{"drops dcs", "a\x1bP1;2q\x1b\\b", "ab"},
		{"drops apc", "a\x1b_Gm\x1b\\b", "ab"},
		{"drops private-prefix csi", "a\x1b[?25lb", "ab"},
		{"drops 8-bit csi", "a\x9b31mb", "ab"},
		{"drops incomplete trailing sgr", "a\x1b[38;5;19", "a"},
		{"rejects blink", "a\x1b[5mb", "ab"},
		{"rejects conceal", "a\x1b[8mb", "ab"},
		{"rejects font selection", "a\x1b[10mb", "ab"},
		{"rejects overline", "a\x1b[53mb", "ab"},
		{"rejects superscript", "a\x1b[73mb", "ab"},
		{"rejects out-of-range 256-color", "a\x1b[38;5;300mb", "ab"},
		{"rejects malformed extended color", "a\x1b[38;5mb", "ab"},
		{"expands tabs", "\ta", "        a"},
		{"drops carriage return", "a\rb", "ab"},
		{"mixture keeps sgr and drops the rest", "\x1b[1;32mok\x1b[0m\x1b[2J\x1b]52;c;x\x07", "\x1b[1;32mok\x1b[0m" + open},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeToolOutput(tt.in, true, open); got != tt.want {
				t.Errorf("SanitizeToolOutput(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
	// An empty enclosing sequence re-emits nothing, so the reset stands alone.
	if got := SanitizeToolOutput("a\x1b[0mb", true, ""); got != "a\x1b[0mb" {
		t.Errorf("SanitizeToolOutput with empty open = %q, want %q", got, "a\x1b[0mb")
	}
}

// TestSanitizeToolOutputDisabledMatchesSanitize guards the default path: with
// keepSGR false, tool output is sanitized exactly as every other surface.
func TestSanitizeToolOutputDisabledMatchesSanitize(t *testing.T) {
	for _, in := range []string{
		"",
		"\x1b[31mred\x1b[0m",
		"\x1b]52;c;aGF4\x07\r\n",
		"tab\there\nnext",
		"wide 日本語\temoji 👍",
	} {
		if got, want := SanitizeToolOutput(in, false, "\x1b[2m"), Sanitize(in); got != want {
			t.Errorf("SanitizeToolOutput(%q, false) = %q, want Sanitize = %q", in, got, want)
		}
	}
}

// TestSanitizeToolOutputKeepsWidthHonest checks the invariant the padding math
// depends on: a kept sequence is zero-width to the terminal and to width
// measurement, so SGR passthrough cannot desync a line.
func TestSanitizeToolOutputKeepsWidthHonest(t *testing.T) {
	got := SanitizeToolOutput("\x1b[31mred\x1b[0m\x1b[2J plain", true, "\x1b[2m")
	if strings.Contains(got, "\x1b[2J") {
		t.Fatalf("erase-display survived: %q", got)
	}
	if w, want := lipgloss.Width(got), lipgloss.Width("red plain"); w != want {
		t.Errorf("width = %d, want %d for %q", w, want, got)
	}
}

// TestSanitizeToolOutputEscapeFreeMatchesSanitize pins the property that makes
// passthrough safe to reason about: for input with no escape sequences, the
// enabled path is byte-for-byte Sanitize, so enabling color cannot change how
// ordinary text or stray bytes render.
func TestSanitizeToolOutputEscapeFreeMatchesSanitize(t *testing.T) {
	for _, in := range []string{
		"plain output\nwith a tab\tstop",
		"stray high byte a\xffb",
		"two stray bytes a\xff\xfeb",
		"truncated lead byte a\xc2",
		"lone c1 byte bad\x80byte",
		"wide 日本語 and emoji 👍",
	} {
		if got, want := SanitizeToolOutput(in, true, "\x1b[2m"), Sanitize(in); got != want {
			t.Errorf("SanitizeToolOutput(%q, true) = %q, want Sanitize = %q", in, got, want)
		}
	}
}

// TestSanitizeCleanTextIsIdentical guards the no-allocation fast path: clean
// input must come back as the same string, not a rebuilt copy.
func TestSanitizeCleanTextIsIdentical(t *testing.T) {
	in := "plain output\nwith unicode ✓ and wide 日本語"
	if got := Sanitize(in); got != in {
		t.Errorf("Sanitize altered clean text: %q", got)
	}
}

// TestSanitizeTabStopsWithWideRunes checks that the column used for tab
// expansion counts rendered cells, so CJK, emoji, and combining sequences
// advance the column by the width the terminal actually draws. Each case ends
// with the marker "a" on the next tab stop after its prefix, and the width
// assertion verifies the marker lands exactly on that stop.
func TestSanitizeTabStopsWithWideRunes(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantCol int // cell column the marker must land on
	}{
		{"cjk", "日本語\ta", "日本語  a", 8},                                               // 3 wide runes = 6 cells, pad 2
		{"emoji", "👍\ta", "👍      a", 8},                                             // 1 emoji = 2 cells, pad 6
		{"combining", "e\u0301\ta", "e\u0301       a", 8},                            // 1 cell, pad 7
		{"cjk across a stop", "日本語日本語\ta", "日本語日本語    a", 16},                        // 12 cells, pad 4
		{"grapheme split by dropped control", "❤\x00\ufe0f\ta", "❤\ufe0f      a", 8}, // 2 cells as one grapheme, pad 6
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Sanitize(tt.in)
			if got != tt.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tt.in, got, tt.want)
			}
			i := strings.Index(got, "a")
			if i < 0 {
				t.Fatalf("marker rune lost: %q", got)
			}
			if w := lipgloss.Width(got[:i]); w != tt.wantCol {
				t.Errorf("column before marker = %d, want %d in %q", w, tt.wantCol, got)
			}
		})
	}
}

// TestSanitizeKeepsWidthHonest checks the property the padding math depends
// on: after sanitization the measured width equals the number of cells the
// terminal will actually advance, so no zero-measured byte moves the cursor.
func TestSanitizeKeepsWidthHonest(t *testing.T) {
	raw := "abc\rXYZ\bq\x1b[2Jtail\t"
	got := Sanitize(raw)
	if raw == got {
		t.Fatalf("test input is not exercising sanitization")
	}
	if w := lipgloss.Width(got); w != len([]rune(got)) {
		t.Errorf("sanitized width = %d, want %d for %q", w, len([]rune(got)), got)
	}
	if strings.ContainsAny(got, "\t") {
		t.Errorf("tab survived sanitization: %q", got)
	}
	if strings.ContainsAny(got, "\r\b\x1b") {
		t.Errorf("control characters survived: %q", got)
	}
}
