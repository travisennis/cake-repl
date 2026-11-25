package app

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
)

const composerWheelLines = 1

func composerRows(input textarea.Model) (total, cursor int) {
	probe := input
	for {
		info := probe.LineInfo()
		line, row := probe.Line(), info.RowOffset
		if line == 0 && row == 0 {
			break
		}
		probe.CursorUp()
		if next := probe.LineInfo(); probe.Line() == line && next.RowOffset == row {
			break
		}
		cursor++
	}
	total = 1
	for {
		info := probe.LineInfo()
		line, row := probe.Line(), info.RowOffset
		probe.CursorDown()
		if next := probe.LineInfo(); probe.Line() == line && next.RowOffset == row {
			break
		}
		total++
	}
	return total, cursor
}

func (m Model) composerHeight() int {
	return inputHeightFor(m.input.LineCount())
}

func (m Model) composerMaxScroll() int {
	return max(0, m.input.Height()-1-m.composerHeight())
}

func (m Model) composerView() string {
	content := m.input.View()
	visible := m.composerHeight()
	lines := strings.Split(content, "\n")
	if len(lines) <= visible {
		return content
	}
	offset := max(0, min(m.composerScroll, len(lines)-1-visible))
	return strings.Join(lines[offset:offset+visible], "\n")
}

func (m Model) composerHit(y int) bool {
	if !m.ready || m.cfg.Inline {
		return false
	}
	top := m.viewport.Height
	return y >= top && y < top+m.composerHeight()+composerVerticalChrome
}

func (m *Model) scrollComposer(delta int) {
	m.composerScroll = max(0, min(m.composerScroll+delta, m.composerMaxScroll()))
}

func (m *Model) followComposerCursor() {
	if !m.ready {
		return
	}
	cursor := m.composerCursor
	visible := m.composerHeight()
	if cursor < m.composerScroll {
		m.composerScroll = cursor
	} else if cursor >= m.composerScroll+visible {
		m.composerScroll = cursor - visible + 1
	}
	m.composerScroll = max(0, min(m.composerScroll, m.composerMaxScroll()))
}

func (m *Model) resizeComposer() {
	visible := m.composerHeight()
	total, cursor := composerRows(m.input)
	m.composerCursor = cursor
	if total > m.input.Height() {
		m.resetComposerViewport()
	}
	height := max(visible, total+1)
	maxHeight := m.input.MaxHeight
	if height > maxHeight {
		m.input.MaxHeight = height
	}
	m.input.SetHeight(height)
	m.input.MaxHeight = maxHeight
	m.composerScroll = max(0, min(m.composerScroll, m.composerMaxScroll()))
}

func (m *Model) resetComposerViewport() {
	row := m.input.Line()
	col := composerCursorColumn(m.input)
	value := m.input.Value()
	m.input.SetValue(value)
	for m.input.Line() > 0 {
		m.input.CursorUp()
	}
	for m.input.Line() < row {
		m.input.CursorDown()
	}
	m.input.SetCursor(col)
}

func composerCursorColumn(input textarea.Model) int {
	info := input.LineInfo()
	return info.StartColumn + info.ColumnOffset
}
