package ui

import (
	"strconv"
	"strings"
	"testing"
)

// benchArgSizes are the argument sizes the task 099 audit measured: a
// 1,000-byte token is a long flag or path list, 10,000 bytes is a large
// pasted command, and 100,000 bytes is the pathological single token that
// exposes the quadratic assembly cost.
var benchArgSizes = []int{1000, 10000, 100000}

// benchArgWidth and benchArgIndent pin the measurement point from the audit:
// content width 80 cells with a 10-cell continuation indent. A single token
// longer than the width hard-wraps, so the final assembly loop receives many
// lines.
const (
	benchArgWidth  = 80
	benchArgIndent = 10
)

// benchArgSink keeps the wrapped output observable so the compiler cannot
// drop the call being measured.
var benchArgSink string

// BenchmarkWrapToolArgs measures wrapping one oversized single token and
// assembling the final indented text. It is hermetic: a fixed deterministic
// payload, fixed content width and indent, no terminal, and no cake binary.
//
// The wrapped text is plain ASCII with no styling, so no color profile needs
// pinning here; every cost measured is cell counting and string assembly.
func BenchmarkWrapToolArgs(b *testing.B) {
	for _, n := range benchArgSizes {
		b.Run("bytes="+strconv.Itoa(n), func(b *testing.B) {
			token := strings.Repeat("x", n)
			b.SetBytes(int64(n))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				benchArgSink = wrapToolArgs(token, benchArgWidth, benchArgIndent)
			}
		})
	}
}
