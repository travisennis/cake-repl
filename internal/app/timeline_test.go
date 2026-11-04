package app

import (
	"fmt"
	"slices"
	"testing"

	"github.com/travisennis/cake-repl/internal/ui"
)

func timelineTestItem(text string) ui.Item {
	return ui.Item{Kind: ui.KindInfo, Text: text}
}

func timelineTestRender(it ui.Item, width int) string {
	return fmt.Sprintf("%s@%d", it.Text, width)
}

const timelineTestWidth = 20

func newRenderedTimeline() *timeline {
	tl := &timeline{}
	tl.resize(timelineTestWidth, timelineTestRender)
	return tl
}

func assertTimelineState(t *testing.T, tl *timeline, wantItems, wantRows []string) {
	t.Helper()
	if len(tl.items) != len(tl.rendered) {
		t.Fatalf("items=%d rendered=%d, want equal", len(tl.items), len(tl.rendered))
	}
	gotItems := make([]string, len(tl.items))
	for i := range tl.items {
		gotItems[i] = tl.items[i].Text
	}
	if !slices.Equal(gotItems, wantItems) {
		t.Errorf("items = %v, want %v", gotItems, wantItems)
	}
	if !slices.Equal(tl.rendered, wantRows) {
		t.Errorf("rendered = %v, want %v", tl.rendered, wantRows)
	}
}

func TestTimelineAppendAtCapKeepsItemsAndRowsInStep(t *testing.T) {
	tl := newRenderedTimeline()

	if got := tl.append(timelineTestItem("a"), 2, timelineTestRender); got != 0 {
		t.Fatalf("first append trimmed %d, want 0", got)
	}
	if got := tl.append(timelineTestItem("b"), 2, timelineTestRender); got != 0 {
		t.Fatalf("second append trimmed %d, want 0", got)
	}
	if got := tl.append(timelineTestItem("c"), 2, timelineTestRender); got != 1 {
		t.Fatalf("append at cap trimmed %d, want 1", got)
	}

	assertTimelineState(t, tl, []string{"b", "c"}, []string{"b@20", "c@20"})
	if !tl.dirty {
		t.Error("append did not mark the payload dirty")
	}
	if got, want := tl.content(), "b@20\n\nc@20"; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestTimelineAppendBeforeLayoutRendersOnResize(t *testing.T) {
	tl := &timeline{}
	tl.append(timelineTestItem("a"), 0, timelineTestRender)
	if len(tl.rendered) != 0 {
		t.Fatalf("pre-layout rendered = %d, want 0", len(tl.rendered))
	}
	if tl.dirty {
		t.Error("pre-layout append marked an empty payload dirty")
	}

	tl.resize(10, timelineTestRender)
	assertTimelineState(t, tl, []string{"a"}, []string{"a@10"})

	tl.resize(6, timelineTestRender)
	assertTimelineState(t, tl, []string{"a"}, []string{"a@6"})
}

func TestTimelineInsertAtCapKeepsInsertedItem(t *testing.T) {
	tl := newRenderedTimeline()
	tl.append(timelineTestItem("a"), 2, timelineTestRender)
	tl.append(timelineTestItem("b"), 2, timelineTestRender)

	if got := tl.insert(0, timelineTestItem("c"), 2, timelineTestRender); got != 1 {
		t.Fatalf("insert at cap trimmed %d, want 1", got)
	}
	assertTimelineState(t, tl, []string{"c", "b"}, []string{"c@20", "b@20"})

	// An append-position insert at the cap must also survive, with idx clamped
	// after the front trim.
	tl = newRenderedTimeline()
	tl.append(timelineTestItem("a"), 2, timelineTestRender)
	tl.append(timelineTestItem("b"), 2, timelineTestRender)
	if got := tl.insert(2, timelineTestItem("c"), 2, timelineTestRender); got != 1 {
		t.Fatalf("append-position insert at cap trimmed %d, want 1", got)
	}
	assertTimelineState(t, tl, []string{"b", "c"}, []string{"b@20", "c@20"})
}

func TestTimelineRemoveAtCapKeepsItemsAndRowsInStep(t *testing.T) {
	tl := newRenderedTimeline()
	for _, text := range []string{"a", "b", "c", "d"} {
		tl.append(timelineTestItem(text), 3, timelineTestRender)
	}
	assertTimelineState(t, tl, []string{"b", "c", "d"}, []string{"b@20", "c@20", "d@20"})

	if !tl.remove(1) {
		t.Fatal("remove(1) reported an invalid index")
	}
	assertTimelineState(t, tl, []string{"b", "d"}, []string{"b@20", "d@20"})
	if cap(tl.items) != len(tl.items) || cap(tl.rendered) != len(tl.rendered) {
		t.Errorf("remove kept a larger backing array: items cap=%d rendered cap=%d", cap(tl.items), cap(tl.rendered))
	}
	if tl.remove(-1) || tl.remove(2) {
		t.Error("remove accepted an out-of-range index")
	}
}

func TestTimelineTrimAtCapKeepsItemsAndRowsInStep(t *testing.T) {
	tl := newRenderedTimeline()
	for _, text := range []string{"a", "b", "c", "d"} {
		tl.append(timelineTestItem(text), 3, timelineTestRender)
	}
	assertTimelineState(t, tl, []string{"b", "c", "d"}, []string{"b@20", "c@20", "d@20"})

	if got := tl.trimFront(1); got != 1 {
		t.Fatalf("trimFront(1) removed %d, want 1", got)
	}
	assertTimelineState(t, tl, []string{"c", "d"}, []string{"c@20", "d@20"})

	if got := tl.trimFront(9); got != 2 {
		t.Fatalf("trimFront(9) removed %d, want 2", got)
	}
	assertTimelineState(t, tl, nil, nil)
	if got := tl.content(); got != "" {
		t.Errorf("empty timeline content = %q, want empty", got)
	}
}

func TestTimelineRerenderAtCapUpdatesOnlyRequestedRow(t *testing.T) {
	tl := newRenderedTimeline()
	for _, text := range []string{"a", "b", "c"} {
		tl.append(timelineTestItem(text), 2, timelineTestRender)
	}
	assertTimelineState(t, tl, []string{"b", "c"}, []string{"b@20", "c@20"})

	tl.items[0].Text = "B"
	tl.rerender(0, timelineTestRender)
	assertTimelineState(t, tl, []string{"B", "c"}, []string{"B@20", "c@20"})
	if !tl.dirty {
		t.Error("rerender did not mark the payload dirty")
	}

	tl.rerender(9, timelineTestRender)
	assertTimelineState(t, tl, []string{"B", "c"}, []string{"B@20", "c@20"})
}

func TestTimelineClearAtCapReleasesBothSlices(t *testing.T) {
	tl := newRenderedTimeline()
	for _, text := range []string{"a", "b", "c"} {
		tl.append(timelineTestItem(text), 2, timelineTestRender)
	}
	assertTimelineState(t, tl, []string{"b", "c"}, []string{"b@20", "c@20"})

	tl.clear()
	if tl.items != nil || tl.rendered != nil {
		t.Errorf("clear kept backing arrays: items=%v rendered=%v", tl.items, tl.rendered)
	}
	if !tl.dirty {
		t.Error("clear did not mark the payload dirty")
	}
	if got := tl.content(); got != "" {
		t.Errorf("cleared content = %q, want empty", got)
	}

	tl.append(timelineTestItem("d"), 2, timelineTestRender)
	assertTimelineState(t, tl, []string{"d"}, []string{"d@20"})
}
