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
