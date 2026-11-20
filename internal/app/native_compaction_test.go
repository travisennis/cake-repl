package app

import (
	"strings"
	"testing"

	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

// nativeCompactionItemIndex returns the last row whose text starts with prefix,
// or -1 when none exists. It is deliberately text-based: a start row is
// identified by its stable notice handle elsewhere, but these tests need to
// prove the whole lifecycle left exactly one recognizable outcome.
func nativeCompactionItemIndex(m Model, prefix string) int {
	for i := len(m.timeline.items) - 1; i >= 0; i-- {
		if strings.HasPrefix(m.timeline.items[i].Text, prefix) {
			return i
		}
	}
	return -1
}

func TestNativeCompactionCommittedResolvesStartIntoOneMarker(t *testing.T) {
	m := laidOut(160, 24)
	m.applyEvent(cake.CompactionStart{
		Trigger:               "threshold",
		EstimatedBeforeTokens: 118_000,
		ContextWindow:         200_000,
	})
	if got := lastItem(t, m); got.Kind != ui.KindInfo ||
		got.Text != "context compaction started · threshold, 118k before" {
		t.Fatalf("start row = %+v", got)
	}
	if !m.native.pending || m.native.notice == 0 {
		t.Fatalf("start left no pending notice: %+v", m.native)
	}
	beforeCheckpoint := len(m.timeline.items)

	m.applyEvent(cake.Compaction{
		EstimatedInput: cake.CompactionEstimate{BeforeTokens: 118_000, AfterTokens: 24_000},
		RetainedCount:  4,
	})
	if len(m.timeline.items) != beforeCheckpoint {
		t.Fatalf("checkpoint under a live start appended a row: %+v", m.timeline.items[beforeCheckpoint:])
	}
	if got := compactionMarkerCount(m); got != 0 {
		t.Fatalf("marker count after checkpoint = %d, want 0", got)
	}

	m.applyEvent(cake.CompactionEnd{
		Status:                cake.CompactionStatusCommitted,
		EstimatedBeforeTokens: 118_000,
		EstimatedAfterTokens:  24_000,
		RetainedCount:         4,
	})
	if got := compactionMarkerCount(m); got != 1 {
		t.Fatalf("marker count after committed end = %d, want 1", got)
	}
	if got := lastItem(t, m); got.Kind != ui.KindInfo ||
		got.Text != "context compacted 118k -> 24k, 4 records retained" {
		t.Fatalf("committed end row = %+v", got)
	}
	if idx := nativeCompactionItemIndex(m, "context compaction started"); idx >= 0 {
		t.Fatalf("start row survived the committed end: %+v", m.timeline.items[idx])
	}
	if m.native != (nativeCompactionState{}) {
		t.Fatalf("native state after end = %+v, want zero", m.native)
	}
	ctx := m.session.Context
	if ctx == nil || ctx.Tokens != 24_000 || ctx.Window != 200_000 || !ctx.Estimated {
		t.Fatalf("context after committed end = %+v", ctx)
	}
}

// A checkpoint supplies the estimates when the end omits them, and the end's
// context window comes from the start because the end record has none.
func TestNativeCompactionEndFallsBackToCheckpointEstimates(t *testing.T) {
	m := newLaidOutModel()
	m.applyEvent(cake.CompactionStart{Trigger: "manual", ContextWindow: 100_000})
	if got := lastItem(t, m); got.Text != "context compaction started · manual" {
		t.Fatalf("start row = %q", got.Text)
	}
	m.applyEvent(cake.Compaction{
		EstimatedInput: cake.CompactionEstimate{BeforeTokens: 88_000, AfterTokens: 12_345},
		RetainedCount:  3,
	})
	m.applyEvent(cake.CompactionEnd{Status: cake.CompactionStatusCommitted})

	if got := lastItem(t, m); got.Text != "context compacted 88k -> 12k, 3 records retained" {
		t.Fatalf("committed end row = %q", got.Text)
	}
	ctx := m.session.Context
	if ctx == nil || ctx.Tokens != 12_345 || ctx.Window != 100_000 || !ctx.Estimated {
		t.Fatalf("context after fallback end = %+v", ctx)
	}
}

// A new start owns a new lifecycle; estimates stashed by an earlier bare
// checkpoint must not become this end's fallback.
func TestNativeCompactionStartDropsPriorCheckpointEstimates(t *testing.T) {
	m := newLaidOutModel()
	m.applyEvent(cake.Compaction{
		EstimatedInput: cake.CompactionEstimate{BeforeTokens: 50_000, AfterTokens: 10_000},
		RetainedCount:  2,
	})
	m.applyEvent(cake.CompactionStart{Trigger: "manual"})
	m.applyEvent(cake.CompactionEnd{Status: cake.CompactionStatusCommitted})

	if got := lastItem(t, m); got.Text != "context compacted" {
		t.Fatalf("committed end inherited prior estimates: %q", got.Text)
	}
	if got := compactionMarkerCount(m); got != 2 {
		t.Fatalf("marker count = %d, want bare checkpoint plus this end", got)
	}
}

func TestNativeCompactionSkipAndFailureResolveTheStart(t *testing.T) {
	tests := []struct {
		name   string
		end    cake.CompactionEnd
		kind   ui.Kind
		prefix string
	}{
		{
			name:   "no_op",
			end:    cake.CompactionEnd{Status: cake.CompactionStatusNoOp, Reason: "below threshold"},
			kind:   ui.KindInfo,
			prefix: "context compaction skipped: below threshold",
		},
		{
			name:   "no_fit",
			end:    cake.CompactionEnd{Status: cake.CompactionStatusNoFit, Reason: "no history fit the window"},
			kind:   ui.KindInfo,
			prefix: "context compaction skipped: no history fit the window",
		},
		{
			name:   "failed",
			end:    cake.CompactionEnd{Status: cake.CompactionStatusFailed, Reason: "provider unavailable"},
			kind:   ui.KindWarning,
			prefix: "context compaction failed: provider unavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newLaidOutModel()
			m.applyEvent(cake.CompactionStart{
				Trigger:               "overflow",
				EstimatedBeforeTokens: 190_000,
				ContextWindow:         200_000,
			})
			m.applyEvent(tt.end)

			if got := lastItem(t, m); got.Kind != tt.kind || got.Text != tt.prefix {
				t.Fatalf("outcome row = %+v, want kind %v text %q", got, tt.kind, tt.prefix)
			}
			if idx := nativeCompactionItemIndex(m, "context compaction started"); idx >= 0 {
				t.Fatalf("start row survived the outcome: %+v", m.timeline.items[idx])
			}
			if got := compactionMarkerCount(m); got != 0 {
				t.Fatalf("outcome appended %d success markers, want 0", got)
			}
			if m.native != (nativeCompactionState{}) {
				t.Fatalf("native state after outcome = %+v, want zero", m.native)
			}
			if ctx := m.session.Context; ctx != nil && ctx.Estimated {
				t.Fatalf("non-committed outcome seeded an estimated context: %+v", ctx)
			}
		})
	}
}

