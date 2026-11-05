package childproc

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

// CapWriter keeps at most limit bytes and records whether more arrived. It
// keeps the head because callers parse one JSON object from its first byte and
// because the head names the command that produced it. A chatty or wrong child
// therefore cannot grow the REPL's memory without bound.
type CapWriter struct {
	limit    int
	buf      bytes.Buffer
	overflow bool
}

// NewCapWriter returns a writer that retains the first limit bytes written.
func NewCapWriter(limit int) *CapWriter {
	return &CapWriter{limit: limit}
}

func (w *CapWriter) Write(p []byte) (int, error) {
	room := w.limit - w.buf.Len()
	if room <= 0 {
		w.overflow = true
		return len(p), nil
	}
	if len(p) > room {
		w.buf.Write(p[:room])
		w.overflow = true
		return len(p), nil
	}
	w.buf.Write(p)
	return len(p), nil
}

// Bytes returns the retained head. The slice is valid until the next Write.
func (w *CapWriter) Bytes() []byte {
	return w.buf.Bytes()
}

// Overflowed reports whether any bytes were discarded.
func (w *CapWriter) Overflowed() bool {
	return w.overflow
}

// TailBuffer retains the last limit bytes of output, which is enough to show a
// diagnostic without letting a chatty or wrong child grow memory.
type TailBuffer struct {
	limit     int
	buf       []byte
	truncated bool
}

// NewTailBuffer returns a writer that retains the last limit bytes written.
func NewTailBuffer(limit int) *TailBuffer {
	return &TailBuffer{limit: limit}
}

func (b *TailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if n > b.limit {
		b.truncated = true
		p = p[n-b.limit:]
	}
	b.buf = append(b.buf, p...)
	if overflow := len(b.buf) - b.limit; overflow > 0 {
		b.truncated = true
		copy(b.buf, b.buf[overflow:])
		b.buf = b.buf[:b.limit]
	}
	return n, nil
}

// String returns the retained tail, trimmed, with an ellipsis marking any
// dropped output. A cut that lands mid-rune drops the continuation bytes so the
// kept tail starts on a rune boundary.
func (b *TailBuffer) String() string {
	buf := b.buf
	if b.truncated {
		for len(buf) > 0 && !utf8.RuneStart(buf[0]) {
			buf = buf[1:]
		}
	}
	s := strings.TrimSpace(string(buf))
	if b.truncated {
		return "…" + s
	}
	return s
}
