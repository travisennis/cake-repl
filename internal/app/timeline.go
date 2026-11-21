package app

import (
	"strings"

	"github.com/travisennis/cake-repl/internal/ui"
)

type timelineRenderer func(ui.Item, int) string

type noticeID uint64

func (m *Model) appendNotice(it ui.Item) noticeID {
	m.noticeSeq++
	it.ID = m.noticeSeq
	m.appendItem(it)
	return noticeID(it.ID)
}

func (m *Model) removeNotice(id noticeID) bool {
	if id == 0 {
		return false
	}
	for i := len(m.timeline.items) - 1; i >= 0; i-- {
		if noticeID(m.timeline.items[i].ID) == id {
			m.removeItemAt(i)
			return true
		}
	}
	return false
}

type timeline struct {
	items    []ui.Item
	rendered []string
	width    int
	dirty    bool
}

func (t *timeline) append(it ui.Item, maxItems int, render timelineRenderer) int {
	t.items = append(t.items, it)
	trimmed := 0
	if maxItems > 0 && len(t.items) > maxItems {
		trimmed = len(t.items) - maxItems
		t.trimFront(trimmed)
	}
	if t.width > 0 {
		t.rendered = append(t.rendered, render(it, t.width))
		t.dirty = true
	}
	return trimmed
}

func (t *timeline) insert(idx int, it ui.Item, maxItems int, render timelineRenderer) int {
	trimmed := 0
	if maxItems > 0 && len(t.items) >= maxItems {
		trimmed = len(t.items) - maxItems + 1
		t.trimFront(trimmed)
		idx -= trimmed
	}
	if idx < 0 {
		idx = 0
	}
	if idx > len(t.items) {
		idx = len(t.items)
	}

	t.items = append(t.items, ui.Item{})
	copy(t.items[idx+1:], t.items[idx:])
	t.items[idx] = it

	if t.width > 0 {
		rendered := render(it, t.width)
		t.rendered = append(t.rendered, "")
		copy(t.rendered[idx+1:], t.rendered[idx:])
		t.rendered[idx] = rendered
		t.dirty = true
	}
	return trimmed
}

func (t *timeline) remove(idx int) bool {
	if idx < 0 || idx >= len(t.items) {
		return false
	}
	items := make([]ui.Item, len(t.items)-1)
	copy(items, t.items[:idx])
	copy(items[idx:], t.items[idx+1:])
	t.items = items
	if t.width > 0 {
		rendered := make([]string, len(t.rendered)-1)
		copy(rendered, t.rendered[:idx])
		copy(rendered[idx:], t.rendered[idx+1:])
		t.rendered = rendered
		t.dirty = true
	}
	return true
}

func (t *timeline) trimFront(over int) {
	if over <= 0 || len(t.items) == 0 {
		return
	}
	if over > len(t.items) {
		over = len(t.items)
	}
	items := make([]ui.Item, len(t.items)-over)
	copy(items, t.items[over:])
	t.items = items
	if t.width > 0 {
		rendered := make([]string, len(t.rendered)-over)
		copy(rendered, t.rendered[over:])
		t.rendered = rendered
		t.dirty = true
	}
}

func (t *timeline) rerender(idx int, render timelineRenderer) {
	if t.width == 0 || idx < 0 || idx >= len(t.items) {
		return
	}
	t.rendered[idx] = render(t.items[idx], t.width)
	t.dirty = true
}

func (t *timeline) rerenderTools(render timelineRenderer) {
	if t.width == 0 {
		return
	}
	for i, it := range t.items {
		if it.Kind == ui.KindTool {
			t.rendered[i] = render(it, t.width)
		}
	}
	t.dirty = true
}

func (t *timeline) resize(width int, render timelineRenderer) {
	t.width = width
	t.rebuild(render)
}

func (t *timeline) rebuild(render timelineRenderer) {
	if t.width == 0 || len(t.items) == 0 {
		t.rendered = nil
		if t.width > 0 {
			t.dirty = true
		}
		return
	}
	t.rendered = t.rendered[:0]
	for _, it := range t.items {
		t.rendered = append(t.rendered, render(it, t.width))
	}
	t.dirty = true
}

func (t *timeline) clear() {
	t.items = nil
	t.rendered = nil
	if t.width > 0 {
		t.dirty = true
	}
}

func (t *timeline) content() string {
	return strings.Join(t.rendered, "\n\n")
}

func (t *timeline) takeDirty() bool {
	dirty := t.dirty
	t.dirty = false
	return dirty
}
