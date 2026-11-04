package app

import (
	"strings"

	"github.com/travisennis/cake-repl/internal/ui"
)

// timelineRenderer renders one item at the timeline's current width. The
// timeline passes its own width, so callers cannot render at a stale size.
type timelineRenderer func(ui.Item, int) string

// timeline owns the structured items and their cached rendered rows. Every
// mutation goes through this type, so the slices cannot drift apart:
// whenever width is non-zero, len(rendered) == len(items). Before the first
// layout width is zero and no rows are cached; resize renders the items that
// accumulated while the terminal size was unknown.
//
// Mutations that remove entries copy the survivors into fresh slices. That
// releases the removed payloads instead of leaving them reachable through an
// old backing array, which is what -max-timeline-items and /clear promise
// (ADR 010).
type timeline struct {
	items    []ui.Item
	rendered []string
	width    int
	dirty    bool
}

// append adds it and enforces maxItems (zero or negative means unbounded). It
// returns how many front items were trimmed, so the caller can drop pending
// tool calls that referenced them.
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

// insert places it at idx, shifting later items right. At the cap it trims
// before inserting, so the new item always survives and idx is clamped into
// the surviving slice. It returns how many front items were trimmed.
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

	t.items = append(t.items, ui.Item{}) // make room
	copy(t.items[idx+1:], t.items[idx:])
	t.items[idx] = it

	if t.width > 0 {
		rendered := render(it, t.width)
		t.rendered = append(t.rendered, "") // make room
		copy(t.rendered[idx+1:], t.rendered[idx:])
		t.rendered[idx] = rendered
		t.dirty = true
	}
	return trimmed
}

// remove deletes the item and row at idx. It reports whether idx was valid.
// There is no length-based fallback: the owner's invariant makes the two
// slices move together.
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

// trimFront drops the first over items and rows, copying survivors so the
// trimmed payloads are released. It returns how many entries were removed.
func (t *timeline) trimFront(over int) int {
	if over <= 0 || len(t.items) == 0 {
		return 0
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
	return over
}

// rerender refreshes one item's cached row after it mutated in place.
func (t *timeline) rerender(idx int, render timelineRenderer) {
	if t.width == 0 || idx < 0 || idx >= len(t.items) {
		return
	}
	t.rendered[idx] = render(t.items[idx], t.width)
	t.dirty = true
}

// rerenderTools refreshes every tool item's row while reusing the cached rows
// for all other item kinds.
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

// resize sets the rendering width and rebuilds every cached row. It is the
// explicit rebuild path for the first layout and for terminal width changes.
func (t *timeline) resize(width int, render timelineRenderer) {
	t.width = width
	t.rebuild(render)
}

// rebuild re-renders every item at the current width. Removing entries never
// calls this; removal and trimming maintain the cache incrementally.
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

// clear empties the timeline and drops both backing arrays so /clear and a new
// session release the transcript instead of keeping it reachable.
func (t *timeline) clear() {
	t.items = nil
	t.rendered = nil
	if t.width > 0 {
		t.dirty = true
	}
}

// content returns the viewport payload: every cached row joined by a double
// newline. syncViewport calls it once per Update after taking the dirty flag.
func (t *timeline) content() string {
	return strings.Join(t.rendered, "\n\n")
}

// takeDirty reports whether a mutation changed the payload since the last
// sync, clearing the flag so a burst of events costs one viewport push.
func (t *timeline) takeDirty() bool {
	dirty := t.dirty
	t.dirty = false
	return dirty
}