// Cake emits an end-only record for a request that never prepared a candidate.
// The skip must stay visible even with no start or checkpoint to resolve.
func TestNativeCompactionEndOnlyOutcomeIsVisible(t *testing.T) {
	m := newLaidOutModel()
	m.applyEvent(cake.CompactionEnd{Status: cake.CompactionStatusNoOp, Reason: "below threshold"})
	if got := lastItem(t, m); got.Kind != ui.KindInfo ||
		got.Text != "context compaction skipped: below threshold" {
		t.Fatalf("end-only skip row = %+v", got)
	}
	if got := compactionMarkerCount(m); got != 0 {
		t.Fatalf("end-only skip appended %d markers, want 0", got)
	}
}

func TestNativeCompactionUnknownStatusStaysVisible(t *testing.T) {
	m := newLaidOutModel()
	m.applyEvent(cake.CompactionStart{Trigger: "threshold"})
	m.applyEvent(cake.CompactionEnd{Status: "future_outcome"})
	if got := lastItem(t, m); got.Kind != ui.KindInfo ||
		got.Text != "context compaction ended: future_outcome" {
		t.Fatalf("unknown status row = %+v", got)
	}
	if idx := nativeCompactionItemIndex(m, "context compaction started"); idx >= 0 {
		t.Fatalf("start row survived an unknown status: %+v", m.timeline.items[idx])
	}
	if m.native != (nativeCompactionState{}) {
		t.Fatalf("native state after unknown status = %+v, want zero", m.native)
	}
}

