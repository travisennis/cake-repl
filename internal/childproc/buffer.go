package childproc

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

type CapWriter struct {
	limit    int
	buf      bytes.Buffer
	overflow bool
}

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

func (w *CapWriter) Bytes() []byte {
	return w.buf.Bytes()
}

func (w *CapWriter) Overflowed() bool {
	return w.overflow
}

type TailBuffer struct {
	limit     int
	buf       []byte
	truncated bool
}

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
