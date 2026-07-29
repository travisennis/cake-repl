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
		{"tab expanded", "a\tb", "a    b"},
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

// TestSanitizeKeepsWidthHonest checks the property the padding math depends
// on: after sanitization the measured width equals the number of cells the
// terminal will actually advance, so no zero-measured byte moves the cursor.
func TestSanitizeKeepsWidthHonest(t *testing.T) {
	raw := "abc\rXYZ\bq\x1b[2Jtail"
	got := Sanitize(raw)
	if raw == got {
		t.Fatalf("test input is not exercising sanitization")
	}
	if w := lipgloss.Width(got); w != len([]rune(got)) {
		t.Errorf("sanitized width = %d, want %d for %q", w, len([]rune(got)), got)
	}
	if strings.ContainsAny(got, "\r\b\x1b") {
		t.Errorf("control characters survived: %q", got)
	}
}