func TestNativeCompactionUnresolvedStartSettlesAtRunEnd(t *testing.T) {
	tests := []struct {
		name     string
		result   cake.Result
		prefix   string
		wantKind ui.Kind
	}{
		{
			name:     "incomplete",
			result:   cake.Result{ExitCode: 0},
			prefix:   "context compaction incomplete",
			wantKind: ui.KindWarning,
		},
		{
			name:     "canceled",
			result:   cake.Result{Canceled: true},
			prefix:   "context compaction canceled",
			wantKind: ui.KindWarning,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newLaidOutModel()
			beginTestRun(&m)
			m.applyEvent(cake.CompactionStart{Trigger: "threshold", EstimatedBeforeTokens: 118_000})

			tm, _ := m.finishRun(tt.result)
			m = tm.(Model)

			idx := nativeCompactionItemIndex(m, tt.prefix)
			if idx < 0 {
				t.Fatalf("run end left no %q row: %+v", tt.prefix, m.timeline.items)
			}
			if got := m.timeline.items[idx]; got.Kind != tt.wantKind {
				t.Fatalf("settled row = %+v, want kind %v", got, tt.wantKind)
			}
			if start := nativeCompactionItemIndex(m, "context compaction started"); start >= 0 {
				t.Fatalf("start row survived run end: %+v", m.timeline.items[start])
			}
			if m.native != (nativeCompactionState{}) {
				t.Fatalf("native state after run end = %+v, want zero", m.native)
			}
		})
	}
}

func TestNativeCompactionClearedBySessionBoundary(t *testing.T) {
	tests := []struct {
		name string
		run  func(Model) Model
		// clearTimeline distinguishes a boundary that removes the row from
		// /new, which keeps the transcript and only resets the lifecycle.
		clearTimeline bool
	}{
		{
			name: "clear",
			run: func(m Model) Model {
				tm, _ := m.execCommand(Command{Kind: CmdClear})
				return tm.(Model)
			},
			clearTimeline: true,
		},
		{
			name: "new",
			run: func(m Model) Model {
				tm, _ := m.execCommand(Command{Kind: CmdNew})
				return tm.(Model)
			},
		},
		{
			name: "ctrl+n",
			run: func(m Model) Model {
				tm, _ := m.startNewSession()
				return tm.(Model)
			},
			clearTimeline: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newLaidOutModel()
			m.applyEvent(cake.CompactionStart{Trigger: "threshold", EstimatedBeforeTokens: 118_000})

			m = tt.run(m)

			if m.native != (nativeCompactionState{}) {
				t.Fatalf("native state after boundary = %+v, want zero", m.native)
			}
			idx := nativeCompactionItemIndex(m, "context compaction started")
			if tt.clearTimeline && idx >= 0 {
				t.Fatalf("start row survived the boundary: %+v", m.timeline.items[idx])
			}
			if !tt.clearTimeline && idx < 0 {
				t.Fatalf("boundary unexpectedly removed the prior transcript: %+v", m.timeline.items)
			}
		})
	}
}

