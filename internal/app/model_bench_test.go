package app

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

// Timeline hot-path benchmarks. They separate the three costs every event
// pays — rendering the new item, maintaining the cached timeline, and
// handing the payload to the Bubble viewport — so an optimization can target
// the dominant layer instead of the most visible one. See task 059 for the
// baseline and task 054 for the coalescing change these benchmarks verify.
//
// Everything here is hermetic: no cake binary, no terminal, fixed dimensions
// and fixed payloads.

const (
	// benchWidth and benchHeight are a fixed terminal size so rendered payload
	// sizes stay comparable between runs and machines.
	benchWidth  = 100
	benchHeight = 30

	// benchCallID is the pending tool call the tool-output benchmark answers.
	benchCallID = "c-bench"
)

// benchSizes are representative small, medium, and long sessions.
var benchSizes = []int{50, 200, 800}

// benchSink keeps rendered output observable so the compiler cannot drop the
// call being measured.
var benchSink string

// benchAssistantText is a representative assistant reply: prose plus a fenced
// code block, which is the most expensive item kind to render.
const benchAssistantText = "Here is what I found in the runner.\n\n" +
	"The decoder buffers each NDJSON line and dispatches one typed event:\n\n" +
	"```go\n" +
	"for sc.Scan() {\n" +
	"\tev, err := decode(sc.Bytes())\n" +
	"\tif err != nil {\n" +
	"\t\tcontinue\n" +
	"\t}\n" +
	"\tevents <- ev\n" +
	"}\n" +
	"```\n\n" +
	"Next I will check the cancellation path."

const benchUserText = "Trace the append path and tell me where the per-event\n" +
	"cost comes from. Include allocations, not just wall time."

const benchReasoningText = "The append path extends a cached string, then the viewport " +
	"re-splits the whole payload into lines, so both scale with the session."

// benchToolOutput is a realistic multi-line tool result that stays under
// ui.DefaultOutputLimit, so truncation never changes the measured payload.
var benchToolOutput = strings.Repeat("internal/app/model.go:301: appendTimelineContent copies the timeline\n", 12)

// benchHugeOutput is a tool result far above the retention ceiling — the case
// that used to pin tens of MB for the life of the session. "x" has no
// newlines so the cap math is exact.
const benchHugeOutputSize = 50 << 20 // 50 MiB

var benchHugeOutput = strings.Repeat("x", benchHugeOutputSize)

// benchWorkload builds the timeline items for one traffic pattern.
type benchWorkload struct {
	name string
	// item builds the timeline filler at index i, cycling the item kinds the
	// workload represents.
	item func(i int) ui.Item
	// newItem is the single item the measurement loops append or render. It is
	// fixed rather than derived from the timeline size so the marginal item is
	// the same at every size — deriving it from the size silently picked one
	// kind, because every entry in benchSizes happens to hit the same point in
	// the cycle. Text traffic uses the assistant reply: it is the most
	// expensive kind to render (markdown), so it is the honest denominator
	// when attributing per-event cost. Cheaper kinds are noted below.
	newItem ui.Item
}

// benchWorkloads covers append-only conversation traffic and tool-call
// traffic, which stress different item kinds and payload sizes.
//
// Rough per-kind ui.RenderItem cost at benchWidth (Apple M5, TrueColor), for
// orientation only, since just the assistant and tool kinds are measured as
// marginal items: reasoning ≈ 4µs / 28 allocs, user ≈ 12µs / 54 allocs,
// tool ≈ 21µs / 138 allocs, assistant ≈ 221µs / 1822 allocs — markdown
// rendering dominates. Point newItem at another kind to measure it properly.
var benchWorkloads = []benchWorkload{
	{name: "text", item: benchTextItem, newItem: ui.Item{Kind: ui.KindAssistant, Text: benchAssistantText}},
	{name: "tools", item: benchToolItem, newItem: benchToolItem(0)},
}

// benchTextItem cycles the conversation item kinds deterministically.
func benchTextItem(i int) ui.Item {
	switch i % 3 {
	case 0:
		return ui.Item{Kind: ui.KindUser, Text: benchUserText}
	case 1:
		return ui.Item{Kind: ui.KindAssistant, Text: benchAssistantText}
	default:
		return ui.Item{Kind: ui.KindReasoning, Text: benchReasoningText}
	}
}

// benchToolItem returns a completed tool block, the shape a timeline holds
// after a tool call and its output have both arrived.
func benchToolItem(i int) ui.Item {
	return ui.Item{Kind: ui.KindTool, Tool: &ui.ToolBlock{
		Name:      "bash",
		Arguments: fmt.Sprintf(`{"command":"rg -n appendTimelineContent internal/app/file_%02d.go"}`, i%10),
		Output:    benchToolOutput,
		Done:      true,
	}}
}

