package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEnterAddsNewlineAfterLargePaste(t *testing.T) {
	m := newLaidOutModel()

	// Simulate a large paste by setting the value directly (pasteMsg bypasses
	// the textarea's line cap, mirroring observed behavior of 99/257-line
	// pastes). Use exactly 99 lines: that is the bubble textarea's default
	// MaxHeight, at which Enter used to become a no-op.
	lines := make([]string, 99)
	for i := range lines {
		lines[i] = "line"
	}
	m.input.SetValue(strings.Join(lines, "\n"))
	m.layout()

	before := m.input.LineCount()

	// Press Enter: this should insert a new line in the multiline input.
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter, Runes: []rune{'\n'}})
	m2 := mm.(Model)
	after := m2.input.LineCount()

	if after != before+1 {
		t.Fatalf("Enter after paste did not add a line: before=%d after=%d", before, after)
	}
}