// A committed end after /clear has no start or checkpoint left to resolve, but
// the outcome still becomes exactly one visible marker and seeds the estimate.
func TestNativeCompactionEndAfterClearReportsOneOutcome(t *testing.T) {
	m := newLaidOutModel()
	m.applyEvent(cake.CompactionStart{Trigger: "threshold", EstimatedBeforeTokens: 118_000})
	tm, _ := m.execCommand(Command{Kind: CmdClear})
	m = tm.(Model)

	m.applyEvent(cake.CompactionEnd{
		Status:                cake.CompactionStatusCommitted,
		EstimatedBeforeTokens: 118_000,
		EstimatedAfterTokens:  24_000,
		RetainedCount:         4,
	})
	if got := compactionMarkerCount(m); got != 1 {
		t.Fatalf("post-clear marker count = %d, want 1", got)
	}
	if got := lastItem(t, m); got.Text != "context compacted 118k -> 24k, 4 records retained" {
		t.Fatalf("post-clear outcome = %+v", got)
	}
}

func TestNativeCompactionReplayKeepsCheckpointAsTheOneOutcome(t *testing.T) {
	m := laidOut(160, 24)
	m.session.Context = &ContextUsage{Tokens: 1_000, Window: 200_000}
	m.applyReplayEvent(cake.CompactionStart{
		Trigger:               "threshold",
		EstimatedBeforeTokens: 118_000,
		ContextWindow:         200_000,
	})
	if got := nativeCompactionItemIndex(m, "context compaction started"); got >= 0 {
		t.Fatalf("replayed start rendered: %+v", m.timeline.items[got])
	}
	afterStart := len(m.timeline.items)

	m.applyReplayEvent(cake.Compaction{
		EstimatedInput: cake.CompactionEstimate{BeforeTokens: 118_000, AfterTokens: 24_000},
		RetainedCount:  4,
	})
	if got := compactionMarkerCount(m); got != 1 {
		t.Fatalf("replayed checkpoint marker count = %d, want 1", got)
	}
	if got := len(m.timeline.items); got != afterStart+1 {
		t.Fatalf("replayed checkpoint appended %d rows, want 1", got-afterStart)
	}
	ctx := m.session.Context
	if ctx == nil || ctx.Tokens != 24_000 || ctx.Window != 200_000 || !ctx.Estimated {
		t.Fatalf("replayed checkpoint context = %+v", ctx)
	}
	afterCheckpoint := len(m.timeline.items)

	m.applyReplayEvent(cake.CompactionEnd{
		Status:                cake.CompactionStatusCommitted,
		EstimatedBeforeTokens: 118_000,
		EstimatedAfterTokens:  24_000,
		RetainedCount:         4,
	})
	if got := compactionMarkerCount(m); got != 1 {
		t.Fatalf("marker count after replayed committed end = %d, want 1", got)
	}
	if got := len(m.timeline.items); got != afterCheckpoint {
		t.Fatalf("replayed committed end appended %d rows, want 0", got-afterCheckpoint)
	}
}

func TestNativeCompactionReplayRendersNonCommittedEnds(t *testing.T) {
	tests := []struct {
		name   string
		end    cake.CompactionEnd
		kind   ui.Kind
		prefix string
	}{
		{
			name:   "no_op",
			end:    cake.CompactionEnd{Status: cake.CompactionStatusNoOp, Reason: "below threshold"},
			kind:   ui.KindInfo,
			prefix: "context compaction skipped: below threshold",
		},
		{
			name:   "failed",
			end:    cake.CompactionEnd{Status: cake.CompactionStatusFailed, Reason: "provider unavailable"},
			kind:   ui.KindWarning,
			prefix: "context compaction failed: provider unavailable",
		},
		{
			name:   "unknown",
			end:    cake.CompactionEnd{Status: "future_outcome"},
			kind:   ui.KindInfo,
			prefix: "context compaction ended: future_outcome",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newLaidOutModel()
			m.applyReplayEvent(tt.end)
			if got := lastItem(t, m); got.Kind != tt.kind || got.Text != tt.prefix {
				t.Fatalf("replayed outcome = %+v, want kind %v text %q", got, tt.kind, tt.prefix)
			}
			if got := compactionMarkerCount(m); got != 0 {
				t.Fatalf("non-committed replay appended %d markers, want 0", got)
			}
		})
	}
}