// forceColorProfile pins lipgloss to a color profile for the duration of a
// benchmark. Benchmarks run without a TTY, which would otherwise strip every
// escape sequence and hide the ANSI-parsing work viewport.SetContent does on
// real terminal output.
func forceColorProfile(b *testing.B) {
	b.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	b.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// newBenchModel returns a laid-out model holding exactly n workload items,
// with the render cache and the viewport already populated.
func newBenchModel(w benchWorkload, n int) Model {
	m := New(Config{})
	m.width, m.height = benchWidth, benchHeight
	m.layout()
	// Drop New's banner item so the timeline holds exactly n items.
	m.items = nil
	m.rebuildTimeline()
	for i := range n {
		m.appendItem(w.item(i))
	}
	return m
}

// runBenchCases runs fn for every workload at every timeline size. fn owns its
// own setup and must call b.ResetTimer after it.
func runBenchCases(b *testing.B, fn func(b *testing.B, m *Model, w benchWorkload, n int)) {
	b.Helper()
	for _, w := range benchWorkloads {
		for _, n := range benchSizes {
			b.Run(fmt.Sprintf("%s/size=%d", w.name, n), func(b *testing.B) {
				forceColorProfile(b)
				m := newBenchModel(w, n)
				b.ReportAllocs()
				fn(b, &m, w, n)
			})
		}
	}
}

// BenchmarkTimelineRenderItem measures rendering one newly appended item in
// isolation: no cache maintenance and no viewport work. It should not grow
// with the timeline size.
func BenchmarkTimelineRenderItem(b *testing.B) {
	runBenchCases(b, func(b *testing.B, m *Model, w benchWorkload, _ int) {
		it := w.newItem
		b.ResetTimer()
		for range b.N {
			benchSink = ui.RenderItem(m.theme, it, m.renderedWidth, m.cfg.OutputLimit, m.toolOutputMode)
		}
	})
}

// BenchmarkTimelineAppendContent measures the append-path cache extension
// (one entry added to the render slice, payload marked dirty), with no
// viewport work. Restoring the base slice between iterations is O(1) — a
// slice-header assignment — so the loop measures one append onto a timeline
// of size n. Task 054 made this O(1): the old `timelineContent += rendered`
// copied the whole accumulated string on every append.
func BenchmarkTimelineAppendContent(b *testing.B) {
	runBenchCases(b, func(b *testing.B, m *Model, w benchWorkload, _ int) {
		rendered := ui.RenderItem(m.theme, w.newItem, m.renderedWidth, m.cfg.OutputLimit, m.toolOutputMode)
		base := m.rendered[:len(m.rendered)-1]
		b.ResetTimer()
		for range b.N {
			m.rendered = base
			m.timelineDirty = false
			m.appendTimelineContent(rendered)
		}
	})
}

// BenchmarkTimelineRebuildContent measures the strings.Join that
// syncViewport performs once per Update to build the viewport payload, with
// no viewport work. Task 054 moved the join from every in-place item update
// to the single sync at the end of the Update.
func BenchmarkTimelineRebuildContent(b *testing.B) {
	runBenchCases(b, func(b *testing.B, m *Model, _ benchWorkload, _ int) {
		b.ResetTimer()
		for range b.N {
			m.rebuildTimelineContent()
		}
	})
}

// BenchmarkTimelineViewportSetContent measures Bubble viewport ingestion of
// already-rendered content. This is the cost the app cannot remove by
// changing how it builds the timeline string; task 054 attacks how often it
// runs instead.
func BenchmarkTimelineViewportSetContent(b *testing.B) {
	runBenchCases(b, func(b *testing.B, m *Model, _ benchWorkload, _ int) {
		content := m.rebuildTimelineContent()
		b.ResetTimer()
		for range b.N {
			m.timeline.SetContent(content)
		}
	})
}

// BenchmarkTimelineAppendEndToEnd measures the whole append hot path for one
// event as one Update pays it after task 054: render, cache extension, join,
// and a single viewport sync. The per-iteration reset is O(1) — slice
// reslices and a bool — so each iteration appends onto a timeline of size n.
func BenchmarkTimelineAppendEndToEnd(b *testing.B) {
	runBenchCases(b, func(b *testing.B, m *Model, w benchWorkload, n int) {
		it := w.newItem
		// Give both caches spare capacity so the reset-and-append loop measures
		// the append path rather than one slice growth per iteration; in
		// production that growth is amortized across many appends.
		m.items = append(make([]ui.Item, 0, n+1), m.items...)
		m.rendered = append(make([]string, 0, n+1), m.rendered...)
		items, rendered := m.items, m.rendered
		b.ResetTimer()
		for range b.N {
			m.items, m.rendered, m.timelineDirty = items, rendered, false
			m.appendItem(it)
			m.syncViewport()
		}
	})
}

// BenchmarkTimelineToolOutputEndToEnd measures the FunctionCallOutput hot
// path as one Update pays it after task 054: an in-place item mutation, a
// re-render of that item, and a single viewport sync.
func BenchmarkTimelineToolOutputEndToEnd(b *testing.B) {
	runBenchCases(b, func(b *testing.B, m *Model, _ benchWorkload, _ int) {
		idx := m.appendItem(ui.Item{Kind: ui.KindTool, Tool: &ui.ToolBlock{
			Name:      "bash",
			Arguments: `{"command":"go test ./internal/app/"}`,
		}})
		tool := m.items[idx].Tool
		b.ResetTimer()
		for range b.N {
			tool.Output, tool.Done = "", false
			m.pendingCalls[benchCallID] = idx
			m.applyEvent(cake.FunctionCallOutput{CallID: benchCallID, Output: benchToolOutput})
			m.syncViewport()
		}
	})
}

// BenchmarkTimelineBurstCoalesced measures a burst of maxEventsPerBatch
// appended events with a single viewport sync: one SetContent per burst, the
// shape the eventsMsg path pays after task 054. Per-event SetContent count is
// 1/K.
func BenchmarkTimelineBurstCoalesced(b *testing.B) {
	runBenchCases(b, func(b *testing.B, m *Model, w benchWorkload, n int) {
		it := w.newItem
		m.items = append(make([]ui.Item, 0, n+maxEventsPerBatch), m.items...)
		m.rendered = append(make([]string, 0, n+maxEventsPerBatch), m.rendered...)
		items, rendered := m.items, m.rendered
		b.ResetTimer()
		for range b.N {
			m.items, m.rendered, m.timelineDirty = items, rendered, false
			for range maxEventsPerBatch {
				m.appendItem(it)
			}
			m.syncViewport()
		}
	})
}

// BenchmarkTimelineBurstPerEventSync measures the same burst with the
// pre-task-054 shape: a viewport sync after every event (K SetContent calls
// per burst). It is the "before" instrument: at the same sizes and workloads
// the coalesced burst must cost less by roughly (K-1) × the per-event
// SetContent term.
func BenchmarkTimelineBurstPerEventSync(b *testing.B) {
	runBenchCases(b, func(b *testing.B, m *Model, w benchWorkload, n int) {
		it := w.newItem
		m.items = append(make([]ui.Item, 0, n+maxEventsPerBatch), m.items...)
		m.rendered = append(make([]string, 0, n+maxEventsPerBatch), m.rendered...)
		items, rendered := m.items, m.rendered
		b.ResetTimer()
		for range b.N {
			m.items, m.rendered, m.timelineDirty = items, rendered, false
			for range maxEventsPerBatch {
				m.appendItem(it)
				m.syncViewport()
			}
		}
	})
}

// retainedPayloadBytes sums the string bytes one model pins for its timeline:
// item payloads (text, tool arguments, retained tool output) plus the render
// cache. The viewport's own line-split copy is Bubble internals and is
// excluded; it is proportional to the joined rendered payload, so this sum
// tracks the same retention policy. The sum is deterministic — no GC
// involved — so retained-bytes numbers stay comparable between machines and
// runs.
func retainedPayloadBytes(m *Model) int64 {
	var n int64
	for i := range m.items {
		it := &m.items[i]
		n += int64(len(it.Text))
		if it.Tool != nil {
			n += int64(len(it.Tool.Arguments) + len(it.Tool.Output))
		}
	}
	for i := range m.rendered {
		n += int64(len(m.rendered[i]))
	}
	return n
}

// BenchmarkTimelineRetainedMemory reports the bytes one representative
// tool-call session pins, with and without an oversized tool result. It is the
// task 065 before/after instrument: the oversized-result case used to retain
// the full output for the life of the session, and now retains only the
// ingest ceiling plus its marker. Run with -count=6 and read the retained-B
// column; the -benchmem allocs are secondary.
func BenchmarkTimelineRetainedMemory(b *testing.B) {
	forceColorProfile(b)

	run := func(name string, huge bool) {
		b.Run(name, func(b *testing.B) {
			m := newBenchModel(benchWorkloads[1], 200)
			if huge {
				idx := m.appendItem(ui.Item{Kind: ui.KindTool, Tool: &ui.ToolBlock{
					Name:      "bash",
					Arguments: `{"command":"cat huge.bin"}`,
				}})
				m.pendingCalls["c-huge"] = idx
				m.applyEvent(cake.FunctionCallOutput{CallID: "c-huge", Output: benchHugeOutput})
				m.syncViewport()
			}
			b.StopTimer()
			b.ReportMetric(float64(retainedPayloadBytes(&m)), "retained-B")
			b.StartTimer()
			for range b.N {
				runtime.KeepAlive(m)
			}
		})
	}

	run("normal-tools", false)
	run("oversized-output", true)
}
