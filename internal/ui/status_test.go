package ui

import (
	"testing"
	"unicode/utf8"
)

func TestShortID(t *testing.T) {
	if got := ShortID("11111111-2222-3333-4444-555555555555"); got != "11111111" {
		t.Errorf("got %q", got)
	}
	if got := ShortID("short"); got != "short" {
		t.Errorf("short id should be unchanged, got %q", got)
	}
	if got := ShortID("ééééééééé"); got != "éééééééé" || !utf8.ValidString(got) {
		t.Errorf("multibyte id mangled: %q", got)
	}
}
