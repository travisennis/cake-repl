package childproc

import "testing"

func TestTailBuffer(t *testing.T) {
	tests := []struct {
		name   string
		limit  int
		writes []string
		want   string
	}{
		{"under limit", 8, []string{"  hi\n"}, "hi"},
		{"rolls across writes", 8, []string{"aaaa", "bbbb", "cccc"}, "…bbbbcccc"},
		{"single write over limit", 4, []string{"0123456789"}, "…6789"},
		{"empty", 8, nil, ""},
		// "aé日本" is 9 bytes; keeping the last 5 lands mid-rune inside 日,
		// whose continuation bytes must be dropped.
		{"utf8 boundary", 5, []string{"aé日本"}, "…本"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewTailBuffer(tt.limit)
			for _, w := range tt.writes {
				n, err := b.Write([]byte(w))
				if n != len(w) || err != nil {
					t.Fatalf("Write(%q) = (%d, %v), want (%d, nil)", w, n, err, len(w))
				}
			}
			if got := b.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if len(b.buf) > tt.limit {
				t.Errorf("retained %d bytes, limit %d", len(b.buf), tt.limit)
			}
		})
	}
}

func TestCapWriterKeepsHeadAndReportsOverflow(t *testing.T) {
	tests := []struct {
		name         string
		limit        int
		writes       []string
		want         string
		wantOverflow bool
	}{
		{"under limit", 8, []string{"abc"}, "abc", false},
		{"exact limit", 3, []string{"abc"}, "abc", false},
		{"single write over limit", 4, []string{"0123456789"}, "0123", true},
		{"rolls across writes", 4, []string{"ab", "cdef"}, "abcd", true},
		{"zero limit", 0, []string{"abc"}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewCapWriter(tt.limit)
			for _, s := range tt.writes {
				n, err := w.Write([]byte(s))
				if n != len(s) || err != nil {
					t.Fatalf("Write(%q) = (%d, %v), want (%d, nil)", s, n, err, len(s))
				}
			}
			if got := string(w.Bytes()); got != tt.want {
				t.Errorf("Bytes() = %q, want %q", got, tt.want)
			}
			if got := w.Overflowed(); got != tt.wantOverflow {
				t.Errorf("Overflowed() = %v, want %v", got, tt.wantOverflow)
			}
		})
	}
}
